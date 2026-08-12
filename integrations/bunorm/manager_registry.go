package bunorm

import (
    "context"
    "fmt"
    "reflect"
    "runtime/debug"
    "sort"
    "sync"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/config"
    configcontract "github.com/precision-soft/melody/config/contract"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
)

type ManagerRegistry struct {
    /* openResolver is what the lazy opens replay through, long after the construction-time resolution has ended: the container behind the given resolver when it can name one through the ContainerCarrier door, the resolver as handed otherwise — correct exactly when the caller handed the container itself. A resolution context must not be replayed: it is single-threaded, it dies with its scope, and a definition first dialed after that scope closed would fail "scope is closed" forever over a healthy configuration. */
    openResolver containercontract.Resolver
    /* openContext bounds the lazy opens of providers that implement ContextOpener, so a shutdown that cancels it reaches a retry loop in flight instead of sleeping through the whole retry budget. */
    openContext context.Context

    providerDefinitionByName      map[string]ProviderDefinition
    defaultProviderDefinitionName string

    lock              sync.Mutex
    managers          map[string]*Manager
    pendingOpenByName map[string]*managerOpen
    /* the migration databases live beside the request pools, never inside them: a migration connection lifts the driver deadlines, and handing it to request traffic would trade one failure mode for another */
    migrationDatabases map[string]*bun.DB
    closed             bool
}

/*
   managerOpen tracks a single in-flight Provider.Open for one definition name so
   that concurrent openers of the same name coalesce onto one attempt instead of
   each dialing the database while holding the registry-wide lock.
*/
type managerOpen struct {
    done      chan struct{}
    manager   *Manager
    openError error
}

/* NewManagerRegistry builds a container-level registry over lazily-dialed pools. Container-level deliberately: a *bun.DB is a connection pool — the process-lifetime shape of database/sql — and per-unit work takes a transaction or a Conn from the pool, not a pool per scope. The lazy opens replay through the container behind the given resolver (asked via the ContainerCarrier door), never through the resolution context that built the registry. */
func NewManagerRegistry(resolver containercontract.Resolver, providerDefinitions ...ProviderDefinition) (*ManagerRegistry, error) {
    return NewManagerRegistryWithContext(context.Background(), resolver, providerDefinitions...)
}

