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

    serviceDatabaseRegistry = "service-example-database-registry"

    serviceDatabase = "service-example-database"

    serviceArchiveDatabase = "service-example-archive-database"

    databaseManagerName = "default"

    databaseArchiveManagerName = "archive"
)

func dialIsInsecure(insecureValue string) bool {
    return "true" == insecureValue
}

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

func (instance *Module) catalogWired() bool {
    return "" != instance.environmentValue(environmentKeyMysqlHost)
}

func databaseLocationOf(parameters melodybunorm.ConnectionParameters) string {
    return parameters.Host + ":" + parameters.Port + "/" + parameters.Database
}

func (instance *Module) catalogProviderDefinition() (melodybunorm.ProviderDefinition, bool) {
    host := instance.environmentValue(environmentKeyMysqlHost)
    if "" == host {
        return melodybunorm.ProviderDefinition{}, false
    }

    port := instance.environmentValue(environmentKeyMysqlPort)
    if "" == port {
        port = "3306"
    }

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

const (
    archiveOpenAttempts     = 3
    archiveOpenFirstBackoff = 250 * time.Millisecond
    archiveOpenMaxBackoff   = time.Second
)

func (instance *Module) registerDatabaseServices(registrar melodyapplicationcontract.ServiceRegistrar) {
    if nil == instance.databaseRegistry {
        return
    }

    registry := instance.databaseRegistry

    registrar.RegisterService(
        serviceDatabaseRegistry,
        func(resolver melodycontainercontract.Resolver) (*melodybunorm.ManagerRegistry, error) {

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

        func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {

            resolvedRegistry, registryErr := melodycontainer.FromResolver[*melodybunorm.ManagerRegistry](resolver, serviceDatabaseRegistry)
            if nil != registryErr {
                return nil, registryErr
            }

            return databaseOpenedBy(resolvedRegistry, databaseManagerName, instance.catalogLocation)
        },
        melodycontainer.WithoutTypeRegistration(),
    )
}

func databaseOpenedBy(registry *melodybunorm.ManagerRegistry, managerName string, location string) (*bun.DB, error) {
    database, openErr := registry.Database(managerName)
    if nil != openErr {
        return nil, exception.NewError(
            "the "+managerName+" database at "+location+" could not be opened",
            exceptioncontract.Context{"manager": managerName, "location": location},
            openErr,
        )
    }

    return database, nil
}
