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

    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

type ManagerRegistry struct {
    logger loggingcontract.Logger
    /* kept so Close hands back exactly this destination, even for a logger that has no identity to be recognised by */
    routedDiagnostics *diagnosticsTarget
    openContext       context.Context
    openCancel        context.CancelFunc

    providerDefinitionByName      map[string]ProviderDefinition
    sortedProviderDefinitionNames []string
    defaultProviderDefinitionName string

    lock                  sync.Mutex
    managers              map[string]*Manager
    pendingOpenByName     map[string]*managerOpen
    pendingMigrationOpens map[chan struct{}]struct{}
    /* a migration connection lifts the driver deadlines: it lives beside the request pools, never inside them */
    migrationDatabases map[string]*bun.DB
    migrationHolders   map[string]int
    closed             bool
}

type managerOpen struct {
    done      chan struct{}
    manager   *Manager
    openError error
}

/* NewManagerRegistry is container-level deliberately: a *bun.DB is a process-lifetime pool, not a pool per scope. */
func NewManagerRegistry(logger loggingcontract.Logger, providerDefinitions ...ProviderDefinition) (*ManagerRegistry, error) {
    return NewManagerRegistryWithContext(context.Background(), logger, providerDefinitions...)
}

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
        logger:                        newRegistryLogger(logger),
        openContext:                   openContext,
        openCancel:                    openCancel,
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

/* duplicated from the framework's internal package, which a separate module cannot import */
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

/* MigrationDatabase takes a hold when it answers the dedicated connection; CloseMigrationDatabase releases the hold. */
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
        instance.migrationHolders[name]++
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

    /* the dial runs outside the registry-wide lock: a down database must not serialize cache hits or a concurrent Close */

    /* the dial is announced before the lock is released, so a Close arriving during it waits this open out */
    migrationOpenDone := make(chan struct{})
    instance.pendingMigrationOpens[migrationOpenDone] = struct{}{}

    instance.lock.Unlock()

    /* registered first so it runs last: a cleanup taking the lock ahead of the later unlock defers would deadlock */
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
        instance.migrationHolders[name]++

        return existingDatabase, true, nil
    }

    instance.migrationDatabases[name] = database
    instance.migrationHolders[name] = 1

    return database, true, nil
}

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

    instance.migrationHolders[name]--
    if 0 < instance.migrationHolders[name] {
        instance.lock.Unlock()

        return nil
    }

    delete(instance.migrationDatabases, name)
    delete(instance.migrationHolders, name)

    instance.lock.Unlock()

    /* the close travels the wire to a peer that may be partitioned, so it runs outside the registry-wide lock */
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

func (instance *ManagerRegistry) HasProviderDefinition(name string) bool {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    _, exists := instance.providerDefinitionByName[name]

    return exists
}

