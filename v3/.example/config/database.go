package config

import (
    "time"

    melodymysql "github.com/precision-soft/melody/integrations/bunorm/mysql/v3"
    melodypgsql "github.com/precision-soft/melody/integrations/bunorm/pgsql/v3"
    melodybunorm "github.com/precision-soft/melody/integrations/bunorm/v3"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    bun "github.com/uptrace/bun"
)

const (
    /* serviceDatabaseRegistry is the container name of the bunorm manager registry. The db:* family resolves the connection through it alone, so the operator and the application share one pool, and closing it at shutdown closes the pool. */
    serviceDatabaseRegistry = "service-example-database-registry"

    /* serviceDatabase is the container name of the shared catalogue *bun.DB, the registry's default manager, resolved by name at first use by the outbox store and the encrypt database factories. */
    serviceDatabase = "service-example-database"

    /* serviceArchiveDatabase is the container name of the postgres handle the reading archive is kept on, addressed by name and opened by its provider at the first resolution rather than at boot. */
    serviceArchiveDatabase = "service-example-archive-database"

    /* databaseManagerName names the catalogue's manager. The db:* commands are pinned to it rather than to the registry's default, because in an environment that wires the archive alone an unqualified db:migrate would otherwise aim the catalogue's mysql DDL at postgres. */
    databaseManagerName = "default"

    /* databaseArchiveManagerName names the manager the archive is declared under, and the name is not free: the migrate module derives a context's manager from the context's own Name unless the context pins one, so this constant and the migration context's name are the same string by contract. */
    databaseArchiveManagerName = "archive"
)

/* dialIsInsecure reads the transport switch both providers share. Each dials verified TLS by default and the shipped .env arms the insecure dial for the plain-TCP compose databases; any value but the exact "true" keeps the verified handshake, so a credential-bearing dial downgrades only on an unambiguous instruction. */
func dialIsInsecure(insecureValue string) bool {
    return "true" == insecureValue
}

/* buildDatabase declares this example's connections in one bunorm manager registry and opens the catalogue's, so db:migrate and the repositories reach one pool. The catalogue on mysql and the archive on postgres follow their own empty-means-unwired keys, so every combination boots, and with neither the registry stays nil and nothing is dialed. Only the catalogue is opened here: the archive is opened at the first resolution of its service, so a process that never takes a reading pays no second handshake. */
func (instance *Module) buildDatabase() {
    definitionList := make([]melodybunorm.ProviderDefinition, 0, 2)

    if catalogDefinition, wired := instance.catalogProviderDefinition(); true == wired {
        definitionList = append(definitionList, catalogDefinition)
        instance.catalogLocation = databaseLocationOf(catalogDefinition.Params)
    }

    if archiveDefinition, wired := instance.archiveProviderDefinition(); true == wired {
        definitionList = append(definitionList, archiveDefinition)
        instance.archiveWired = true
        instance.archiveLocation = databaseLocationOf(archiveDefinition.Params)
    }

    if 0 == len(definitionList) {
        return
    }

    /* the emergency logger carries the registry's reporting because the framework's logger does not exist yet while the modules are wired; the passwords are marked by name in RegisterParameters, not here. */
    registry, registryErr := melodybunorm.NewManagerRegistryWithContext(
        instance.processContext,
        melodylogging.EmergencyLogger(),
        definitionList...,
    )
    if nil != registryErr {
        exception.Panic(exception.FromError(registryErr))
    }

    instance.databaseRegistry = registry

    if false == instance.catalogWired() {
        return
    }

    database, databaseErr := registry.Database(databaseManagerName)
    if nil != databaseErr {
        exception.Panic(exception.FromError(databaseErr))
    }

    instance.database = database
}

/* catalogWired answers whether the catalogue definition was declared, read off the handle because a definition that failed to open never becomes one. */
func (instance *Module) catalogWired() bool {
    return "" != instance.environmentValue(environmentKeyMysqlHost)
}

/* databaseLocationOf spells a declared connection as host:port/schema, the one line that separates a reset of this example's volume from a reset of whatever the host happens to point at; the credentials stay out of it, because it is printed. */
func databaseLocationOf(parameters melodybunorm.ConnectionParameters) string {
    return parameters.Host + ":" + parameters.Port + "/" + parameters.Database
}

/* catalogProviderDefinition declares the mysql connection, or answers that this environment did not ask for one. */
func (instance *Module) catalogProviderDefinition() (melodybunorm.ProviderDefinition, bool) {
    host := instance.environmentValue(environmentKeyMysqlHost)
    if "" == host {
        return melodybunorm.ProviderDefinition{}, false
    }

    port := instance.environmentValue(environmentKeyMysqlPort)
    if "" == port {
        port = "3306"
    }

    /* retry the initial connection with backoff so a cold start against the database container survives; only transient errors (connection refused) retry, so a real misconfiguration still fails fast. */
    optionList := []melodymysql.ProviderOption{
        melodymysql.WithRetryConfig(melodymysql.NewRetryConfig(10, time.Second, 5*time.Second, 2.0)),
    }
    if true == dialIsInsecure(instance.environmentValue(environmentKeyMysqlInsecure)) {
        optionList = append(optionList, melodymysql.WithInsecure(true))
    }

    return melodybunorm.ProviderDefinition{
        Name:     databaseManagerName,
        Provider: melodymysql.NewProvider(optionList...),
        Params: melodybunorm.ConnectionParameters{
            Host:     host,
            Port:     port,
            Database: instance.environmentValue(environmentKeyMysqlDatabase),
            User:     instance.environmentValue(environmentKeyMysqlUser),
            Password: instance.environmentValue(environmentKeyMysqlPassword),
        },
        IsDefault: true,
    }, true
}

