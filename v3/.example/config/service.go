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
)

func (instance *Module) RegisterServices(registrar melodyapplicationcontract.ServiceRegistrar) {
    /* the outbox transport is container-owned (see outbox.go): registered here so its Close() error joins the ordered teardown, gated like the outbox module itself on a configured database */
    if nil != instance.database {
        instance.registerOutboxTransportService(registrar)
    }

    /* the storage handle is registered whether or not there is a connection behind it, because the generated wiring fills the repository constructors by resolving their arguments from the container by type: a handle that were absent without a database would take the whole nomenclature with it. */
    database := instance.database

    registrar.RegisterService(
        persistence.ServiceCatalogStorage,
        func(resolver melodycontainercontract.Resolver) (*persistence.CatalogStorage, error) {
            return persistence.NewCatalogStorage(database), nil
        },
    )

    /* the hub is registered so the event listeners can reach it. They follow every change to the nomenclature and are where the notification belongs, beside the cache invalidation and the journal entry — but a listener is handed a runtime rather than this module, and the container is what the two have in common. */
    serverSentEventHub := instance.serverSentEventHub

    registrar.RegisterService(
        subscriber.ServiceCatalogNotificationHub,
        func(resolver melodycontainercontract.Resolver) (*melodyhttp.ServerSentEventHub, error) {
            /* the hub files its own failures — a backplane whose publish fails, a subscriber whose
               buffer overflows — and without a journal those are counted into an atomic nobody reads:
               a redis outage would silence cross-node delivery with no record anywhere */
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
        /* the dispatch bus already claims the contract.Bus type; the consume bus is resolved by name only, so it must not also register under the shared type. */
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
    instance.registerDatabaseServices(registrar)

    /* the repositories, the domain services and the reporting services are not registered here: melody:wiring:generate scans the packages declared in NewWiringBindSet, resolves every constructor argument that is a service from the container and every scalar from the parameter it is bound to, and renders the registrations below. Adding one is a matter of writing the constructor and regenerating. Regenerate with `go run . melody:wiring:generate --package generated --function RegisterGeneratedServices --out generated/wiring_gen.go`. */
    generated.RegisterGeneratedServices(registrar)
}

var _ melodyapplicationcontract.ServiceModule = (*Module)(nil)

/* RegisterScopedServices declares the services that belong to one scope — one http request here. The
generator emits them into their own function because the two registrars share no method: this hook receives a
scoped registrar, RegisterServices receives a container one, and handing either to the other does not compile.

What lands here is built on the first resolution through a scope, shared by everything inside that request,
and closed when the request ends. Regenerate with the same command the container services use. */
func (instance *Module) RegisterScopedServices(registrar melodyapplicationcontract.ScopedServiceRegistrar) {
    generated.RegisterGeneratedServicesScoped(registrar)
}

var _ melodyapplicationcontract.ScopedServiceModule = (*Module)(nil)