func (instance *ManagerRegistry) providerDefinitionNotFoundErrorLocked(name string) error {
    errorContext := map[string]any{
        "requested":  name,
        "registered": instance.sortedProviderDefinitionNames,
    }

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

const maxRequestedNameLength = 128

const truncatedNameMarker = "...(truncated)"

func sortedProviderDefinitionNamesOf(providerDefinitionByName map[string]ProviderDefinition) []string {
    names := make([]string, 0, len(providerDefinitionByName))
    for definitionName := range providerDefinitionByName {
        names = append(names, definitionName)
    }

    sort.Strings(names)

    return names
}

func runeBoundaryAtOrBefore(value string, limit int) int {
    boundary := limit
    for 0 < boundary && false == utf8.RuneStart(value[boundary]) {
        boundary--
    }

    return boundary
}

func (instance *ManagerRegistry) Manager(name string) (*Manager, error) {
    if "" == name {
        return nil, ErrProviderDefinitionNameIsRequired
    }

    instance.lock.Lock()

    /* the refusal stands ahead of the cache: Close ends every memoized pool without emptying the map */
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

    /* open outside the registry-wide lock: a down database's dial and retry sleeps must not serialize cache hits or a Close */

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

        /* coalesced waiters never see the re-raised panic, so the panic value and the stack travel in the refusal */
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

    /* deferred unlock: a panic with the lock held would wedge the recovery defer on the same non-reentrant mutex */
    func() {
        instance.lock.Lock()
        defer instance.lock.Unlock()

        delete(instance.pendingOpenByName, name)

        if nil != openErr {
            /* the Provider contract does not promise a nil database beside a non-nil error; this may be the last reference */
            if nil != database {
                _ = database.Close()
            }

            if true == instance.closed && nil != instance.openContext.Err() && true == exception.ChainIs(openErr, context.Canceled) {
                pendingOpen.openError = keepLoggedMark(exception.NewError(
                    fmt.Sprintf("bunorm manager %s open ended by the registry closing while it was in flight", name),
                    map[string]any{"manager": name},
                    &openEndedByClose{openErr: openErr},
                ), openErr)

                return
            }

            pendingOpen.openError = openErr

            return
        }

        if nil == database {
            pendingOpen.openError = ErrProviderReturnedNilDatabase

            return
        }

        if true == instance.closed {
            /* Close ran during this open and missed this entry: memoizing would leak the pool, so close the database and refuse */
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

/* currentLogger takes the lock: SetLogger replaces the field while opens run outside the registry-wide lock */
func (instance *ManagerRegistry) currentLogger() loggingcontract.Logger {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    return instance.logger
}

func (instance *ManagerRegistry) SetLogger(logger loggingcontract.Logger) error {
    if true == isNilInterface(logger) {
        return ErrLoggerIsRequired
    }

    instance.lock.Lock()
    defer instance.lock.Unlock()

    if true == instance.closed {
        return ErrManagerRegistryClosed
    }

    /* the logger handed out before is retired first, so an open in flight on it cannot route after the new one */
    retireRegistryLogger(instance.logger)

    ownedLogger := newRegistryLogger(logger)
    instance.logger = ownedLogger
    instance.routedDiagnostics = routeDiagnosticsTo(ownedLogger)

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

/* CloseWithContext tears the pools down whatever the deadline says; the deadline bounds only the wait for opens in flight. */
func (instance *ManagerRegistry) CloseWithContext(closeContext context.Context) error {
    /* published under the lock, torn down outside it: a pool close travels the wire; the maps are never emptied */
    instance.lock.Lock()

    instance.closed = true

    /* cancelled with the refusal: its retry loop would go on routing bun's diagnostics onto the logger handed back below */
    instance.openCancel()

    /* the nil skip stays here, where each value has its concrete type: in the list a nil pool would be a non-nil interface */
    closers := make([]namedCloser, 0, len(instance.managers)+len(instance.migrationDatabases))

    for _, name := range sortedNamesOf(instance.managers) {
        if manager := instance.managers[name]; nil != manager {
            closers = append(closers, namedCloser{name: name, close: manager.Close})
        }
    }

    for _, name := range sortedNamesOf(instance.migrationDatabases) {
        closers = append(closers, namedCloser{name: name + " (migration)", close: instance.migrationDatabases[name].Close})
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

    abandonedOpens := 0

    /* with both channels ready the select picks at random, so done is read once more before the deadline is believed */
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

    /* bun's channel is handed back last, while the registry's logger is alive, and only when it is this registry's */
    instance.lock.Lock()
    closingLogger, routedDiagnostics := instance.logger, instance.routedDiagnostics
    instance.lock.Unlock()

    retireDiagnosticsRoutedBy(closingLogger, routedDiagnostics)

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

/* keepLoggedMark: the mark is read at the outermost link, so an unmarked wrapper would file the failure a second time */
func keepLoggedMark(wrapper error, cause error) error {
    if true == exception.IsAlreadyLogged(cause) {
        return exception.MarkLogged(wrapper)
    }

    return wrapper
}
