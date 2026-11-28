package config

import (
    "context"
    "path/filepath"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v4"
    "github.com/precision-soft/melody/v4/.example/cache"
    "github.com/precision-soft/melody/v4/.example/generated"
    "github.com/precision-soft/melody/v4/.example/persistence"
    "github.com/precision-soft/melody/v4/.example/reporting"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/.example/subscriber"
    examplevalidation "github.com/precision-soft/melody/v4/.example/validation"
    melodyapplicationcontract "github.com/precision-soft/melody/v4/application/contract"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodymailer "github.com/precision-soft/melody/v4/mailer"
    melodymailercontract "github.com/precision-soft/melody/v4/mailer/contract"
    melodymessagebus "github.com/precision-soft/melody/v4/messagebus"
    melodymessagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
    melodyopenapi "github.com/precision-soft/melody/v4/openapi"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
    melodysession "github.com/precision-soft/melody/v4/session"
    melodysessioncontract "github.com/precision-soft/melody/v4/session/contract"
    melodytranslation "github.com/precision-soft/melody/v4/translation"
    melodytranslationcontract "github.com/precision-soft/melody/v4/translation/contract"
    melodyvalidation "github.com/precision-soft/melody/v4/validation"
    bun "github.com/uptrace/bun"
)

func (instance *Module) RegisterServices(registrar melodyapplicationcontract.ServiceRegistrar) {
    /* the outbox transport is container-owned (see outbox.go): registered here so its CloseWithContext joins the ordered teardown, gated like the outbox module itself on a configured database */
    if true == instance.catalogueWired {
        instance.registerOutboxTransportService(registrar)
    }

    /* the framework registers its validator only when the application has not: this one carries the application's own rules, so every process that binds a request knows them */
    registrar.RegisterService(
        melodyvalidation.ServiceValidator,
        func(resolver melodycontainercontract.Resolver) (*melodyvalidation.Validator, error) {
            return examplevalidation.NewValidator(), nil
        },
        melodycontainer.RegisterOptions{},
    )

    instance.registerCatalogStorageService(registrar)
    instance.registerCatalogSchemaService(registrar)
    instance.registerArchiveStorageService(registrar)
    registerCatalogJournalSourceService(registrar)
    repository.RegisterSeeders(registrar)

    /* the two outbound clients, each env-gated on the endpoint it points at: an application configured with neither registers no client and opens no pool */
    instance.registerRatesHttpClientService(registrar)
    instance.registerReportExportHttpClientService(registrar)

    instance.registerServerSentEventHubService(registrar)

    instance.registerInternalAuthSignerService(registrar)

    instance.registerSessionStorage(registrar)

    /* the token store's namespace on redis, which example:db:reset empties, the limiters and the nonce guards; without redis the store is the server process's own memory and has none */
    instance.registerRedisServices(registrar)

    if false == instance.redisConfigured() {
        opaqueTokenStore := instance.opaqueTokenStore

        registrar.RegisterService(
            melodyrueidis.ServiceTokenStore,
            func(resolver melodycontainercontract.Resolver) (melodysecuritycontract.RevocableTokenStore, error) {
                return opaqueTokenStore, nil
            },
            melodycontainer.RegisterOptions{},
        )
    }

    registrar.RegisterService(
        melodycache.ServiceCacheSerializer,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Serializer, error) {
            return cache.NewGobSerializer(), nil
        },
        melodycontainer.RegisterOptions{},
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
        melodycontainer.RegisterOptions{},
    )

    registrar.RegisterService(
        melodymailer.ServiceMailer,
        func(resolver melodycontainercontract.Resolver) (melodymailercontract.Mailer, error) {
            return instance.mailer, nil
        },
        melodycontainer.RegisterOptions{},
    )

    registrar.RegisterService(
        melodyopenapi.ServiceOpenApiRegistry,
        func(resolver melodycontainercontract.Resolver) (*melodyopenapi.Registry, error) {
            return instance.openApiRegistry, nil
        },
        melodycontainer.RegisterOptions{},
    )

    registrar.RegisterService(
        melodyopenapi.ServiceOpenApiInfo,
        func(resolver melodycontainercontract.Resolver) (melodyopenapi.Info, error) {
            return instance.openApiDocumentInfo(), nil
        },
        melodycontainer.RegisterOptions{},
    )

    instance.registerStorageServices(registrar)
    instance.registerLockerService(registrar)
    instance.registerArchiveLockerService(registrar)
    instance.registerDatabaseServices(registrar)
    instance.registerTwoFactorStoreService(registrar)

    /* the repositories, the domain services and the reporting services are not registered here: melody:wiring:generate scans the packages declared in NewWiringBindSet, resolves every constructor argument that is a service from the container and every scalar from the parameter it is bound to, and renders the registrations below. Adding one is a matter of writing the constructor and regenerating. The file is generated, not committed: regenerate with `go generate ./generated`, which .dev/generate.sh runs at the dev container's start and before every gate; once it exists, `go run . melody:wiring:generate --package generated --function RegisterGeneratedServices --out generated/wiring_gen.go` renders the same file from inside the application. */
    generated.RegisterGeneratedServices(registrar)
}

