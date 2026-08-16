package config

import (
    "time"

    "github.com/precision-soft/melody/v2/.example/repository"
    melodyapplicationcontract "github.com/precision-soft/melody/v2/application/contract"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyexception "github.com/precision-soft/melody/v2/exception"
    melodybunorm "github.com/precision-soft/melody/integrations/bunorm/v2"
    melodymysql "github.com/precision-soft/melody/integrations/bunorm/mysql/v2"
    melodykernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    melodylogging "github.com/precision-soft/melody/v2/logging"
    "github.com/uptrace/bun"
)

const (
    ServiceExampleDatabaseRegistry = "service.example.database.registry"
    ServiceExampleDatabase         = "service.example.database"
)

/* buildDatabase declares the connection without opening it. bunorm's registry validates the definitions here and dials on the first Manager call, which lands after the framework has registered its own services.

This major's bunorm takes the connection values and a logger directly, rather than the parameter names v1 resolves through a container, so the values are read here and the emergency logger carries the retry reporting: the framework's own logger does not exist yet while the modules are being wired, and the emergency one is what the framework itself writes through in that window.

An unset host leaves the registry nil and the database unwired: no services, no routes, no dial. */
func (instance *Module) buildDatabase(kernelInstance melodykernelcontract.Kernel) {
    host := parameterValue(kernelInstance, ParameterDatabaseHost)
    if "" == host {
        return
    }

    provider := melodymysql.NewProvider().
        WithPoolConfig(melodymysql.NewPoolConfig(10, 2, 5*time.Minute, time.Minute)).
        WithRetryConfig(melodymysql.NewRetryConfig(10, time.Second, 5*time.Second, 2.0))

    registry, registryErr := melodybunorm.NewManagerRegistry(
        melodylogging.EmergencyLogger(),
        melodybunorm.ProviderDefinition{
            Name:     "default",
            Provider: provider,
            Params: melodybunorm.ConnectionParameters{
                Host:     host,
                Port:     parameterValue(kernelInstance, ParameterDatabasePort),
                Database: parameterValue(kernelInstance, ParameterDatabaseName),
                User:     parameterValue(kernelInstance, ParameterDatabaseUser),
                Password: parameterValue(kernelInstance, ParameterDatabasePassword),
            },
            IsDefault: true,
        },
    )
    if nil != registryErr {
        melodyexception.Panic(melodyexception.FromError(registryErr))
    }

    /* this major hands the provider the connection value rather than the parameter name it came from, so naming the credential key is the application's job. Measured, it changes nothing here: the password parameter reads `%env(default::MYSQL_PASSWORD)%`, and the framework already marks a parameter whose template reads a marked environment key — the call is what an application whose credential does NOT come from one would need, and the example carries it because it is the wiring the integration documents. */
    registry.MarkSecretParameters(kernelInstance.Config(), ParameterDatabasePassword)

    instance.databaseRegistry = registry
}

/* databaseServiceName names the connection when the environment gave the example one, and answers with the empty string when it did not. The repositories read that answer to decide which of their two implementations they are, so the decision is made once, here, by the code that knows whether the dial was even attempted. */
func (instance *Module) databaseServiceName() string {
    if nil == instance.databaseRegistry {
        return ""
    }

    return ServiceExampleDatabase
}

func (instance *Module) registerDatabaseServices(registrar melodyapplicationcontract.ServiceRegistrar) {
    if nil == instance.databaseRegistry {
        return
    }

    registry := instance.databaseRegistry

    /* the registry is what the container closes at shutdown, and closing it is what closes the pool */
    registrar.RegisterService(
        ServiceExampleDatabaseRegistry,
        func(resolver melodycontainercontract.Resolver) (*melodybunorm.ManagerRegistry, error) {
            return registry, nil
        },
    )

    registrar.RegisterService(
        ServiceExampleDatabase,
        func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
            return registry.DefaultDatabase()
        },
    )

    registrar.RegisterService(
        repository.ServiceCatalogJournalRepository,
        repository.CatalogJournalRepositoryProvider(ServiceExampleDatabase),
    )
}
