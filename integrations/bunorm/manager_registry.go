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
    /* openResolver is what the lazy opens replay through after the construction-time resolution has ended: the container behind the given resolver when it can name one through the ContainerCarrier door, the resolver as given otherwise. A resolution context is never replayed: it is single-threaded and dies with its scope, so a later dial through it would fail "scope is closed". */
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

/* managerOpen tracks a single in-flight Provider.Open for one definition name so that concurrent openers of the same name coalesce onto one attempt instead of each dialing the database while holding the registry-wide lock. */
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

    openResolver := resolver
    if carrier, isCarrier := resolver.(containercontract.ContainerCarrier); true == isCarrier {
        if carried := carrier.Container(); false == isNilInterface(carried) {
            openResolver = carried
        }
    }

    /* the marking runs after the validation loop, so a refused definition set changes nothing and leaves no partially redacted configuration; here the set is known good and the resolver is the one the opens use */
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

/* markProviderSecretParameters arms the framework's redaction for every credential parameter the definitions name, at construction rather than at the first dial. The configuration is asked through the tolerant door, since a registry is legitimately built over a resolver without a configuration service; an absent configuration or an unknown name leaves the marking undone, as MarkSecret leaves an absent parameter alone. */
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

/* panicCause reads a recovered panic value as the cause of the error the recovery boundary fabricates in its place. It mirrors exception.PanicCause and is kept because this major is sealed; a typed nil answers no cause, since its Error() would dereference a nil receiver. */
func panicCause(recovered any) error {
    recoveredErr, isRecoveredError := recovered.(error)
    if false == isRecoveredError || true == isNilInterface(recoveredErr) {
        return nil
    }

    return recoveredErr
}

/* MigrationDatabase answers the connection the migration commands run on: a dedicated one with the driver deadlines lifted when the provider implements MigrationProvider, reported through the second return, and the pooled connection otherwise, since a DDL statement cut by a request deadline is not rolled back by MySQL. An empty name selects the default definition; the dedicated database is opened once per name, cached, and closed by Close. */
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

    /* the dial runs outside the registry-wide lock, as Manager's does, so a down database does not serialize cache hits or a concurrent Close; migrations run from a sequential command, so a concurrent duplicate open is resolved by closing the loser */
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

/* providerDefinitionRefusal names the definition a construction-time refusal is about, by its position in the argument list and by its name when it has one, so over several definitions the operator knows which one is broken. The sentinel stays the cause, so errors.Is keeps its answer. */
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

/* providerDefinitionNotFoundErrorLocked names the definition asked for and the ones registered, as the framework's container names an unregistered service id. It is called with the registry lock held, and the sentinel stays the cause, so errors.Is(err, ErrProviderDefinitionNotFound) keeps its answer. */
func (instance *ManagerRegistry) providerDefinitionNotFoundErrorLocked(name string) error {
    registered := make([]string, 0, len(instance.providerDefinitionByName))
    for definitionName := range instance.providerDefinitionByName {
        registered = append(registered, definitionName)
    }

    /* sorted so one misspelling always prints one list */
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

    /* the refusal stands at the entry, ahead of the cache: Close ends every memoized pool without emptying the map, so a cache hit would hand back a manager over a dead pool with a nil error */
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

    /* the provider opens outside the registry-wide lock, so dialling, pinging and retry sleeps of a down database do not serialize cache hits for other managers or a concurrent Close; a failed open is never memoized, so a later call retries */

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

        /* the refusal names what unwound: an unwind after the provider returned is the registry's own publish, which calls into the fresh database, and an unwind carrying no value is a goroutine exit, a t.Fatalf or runtime.Goexit inside a provider, not a panic */
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

        /* the panic value travels to the coalesced waiters, who receive this error instead of the re-raised panic, as the cause and in the context, with the stack captured here, so they carry the same failure the re-raised panic's boundary records */
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
            panicCause(recovered),
        )
        close(pendingOpen.done)

        if nil != recovered {
            panic(recovered)
        }
    }()
    database, openErr := instance.openProviderDatabase(providerDefinition.Provider)
    providerReturned = true

    /* the publish runs in a closure with a deferred unlock: it calls into the fresh database, and a panic there would otherwise unwind with the lock held and the recovery defer would re-acquire the non-reentrant mutex, wedging the registry */
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
            /* Close ran while this open was in flight and iterated the manager map without this entry, so memoizing it would leak its pool: the fresh database is closed and the call refused */
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

/* openProviderMigrationDatabase runs one migration open, under the registry's context when the provider can honour one, as openProviderDatabase does for the ordinary open. */
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
    /* the refusal is published under the lock and the pools are torn down outside it, since a pool close travels the wire and would park every caller on the lock. The maps are snapshotted, never emptied: the entry refusal reads the flag, and a manager handed out earlier keeps working through its own pool's close. */
    instance.lock.Lock()

    instance.closed = true

    /* both maps are walked in sorted name order, so the carried cause and the failed-name list are the same for the same failing teardown on every run */
    managerNames := make([]string, 0, len(instance.managers))
    for name := range instance.managers {
        managerNames = append(managerNames, name)
    }
    sort.Strings(managerNames)

    managers := make([]*Manager, 0, len(managerNames))
    for _, name := range managerNames {
        managers = append(managers, instance.managers[name])
    }

    migrationNames := make([]string, 0, len(instance.migrationDatabases))
    for name := range instance.migrationDatabases {
        migrationNames = append(migrationNames, name)
    }
    sort.Strings(migrationNames)

    migrationDatabases := make([]*bun.DB, 0, len(migrationNames))
    for _, name := range migrationNames {
        migrationDatabases = append(migrationDatabases, instance.migrationDatabases[name])
    }

    instance.lock.Unlock()

    var closeErr error
    failedNames := make([]string, 0)

    for index, name := range managerNames {
        manager := managers[index]
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

    for index, name := range migrationNames {
        migrationDatabase := migrationDatabases[index]
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
