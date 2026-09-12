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
    melodylogging "github.com/precision-soft/melody/v3/logging"
    bun "github.com/uptrace/bun"
)

const (
    /* serviceDatabaseRegistry is the container name of the bunorm manager registry the connection is declared in. The db:* command family resolves the connection through this name and nothing else, so the registry is what makes the operator's door and the application's door the same connection rather than two pools onto one database. It is also what the container closes at shutdown, and closing it is what closes the pool. */
    serviceDatabaseRegistry = "service-example-database-registry"

    /* serviceDatabase is the container name of the example's shared *bun.DB; the outbox store factory and the encrypt database factory resolve it at their first use instead of receiving the prebuilt handle at composition-root time. It is the registry's default manager, published under a name of its own so a consumer that only wants a handle does not have to know the registry exists. */
    serviceDatabase = "service-example-database"

    /* serviceArchiveDatabase is the container name of the example's SECOND handle, the postgres connection the reading archive is kept on. It is addressed by name alone, like the catalogue handle beside it, and it is resolved lazily: the provider below is what opens it, at the first resolution rather than at boot. */
    serviceArchiveDatabase = "service-example-archive-database"

    /* databaseManagerName names the manager the catalogue is declared under. The db:* commands are PINNED to it rather than taking the registry's default, because with a second definition present "the default" and "the catalogue" stop being the same question: an environment that wires the archive alone would otherwise let an unqualified db:migrate aim the catalogue's mysql DDL at postgres. */
    databaseManagerName = "default"

    /* databaseArchiveManagerName names the manager the archive is declared under, and the name is not free: the migrate module derives a context's manager from the context's own Name unless the context pins one, so this constant and the migration context's name are the same string by contract. */
    databaseArchiveManagerName = "archive"
)

/* dialIsInsecure reads a transport switch, and both providers read it through this one function: each negotiates a verified TLS handshake by default, and the development compose mysql and postgres both speak plain TCP, so the shipped .env arms the insecure dial explicitly for each — the decision is visible in configuration rather than buried in the wiring. The spelling is exact: any value but "true" keeps the verified handshake, because a credential-bearing dial downgrades only on an unambiguous instruction. */
func dialIsInsecure(insecureValue string) bool {
    return "true" == insecureValue
}

/* buildDatabase declares this example's connections in one bunorm manager registry and opens the catalogue's. The registry validates the definitions as it is built, and the handle the rest of the application holds is the catalogue manager's — the operator running db:migrate and the repository resolving at first request reach one pool rather than two.

   There are two definitions and two switches, and they are independent on purpose: the catalogue on mysql and the reading archive on postgres each follow their own empty-means-unwired key, so every combination boots — both live, either one alone, or neither. With neither host set the registry stays nil and the whole database surface is unwired: no services, no migration set, no dial.

   Only the catalogue is OPENED here. The archive's definition is declared and left closed, because opening it would be a second handshake paid by every process that never takes a reading — measured on this application's cheapest entry point, db:migrate --help, which already pays the catalogue's. Its handle is opened at the first resolution of the service that publishes it. */
