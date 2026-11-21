package bunorm

import (
    "context"
    "fmt"
    "reflect"
    "runtime/debug"
    "sort"
    "sync"
    "unicode/utf8"

    "github.com/uptrace/bun"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

type ManagerRegistry struct {
    /* written once by the constructor and read without the lock */
    logger *registryLogger
    /* closeSignal is the registry's own signal, made from nothing at construction: CloseWithContext fires it when the refusal is published, and every open runs under the caller's context ended by it as well, so no open goes on dialling, or routing bun's diagnostics, after the registry is gone. */
    closeSignal       context.Context
    closeSignalCancel context.CancelFunc

    providerDefinitionByName map[string]ProviderDefinition
    /* the definition names sorted once, since the definitions never change after the constructor: an unknown-name refusal then reads them under the lock without sorting or allocating */
    sortedProviderDefinitionNames []string
    defaultProviderDefinitionName string

    lock              sync.Mutex
    managers          map[string]*Manager
    pendingOpenByName map[string]*managerOpen
    /* the migration opens in flight, kept as bare channels because migrations are not coalesced — a caller waits for its own dial, never for another's — while a teardown still has to wait for all of them */
    pendingMigrationOpens map[chan struct{}]struct{}
    /* the migration databases live beside the request pools, never inside them: a migration connection lifts the driver deadlines, and handing it to request traffic would trade one failure mode for another */
    migrationDatabases map[string]*bun.DB
    /* how many callers hold each dedicated migration connection: every MigrationDatabase answering it takes a hold, and CloseMigrationDatabase closes it when the last hold is released */
    migrationHolders map[string]int
    closed           bool
}

/* managerOpen tracks a single in-flight Provider.Open for one definition name so that concurrent openers of the same name coalesce onto one attempt instead of each dialing the database while holding the registry-wide lock. */
type managerOpen struct {
    done      chan struct{}
    manager   *Manager
    openError error
    /* set before done is closed when the open ended by its opener's own context, so a waiter whose context is live opens again instead of receiving another caller's cancellation */
    endedByOpenerContext bool
}

/* NewManagerRegistry builds a container-level registry over lazily-dialed pools; it captures no context, each open running under the runtime of the caller that asked for it. Container-level deliberately: a *bun.DB is a connection pool — the process-lifetime shape of database/sql — and per-unit work takes a transaction or a Conn from the pool, not a pool per scope. The logger is the one the registry and its providers report through for its whole life, so the registry is built where the application's logger resolves, inside its provider. A nil logger is refused. The definitions are read in order and refused by position: an empty name, a nil provider, an empty Parameters.Host or Parameters.Database, a duplicate name, more than one default. */
func NewManagerRegistry(logger loggingcontract.Logger, providerDefinitions []ProviderDefinition) (*ManagerRegistry, error) {
    if true == isNilInterface(logger) {
        return nil, ErrLoggerIsRequired
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

        if "" == providerDefinition.Parameters.Host {
            return nil, providerDefinitionFieldRefusal(ErrConnectionHostIsRequired, position, providerDefinition.Name, "ProviderDefinition.Parameters.Host")
        }

        if "" == providerDefinition.Parameters.Database {
            return nil, providerDefinitionFieldRefusal(ErrConnectionDatabaseIsRequired, position, providerDefinition.Name, "ProviderDefinition.Parameters.Database")
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

    closeSignal, closeSignalCancel := context.WithCancel(context.Background())

    return &ManagerRegistry{
        logger:                        newRegistryLogger(logger),
        closeSignal:                   closeSignal,
        closeSignalCancel:             closeSignalCancel,
        providerDefinitionByName:      providerDefinitionByName,
        sortedProviderDefinitionNames: sortedProviderDefinitionNamesOf(providerDefinitionByName),
        defaultProviderDefinitionName: defaultProviderDefinitionName,
        managers:                      make(map[string]*Manager),
        pendingOpenByName:             make(map[string]*managerOpen),
        pendingMigrationOpens:         make(map[chan struct{}]struct{}),
        migrationDatabases:            make(map[string]*bun.DB),
        migrationHolders:              make(map[string]int),
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

/* MigrationDatabase answers the connection the migration commands run on, the provider's OpenForMigration, opened under the caller's runtime once per name and shared: every call answering it takes a hold, CloseMigrationDatabase releases one and closes the connection with the last, and the registry's CloseWithContext is the net for whatever was not released. An answer that is the very pool the registry holds for the name is shared with that pool and takes no hold. A caller whose context is already done is refused before anything is opened or held; an empty name selects the default definition. A nil runtime, or one carrying no context or no scope, is refused before the lock is taken. */
func (instance *ManagerRegistry) MigrationDatabase(runtimeInstance containercontract.Runtime, name string) (*bun.DB, error) {
    if runtimeErr := refuseOpenRuntime(runtimeInstance); nil != runtimeErr {
        return nil, runtimeErr
    }

    if "" == name {
        name = instance.defaultProviderDefinitionName
    }

    callerContext := runtimeInstance.Context()

    instance.lock.Lock()

    if true == instance.closed {
        instance.lock.Unlock()

        return nil, ErrManagerRegistryClosed
    }

    if database, exists := instance.migrationDatabases[name]; true == exists {
        instance.migrationHolders[name]++
        instance.lock.Unlock()

        return database, nil
    }

    providerDefinition, exists := instance.providerDefinitionByName[name]
    if false == exists {
        notFoundErr := instance.providerDefinitionNotFoundErrorLocked(name)
        instance.lock.Unlock()

        return nil, notFoundErr
    }

    if nil != callerContext.Err() {
        instance.lock.Unlock()

        return nil, openRefusedByCallerContext(name, callerContext)
    }

    /* the dial runs outside the registry-wide lock for the same reason Manager's does: a down database must not serialize cache hits or a concurrent CloseWithContext. Migrations run from a sequential cli command, so no coalescing machinery is warranted — a concurrent duplicate open is resolved below by closing the loser. */

    /* the dial is announced before the lock is released, so a CloseWithContext arriving during it waits this open out instead of returning over it. It is a bare channel rather than the coalescing record its sibling keeps: nobody waits for this dial except a teardown, and never for its value. */
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

    openRuntime, releaseOpenRuntime := instance.openRuntimeOf(runtimeInstance)
    defer releaseOpenRuntime()

    database, openErr := providerDefinition.Provider.OpenForMigration(openRuntime, providerDefinition.Parameters, instance.logger)
    if nil != openErr {
        if nil != database {
            _ = database.Close()
        }

        return nil, openErr
    }

    if nil == database {
        return nil, ErrProviderReturnedNilDatabase
    }

    kept, keepErr := instance.keepMigrationDatabase(name, database)
    if kept != database {
        /* the loser of the open race, or a database opened into a registry closed meanwhile, is closed outside the lock, so a slow close does not hold every other registry call */
        _ = database.Close()
    }

    if nil != keepErr {
        return nil, keepErr
    }

    return kept, nil
}

/* keepMigrationDatabase registers the database under the lock and answers it, or answers the one another call registered first, or the name's pool when the database is that very pool; an answer other than the database handed in tells the caller its own database is not kept */
func (instance *ManagerRegistry) keepMigrationDatabase(name string, database *bun.DB) (*bun.DB, error) {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    if true == instance.closed {
        return nil, ErrManagerRegistryClosed
    }

    if manager, exists := instance.managers[name]; true == exists && database == manager.Database() {
        return database, nil
    }

    if existingDatabase, exists := instance.migrationDatabases[name]; true == exists {
        instance.migrationHolders[name]++

        return existingDatabase, nil
    }

    instance.migrationDatabases[name] = database
    instance.migrationHolders[name] = 1

    return database, nil
}

/* CloseMigrationDatabase releases one hold on the dedicated migration connection opened for one definition, and with the last hold ends it and forgets it, so the next MigrationDatabase opens a fresh one; an empty name selects the default. That connection lifts the driver deadlines and recycles nothing, so it must not outlive the migration run. A name with no migration connection closes nothing, and a closed registry refuses the call. */
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

    /* another caller still holds the connection: two migration commands in one process share it, and the first to release must not close it under the second */
    instance.migrationHolders[name]--
    if 0 < instance.migrationHolders[name] {
        instance.lock.Unlock()

        return nil
    }

    delete(instance.migrationDatabases, name)
    delete(instance.migrationHolders, name)

    instance.lock.Unlock()

    /* the close travels the wire — COM_QUIT to a peer that may be partitioned, on a connection whose write deadlines are deliberately lifted — so it runs outside the registry-wide lock, the same discipline CloseWithContext keeps and for the same reason */
    return database.Close()
}

/* DefaultManager is Manager for the default definition, under the same runtime rules. */
func (instance *ManagerRegistry) DefaultManager(runtimeInstance containercontract.Runtime) (*Manager, error) {
    return instance.Manager(runtimeInstance, instance.defaultProviderDefinitionName)
}

func (instance *ManagerRegistry) MustDefaultManager(runtimeInstance containercontract.Runtime) *Manager {
    manager, managerErr := instance.DefaultManager(runtimeInstance)
    if nil != managerErr {
        exception.Panic(exception.FromError(managerErr))
    }

    return manager
}

func (instance *ManagerRegistry) DefaultDatabase(runtimeInstance containercontract.Runtime) (*bun.DB, error) {
    manager, managerErr := instance.DefaultManager(runtimeInstance)
    if nil != managerErr {
        return nil, managerErr
    }

    return manager.Database(), nil
}

func (instance *ManagerRegistry) MustDefaultDatabase(runtimeInstance containercontract.Runtime) *bun.DB {
    database, databaseErr := instance.DefaultDatabase(runtimeInstance)
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

/* providerDefinitionFieldRefusal is providerDefinitionRefusal for a definition field left empty, naming the field beside the position and the name. */
func providerDefinitionFieldRefusal(sentinel error, position int, name string, field string) error {
    return exception.NewError(
        sentinel.Error(),
        map[string]any{
            "position": position,
            "name":     name,
            "field":    field,
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
    errorContext := map[string]any{
        "requested":  name,
        "registered": instance.sortedProviderDefinitionNames,
    }

    /* a requested name past the bound is cut on a rune boundary, its full length kept beside it, so a caller-supplied name cannot carry a megabyte into the error and the journal */
    if maxRequestedNameLength < len(name) {
        errorContext["requested"] = name[:runeBoundaryAtOrBefore(name, maxRequestedNameLength)] + truncatedNameMarker
        errorContext["requestedLength"] = len(name)
    }

    return exception.NewError(
        "provider definition not found",
        errorContext,
        ErrProviderDefinitionNotFound,
    )
}

/* maxRequestedNameLength bounds, in bytes, the requested name an unknown-name refusal carries */
const maxRequestedNameLength = 128

const truncatedNameMarker = "...(truncated)"

/* sortedProviderDefinitionNamesOf answers the definition names sorted, so one misspelling always prints one list: the map walk is random, and an operator comparing two runs would otherwise read two different answers to the same question */
func sortedProviderDefinitionNamesOf(providerDefinitionByName map[string]ProviderDefinition) []string {
    names := make([]string, 0, len(providerDefinitionByName))
    for definitionName := range providerDefinitionByName {
        names = append(names, definitionName)
    }

    sort.Strings(names)

    return names
}

/* runeBoundaryAtOrBefore answers the largest index at or before limit that starts a rune, so a cut never splits a multi-byte character */
func runeBoundaryAtOrBefore(value string, limit int) int {
    boundary := limit
    for 0 < boundary && false == utf8.RuneStart(value[boundary]) {
        boundary--
    }

    return boundary
}

/* Manager answers the manager of one definition, opening its pool at the first call under the caller's runtime: the provider is handed that runtime over a context the caller's and the registry's Close both end. Concurrent callers of one name coalesce onto one open, the opener's; a waiter leaves when its own context ends, the open going on for its opener, and a waiter whose context is live opens again when the open it waited on ended by its opener's context, so no caller receives another's cancellation. A failed open is never memoized. A caller whose context is already done is refused before an open starts; a memoized manager is answered whatever the context. A nil runtime, or one carrying no context or no scope, is refused before the lock is taken. */
func (instance *ManagerRegistry) Manager(runtimeInstance containercontract.Runtime, name string) (*Manager, error) {
    if runtimeErr := refuseOpenRuntime(runtimeInstance); nil != runtimeErr {
        return nil, runtimeErr
    }

    if "" == name {
        return nil, ErrProviderDefinitionNameIsRequired
    }

    callerContext := runtimeInstance.Context()

    for {
        pendingOpen, opener, settledManager, settledErr := instance.joinOrStartOpen(callerContext, name)
        if nil == pendingOpen {
            return settledManager, settledErr
        }

        if true == opener {
            return instance.openManager(runtimeInstance, name, pendingOpen)
        }

        select {
        case <-pendingOpen.done:
        case <-callerContext.Done():
            /* an open that finished in the same instant is answered, the departure is not */
            select {
            case <-pendingOpen.done:
            default:
                return nil, openWaitCancelledByCallerContext(name, callerContext)
            }
        }

        if true == pendingOpen.endedByOpenerContext && nil == callerContext.Err() {
            continue
        }

        return pendingOpen.manager, pendingOpen.openError
    }
}

/* joinOrStartOpen answers, under the lock, a settled answer with a nil record, or the open in flight to wait on, or a new open the caller runs as its opener */
func (instance *ManagerRegistry) joinOrStartOpen(callerContext context.Context, name string) (*managerOpen, bool, *Manager, error) {
    instance.lock.Lock()

    /* the refusal stands at the entry, ahead of the cache: CloseWithContext ends every pool it memoized without emptying the map, so a cache hit would hand back a manager over a dead pool with a nil error, while the open path below refuses the same call by name — one registry answering the same question two ways, and the answer that looks like success fails at the first query instead */
    if true == instance.closed {
        instance.lock.Unlock()

        return nil, false, nil, ErrManagerRegistryClosed
    }

    if manager, exists := instance.managers[name]; true == exists {
        instance.lock.Unlock()

        return nil, false, manager, nil
    }

    if _, exists := instance.providerDefinitionByName[name]; false == exists {
        notFoundErr := instance.providerDefinitionNotFoundErrorLocked(name)
        instance.lock.Unlock()

        return nil, false, nil, notFoundErr
    }

    if pendingOpen, inFlight := instance.pendingOpenByName[name]; true == inFlight {
        instance.lock.Unlock()

        return pendingOpen, false, nil, nil
    }

    if nil != callerContext.Err() {
        instance.lock.Unlock()

        return nil, false, nil, openRefusedByCallerContext(name, callerContext)
    }

    pendingOpen := &managerOpen{done: make(chan struct{})}
    instance.pendingOpenByName[name] = pendingOpen

    instance.lock.Unlock()

    return pendingOpen, true, nil, nil
}

/* openManager runs the open the caller registered as its opener and publishes its outcome to the waiters */
func (instance *ManagerRegistry) openManager(runtimeInstance containercontract.Runtime, name string, pendingOpen *managerOpen) (*Manager, error) {
    /* the definitions never change after the constructor, so the read needs no lock */
    providerDefinition := instance.providerDefinitionByName[name]
    callerContext := runtimeInstance.Context()

    /* Open the provider outside the registry-wide lock: dialing, pinging and any uninterruptible retry sleeps of a down database must not serialize cache hits for other managers or a concurrent CloseWithContext. A failed open is never memoized, so a later call retries. */

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

        /* the panic value travels in the context always and as the cause when it is an error value, with the stack captured here, so the coalesced waiters, who never see the re-raised panic, get the diagnosis the unwinding goroutine gets */
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
    openRuntime, releaseOpenRuntime := instance.openRuntimeOf(runtimeInstance)
    defer releaseOpenRuntime()

    database, openErr := providerDefinition.Provider.Open(openRuntime, providerDefinition.Parameters, instance.logger)
    providerReturned = true

    /* a database opened into a registry closed meanwhile is closed after the lock is released, so a slow close does not hold every other registry call; the teardown photographed this open before the publish deleted it, so it still waits for it */
    var refusedDatabase *bun.DB

    /* the publish runs in a closure with a deferred unlock: it calls into the freshly opened database, and a panic there would otherwise unwind with the lock held, whereupon the recovery defer above re-acquires the same non-reentrant mutex and wedges the whole registry with no waiter ever released */
    func() {
        instance.lock.Lock()
        defer instance.lock.Unlock()

        delete(instance.pendingOpenByName, name)

        if nil != openErr {
            /* the Provider contract does not promise a nil database beside a non-nil error, and a pool handed over with an error would otherwise be the last reference anyone holds */
            /* closed under the lock, as the entry is deleted here: a teardown photographing the in-flight opens would otherwise miss this one and return with its pool still open */
            if nil != database {
                _ = database.Close()
            }

            /* an open the registry ended through its close signal reaches its waiter as the registry's refusal with the cancellation under it; a refusal that does not carry the cancellation is the provider's own and travels unchanged. A provider that refuses with a cancellation of its own after the close is still read as ended by the registry, since nothing on the error says whose it is */
            if true == instance.closed && nil != instance.closeSignal.Err() && true == exception.ChainIs(openErr, context.Canceled) {
                pendingOpen.openError = keepLoggedMark(exception.NewError(
                    fmt.Sprintf("bunorm manager %s open ended by the registry closing while it was in flight", name),
                    map[string]any{"manager": name},
                    &openEndedByClose{openErr: openErr},
                ), openErr)

                return
            }

            /* an open its opener's context ended, read off that context and not the failure's class, so a provider's own cancellation under a live opener travels unchanged */
            if nil != callerContext.Err() && true == exception.ChainIs(openErr, callerContext.Err()) {
                pendingOpen.endedByOpenerContext = true
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
            /* CloseWithContext ran while this open was in flight: it already iterated the manager map without this entry, so memoizing the manager now would leak its connection pool. The freshly opened database is closed once the lock is released, and the open refused. */
            refusedDatabase = database
            pendingOpen.openError = ErrManagerRegistryClosed

            return
        }

        manager := NewManager(name, database)
        instance.managers[name] = manager
        pendingOpen.manager = manager
    }()

    /* closed before the waiters are released, so a teardown waiting on this open never finishes with its pool still open */
    if nil != refusedDatabase {
        _ = refusedDatabase.Close()
    }

    settled = true

    close(pendingOpen.done)

    return pendingOpen.manager, pendingOpen.openError
}

/* refuseOpenRuntime refuses a runtime an open cannot run under: nil, a typed nil, one carrying no context, and one carrying no scope, since the provider is handed the runtime re-based on the open's context over the caller's scope */
func refuseOpenRuntime(runtimeInstance containercontract.Runtime) error {
    if runtimeErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != runtimeErr {
        return runtimeErr
    }

    if true == isNilInterface(runtimeInstance.Scope()) {
        return exception.NewError(
            "the runtime carries no scope",
            exceptioncontract.Context{"argument": "runtimeInstance"},
            nil,
        )
    }

    return nil
}

/* openRuntimeOf answers the caller's runtime over a context that the caller's own context and the registry's close signal both end, and the release that ends it once the open settled */
func (instance *ManagerRegistry) openRuntimeOf(runtimeInstance containercontract.Runtime) (containercontract.Runtime, func()) {
    openContext, openCancel := context.WithCancel(runtimeInstance.Context())
    stopAfterClose := context.AfterFunc(instance.closeSignal, openCancel)

    return runtime.WithContext(runtimeInstance, openContext), func() {
        stopAfterClose()
        openCancel()
    }
}

/* openRefusedByCallerContext is the refusal of an open whose caller's context was done before it started: nothing is dialled, held or memoized, and the cause is context.Cause, so errors.Is matches the cause set through WithCancelCause */
func openRefusedByCallerContext(name string, callerContext context.Context) error {
    return exception.NewError(
        fmt.Sprintf("bunorm manager %s open refused: the caller's context is done", name),
        map[string]any{"manager": name},
        context.Cause(callerContext),
    )
}

/* openWaitCancelledByCallerContext is the refusal of a waiter on a coalesced open whose own context ended first; the open goes on for its opener */
func openWaitCancelledByCallerContext(name string, waiterContext context.Context) error {
    return exception.NewError(
        fmt.Sprintf("bunorm manager %s open wait was cancelled by the caller's context", name),
        map[string]any{"manager": name},
        context.Cause(waiterContext),
    )
}

func (instance *ManagerRegistry) MustManager(runtimeInstance containercontract.Runtime, name string) *Manager {
    manager, managerErr := instance.Manager(runtimeInstance, name)
    if nil != managerErr {
        exception.Panic(exception.FromError(managerErr))
    }

    return manager
}

func (instance *ManagerRegistry) Database(runtimeInstance containercontract.Runtime, name string) (*bun.DB, error) {
    manager, managerErr := instance.Manager(runtimeInstance, name)
    if nil != managerErr {
        return nil, managerErr
    }

    return manager.Database(), nil
}

func (instance *ManagerRegistry) MustDatabase(runtimeInstance containercontract.Runtime, name string) *bun.DB {
    database, databaseErr := instance.Database(runtimeInstance, name)
    if nil != databaseErr {
        exception.Panic(exception.FromError(databaseErr))
    }

    return database
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

/* CloseWithContext closes every pool under a deadline its caller declares. The pools are torn down whatever the deadline says; the deadline bounds the wait for each pool's close, the migration databases' included, which goes on in the background past it, and the wait for opens still in flight when the refusal was published, which end against the closed flag on their own. */
func (instance *ManagerRegistry) CloseWithContext(closeContext context.Context) error {
    /* the refusal is published under the lock and the pools are torn down outside it, since a pool close travels the wire and would park every caller on the lock. The maps are snapshotted, never emptied: the entry refusal reads the flag, and a manager handed out earlier keeps working through its own pool's close. */
    instance.lock.Lock()

    instance.closed = true

    /* an open still in flight is cancelled here, with the refusal: it has no consumer left, and its retry loop would go on routing bun's diagnostics onto the logger handed back below */
    instance.closeSignalCancel()

    /* both maps are walked in sorted name order, pools then migration databases, so the carried cause and the failed-name list are the same on every run. The nil skip stays here, where each value has its concrete type; carried into the list, a nil pool would be a non-nil interface. */
    closers := make([]namedCloser, 0, len(instance.managers)+len(instance.migrationDatabases))

    for _, name := range sortedNamesOf(instance.managers) {
        if manager := instance.managers[name]; nil != manager {
            closers = append(closers, namedCloser{name: name, close: func() error {
                return manager.CloseWithContext(closeContext)
            }})
        }
    }

    for _, name := range sortedNamesOf(instance.migrationDatabases) {
        migrationName := name + " (migration)"
        migrationDatabase := instance.migrationDatabases[name]
        closers = append(closers, namedCloser{name: migrationName, close: func() error {
            return startDatabaseClose(migrationDatabase).await(closeContext, migrationName)
        }})
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
    retireDiagnosticsRoutedBy(instance.logger, nil)

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

/* keepLoggedMark marks a wrapper logged when the cause it wraps was: the mark is read at the outermost link, so a wrapper over a failure the provider already journaled would otherwise be filed a second time by the kernel or the console */
func keepLoggedMark(wrapper error, cause error) error {
    if true == exception.IsAlreadyLogged(cause) {
        return exception.MarkLogged(wrapper)
    }

    return wrapper
}
