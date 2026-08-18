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

/* the registry names carry the function of each connection, not its engine: the catalog keeps the name the single-database wiring always had, and the journal name is what the db:journal:* command family is pinned to */
const (
    databaseProviderNameDefault = "default"
    databaseProviderNameJournal = "journal"
)

/*
databaseWiring is the decision of which connections the environment armed. The
two switches are independent on purpose — the catalog on mysql and the journal
on postgres each follow their own empty-means-unwired key, so every
combination boots: both live, either one alone, or none at all.
*/
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

/*
dialIsInsecure reads a transport switch. Both providers negotiate a verified
TLS handshake by default; the development compose mysql and postgres both
speak plain TCP, so the shipped .env arms the insecure dial explicitly for
each — the decision is visible in configuration rather than buried in the
wiring. The spelling is exact: any value but "true" keeps the verified
handshake, because a credential-bearing dial downgrades only on an
unambiguous instruction.
*/
func dialIsInsecure(insecureValue string) bool {
    return "true" == insecureValue
}

/* buildDatabase declares the connections without opening them. bunorm's registry validates the definitions here and dials each one on the first Manager call, which lands after the framework has registered its own services — so the providers find the configuration and the logger they read while connecting, and the retry backoff is reported through the real logger instead of the emergency one.

An unset host leaves its definition out; with both hosts unset the registry stays nil and nothing is wired: no services, no dial. */
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

/* databaseServiceName names the catalog connection when the environment gave the example one, and answers with the empty string when it did not. The catalog repositories read that answer to decide which of their two implementations they are, so the decision is made once, here, by the code that knows whether the dial was even attempted. The journal is not part of this answer: its presence is a switch of its own. */
func (instance *Module) databaseServiceName() string {
    if false == instance.databaseWiring.catalog {
        return ""
    }

    return ServiceExampleDatabase
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
        /* the journal handle is addressed by name alone: the catalog registration above already claims the *bun.DB type index, and a second claim is a boot collision — which is correct, because "the" database of the application is the catalog, and the journal is a connection with a function of its own */
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
