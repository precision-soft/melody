package config

import (
    "time"

    melodymysql "github.com/precision-soft/melody/integrations/bunorm/mysql/v3"
    melodybunorm "github.com/precision-soft/melody/integrations/bunorm/v3"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    bun "github.com/uptrace/bun"
)

const (
    /* serviceDatabaseRegistry is the container name of the bunorm manager registry the connection is
    declared in. The db:* command family resolves the connection through this name and nothing else, so the
    registry is what makes the operator's door and the application's door the same connection rather than two
    pools onto one database. It is also what the container closes at shutdown, and closing it is what closes
    the pool. */
    serviceDatabaseRegistry = "service-example-database-registry"

    /* serviceDatabase is the container name of the example's shared *bun.DB; the outbox store factory and the
    encrypt database factory resolve it at their first use instead of receiving the prebuilt handle at
    composition-root time. It is the registry's default manager, published under a name of its own so a
    consumer that only wants a handle does not have to know the registry exists. */
    serviceDatabase = "service-example-database"

    /* databaseManagerName names the one manager this example declares. The db:* commands take it as their
    default because the definition is the registry's default, so omitting --manager can only ever reach this
    connection. */
    databaseManagerName = "default"
)

/*
dialIsInsecure reads a transport switch. The provider negotiates a verified
TLS handshake by default; the development compose mysql speaks plain TCP, so
the shipped .env arms the insecure dial explicitly — the decision is visible
in configuration rather than buried in the wiring. The spelling is exact: any
value but "true" keeps the verified handshake, because a credential-bearing
dial downgrades only on an unambiguous instruction.
*/
func dialIsInsecure(insecureValue string) bool {
    return "true" == insecureValue
}

/*
buildDatabase declares the connection in a bunorm manager registry and opens the registry's default
manager. The registry validates the definitions as it is built, and the handle the rest of the application
holds is that manager's — the operator running db:migrate and the repository resolving at first request
reach one pool rather than two.

An unset host leaves both the registry and the handle nil, and the whole database surface unwired: no
services, no migration set, no dial.
*/
func (instance *Module) buildDatabase() {
    host := instance.environmentValue(environmentKeyMysqlHost)
    if "" == host {
        return
    }

    port := instance.environmentValue(environmentKeyMysqlPort)
    if "" == port {
        port = "3306"
    }

    /* retry the initial connection with backoff so the example survives a cold-start race against the
       database container — mysql often takes 20-30s to accept connections while the app boots in seconds,
       and buildDatabase panics on a hard failure. The provider only retries transient errors (connection
       refused), so a real misconfiguration still fails fast. */
    optionList := []melodymysql.ProviderOption{
        melodymysql.WithRetryConfig(melodymysql.NewRetryConfig(10, time.Second, 5*time.Second, 2.0)),
    }
    if true == dialIsInsecure(instance.environmentValue(environmentKeyMysqlInsecure)) {
        optionList = append(optionList, melodymysql.WithInsecure(true))
    }

    /* the emergency logger carries the registry's reporting: the framework's own logger does not exist yet
       while the modules are being wired, and the emergency one is what the framework itself writes through in
       that window. The password is not marked here the way the two frozen majors mark it through the
       registry: this major registers its parameters itself and marks MYSQL_PASSWORD by name in
       RegisterParameters, so a second marking would say the same thing twice. */
    registry, registryErr := melodybunorm.NewManagerRegistry(
        melodylogging.EmergencyLogger(),
        melodybunorm.ProviderDefinition{
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
        },
    )
    if nil != registryErr {
        exception.Panic(exception.FromError(registryErr))
    }

    database, databaseErr := registry.DefaultDatabase()
    if nil != databaseErr {
        exception.Panic(exception.FromError(databaseErr))
    }

    instance.databaseRegistry = registry
    instance.database = database
}

/* registerDatabaseServices publishes the registry and the handle it opened, so the db:* command family can
resolve the one and the lazy factories (outbox store, encrypt bulk command) the other; without a configured
database neither service is registered, and a factory's first use reports the missing service instead of
failing boot. */
func (instance *Module) registerDatabaseServices(registrar melodyapplicationcontract.ServiceRegistrar) {
    if nil == instance.databaseRegistry {
        return
    }

    registry := instance.databaseRegistry

    registrar.RegisterService(
        serviceDatabaseRegistry,
        func(resolver melodycontainercontract.Resolver) (*melodybunorm.ManagerRegistry, error) {
            return registry, nil
        },
    )

    registrar.RegisterService(
        serviceDatabase,
        func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
            return registry.DefaultDatabase()
        },
    )
}