var _ melodyapplicationcontract.ServiceModule = (*Module)(nil)

/* registerServerSentEventHubService registers the hub so the event listeners and the stream handlers, which are handed a runtime rather than this module, reach it through the container. */
func (instance *Module) registerServerSentEventHubService(registrar melodyapplicationcontract.ServiceRegistrar) {
    /* built at boot: the stream handlers and the nomenclature listener resolve it on a request, and the http shutdown hook closes the hub the boot built */
    registrar.RegisterService(
        subscriber.ServiceCatalogNotificationHub,
        instance.newServerSentEventHub,
        melodycontainer.RegisterOptions{BuiltAtBoot: true, BootFailure: melodycontainer.BootFailureFailsBoot},
    )
}

/* registerMessageBusServices publishes the two buses: the dispatch bus, which sends a routed message to its transport and handles the rest in process, and the consume bus the worker hands what it received. */
func (instance *Module) registerMessageBusServices(registrar melodyapplicationcontract.ServiceRegistrar) {
    registrar.RegisterService(
        melodymessagebus.ServiceBus,
        func(resolver melodycontainercontract.Resolver) (melodymessagebuscontract.Bus, error) {
            return instance.messageBusDispatch, nil
        },
        melodycontainer.RegisterOptions{},
    )

    registrar.RegisterService(
        melodymessagebus.ServiceConsumeBus,
        func(resolver melodycontainercontract.Resolver) (melodymessagebuscontract.Bus, error) {
            return instance.messageBusConsume, nil
        },
        /* the dispatch bus already claims the contract.Bus type; the consume bus is resolved by name only, so it must not also register under the shared type. */
        melodycontainer.RegisterOptions{TypeRegistrationDisabled: true},
    )
}

/* registerSessionStorage swaps the framework's in-memory default for the file-backed storage when the environment names a file, so a signed-in session survives a restart — the development supervisor restarts the example on every saved change — and the storage drops a session once MELODY_HTTP_SESSION_TTL has passed without a request. An empty value keeps the in-memory default. */
func (instance *Module) registerSessionStorage(registrar melodyapplicationcontract.ServiceRegistrar) {
    sessionFilePath := resolvedSessionFilePath(instance.parameterValue(parameterSessionFile), instance.configuration.Kernel().ProjectDirectory())
    if "" == sessionFilePath {
        return
    }

    registrar.RegisterService(
        melodysession.ServiceSessionStorage,
        func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Storage, error) {
            logger, loggerErr := melodylogging.LoggerFromResolver(resolver.Runtime().Context(), resolver)
            if nil != loggerErr {
                return nil, loggerErr
            }

            return melodysession.NewFileStorage(sessionFilePath, melodysession.FileStorageConfig{Logger: logger})
        },
        melodycontainer.RegisterOptions{},
    )
}

/* resolvedSessionFilePath keeps the empty value empty — the switch that says "in-memory" — and anchors a relative path to the project directory rather than the working directory, so a console run and the http process read the same file. */
func resolvedSessionFilePath(sessionFilePath string, projectDirectory string) string {
    if "" == sessionFilePath {
        return ""
    }

    if false == filepath.IsAbs(sessionFilePath) {
        return filepath.Join(projectDirectory, sessionFilePath)
    }

    return sessionFilePath
}

