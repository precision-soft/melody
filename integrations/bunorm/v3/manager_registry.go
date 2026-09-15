package bunorm

import (
    "context"
    "fmt"
    "reflect"
    "runtime/debug"
    "sort"
    "sync"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

/* ManagerRegistry owns process database pools and coordinates opens and shutdown under its lock. Its bound context must have process lifetime; per-request transactions belong to the caller. */
type ManagerRegistry struct {
    logger loggingcontract.Logger

    openContext context.Context

    providerDefinitionByName      map[string]ProviderDefinition
    defaultProviderDefinitionName string

    lock              sync.Mutex
    managers          map[string]*Manager
    pendingOpenByName map[string]*managerOpen

    pendingMigrationOpens map[chan struct{}]struct{}

    migrationDatabases map[string]*bun.DB
    closed             bool
}

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

    return &ManagerRegistry{
        logger:                        diagnosticLoggerWithIdentity(logger),
        openContext:                   ctx,
        providerDefinitionByName:      providerDefinitionByName,
        defaultProviderDefinitionName: defaultProviderDefinitionName,
        managers:                      make(map[string]*Manager),
        pendingOpenByName:             make(map[string]*managerOpen),
        pendingMigrationOpens:         make(map[chan struct{}]struct{}),
        migrationDatabases:            make(map[string]*bun.DB),
    }, nil
}

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

/* MigrationDatabase answers the connection the migration commands should run on: a dedicated one with the driver deadlines lifted when the provider implements MigrationProvider — reported through the second return — and the ordinary pooled connection otherwise. A request pool carries read and write deadlines sized for requests, and a DDL statement that legitimately runs past them is cut mid-statement with "invalid connection", outside any transaction MySQL would roll back; the dedicated connection exists so a long migration finishes instead. An empty name selects the default definition. The dedicated database is opened once per name and cached under it; the migration commands end it through CloseMigrationDatabase on their way out, and the registry's own Close stays the net underneath for whatever did not. */
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

    migrationOpenDone := make(chan struct{})
    instance.pendingMigrationOpens[migrationOpenDone] = struct{}{}

    instance.lock.Unlock()

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

/* CloseMigrationDatabase closes and forgets a dedicated migration connection. Empty name selects the default manager. An unopened or already-closed connection is a no-op; a closed registry returns an error. Close migration handles after use because their relaxed I/O deadlines are intended for DDL. */
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

    if nil == database {
        return nil
    }

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

func (instance *ManagerRegistry) providerDefinitionNotFoundErrorLocked(name string) error {
    registered := make([]string, 0, len(instance.providerDefinitionByName))
    for definitionName := range instance.providerDefinitionByName {
        registered = append(registered, definitionName)
    }

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

    func() {
        instance.lock.Lock()
        defer instance.lock.Unlock()

        delete(instance.pendingOpenByName, name)

        if nil != openErr {

            if nil != database {
                _ = database.Close()
            }

            pendingOpen.openError = openErr

            return
        }

        if nil == database {

            pendingOpen.openError = ErrProviderReturnedNilDatabase

            return
        }

        if true == instance.closed {

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

func (instance *ManagerRegistry) currentLogger() loggingcontract.Logger {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    return instance.logger
}

/* SetLogger replaces the registry logger and routes bun diagnostics to it in the same critical section. Nil and typed-nil loggers are refused. A closed registry returns ErrManagerRegistryClosed without changing either destination. */
func (instance *ManagerRegistry) SetLogger(logger loggingcontract.Logger) error {
    if true == isNilInterface(logger) {
        return ErrLoggerIsRequired
    }

    instance.lock.Lock()
    defer instance.lock.Unlock()

    if true == instance.closed {
        return ErrManagerRegistryClosed
    }

    retireRegistryDiagnostics(instance.logger)
    logger = diagnosticLoggerWithIdentity(logger)
    instance.logger = logger
    RouteDiagnostics(logger)

    return nil
}

func (instance *ManagerRegistry) openProviderDatabase(provider Provider, params ConnectionParameters) (*bun.DB, error) {
    logger := instance.currentLogger()

    if contextOpener, isContextOpener := provider.(ContextOpener); true == isContextOpener {
        return contextOpener.OpenContext(instance.openContext, params, logger)
    }

    return provider.Open(params, logger)
}

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

/* CloseWithContext tears down the pools and bounds the wait for in-flight opens with the caller’s deadline. Pool Close calls still run even if the deadline is spent. Abandoned opens reject their results against the closed registry when they finish. */
func (instance *ManagerRegistry) CloseWithContext(closeContext context.Context) error {

    instance.lock.Lock()

    instance.closed = true

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

    abandonedOpens := 0

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

    instance.lock.Lock()
    closingLogger := instance.logger
    instance.lock.Unlock()

    retireRegistryDiagnostics(closingLogger)

    if 1 < len(failedNames) {
        return exception.NewError(
            "bunorm manager registry close failed for multiple databases",
            map[string]any{"names": failedNames},
            closeErr,
        )
    }

    if nil == closeErr && 0 < abandonedOpens {
        return exception.NewError(
            "bunorm manager registry stopped waiting for opens still in flight when its close deadline passed; they end on their own and leave their sessions to be reaped",
            map[string]any{"abandonedOpens": abandonedOpens},
            closeContext.Err(),
        )
    }

    return closeErr
}