func (instance *Module) buildDatabase() {
    definitionList := make([]melodybunorm.ProviderDefinition, 0, 2)

    if catalogDefinition, wired := instance.catalogProviderDefinition(); true == wired {
        definitionList = append(definitionList, catalogDefinition)
    }

    if archiveDefinition, wired := instance.archiveProviderDefinition(); true == wired {
        definitionList = append(definitionList, archiveDefinition)
        instance.archiveWired = true
    }

    if 0 == len(definitionList) {
        return
    }

    /* the emergency logger carries the registry's reporting: the framework's own logger does not exist yet while the modules are being wired, and the emergency one is what the framework itself writes through in that window. The passwords are not marked here the way the two frozen majors mark them through the registry: this major registers its parameters itself and marks MYSQL_PASSWORD and PGSQL_PASSWORD by name in RegisterParameters, so a second marking would say the same thing twice. */
    registry, registryErr := melodybunorm.NewManagerRegistry(
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

/* catalogWired answers whether the catalogue definition was declared, which is the question every consumer of the shared handle asks — the repositories, the outbox store, the encrypt command. It is read off the handle rather than off the host key because the handle is what those consumers need, and a definition that failed to open never becomes one. */
func (instance *Module) catalogWired() bool {
    return "" != instance.environmentValue(environmentKeyMysqlHost)
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

    /* retry the initial connection with backoff so the example survives a cold-start race against the database container — mysql often takes 20-30s to accept connections while the app boots in seconds, and buildDatabase panics on a hard failure. The provider only retries transient errors (connection refused), so a real misconfiguration still fails fast. */
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

/* archiveProviderDefinition declares the postgres connection the reading archive is kept on, or answers that this environment did not ask for one. It is not the registry's default and must never be: the default is what an unqualified consumer reaches, and "the database" of this application is its catalogue.

   The retry budget is the catalogue's, for the catalogue's reason — a cold-start race against a container that takes tens of seconds to accept connections while the application boots in one — and the provider retries only transient failures, so a real misconfiguration still fails fast. */
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
        melodypgsql.WithRetryConfig(melodypgsql.NewRetryConfig(10, time.Second, 5*time.Second, 2.0)),
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

/* registerDatabaseServices publishes the registry and the handle it opened, so the db:* command family can resolve the one and the lazy factories (outbox store, encrypt bulk command) the other; without a configured database neither service is registered, and a factory's first use reports the missing service instead of failing boot. */
func (instance *Module) registerDatabaseServices(registrar melodyapplicationcontract.ServiceRegistrar) {
    if nil == instance.databaseRegistry {
        return
    }

    registry := instance.databaseRegistry

    registrar.RegisterService(
        serviceDatabaseRegistry,
        func(resolver melodycontainercontract.Resolver) (*melodybunorm.ManagerRegistry, error) {
            /* the logger is RESOLVED rather than captured, and that is load-bearing twice over.

               It gives the registry the application's real journal. The registry is built during module wiring, where the framework's logger does not exist yet, so it is constructed on the emergency logger — and without this every later open, every retry warning and every terminal connection failure would keep bypassing the json journal for the life of the process, along with bun's own diagnostics, which the registry routes with it.

               It also writes the TEARDOWN EDGE. The container records a dependency at the moment one service resolves another, and closes dependents before their dependencies; a provider that only captured an already-built collaborator resolves nothing, so no edge existed and the order of these two closes was decided by creation order — which is not an ordering, only a coincidence. With the edge, the registry closes first: its pools are torn down and it hands bun's diagnostic channel back while the journal it routed to is still open. */
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

                return resolvedRegistry.Database(databaseArchiveManagerName)
            },
            melodycontainer.WithoutTypeRegistration(),
        )
    }

    if false == instance.catalogWired() {
        return
    }

    registrar.RegisterService(
        serviceDatabase,
        /* BOTH handles are kept off the type index, not just the second one, and that is the whole door rather than half of it. The refusal a second *bun.DB registration draws is the framework's and it is correct — a resolution by type between two handles of the same concrete type can only answer with whichever landed first, so "give me the database" has no answer in an application that holds two. Taking the second off the index would silence the refusal and leave the ambiguous question askable, answered by the catalogue purely because it was registered earlier. What has to stop existing is the question: a handle is asked for by naming the connection it opens. Measured before this was written — nothing in this example resolves *bun.DB by type: the three sites that want a handle (the catalog storage provider, the outbox store factory, the encrypt database factory) all resolve serviceDatabase BY NAME, and the generated wiring carries no reference to bun.DB at all, which is exactly why the handle lives in the unscanned persistence package. */
        func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
            /* the registry is RESOLVED here for the same reason the logger is resolved above, and it is the teardown half that matters: this name publishes the registry's own pool under a second identity, and the container closes anything carrying a Close, so without the edge the shared pool was torn down in creation order — before or after the registry that owns it, and before or after whatever still had queries to make, by coincidence rather than by rule. With the edge the handle closes ahead of its owner, and the whole chain — handle, registry, journal — unwinds in that order. */
            resolvedRegistry, registryErr := melodycontainer.FromResolver[*melodybunorm.ManagerRegistry](resolver, serviceDatabaseRegistry)
            if nil != registryErr {
                return nil, registryErr
            }

            return resolvedRegistry.Database(databaseManagerName)
        },
        melodycontainer.WithoutTypeRegistration(),
    )
}
