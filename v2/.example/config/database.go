package config

import (
    "time"

    melodymysql "github.com/precision-soft/melody/integrations/bunorm/mysql/v2"
    melodybunorm "github.com/precision-soft/melody/integrations/bunorm/v2"
    "github.com/precision-soft/melody/v2/.example/repository"
    melodyapplicationcontract "github.com/precision-soft/melody/v2/application/contract"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyexception "github.com/precision-soft/melody/v2/exception"
    melodykernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    melodylogging "github.com/precision-soft/melody/v2/logging"
    "github.com/uptrace/bun"
)

const (
    ServiceExampleDatabaseRegistry = "service.example.database.registry"
    ServiceExampleDatabase         = "service.example.database"
)

/* dialIsInsecure reads a transport switch. The provider negotiates a verified TLS handshake by default, and only the exact value "true" arms the plain dial the development compose mysql needs, so a credential-bearing dial downgrades only on an unambiguous instruction. */
func dialIsInsecure(insecureValue string) bool {
    return "true" == insecureValue
}

/* buildDatabase declares the connection without opening it: bunorm's registry validates the definition here and dials on the first Manager call, after the framework's own services exist. This major's bunorm takes the connection values and a logger directly, so the values are read here and the emergency logger carries the retry reporting, since the framework's logger does not exist yet while the modules are wired. An unset host leaves the registry nil and the database unwired. */
func (instance *Module) buildDatabase(kernelInstance melodykernelcontract.Kernel) {
    host := parameterValue(kernelInstance, ParameterDatabaseHost)
    if "" == host {
        return
    }

    optionList := []melodymysql.ProviderOption{}
    if true == dialIsInsecure(parameterValue(kernelInstance, ParameterDatabaseInsecure)) {
        optionList = append(optionList, melodymysql.WithInsecure(true))
    }

    provider := melodymysql.NewProvider(optionList...).
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

    /* this major hands the provider the connection value rather than the parameter name, so naming the credential key is the application's job; the password here already reads a marked environment key, and the call is the wiring the integration documents for a credential that does not */
    registry.MarkSecretParameters(kernelInstance.Config(), ParameterDatabasePassword)

    instance.databaseRegistry = registry
}

/* databaseServiceName names the connection when the environment configured one, and answers the empty string otherwise; the repositories read it to pick their implementation. */
func (instance *Module) databaseServiceName() string {
    if nil == instance.databaseRegistry {
        return ""
    }

    return ServiceExampleDatabase
}

/* databaseLocation spells the connection as host:port/schema from the parameters the provider reads, without the credentials, since it is printed. */
func (instance *Module) databaseLocation(kernelInstance melodykernelcontract.Kernel) string {
    return parameterValue(kernelInstance, ParameterDatabaseHost) +
        ":" + parameterValue(kernelInstance, ParameterDatabasePort) +
        "/" + parameterValue(kernelInstance, ParameterDatabaseName)
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
