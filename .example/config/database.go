package config

import (
    "time"

    "github.com/precision-soft/melody/.example/repository"
    melodyapplicationcontract "github.com/precision-soft/melody/application/contract"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodyexception "github.com/precision-soft/melody/exception"
    melodybunorm "github.com/precision-soft/melody/integrations/bunorm"
    melodymysql "github.com/precision-soft/melody/integrations/bunorm/mysql"
    melodykernelcontract "github.com/precision-soft/melody/kernel/contract"
    "github.com/uptrace/bun"
)

const (
    ServiceExampleDatabaseRegistry = "service.example.database.registry"
    ServiceExampleDatabase         = "service.example.database"
)

/* buildDatabase declares the connection without opening it. bunorm's registry validates the definitions here and dials on the first Manager call, which lands after the framework has registered its own services — so the provider finds the configuration and the logger it reads while connecting, and the retry backoff is reported through the real logger instead of the emergency one.

An unset host leaves the registry nil and the database unwired: no services, no routes, no dial. */
func (instance *Module) buildDatabase(kernelInstance melodykernelcontract.Kernel) {
    host := parameterValue(kernelInstance, ParameterDatabaseHost)
    if "" == host {
        return
    }

    provider := melodymysql.NewProvider(
        ParameterDatabaseHost,
        ParameterDatabasePort,
        ParameterDatabaseName,
        ParameterDatabaseUser,
        ParameterDatabasePassword,
    ).
        WithPoolConfig(melodymysql.NewPoolConfig(10, 2, 5*time.Minute, time.Minute)).
        WithRetryConfig(melodymysql.NewRetryConfig(10, time.Second, 5*time.Second, 2.0))

    registry, registryErr := melodybunorm.NewManagerRegistry(
        newConfigurationResolver(kernelInstance),
        melodybunorm.ProviderDefinition{
            Name:      "default",
            Provider:  provider,
            IsDefault: true,
        },
    )
    if nil != registryErr {
        melodyexception.Panic(melodyexception.FromError(registryErr))
    }

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
