package bunorm

import (
    "sync"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

type ManagerRegistry struct {
    logger loggingcontract.Logger

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

func NewManagerRegistry(logger loggingcontract.Logger, providerDefinitions ...ProviderDefinition) (*ManagerRegistry, error) {
    if nil == logger {
        return nil, ErrLoggerIsRequired
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

        if nil == providerDefinition.Provider {
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

    return &ManagerRegistry{
        logger:                        logger,
        providerDefinitionByName:      providerDefinitionByName,
        defaultProviderDefinitionName: defaultProviderDefinitionName,
        managers:                      make(map[string]*Manager),
        pendingOpenByName:             make(map[string]*managerOpen),
        migrationDatabases:            make(map[string]*bun.DB),
    }, nil
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
        instance.lock.Unlock()

        return nil, false, ErrProviderDefinitionNotFound
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

    database, openErr := migrationProvider.OpenForMigration(providerDefinition.Params, instance.logger)
    if nil != openErr {
        return nil, false, openErr
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

func (instance *ManagerRegistry) Manager(name string) (*Manager, error) {
    if "" == name {
        return nil, ErrProviderDefinitionNameIsRequired
    }

    instance.lock.Lock()

    if manager, exists := instance.managers[name]; true == exists {
        instance.lock.Unlock()

        return manager, nil
    }

    providerDefinition, exists := instance.providerDefinitionByName[name]
    if false == exists {
        instance.lock.Unlock()

        return nil, ErrProviderDefinitionNotFound
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

        pendingOpen.openError = exception.NewError(
            "bunorm manager provider panicked while opening",
            map[string]any{"name": name},
            nil,
        )
        close(pendingOpen.done)

        if nil != recovered {
            panic(recovered)
        }
    }()
    database, openErr := providerDefinition.Provider.Open(providerDefinition.Params, instance.logger)

    /* @important the publish runs in a closure with a deferred unlock: it calls into the freshly opened database, and a panic there would otherwise unwind with the lock held, whereupon the recovery defer above re-acquires the same non-reentrant mutex and wedges the whole registry with no waiter ever released */
    func() {
        instance.lock.Lock()
        defer instance.lock.Unlock()

        delete(instance.pendingOpenByName, name)

        if nil != openErr {
            pendingOpen.openError = openErr

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

    for _, manager := range instance.managers {
        if nil == manager {
            continue
        }

        managerCloseErr := manager.Close()
        if nil == closeErr && nil != managerCloseErr {
            closeErr = managerCloseErr
        }
    }

    for _, migrationDatabase := range instance.migrationDatabases {
        if nil == migrationDatabase {
            continue
        }

        migrationDatabaseCloseErr := migrationDatabase.Close()
        if nil == closeErr && nil != migrationDatabaseCloseErr {
            closeErr = migrationDatabaseCloseErr
        }
    }

    return closeErr
}
