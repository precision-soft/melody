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
    /* the outbox transport is container-owned (see outbox.go): registered here so its Close() error joins the ordered teardown, gated like the outbox module itself on a configured database */
    if nil != instance.database {
        instance.registerOutboxTransportService(registrar)
    }

    instance.registerCatalogStorageService(registrar)
    instance.registerArchiveStorageService(registrar)

    /* the two outbound clients, each env-gated on the endpoint it points at: an application configured
       with neither registers no client and opens no pool */
    instance.registerRatesHttpClientService(registrar)
    instance.registerReportExportHttpClientService(registrar)

    instance.registerServerSentEventHubService(registrar)

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

    instance.registerMessageBusServices(registrar)

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
    instance.registerTwoFactorStoreService(registrar)

    /* the repositories, the domain services and the reporting services are not registered here: melody:wiring:generate scans the packages declared in NewWiringBindSet, resolves every constructor argument that is a service from the container and every scalar from the parameter it is bound to, and renders the registrations below. Adding one is a matter of writing the constructor and regenerating. Regenerate with `go run . melody:wiring:generate --package generated --function RegisterGeneratedServices --out generated/wiring_gen.go`. */
    generated.RegisterGeneratedServices(registrar)
}

var _ melodyapplicationcontract.ServiceModule = (*Module)(nil)

/* registerServerSentEventHubService registers the hub so the event listeners, which are handed a runtime rather than this module, reach it through the container. */
func (instance *Module) registerServerSentEventHubService(registrar melodyapplicationcontract.ServiceRegistrar) {
    serverSentEventHub := instance.serverSentEventHub

    registrar.RegisterService(
        subscriber.ServiceCatalogNotificationHub,
        func(resolver melodycontainercontract.Resolver) (*melodyhttp.ServerSentEventHub, error) {
            /* the hub files its own failures — a backplane whose publish fails, a subscriber whose buffer overflows — and without a journal those are counted into an atomic nobody reads: a redis outage would silence cross-node delivery with no record anywhere */
            logger, loggerErr := melodylogging.LoggerFromResolver(resolver)
            if nil != loggerErr {
                return nil, loggerErr
            }

            serverSentEventHub.SetLogger(logger)

            return serverSentEventHub, nil
        },
    )
}

/* registerMessageBusServices publishes the two buses: the dispatch bus, which sends a routed message to its
   transport and handles the rest in process, and the consume bus the worker hands what it received. */
func (instance *Module) registerMessageBusServices(registrar melodyapplicationcontract.ServiceRegistrar) {
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
        /* the dispatch bus already claims the contract.Bus type; the consume bus is resolved by name only, so it must not also register under the shared type. */
        melodycontainer.WithoutTypeRegistration(),
    )
}

/* registerCatalogStorageService publishes the handle every repository is built on, registered with or without a connection because the generated wiring resolves the repository constructors' arguments by type. The handle is resolved rather than captured, so the container records the edge that teardown reads (storage, handle, registry, journal) and the registry's logger swap runs at the first repository resolution. */
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

/* registerArchiveStorageService publishes the reading archive's handle in the catalogue handle's shape, registered with or without a connection. The handle is opened here, at the first resolution, so a process that never takes a reading pays no postgres handshake. */
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

            /* the process context travels with the handle: the repository built over it applies the archive's
               migration set at its first resolution, a wait of up to the lock window on a held migration
               lock, and that wait ends with the process's signal the way the dial does */
            return persistence.NewArchiveStorageAt(database, instance.archiveLocation).WithContext(instance.processContext), nil
        },
    )
}

/* RegisterScopedServices declares the services that belong to one scope, one http request here: built on the first resolution through the scope, shared inside that request and closed when it ends. The generator emits them into their own function because the scoped and the container registrars share no method; regenerate with the command the container services use. */
func (instance *Module) RegisterScopedServices(registrar melodyapplicationcontract.ScopedServiceRegistrar) {
    generated.RegisterGeneratedServicesScoped(registrar)
}

var _ melodyapplicationcontract.ScopedServiceModule = (*Module)(nil)