/* archiveProviderDefinition declares the postgres connection the reading archive is kept on, or answers that this environment did not ask for one. It is never the registry's default, because an unqualified consumer means the catalogue. Its retry budget is request-sized, since the archive is opened lazily by a request or a command and never at boot; only transient failures retry. */
func (instance *Module) archiveProviderDefinition() (melodybunorm.ProviderDefinition, bool) {
    host := instance.environmentValue(environmentKeyPgsqlHost)
    if "" == host {
        return melodybunorm.ProviderDefinition{}, false
    }

    port := instance.environmentValue(environmentKeyPgsqlPort)
    if "" == port {
        port = "5432"
    }

    optionList := []melodypgsql.ProviderOption{
        melodypgsql.WithRetryConfig(melodypgsql.NewRetryConfig(archiveOpenAttempts, archiveOpenFirstBackoff, archiveOpenMaxBackoff, 2.0)),
    }
    if true == dialIsInsecure(instance.environmentValue(environmentKeyPgsqlInsecure)) {
        optionList = append(optionList, melodypgsql.WithInsecure(true))
    }

    return melodybunorm.ProviderDefinition{
        Name:     databaseArchiveManagerName,
        Provider: melodypgsql.NewProvider(optionList...),
        Params: melodybunorm.ConnectionParameters{
            Host:     host,
            Port:     port,
            Database: instance.environmentValue(environmentKeyPgsqlDatabase),
            User:     instance.environmentValue(environmentKeyPgsqlUser),
            Password: instance.environmentValue(environmentKeyPgsqlPassword),
        },
    }, true
}

/* the archive's open budget: three attempts, 250 ms then 500 ms apart, the size of a request, which is where the open is paid */
const (
    archiveOpenAttempts     = 3
    archiveOpenFirstBackoff = 250 * time.Millisecond
    archiveOpenMaxBackoff   = time.Second
)

/* registerDatabaseServices publishes the registry and the catalogue handle; without a configured database neither is registered, and a factory's first use reports the missing service instead of failing boot. */
func (instance *Module) registerDatabaseServices(registrar melodyapplicationcontract.ServiceRegistrar) {
    if nil == instance.databaseRegistry {
        return
    }

    registry := instance.databaseRegistry

    registrar.RegisterService(
        serviceDatabaseRegistry,
        func(resolver melodycontainercontract.Resolver) (*melodybunorm.ManagerRegistry, error) {
            /* the logger is resolved rather than captured, for two reasons: the registry, built on the emergency logger during wiring, gets the application's journal for its opens, its retries and bun's diagnostics; and the resolution writes the teardown edge, so the registry closes and hands bun's diagnostic channel back while the journal is still open. */
            logger, loggerErr := melodylogging.LoggerFromResolver(resolver)
            if nil != loggerErr {
                return nil, loggerErr
            }

            if setLoggerErr := registry.SetLogger(logger); nil != setLoggerErr {
                return nil, setLoggerErr
            }

            return registry, nil
        },
    )

    if true == instance.archiveWired {
        registrar.RegisterService(
            serviceArchiveDatabase,
            func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
                /* the registry is RESOLVED for the teardown edge, exactly as the catalogue handle below resolves it, and the OPEN happens here rather than in buildDatabase: this provider runs at the first resolution, so a process that never reaches the archive never dials postgres. */
                resolvedRegistry, registryErr := melodycontainer.FromResolver[*melodybunorm.ManagerRegistry](resolver, serviceDatabaseRegistry)
                if nil != registryErr {
                    return nil, registryErr
                }

                return databaseOpenedBy(resolvedRegistry, databaseArchiveManagerName, instance.archiveLocation)
            },
            melodycontainer.WithoutTypeRegistration(),
        )
    }

    if false == instance.catalogWired() {
        return
    }

    registrar.RegisterService(
        serviceDatabase,
        /* both handles are kept off the type index: a resolution by type between two handles of one concrete type can only answer with whichever registered first, so a handle is asked for by naming its connection. Every consumer in this example resolves serviceDatabase by name, and the generated wiring never references bun.DB, which is why the handle lives in the unscanned persistence package. */
        func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
            /* the registry is resolved here for the teardown edge: this name publishes the registry's pool under a second identity and the container closes anything carrying a Close, so the edge makes the handle close ahead of its owner, in the order handle, registry, journal. */
            resolvedRegistry, registryErr := melodycontainer.FromResolver[*melodybunorm.ManagerRegistry](resolver, serviceDatabaseRegistry)
            if nil != registryErr {
                return nil, registryErr
            }

            return databaseOpenedBy(resolvedRegistry, databaseManagerName, instance.catalogLocation)
        },
        melodycontainer.WithoutTypeRegistration(),
    )
}

/* databaseFunctionOf spells a manager by the function it serves; the catalogue's manager is bun's default and reads as "default" on the console. */
func databaseFunctionOf(managerName string) string {
    if databaseManagerName == managerName {
        return "catalogue"
    }

    return managerName
}

/* databaseOpenedBy opens one manager's handle and names it when the open refuses, since the registry's refusal reads the same for either database; the registry's own exception stays the cause, so errors.Is still reaches its sentinels. */
func databaseOpenedBy(registry *melodybunorm.ManagerRegistry, managerName string, location string) (*bun.DB, error) {
    database, openErr := registry.Database(managerName)
    if nil != openErr {
        return nil, exception.NewError(
            "the "+databaseFunctionOf(managerName)+" database at "+location+" could not be opened",
            exceptioncontract.Context{"manager": managerName, "location": location},
            openErr,
        )
    }

    return database, nil
}
