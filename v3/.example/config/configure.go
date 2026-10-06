package config

import (
    "context"
    "time"

    melodyawss3 "github.com/precision-soft/melody/integrations/awss3/v3"
    bunormmigrate "github.com/precision-soft/melody/integrations/bunorm/migrate/v3"
    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
    melodycron "github.com/precision-soft/melody/integrations/cron/v3"
    melodyopentelemetry "github.com/precision-soft/melody/integrations/opentelemetry/v3"
    melodyotlp "github.com/precision-soft/melody/integrations/opentelemetry/v3/otlp"
    melodyoutbox "github.com/precision-soft/melody/integrations/outbox/v3"
    melodywebsocket "github.com/precision-soft/melody/integrations/websocket/v3"
    "github.com/precision-soft/melody/v3/.example/migration"
    melodyapplication "github.com/precision-soft/melody/v3/application"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
)

/* Configure wires the application. The context is the signal context main hands the application, because the module binds the database registry's lazy opens to it. */
func Configure(ctx context.Context, app *melodyapplication.Application) {
    moduleInstance := NewExampleModule(ctx, app.Configuration())

    /* observability module first so its metrics middleware wraps outermost, ahead of the example timing middleware. */
    app.RegisterModule(melodyopentelemetry.NewModule(melodyopentelemetry.ModuleConfig{
        Middlewares:       []melodyhttpcontract.Middleware{moduleInstance.metricsMiddleware},
        HandlerDecorators: []melodyapplicationcontract.HttpHandlerDecorator{moduleInstance.lifecycleDecorator},
        MetricsHandler:    moduleInstance.metricsHandler,
        MetricsPath:       metricsPath,
        MetricsRouteName:  "example.metrics",
    }))

    app.RegisterModule(moduleInstance)

    if otelEndpoint := moduleInstance.environmentValue(environmentKeyOtelExporterEndpoint); "" != otelEndpoint {
        app.RegisterModule(melodyotlp.NewModule(melodyotlp.ModuleConfig{
            Config: melodyotlp.Config{
                Endpoint:       otelEndpoint,
                Protocol:       melodyotlp.ProtocolGrpc,
                ServiceName:    "melody.example",
                ServiceVersion: "1.0.0",
                Insecure:       true,
            },
        }))
    }

    /* the encrypt bulk command resolves its database through the factory at its first run, after Boot, so a boot without MYSQL_HOST stays clean and the first run reports the missing service. */
    app.RegisterModule(melodyencrypt.NewModule(melodyencrypt.ModuleConfig{
        DatabaseFactory: moduleInstance.encryptDatabaseFactory,
        Cipher:          moduleInstance.cipher,
    }))

    /* the outbox store and relay are service providers that resolve the shared *bun.DB at first use, so registering the module touches neither the outbox schema nor the transport. */
    if true == moduleInstance.catalogueWired {
        app.RegisterModule(melodyoutbox.NewModule(melodyoutbox.ModuleConfig{
            StoreFactory: moduleInstance.outboxStoreFactory,
            RelayFactory: moduleInstance.outboxRelayFactory,
        }))
    }

    app.RegisterModule(bunormmigrate.NewModule(migrateModuleConfig()))

    /* cron's Configuration is kernel-dependent (reads parameters), so it is supplied as a factory evaluated at command-registration time. */
    app.RegisterModule(melodycron.NewModule(melodycron.ModuleConfig{
        ConfigurationFactory: newCronConfiguration,
        RunnerCommands:       cronRunnerCommands(),
    }))

    moduleInstance.registerHubShutdown(app)
    moduleInstance.registerTrustedProxyShutdown(app)

    app.RegisterModule(melodywebsocket.NewModule(moduleInstance.websocketModuleConfig()))

    if nil != moduleInstance.storageClient {
        app.RegisterModule(melodyawss3.NewModule(melodyawss3.ModuleConfig{
            Client: moduleInstance.storageClient,
            Bucket: moduleInstance.storageBucket,
        }))
    }

    if nil != moduleInstance.redisClient {
        app.RegisterModuleProvider(newRedisInfrastructure(moduleInstance.redisClient, moduleInstance.redisConnection))
    }
}

/* migrateModuleConfig registers the db:* family whether or not a database is configured; without one every db:* command fails at Run naming the registry service. The archive's db:archive:* family is a context, which pins it to the archive's manager, and the base family is pinned to databaseManagerName. */
func migrateModuleConfig() bunormmigrate.ModuleConfig {
    return bunormmigrate.ModuleConfig{
        Migrations: migration.Migrations,
        Contexts: []bunormmigrate.ContextConfig{
            {
                Name:       databaseArchiveManagerName,
                Migrations: migration.ArchiveMigrations,
            },
        },
        Options: bunormmigrate.Options{
            ManagerRegistryServiceId: serviceDatabaseRegistry,
            ManagerName:              databaseManagerName,
        },
    }
}

/* httpShutdownRegistrar is the one door of the application registerHubShutdown needs */
type httpShutdownRegistrar interface {
    OnHttpShutdown(hook func())
}

/* registerHubShutdown closes the hub when the http server begins to shut down: http.Server.Shutdown neither cancels an in-flight request's context nor tracks a hijacked connection, so a connected SSE or websocket client would otherwise hold the whole shutdown timeout. It is also the backplane's only close, since the hub's Shutdown drains and closes the backplane it holds; the container's teardown closes the hub again through its idempotent Close, the only close a process without an http server reaches. */
func (instance *Module) registerHubShutdown(registrar httpShutdownRegistrar) {
    registrar.OnHttpShutdown(instance.serverSentEventHub.Shutdown)
}

/* registerTrustedProxyShutdown waits, as the http server shuts down, for the retry of a failed balancer lookup running off the request path, so its warning reaches the journal before the container's teardown closes it */
func (instance *Module) registerTrustedProxyShutdown(registrar httpShutdownRegistrar) {
    registrar.OnHttpShutdown(func() {
        if nil != instance.trustedProxyResolver {
            instance.trustedProxyResolver.Close()
        }
    })
}

/* websocketRouteName names the /ws route, which StreamSlotMiddleware counts in the event stream's slots */
const websocketRouteName = "example.websocket"

/* websocketModuleConfig serves the hub over /ws beside the event stream. */
func (instance *Module) websocketModuleConfig() melodywebsocket.ModuleConfig {
    return melodywebsocket.ModuleConfig{
        Hub:       instance.serverSentEventHub,
        Path:      "/ws",
        RouteName: websocketRouteName,
        /* IdleTimeout is required: the keepalive ping is the only thing that reaps a tab that went away without a fin. OriginPatterns stays unset on purpose: the upgrade authenticates through the session cookie, which a browser sends cross-site too, so the library's same-origin default is what stops a foreign page riding a visitor's session; a client that sends no Origin header is not origin-checked. */
        Options: melodywebsocket.Options{
            IdleTimeout: 30 * time.Second,
        },
    }
}