/* registerCatalogStorageService publishes the handle every repository is built on, registered with or without a connection because the generated wiring resolves the repository constructors' arguments by type. The handle is resolved rather than captured, so the container records the edge that teardown reads (storage, handle, registry, journal). */
func (instance *Module) registerCatalogStorageService(registrar melodyapplicationcontract.ServiceRegistrar) {
    hasDatabase := instance.catalogueWired
    seedsAccounts := instance.isDevelopment()
    seedsNomenclature := instance.booleanSwitch(environmentKeyCatalogueSeed, parameterCatalogueSeed)

    registrar.RegisterService(
        persistence.ServiceCatalogStorage,
        func(resolver melodycontainercontract.Resolver) (*persistence.CatalogStorage, error) {
            if false == hasDatabase {
                return catalogStorageSeeding(persistence.NewCatalogStorage(nil), seedsAccounts, seedsNomenclature), nil
            }

            database, resolveErr := melodycontainer.FromResolver[*bun.DB](resolver.Runtime().Context(), resolver, serviceDatabase)
            if nil != resolveErr {
                return nil, resolveErr
            }

            return catalogStorageSeeding(persistence.NewCatalogStorageAt(database, instance.catalogLocation), seedsAccounts, seedsNomenclature), nil
        },
        melodycontainer.RegisterOptions{},
    )
}

/* registerCatalogSchemaService publishes the proof that the catalogue is ready: the migration set, the trail's tables and the seeds, under the runtime of whoever resolves it. Every repository over the catalogue takes it, so it sits on the path of every catalogue read and write. An http process builds it at boot, a cli process at the first command that reads the catalogue; when the database does not answer, the http boot goes on with a boot warning, requests answer 503 and the first one after the database is back prepares the catalogue. */
func (instance *Module) registerCatalogSchemaService(registrar melodyapplicationcontract.ServiceRegistrar) {
    registrar.RegisterService(
        persistence.ServiceCatalogSchema,
        func(resolver melodycontainercontract.Resolver) (*persistence.CatalogSchema, error) {
            storage, storageErr := melodycontainer.FromResolver[*persistence.CatalogStorage](resolver.Runtime().Context(), resolver, persistence.ServiceCatalogStorage)
            if nil != storageErr {
                return nil, storageErr
            }

            if prepareErr := repository.PrepareCatalog(resolver.Runtime(), storage); nil != prepareErr {
                return nil, prepareErr
            }

            return &persistence.CatalogSchema{}, nil
        },
        melodycontainer.RegisterOptions{BuiltAtBoot: true, BootFailure: melodycontainer.BootFailureWarns},
    )
}

/* catalogStorageSeeding marks the two seeds: the accounts are the development-only seed, their public passwords accepted there alone; the nomenclature is seeded in every environment unless APP_CATALOGUE_SEED turns it off */
func catalogStorageSeeding(storage *persistence.CatalogStorage, seedsAccounts bool, seedsNomenclature bool) *persistence.CatalogStorage {
    if true == seedsAccounts {
        storage = storage.WithAccountSeed()
    }

    if false == seedsNomenclature {
        storage = storage.WithoutNomenclatureSeed()
    }

    return storage
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

            database, resolveErr := melodycontainer.FromResolver[*bun.DB](resolver.Runtime().Context(), resolver, serviceArchiveDatabase)
            if nil != resolveErr {
                return nil, resolveErr
            }

            /* the process context travels with the handle: the repository built over it applies the archive's migration set at its first resolution, a wait of up to the lock window on a held migration lock, and that wait ends with the process's signal the way the dial does */
            return persistence.NewArchiveStorageAt(database, instance.archiveLocation).WithContext(instance.processContext), nil
        },
        melodycontainer.RegisterOptions{},
    )
}

/* RegisterScopedServices declares the services that belong to one scope, one http request here: built on the first resolution through the scope, shared inside that request and closed when it ends. The generator emits them into their own function because the scoped and the container registrars share no method; regenerate with the command the container services use. */
func (instance *Module) RegisterScopedServices(registrar melodyapplicationcontract.ScopedServiceRegistrar) {
    generated.RegisterGeneratedServicesScoped(registrar)
}

var _ melodyapplicationcontract.ScopedServiceModule = (*Module)(nil)

/* registerCatalogJournalSourceService hands the request report trail the catalogue journal behind a lazy handle: the trail is built for every request, and the journal it writes to is resolved only when a request recorded a change */
func registerCatalogJournalSourceService(registrar melodycontainercontract.Registrar) {
    melodycontainer.MustRegister(
        registrar,
        reporting.ServiceCatalogJournalSource,
        func(resolver melodycontainercontract.Resolver) (reporting.CatalogJournalSource, error) {
            journal := melodycontainer.Lazy[repository.CatalogJournalRepository](resolver, repository.ServiceCatalogJournalRepository)

            /* the trail flushes from its scope's close as well, where no runtime is held, so the journal is created detached over the flush's context */
            return func(ctx context.Context) (repository.CatalogJournalRepository, error) {
                return journal.Resolve(melodyruntime.WithContext(resolver.Runtime(), ctx))
            }, nil
        },
        melodycontainer.RegisterOptions{},
    )
}
