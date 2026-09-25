package bunorm

import (
    "context"
    "errors"
    "fmt"
    "reflect"
    "runtime/debug"
    "sort"
    "sync"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

type ManagerRegistry struct {
    logger loggingcontract.Logger
    /* the destination SetLogger routed bun's diagnostics to, kept so Close hands back exactly that one even for a logger that has no identity to be recognised by */
    routedDiagnostics *diagnosticsTarget
    /* openContext bounds the lazy opens of providers that implement ContextOpener. It is a child of the registry's context, and CloseWithContext cancels it through openCancel when the refusal is published, so no open goes on dialling, or routing bun's diagnostics, after the registry is gone. */
    openContext context.Context
    openCancel  context.CancelFunc

    providerDefinitionByName      map[string]ProviderDefinition
    defaultProviderDefinitionName string

    lock              sync.Mutex
    managers          map[string]*Manager
    pendingOpenByName map[string]*managerOpen
    /* the migration opens in flight, kept as bare channels because migrations are not coalesced — a caller waits for its own dial, never for another's — while a teardown still has to wait for all of them */
    pendingMigrationOpens map[chan struct{}]struct{}
    /* the migration databases live beside the request pools, never inside them: a migration connection lifts the driver deadlines, and handing it to request traffic would trade one failure mode for another */
    migrationDatabases map[string]*bun.DB
    closed             bool
}

/* managerOpen tracks a single in-flight Provider.Open for one definition name so that concurrent openers of the same name coalesce onto one attempt instead of each dialing the database while holding the registry-wide lock. */
type managerOpen struct {
    done      chan struct{}
    manager   *Manager
    openError error
}

/* NewManagerRegistry builds a container-level registry over lazily-dialed pools. Container-level deliberately: a *bun.DB is a connection pool — the process-lifetime shape of database/sql — and per-unit work takes a transaction or a Conn from the pool, not a pool per scope. */
func NewManagerRegistry(logger loggingcontract.Logger, providerDefinitions ...ProviderDefinition) (*ManagerRegistry, error) {
    return NewManagerRegistryWithContext(context.Background(), logger, providerDefinitions...)
}

/* NewManagerRegistryWithContext additionally binds the registry to the given context: a provider that implements ContextOpener has its lazy opens run under it, so a shutdown that cancels the context refuses an open not yet started, reaches the attempt's cancellable steps — the configuration hook, the boot ping, a retry sleep — in flight, and pays at most the dialect handshake bun bounds by the connect timeout. A nil context reads as context.Background(), the exact behaviour of NewManagerRegistry. */
func NewManagerRegistryWithContext(ctx context.Context, logger loggingcontract.Logger, providerDefinitions ...ProviderDefinition) (*ManagerRegistry, error) {
    if true == isNilInterface(logger) {
        return nil, ErrLoggerIsRequired
    }

    if nil == ctx {
        ctx = context.Background()
    }

    if 0 == len(providerDefinitions) {
        return nil, ErrNoProviderDefinitions
    }

    providerDefinitionByName := make(map[string]ProviderDefinition, len(providerDefinitions))
    defaultProviderDefinitionName := ""
    defaultCount := 0

    for position, providerDefinition := range providerDefinitions {
        if "" == providerDefinition.Name {
            return nil, providerDefinitionRefusal(ErrProviderDefinitionNameIsRequired, position, providerDefinition.Name)
        }

        if true == isNilInterface(providerDefinition.Provider) {
            return nil, providerDefinitionRefusal(ErrProviderIsRequired, position, providerDefinition.Name)
        }

        if _, exists := providerDefinitionByName[providerDefinition.Name]; true == exists {
            return nil, providerDefinitionRefusal(ErrProviderDefinitionNameMustBeUnique, position, providerDefinition.Name)
        }

        providerDefinitionByName[providerDefinition.Name] = providerDefinition

        if true == providerDefinition.IsDefault {
            defaultCount = defaultCount + 1
            defaultProviderDefinitionName = providerDefinition.Name
        }
    }

    if 1 < defaultCount {
        return nil, ErrMultipleDefaultProviderDefinitions
    }

    if 0 == defaultCount {
        defaultProviderDefinitionName = providerDefinitions[0].Name
    }

    openContext, openCancel := context.WithCancel(ctx)

    return &ManagerRegistry{
        logger:                        logger,
        openContext:                   openContext,
        openCancel:                    openCancel,
        providerDefinitionByName:      providerDefinitionByName,
        defaultProviderDefinitionName: defaultProviderDefinitionName,
        managers:                      make(map[string]*Manager),
        pendingOpenByName:             make(map[string]*managerOpen),
        pendingMigrationOpens:         make(map[chan struct{}]struct{}),
        migrationDatabases:            make(map[string]*bun.DB),
    }, nil
}

/* isNilInterface answers whether the interface value is nil outright or holds a nil pointer, map, slice, channel or function: a typed nil passes a plain nil comparison and then panics on first use, far from the wiring mistake that produced it. Duplicated from the framework's internal package, which a separate module cannot import. */
func isNilInterface(value any) bool {
    if nil == value {
        return true
    }

    reflected := reflect.ValueOf(value)

    switch reflected.Kind() {
    case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
        return reflected.IsNil()
    default:
        return false
    }
}

/* MigrationDatabase answers the connection the migration commands run on: a dedicated one with the driver deadlines lifted when the provider implements MigrationProvider, reported through the second return, and the pooled connection otherwise, since a DDL statement cut by a request deadline is not rolled back. The dedicated database is opened once per name and ended by CloseMigrationDatabase, with the registry's Close as the net for whatever was not; an empty name selects the default definition. */
func (instance *ManagerRegistry) MigrationDatabase(name string) (*bun.DB, bool, error) {
    if "" == name {
        name = instance.defaultProviderDefinitionName
    }

    instance.lock.Lock()

    if true == instance.closed {
        instance.lock.Unlock()

        return nil, false, ErrManagerRegistryClosed
    }

    if database, exists := instance.migrationDatabases[name]; true == exists {
        instance.lock.Unlock()

        return database, true, nil
    }

    providerDefinition, exists := instance.providerDefinitionByName[name]
    if false == exists {
        notFoundErr := instance.providerDefinitionNotFoundErrorLocked(name)
        instance.lock.Unlock()

        return nil, false, notFoundErr
    }

    migrationProvider, isMigrationProvider := providerDefinition.Provider.(MigrationProvider)
    if false == isMigrationProvider {
        instance.lock.Unlock()

        manager, managerErr := instance.Manager(name)
        if nil != managerErr {
            return nil, false, managerErr
        }

        return manager.Database(), false, nil
    }

    /* the dial runs outside the registry-wide lock for the same reason Manager's does: a down database must not serialize cache hits or a concurrent Close. Migrations run from a sequential cli command, so no coalescing machinery is warranted — a concurrent duplicate open is resolved below by closing the loser. */

    /* the dial is announced before the lock is released, so a Close arriving during it waits this open out instead of returning over it. It is a bare channel rather than the coalescing record its sibling keeps: nobody waits for this dial except a teardown, and never for its value. */
    migrationOpenDone := make(chan struct{})
    instance.pendingMigrationOpens[migrationOpenDone] = struct{}{}

    instance.lock.Unlock()

    /* registered FIRST so it runs LAST: every later defer in this function is the registry lock's own unlock, and a cleanup that took the lock ahead of it would deadlock against it. Unconditional, because every path out of here — the refusals below, a panic in the provider — ends this dial as far as a teardown is concerned. */
    defer func() {
        instance.lock.Lock()
        delete(instance.pendingMigrationOpens, migrationOpenDone)
        instance.lock.Unlock()

        close(migrationOpenDone)
    }()

    database, openErr := instance.openProviderMigrationDatabase(migrationProvider, providerDefinition.Params)
    if nil != openErr {
        if nil != database {
            _ = database.Close()
        }

        return nil, false, openErr
    }

    if nil == database {
        return nil, false, ErrProviderReturnedNilDatabase
    }

    instance.lock.Lock()
    defer instance.lock.Unlock()

    if true == instance.closed {
        _ = database.Close()

        return nil, false, ErrManagerRegistryClosed
    }

    if existingDatabase, exists := instance.migrationDatabases[name]; true == exists {
        _ = database.Close()

        return existingDatabase, true, nil
    }

    instance.migrationDatabases[name] = database

    return database, true, nil
}

/* CloseMigrationDatabase ends the dedicated migration connection opened for one definition and forgets it, so the next MigrationDatabase opens a fresh one; an empty name selects the default. That connection lifts the driver deadlines and recycles nothing, so it must not outlive the migration run. A name with no migration connection closes nothing, and a closed registry refuses the call. */
func (instance *ManagerRegistry) CloseMigrationDatabase(name string) error {
    if "" == name {
        name = instance.defaultProviderDefinitionName
    }

    instance.lock.Lock()

    if true == instance.closed {
        instance.lock.Unlock()

        return ErrManagerRegistryClosed
    }

    database, exists := instance.migrationDatabases[name]
    if false == exists {
        instance.lock.Unlock()

        return nil
    }

    delete(instance.migrationDatabases, name)

    instance.lock.Unlock()

    /* the close travels the wire — COM_QUIT to a peer that may be partitioned, on a connection whose write deadlines are deliberately lifted — so it runs outside the registry-wide lock, the same discipline Close keeps and for the same reason */
    return database.Close()
}

func (instance *ManagerRegistry) DefaultManager() (*Manager, error) {
    return instance.Manager(instance.defaultProviderDefinitionName)
}

func (instance *ManagerRegistry) MustDefaultManager() *Manager {
    manager, managerErr := instance.DefaultManager()
    if nil != managerErr {
        exception.Panic(exception.FromError(managerErr))
    }

    return manager
}

func (instance *ManagerRegistry) DefaultDatabase() (*bun.DB, error) {
    manager, managerErr := instance.DefaultManager()
    if nil != managerErr {
        return nil, managerErr
    }

    return manager.Database(), nil
}

func (instance *ManagerRegistry) MustDefaultDatabase() *bun.DB {
    database, databaseErr := instance.DefaultDatabase()
    if nil != databaseErr {
        exception.Panic(exception.FromError(databaseErr))
    }

    return database
}

/* providerDefinitionRefusal names the definition a construction-time refusal is about, by its position in the argument list and by its name when it has one. The sentinel stays the cause, so errors.Is keeps its answer. */
func providerDefinitionRefusal(sentinel error, position int, name string) error {
    return exception.NewError(
        sentinel.Error(),
        map[string]any{
            "position": position,
            "name":     name,
        },
        sentinel,
    )
}

/* HasProviderDefinition answers whether a definition is registered under the name without opening anything, so a command that only labels a manager refuses a misspelt --manager where it is typed. A closed registry still answers what it was built with. */
func (instance *ManagerRegistry) HasProviderDefinition(name string) bool {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    _, exists := instance.providerDefinitionByName[name]

    return exists
}

/* providerDefinitionNotFoundErrorLocked names the definition asked for and the ones registered; it is called with the registry lock held. The sentinel stays the cause, so errors.Is(err, ErrProviderDefinitionNotFound) keeps its answer. */
func (instance *ManagerRegistry) providerDefinitionNotFoundErrorLocked(name string) error {
    registered := make([]string, 0, len(instance.providerDefinitionByName))
    for definitionName := range instance.providerDefinitionByName {
        registered = append(registered, definitionName)
    }

    /* sorted so one misspelling always prints one list: the map walk is random, and an operator comparing two runs would otherwise read two different answers to the same question */
    sort.Strings(registered)

    return exception.NewError(
        "provider definition not found",
        map[string]any{
            "requested":  name,
            "registered": registered,
        },
        ErrProviderDefinitionNotFound,
    )
}

func (instance *ManagerRegistry) Manager(name string) (*Manager, error) {
    if "" == name {
        return nil, ErrProviderDefinitionNameIsRequired
    }

    instance.lock.Lock()

    /* the refusal stands at the entry, ahead of the cache: Close ends every pool it memoized without emptying the map, so a cache hit would hand back a manager over a dead pool with a nil error, while the open path below refuses the same call by name — one registry answering the same question two ways, and the answer that looks like success fails at the first query instead */
    if true == instance.closed {
        instance.lock.Unlock()

        return nil, ErrManagerRegistryClosed
    }

    if manager, exists := instance.managers[name]; true == exists {
        instance.lock.Unlock()

        return manager, nil
    }

    providerDefinition, exists := instance.providerDefinitionByName[name]
    if false == exists {
        notFoundErr := instance.providerDefinitionNotFoundErrorLocked(name)
        instance.lock.Unlock()

        return nil, notFoundErr
    }

    if pendingOpen, inFlight := instance.pendingOpenByName[name]; true == inFlight {
        instance.lock.Unlock()

        <-pendingOpen.done

        return pendingOpen.manager, pendingOpen.openError
    }

    pendingOpen := &managerOpen{done: make(chan struct{})}
    instance.pendingOpenByName[name] = pendingOpen

    instance.lock.Unlock()

    /* Open the provider outside the registry-wide lock: dialing, pinging and any uninterruptible retry sleeps of a down database must not serialize cache hits for other managers or a concurrent Close. A failed open is never memoized, so a later call retries. */

    settled := false
    providerReturned := false
    defer func() {
        if true == settled {
            return
        }

        recovered := recover()

        instance.lock.Lock()
        delete(instance.pendingOpenByName, name)
        instance.lock.Unlock()

        /* the refusal names what unwound: an unwind after the provider returned is the registry's own publish, and one carrying no value is a goroutine exit such as runtime.Goexit, not a panic */
        stage := "provider"
        detail := "while opening"
        if true == providerReturned {
            stage = "registry"
            detail = "while publishing the opened database"
        }

        outcome := "panicked"
        if nil == recovered {
            outcome = "exited its goroutine"
        }

        /* the panic value travels as the cause and in the context, with the stack captured here, so the coalesced waiters, who never see the re-raised panic, get the diagnosis the unwinding goroutine gets */
        refusalContext := map[string]any{
            "name":       name,
            "panicStack": string(debug.Stack()),
        }

        if nil != recovered {
            refusalContext["panic"] = fmt.Sprintf("%v", recovered)
        }

        pendingOpen.openError = exception.NewError(
            fmt.Sprintf("bunorm manager %s %s %s", stage, outcome, detail),
            refusalContext,
            exception.PanicCause(recovered),
        )
        close(pendingOpen.done)

        if nil != recovered {
            panic(recovered)
        }
    }()
    database, openErr := instance.openProviderDatabase(providerDefinition.Provider, providerDefinition.Params)
    providerReturned = true

    /* the publish runs in a closure with a deferred unlock: it calls into the freshly opened database, and a panic there would otherwise unwind with the lock held, whereupon the recovery defer above re-acquires the same non-reentrant mutex and wedges the whole registry with no waiter ever released */
    func() {
        instance.lock.Lock()
        defer instance.lock.Unlock()

        delete(instance.pendingOpenByName, name)

        if nil != openErr {
            /* the Provider contract does not promise a nil database beside a non-nil error, and a pool handed over with an error would otherwise be the last reference anyone holds */
            if nil != database {
                _ = database.Close()
            }

            /* an open the registry ended through openContext reaches its waiter as the registry's refusal with the cancellation under it; a refusal that does not carry the cancellation is the provider's own and travels unchanged. A provider that refuses with a cancellation of its own after the close is still read as ended by the registry, since nothing on the error says whose it is */
            if true == instance.closed && nil != instance.openContext.Err() && true == errors.Is(openErr, context.Canceled) {
                pendingOpen.openError = exception.NewError(
                    fmt.Sprintf("bunorm manager %s open ended by the registry closing while it was in flight", name),
                    map[string]any{"manager": name},
                    &openEndedByClose{openErr: openErr},
                )

                return
            }

            pendingOpen.openError = openErr

            return
        }

        if nil == database {
            /* a provider answering neither a database nor an error would otherwise be memoized as a manager wrapping nil, turning a wiring bug into a nil dereference at the first query, far from its cause */
            pendingOpen.openError = ErrProviderReturnedNilDatabase

            return
        }

        if true == instance.closed {
            /* Close ran while this open was in flight: it already iterated the manager map without this entry, so memoizing the manager now would leak its connection pool. Close the freshly opened database and refuse. */
            _ = database.Close()
            pendingOpen.openError = ErrManagerRegistryClosed

            return
        }

        manager := NewManager(name, database)
        instance.managers[name] = manager
        pendingOpen.manager = manager
    }()

    settled = true

    close(pendingOpen.done)

    return pendingOpen.manager, pendingOpen.openError
}

/* currentLogger reads the logger the registry reports through. It takes the lock because SetLogger replaces the field while opens are in flight, and both opens below run OUTSIDE the registry-wide lock on purpose — a plain field read there would race the replacement. */
func (instance *ManagerRegistry) currentLogger() loggingcontract.Logger {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    return instance.logger
}

/* SetLogger replaces the logger this registry reports through and routes bun's diagnostic channel with it, so the two cannot drift apart. It serves a registry built before the application logger exists, on the emergency logger; a nil or typed-nil logger is refused. */
func (instance *ManagerRegistry) SetLogger(logger loggingcontract.Logger) error {
    if true == isNilInterface(logger) {
        return ErrLoggerIsRequired
    }

    instance.lock.Lock()
    instance.logger = logger
    instance.routedDiagnostics = routeDiagnosticsTo(logger)
    instance.lock.Unlock()

    return nil
}

/* openProviderDatabase runs one provider open, under the registry's context when the provider can honour one. */
func (instance *ManagerRegistry) openProviderDatabase(provider Provider, params ConnectionParameters) (*bun.DB, error) {
    logger := instance.currentLogger()

    if contextOpener, isContextOpener := provider.(ContextOpener); true == isContextOpener {
        return contextOpener.OpenContext(instance.openContext, params, logger)
    }

    return provider.Open(params, logger)
}

/* openProviderMigrationDatabase runs one migration open, under the registry's context when the provider can honour one. */
func (instance *ManagerRegistry) openProviderMigrationDatabase(provider MigrationProvider, params ConnectionParameters) (*bun.DB, error) {
    logger := instance.currentLogger()

    if contextOpener, isContextOpener := provider.(MigrationContextOpener); true == isContextOpener {
        return contextOpener.OpenForMigrationContext(instance.openContext, params, logger)
    }

    return provider.OpenForMigration(params, logger)
}

func (instance *ManagerRegistry) MustManager(name string) *Manager {
    manager, managerErr := instance.Manager(name)
    if nil != managerErr {
        exception.Panic(exception.FromError(managerErr))
    }

    return manager
}

func (instance *ManagerRegistry) Database(name string) (*bun.DB, error) {
    manager, managerErr := instance.Manager(name)
    if nil != managerErr {
        return nil, managerErr
    }

    return manager.Database(), nil
}

func (instance *ManagerRegistry) MustDatabase(name string) *bun.DB {
    database, databaseErr := instance.Database(name)
    if nil != databaseErr {
        exception.Panic(exception.FromError(databaseErr))
    }

    return database
}

func (instance *ManagerRegistry) Close() error {
    return instance.CloseWithContext(context.Background())
}

/* namedCloser is one thing the teardown closes and the name it is reported under, so pools and migration databases go through one loop. */
type namedCloser struct {
    name  string
    close func() error
}

func sortedNamesOf[T any](entries map[string]T) []string {
    names := make([]string, 0, len(entries))
    for name := range entries {
        names = append(names, name)
    }
    sort.Strings(names)

    return names
}

/* CloseWithContext is Close under a deadline its caller declares. The pools are torn down whatever the deadline says; the deadline bounds only the wait for opens still in flight when the refusal was published, which end against the closed flag on their own. */
func (instance *ManagerRegistry) CloseWithContext(closeContext context.Context) error {
    /* the refusal is published under the lock and the pools are torn down outside it, since a pool close travels the wire and would park every caller on the lock. The maps are snapshotted, never emptied: the entry refusal reads the flag, and a manager handed out earlier keeps working through its own pool's close. */
    instance.lock.Lock()

    instance.closed = true

    /* an open still in flight is cancelled here, with the refusal: it has no consumer left, and its retry loop would go on routing bun's diagnostics onto the logger handed back below */
    instance.openCancel()

    /* both maps are walked in sorted name order, pools then migration databases, so the carried cause and the failed-name list are the same on every run. The nil skip stays here, where each value has its concrete type; carried into the list, a nil pool would be a non-nil interface. */
    closers := make([]namedCloser, 0, len(instance.managers)+len(instance.migrationDatabases))

    for _, name := range sortedNamesOf(instance.managers) {
        if manager := instance.managers[name]; nil != manager {
            closers = append(closers, namedCloser{name: name, close: manager.Close})
        }
    }

    for _, name := range sortedNamesOf(instance.migrationDatabases) {
        closers = append(closers, namedCloser{name: name + " (migration)", close: instance.migrationDatabases[name].Close})
    }

    /* the opens in flight are photographed with the pools so the teardown waits for them below, rather than reporting the teardown over while a dial is still outstanding */
    pendingOpens := make([]*managerOpen, 0, len(instance.pendingOpenByName))
    for _, pendingOpen := range instance.pendingOpenByName {
        pendingOpens = append(pendingOpens, pendingOpen)
    }

    pendingMigrationOpens := make([]chan struct{}, 0, len(instance.pendingMigrationOpens))
    for migrationOpenDone := range instance.pendingMigrationOpens {
        pendingMigrationOpens = append(pendingMigrationOpens, migrationOpenDone)
    }

    instance.lock.Unlock()

    var closeErr error
    failedNames := make([]string, 0)

    for _, closer := range closers {
        closeFailure := closer.close()
        if nil == closeFailure {
            continue
        }

        failedNames = append(failedNames, closer.name)

        if nil == closeErr {
            closeErr = closeFailure
        }
    }

    /* every open in flight at the refusal is waited out here; each ends its own database against the closed flag, and a panicking open closes its channel through the recovery defer. The wait ends with the caller's deadline, and only the wait is abandoned: the answer then names an outstanding session. */
    abandonedOpens := 0

    /* an open that finished is not abandoned: with both channels ready the select picks at random, so the done channel is read once more, without blocking, before the deadline is believed */
    openHasEnded := func(done <-chan struct{}) bool {
        select {
        case <-done:
            return true
        case <-closeContext.Done():
            select {
            case <-done:
                return true
            default:
                return false
            }
        }
    }

    for _, pendingOpen := range pendingOpens {
        if false == openHasEnded(pendingOpen.done) {
            abandonedOpens++
        }
    }

    for _, migrationOpenDone := range pendingMigrationOpens {
        if false == openHasEnded(migrationOpenDone) {
            abandonedOpens++
        }
    }

    /* bun's diagnostic channel is handed back LAST, while the logger this registry reports through is still alive: the container closes the registry before the logging service, because the registry resolves it. Everything above — a pool close that provokes a bun warning, an open finishing against the closed flag — still reaches the journal; what comes after belongs on standard error. It is handed back only when it is this registry's: a second registry in the same process, routed to its own logger, keeps its channel through this teardown. */
    instance.lock.Lock()
    closingLogger, routedDiagnostics := instance.logger, instance.routedDiagnostics
    instance.lock.Unlock()

    resetDiagnosticsRoutedTo(closingLogger, routedDiagnostics)

    /* teardown diagnostics must name every pool that failed to close, not the first alone: the caller gets one error, so the other failures would otherwise leave no trace anywhere */
    if 1 < len(failedNames) {
        return exception.NewError(
            "bunorm manager registry close failed for multiple databases",
            map[string]any{"names": failedNames},
            closeErr,
        )
    }

    /* an abandoned wait is reported even when every pool closed cleanly: the pools ARE closed, and what the operator is being told is that the process is ending with a dial still outstanding, whose server-side session will be reaped by a timeout rather than ended. A pool failure keeps the report, because that is the worse of the two. */
    if nil == closeErr && 0 < abandonedOpens {
        return exception.NewError(
            "bunorm manager registry stopped waiting for opens still in flight when its close deadline passed; they end on their own and leave their sessions to be reaped",
            map[string]any{"abandonedOpens": abandonedOpens},
            closeContext.Err(),
        )
    }

    return closeErr
}