/* NewManagerRegistryWithContext additionally binds the registry to the given context: a provider that implements ContextOpener has its lazy opens run under it, so a shutdown that cancels the context refuses an open not yet started, reaches the attempt's cancellable steps — the configuration hook, the boot ping, a retry sleep — in flight, and pays at most the dialect handshake bun bounds by the connect timeout. A nil context reads as context.Background(), the exact behaviour of NewManagerRegistry. */
func NewManagerRegistryWithContext(ctx context.Context, resolver containercontract.Resolver, providerDefinitions ...ProviderDefinition) (*ManagerRegistry, error) {
    if true == isNilInterface(resolver) {
        return nil, ErrResolverIsRequired
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

    for _, providerDefinition := range providerDefinitions {
        if "" == providerDefinition.Name {
            return nil, ErrProviderDefinitionNameIsRequired
        }

        if true == isNilInterface(providerDefinition.Provider) {
            return nil, ErrProviderIsRequired
        }

        if _, exists := providerDefinitionByName[providerDefinition.Name]; true == exists {
            return nil, ErrProviderDefinitionNameMustBeUnique
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

    openResolver := resolver
    if carrier, isCarrier := resolver.(containercontract.ContainerCarrier); true == isCarrier {
        if carried := carrier.Container(); false == isNilInterface(carried) {
            openResolver = carried
        }
    }

    /* the marking runs here rather than in the validation loop above: that loop must be able to refuse a definition set having changed nothing, and a marking applied from inside it would leave a partially redacted configuration behind a registry that was never built. Here the set is known good and the resolver is the one the opens will use. */
    markProviderSecretParameters(openResolver, providerDefinitions)

    return &ManagerRegistry{
        openResolver:                  openResolver,
        openContext:                   ctx,
        providerDefinitionByName:      providerDefinitionByName,
        defaultProviderDefinitionName: defaultProviderDefinitionName,
        managers:                      make(map[string]*Manager),
        pendingOpenByName:             make(map[string]*managerOpen),
        migrationDatabases:            make(map[string]*bun.DB),
    }, nil
}

/* markProviderSecretParameters arms the framework's redaction for every credential parameter the definitions name, at construction rather than at the first dial. The configuration is asked through the tolerant door: a registry is legitimately built over a resolver that carries no configuration service — a test harness, a hand-wired container — and the Must door would turn that into a panic in the middle of a constructor that has nothing else to do with the configuration. An absent configuration, or a name no parameter answers to, leaves the marking undone the same way MarkSecret leaves an absent parameter alone. */
func markProviderSecretParameters(resolver containercontract.Resolver, providerDefinitions []ProviderDefinition) {
    configuration, configurationErr := container.FromResolver[configcontract.Configuration](resolver, config.ServiceConfig)
    if nil != configurationErr || true == isNilInterface(configuration) {
        return
    }

    for _, providerDefinition := range providerDefinitions {
        secretProvider, isSecretProvider := providerDefinition.Provider.(SecretParameterProvider)
        if false == isSecretProvider {
            continue
        }

        for _, parameterName := range secretProvider.SecretParameterNames() {
            if "" == parameterName {
                continue
            }

            configuration.MarkSecret(parameterName)
        }
    }
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

/* panicCause reads a recovered panic value as the cause of the error the recovery boundary fabricates in its place. It mirrors exception.PanicCause rather than calling it, because this module's go.mod pins a framework version that predates that door. A typed nil answers no cause: its Error() would dereference a nil receiver at the first render of the very record the boundary exists to hand on. */
func panicCause(recovered any) error {
    recoveredErr, isRecoveredError := recovered.(error)
    if false == isRecoveredError || true == isNilInterface(recoveredErr) {
        return nil
    }

    return recoveredErr
}

/* MigrationDatabase answers the connection the migration commands should run on: a dedicated one with the driver deadlines lifted when the provider implements MigrationProvider — reported through the second return — and the ordinary pooled connection otherwise. A request pool carries read and write deadlines sized for requests, and a DDL statement that legitimately runs past them is cut mid-statement with "invalid connection", outside any transaction MySQL would roll back; the dedicated connection exists so a long migration finishes instead. An empty name selects the default definition. The dedicated database is opened once per name, cached, and closed by Close. */
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
    instance.lock.Unlock()

    database, openErr := instance.openProviderMigrationDatabase(migrationProvider)
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

/* providerDefinitionNotFoundErrorLocked names the definition that was asked for and the ones that are registered, the way the framework's own container names an unregistered service id rather than answering a bare sentinel. It is called with the registry lock held, because it reads the definition map. The sentinel stays the CAUSE: every caller testing errors.Is(err, ErrProviderDefinitionNotFound) keeps its answer through Unwrap, and a replacement that dropped it would break them silently. */
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

    /*
       Open the provider outside the registry-wide lock: dialing, pinging and any
       uninterruptible retry sleeps of a down database must not serialize cache
       hits for other managers or a concurrent Close. A failed open is never
       memoized, so a later call retries.
    */

    settled := false
    defer func() {
        if true == settled {
            return
        }

        recovered := recover()

        instance.lock.Lock()
        delete(instance.pendingOpenByName, name)
        instance.lock.Unlock()

        /* the panic value rides along for the coalesced waiters: they receive this error instead of the re-raised panic, and without the value their log names the definition but not the refusal that produced it. It travels as the CAUSE as well as in the context, and the stack is captured here: the re-raised panic reaches a boundary that records both, so the waiters — who never see that panic — were the only callers handed a flattened message, for the same failure, decided by which goroutine they were on. */
        pendingOpen.openError = exception.NewError(
            "bunorm manager provider panicked while opening",
            map[string]any{
                "name":       name,
                "panic":      fmt.Sprintf("%v", recovered),
                "panicStack": string(debug.Stack()),
            },
            panicCause(recovered),
        )
        close(pendingOpen.done)

        if nil != recovered {
            panic(recovered)
        }
    }()
    database, openErr := instance.openProviderDatabase(providerDefinition.Provider)

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

            pendingOpen.openError = openErr

            return
        }

        if nil == database {
            /* a provider answering neither a database nor an error would otherwise be memoized as a manager wrapping nil, turning a wiring bug into a nil dereference at the first query, far from its cause */
            pendingOpen.openError = ErrProviderReturnedNilDatabase

            return
        }

        if true == instance.closed {
            /*
               Close ran while this open was in flight: it already iterated the manager
               map without this entry, so memoizing the manager now would leak its
               connection pool. Close the freshly opened database and refuse.
            */
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

/* openProviderDatabase runs one provider open, under the registry's context when the provider can honour one. */
func (instance *ManagerRegistry) openProviderDatabase(provider Provider) (*bun.DB, error) {
    if contextOpener, isContextOpener := provider.(ContextOpener); true == isContextOpener {
        return contextOpener.OpenContext(instance.openContext, instance.openResolver)
    }

    return provider.Open(instance.openResolver)
}

/* openProviderMigrationDatabase runs one migration open, under the registry's context when the provider can honour one — the same preference its sibling above applies to the ordinary open, on the door the promise had not reached. */
func (instance *ManagerRegistry) openProviderMigrationDatabase(provider MigrationProvider) (*bun.DB, error) {
    if contextOpener, isContextOpener := provider.(MigrationContextOpener); true == isContextOpener {
        return contextOpener.OpenForMigrationContext(instance.openContext, instance.openResolver)
    }

    return provider.OpenForMigration(instance.openResolver)
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
    instance.lock.Lock()
    defer instance.lock.Unlock()

    instance.closed = true

    var closeErr error
    failedNames := make([]string, 0)

    /* both maps are walked in sorted name order so the carried cause and the failed-name list are the same for the same failing teardown on every run: a map walk let two identical failures report different causes and different orders, and the rueidis batch reporting sorts for the same reason */
    managerNames := make([]string, 0, len(instance.managers))
    for name := range instance.managers {
        managerNames = append(managerNames, name)
    }
    sort.Strings(managerNames)

    for _, name := range managerNames {
        manager := instance.managers[name]
        if nil == manager {
            continue
        }

        managerCloseErr := manager.Close()
        if nil != managerCloseErr {
            failedNames = append(failedNames, name)
        }
        if nil == closeErr && nil != managerCloseErr {
            closeErr = managerCloseErr
        }
    }

    migrationNames := make([]string, 0, len(instance.migrationDatabases))
    for name := range instance.migrationDatabases {
        migrationNames = append(migrationNames, name)
    }
    sort.Strings(migrationNames)

    for _, name := range migrationNames {
        migrationDatabase := instance.migrationDatabases[name]
        if nil == migrationDatabase {
            continue
        }

        migrationDatabaseCloseErr := migrationDatabase.Close()
        if nil != migrationDatabaseCloseErr {
            failedNames = append(failedNames, name+" (migration)")
        }
        if nil == closeErr && nil != migrationDatabaseCloseErr {
            closeErr = migrationDatabaseCloseErr
        }
    }

    /* teardown diagnostics must name every pool that failed to close, not the first alone: the caller gets one error, so the other failures would otherwise leave no trace anywhere */
    if 1 < len(failedNames) {
        return exception.NewError(
            "bunorm manager registry close failed for multiple databases",
            map[string]any{"names": failedNames},
            closeErr,
        )
    }

    return closeErr
}
