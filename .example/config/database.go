package config

import (
    "time"

    "github.com/precision-soft/melody/.example/repository"
    melodyapplicationcontract "github.com/precision-soft/melody/application/contract"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodyexception "github.com/precision-soft/melody/exception"
    melodybunorm "github.com/precision-soft/melody/integrations/bunorm"
    melodymysql "github.com/precision-soft/melody/integrations/bunorm/mysql"
    melodypgsql "github.com/precision-soft/melody/integrations/bunorm/pgsql"
    melodykernelcontract "github.com/precision-soft/melody/kernel/contract"
    "github.com/uptrace/bun"
)

const (
    ServiceExampleDatabaseRegistry = "service.example.database.registry"
    ServiceExampleDatabase         = "service.example.database"
    ServiceExampleJournalDatabase  = "service.example.journal.database"
)

/* the registry names carry the function of each connection, not its engine; the journal name is what the db:journal:* command family is pinned to */
const (
    databaseProviderNameDefault = "default"
    databaseProviderNameJournal = "journal"
)

/* databaseWiring is the decision of which connections the environment armed. The two switches are independent on purpose — the catalog on mysql and the journal on postgres each follow their own empty-means-unwired key, so every combination boots: both live, either one alone, or none at all. */
type databaseWiring struct {
    catalog bool
    journal bool
}

func databaseWiringFromHosts(catalogHost string, journalHost string) databaseWiring {
    return databaseWiring{
        catalog: "" != catalogHost,
        journal: "" != journalHost,
    }
}

/* dialIsInsecure reads a transport switch. Both providers negotiate a verified TLS handshake by default, and only the exact value "true" arms the plain dial the development compose databases need, so a credential-bearing dial downgrades only on an unambiguous instruction. */
func dialIsInsecure(insecureValue string) bool {
    return "true" == insecureValue
}

/* buildDatabase declares the connections without opening them: bunorm's registry validates the definitions here and dials each on the first Manager call, after the framework's own services exist, so the providers find the configuration and the logger. An unset host leaves its definition out; with both unset the registry stays nil and nothing is wired. */
func (instance *Module) buildDatabase(kernelInstance melodykernelcontract.Kernel) {
    wiring := databaseWiringFromHosts(
        parameterValue(kernelInstance, ParameterDatabaseHost),
        parameterValue(kernelInstance, ParameterJournalDatabaseHost),
    )
    instance.databaseWiring = wiring

    definitionList := make([]melodybunorm.ProviderDefinition, 0, 2)

    if true == wiring.catalog {
        catalogOptionList := []melodymysql.ProviderOption{}
        if true == dialIsInsecure(parameterValue(kernelInstance, ParameterDatabaseInsecure)) {
            catalogOptionList = append(catalogOptionList, melodymysql.WithInsecure(true))
        }

        provider := melodymysql.NewProvider(
            ParameterDatabaseHost,
            ParameterDatabasePort,
            ParameterDatabaseName,
            ParameterDatabaseUser,
            ParameterDatabasePassword,
            catalogOptionList...,
        ).
            WithPoolConfig(melodymysql.NewPoolConfig(10, 2, 5*time.Minute, time.Minute)).
            WithRetryConfig(melodymysql.NewRetryConfig(10, time.Second, 5*time.Second, 2.0))

        definitionList = append(definitionList, melodybunorm.ProviderDefinition{
            Name:      databaseProviderNameDefault,
            Provider:  provider,
            IsDefault: true,
        })
    }

    if true == wiring.journal {
        journalOptionList := []melodypgsql.ProviderOption{}
        if true == dialIsInsecure(parameterValue(kernelInstance, ParameterJournalDatabaseInsecure)) {
            journalOptionList = append(journalOptionList, melodypgsql.WithInsecure(true))
        }

        journalProvider := melodypgsql.NewProvider(
            ParameterJournalDatabaseHost,
            ParameterJournalDatabasePort,
            ParameterJournalDatabaseName,
            ParameterJournalDatabaseUser,
            ParameterJournalDatabasePassword,
            journalOptionList...,
        ).
            WithPoolConfig(melodypgsql.NewPoolConfig(10, 2, 5*time.Minute, time.Minute)).
            WithRetryConfig(melodypgsql.NewRetryConfig(10, time.Second, 5*time.Second, 2.0))

        definitionList = append(definitionList, melodybunorm.ProviderDefinition{
            Name:     databaseProviderNameJournal,
            Provider: journalProvider,
        })
    }

    if 0 == len(definitionList) {
        return
    }

    registry, registryErr := melodybunorm.NewManagerRegistry(
        newConfigurationResolver(kernelInstance),
        definitionList...,
    )
    if nil != registryErr {
        melodyexception.Panic(melodyexception.FromError(registryErr))
    }

    instance.databaseRegistry = registry
}

/* databaseServiceName names the catalog connection when the environment configured one, and answers the empty string otherwise; the catalog repositories read it to pick their implementation. The journal is a switch of its own. */
func (instance *Module) databaseServiceName() string {
    if false == instance.databaseWiring.catalog {
        return ""
    }

    return ServiceExampleDatabase
}

/* journalDatabaseServiceName is the same answer for the journal connection: the catalog can be wired without it, and the reset command then leaves that set alone. */
func (instance *Module) journalDatabaseServiceName() string {
    if false == instance.databaseWiring.journal {
        return ""
    }

    return ServiceExampleJournalDatabase
}

/* databaseLocation spells the catalog connection as host:port/schema from the parameters the provider reads, without the credentials, since it is printed. */
func (instance *Module) databaseLocation(kernelInstance melodykernelcontract.Kernel) string {
    return databaseLocationOf(
        parameterValue(kernelInstance, ParameterDatabaseHost),
        parameterValue(kernelInstance, ParameterDatabasePort),
        parameterValue(kernelInstance, ParameterDatabaseName),
    )
}

/* journalDatabaseLocation is the same spelling for the journal connection. */
func (instance *Module) journalDatabaseLocation(kernelInstance melodykernelcontract.Kernel) string {
    return databaseLocationOf(
        parameterValue(kernelInstance, ParameterJournalDatabaseHost),
        parameterValue(kernelInstance, ParameterJournalDatabasePort),
        parameterValue(kernelInstance, ParameterJournalDatabaseName),
    )
}

func databaseLocationOf(host string, port string, database string) string {
    return host + ":" + port + "/" + database
}

func (instance *Module) registerDatabaseServices(registrar melodyapplicationcontract.ServiceRegistrar) {
    if nil == instance.databaseRegistry {
        return
    }

    registry := instance.databaseRegistry

    /* the registry is what the container closes at shutdown, and closing it is what closes every pool it opened */
    registrar.RegisterService(
        ServiceExampleDatabaseRegistry,
        func(resolver melodycontainercontract.Resolver) (*melodybunorm.ManagerRegistry, error) {
            return registry, nil
        },
    )

    if true == instance.databaseWiring.catalog {
        registrar.RegisterService(
            ServiceExampleDatabase,
            func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
                return registry.Database(databaseProviderNameDefault)
            },
        )
    }

    if true == instance.databaseWiring.journal {
        /* the journal handle is addressed by name alone: the catalog registration already claims the *bun.DB type index, and "the" database of the application is the catalog */
        registrar.RegisterService(
            ServiceExampleJournalDatabase,
            func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
                return registry.Database(databaseProviderNameJournal)
            },
            melodycontainer.WithoutTypeRegistration(),
        )

        registrar.RegisterService(
            repository.ServiceCatalogJournalRepository,
            repository.CatalogJournalRepositoryProvider(ServiceExampleJournalDatabase),
        )
    }
}
