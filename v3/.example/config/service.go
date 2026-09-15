package config

import (
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    "github.com/precision-soft/melody/v3/.example/cache"
    "github.com/precision-soft/melody/v3/.example/generated"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/subscriber"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodymailer "github.com/precision-soft/melody/v3/mailer"
    melodymailercontract "github.com/precision-soft/melody/v3/mailer/contract"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    melodymessagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    melodyopenapi "github.com/precision-soft/melody/v3/openapi"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
    melodytranslation "github.com/precision-soft/melody/v3/translation"
    melodytranslationcontract "github.com/precision-soft/melody/v3/translation/contract"
    bun "github.com/uptrace/bun"
)

func (instance *Module) RegisterServices(registrar melodyapplicationcontract.ServiceRegistrar) {

    if nil != instance.database {
        instance.registerOutboxTransportService(registrar)
    }

    instance.registerCatalogStorageService(registrar)
    instance.registerArchiveStorageService(registrar)

    instance.registerRatesHttpClientService(registrar)
    instance.registerReportExportHttpClientService(registrar)

    serverSentEventHub := instance.serverSentEventHub

    registrar.RegisterService(
        subscriber.ServiceCatalogNotificationHub,
        func(resolver melodycontainercontract.Resolver) (*melodyhttp.ServerSentEventHub, error) {

            logger, loggerErr := melodylogging.LoggerFromResolver(resolver)
            if nil != loggerErr {
                return nil, loggerErr
            }

            serverSentEventHub.SetLogger(logger)

            return serverSentEventHub, nil
        },
    )

    if nil == instance.redisClient {
        opaqueTokenStore := instance.opaqueTokenStore

        registrar.RegisterService(
            melodyrueidis.ServiceTokenStore,
            func(resolver melodycontainercontract.Resolver) (melodysecuritycontract.RevocableTokenStore, error) {
                return opaqueTokenStore, nil
            },
        )
    }

    registrar.RegisterService(
        melodycache.ServiceCacheSerializer,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Serializer, error) {
            return cache.NewGobSerializer(), nil
        },
    )

    registrar.RegisterService(
        melodymessagebus.ServiceBus,
        func(resolver melodycontainercontract.Resolver) (melodymessagebuscontract.Bus, error) {
            return instance.messageBusDispatch, nil
        },
    )

    registrar.RegisterService(
        melodymessagebus.ServiceConsumeBus,
        func(resolver melodycontainercontract.Resolver) (melodymessagebuscontract.Bus, error) {
            return instance.messageBusConsume, nil
        },

        melodycontainer.WithoutTypeRegistration(),
    )

    melodymessagebus.RegisterTransports(
        registrar,
        map[string]melodymessagebuscontract.Transport{
            messageBusTransportAsync: instance.messageBusTransport,
        },
    )

    registrar.RegisterService(
        melodytranslation.ServiceTranslator,
        func(resolver melodycontainercontract.Resolver) (melodytranslationcontract.Translator, error) {
            return instance.translator, nil
        },
    )

    registrar.RegisterService(
        melodymailer.ServiceMailer,
        func(resolver melodycontainercontract.Resolver) (melodymailercontract.Mailer, error) {
            return instance.mailer, nil
        },
    )

    registrar.RegisterService(
        melodyopenapi.ServiceOpenApiRegistry,
        func(resolver melodycontainercontract.Resolver) (*melodyopenapi.Registry, error) {
            return instance.openApiRegistry, nil
        },
    )

    registrar.RegisterService(
        melodyopenapi.ServiceOpenApiInfo,
        func(resolver melodycontainercontract.Resolver) (melodyopenapi.Info, error) {
            return instance.openApiInfo, nil
        },
    )

    instance.registerStorageService(registrar)
    instance.registerLockerService(registrar)
    instance.registerArchiveLockerService(registrar)
    instance.registerDatabaseServices(registrar)

    generated.RegisterGeneratedServices(registrar)
}

var _ melodyapplicationcontract.ServiceModule = (*Module)(nil)

func (instance *Module) registerCatalogStorageService(registrar melodyapplicationcontract.ServiceRegistrar) {
    hasDatabase := nil != instance.database

    registrar.RegisterService(
        persistence.ServiceCatalogStorage,
        func(resolver melodycontainercontract.Resolver) (*persistence.CatalogStorage, error) {
            if false == hasDatabase {
                return persistence.NewCatalogStorage(nil), nil
            }

            database, resolveErr := melodycontainer.FromResolver[*bun.DB](resolver, serviceDatabase)
            if nil != resolveErr {
                return nil, resolveErr
            }

            return persistence.NewCatalogStorageAt(database, instance.catalogLocation), nil
        },
    )
}

func (instance *Module) registerArchiveStorageService(registrar melodyapplicationcontract.ServiceRegistrar) {
    hasArchive := instance.archiveWired

    registrar.RegisterService(
        persistence.ServiceArchiveStorage,
        func(resolver melodycontainercontract.Resolver) (*persistence.ArchiveStorage, error) {
            if false == hasArchive {
                return persistence.NewArchiveStorage(nil), nil
            }

            database, resolveErr := melodycontainer.FromResolver[*bun.DB](resolver, serviceArchiveDatabase)
            if nil != resolveErr {
                return nil, resolveErr
            }

            return persistence.NewArchiveStorageAt(database, instance.archiveLocation), nil
        },
    )
}

/* RegisterScopedServices declares the services that belong to one scope — one http request here. The generator emits them into their own function because the two registrars share no method: this hook receives a scoped registrar, RegisterServices receives a container one, and handing either to the other does not compile.

   What lands here is built on the first resolution through a scope, shared by everything inside that request, and closed when the request ends. Regenerate with the same command the container services use. */
func (instance *Module) RegisterScopedServices(registrar melodyapplicationcontract.ScopedServiceRegistrar) {
    generated.RegisterGeneratedServicesScoped(registrar)
}

var _ melodyapplicationcontract.ScopedServiceModule = (*Module)(nil)
