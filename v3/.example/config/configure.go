package config

import (
    "time"

    melodyawss3 "github.com/precision-soft/melody/integrations/awss3/v3"
    bunormmigrate "github.com/precision-soft/melody/integrations/bunorm/migrate/v3"
    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
    melodycron "github.com/precision-soft/melody/integrations/cron/v3"
    melodyopentelemetry "github.com/precision-soft/melody/integrations/opentelemetry/v3"
    melodyotlp "github.com/precision-soft/melody/integrations/opentelemetry/v3/otlp"
    melodyoutbox "github.com/precision-soft/melody/integrations/outbox/v3"
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    melodyrueidiscache "github.com/precision-soft/melody/integrations/rueidis/v3/cache"
    melodywebsocket "github.com/precision-soft/melody/integrations/websocket/v3"
    "github.com/precision-soft/melody/v3/.example/migration"
    melodyapplication "github.com/precision-soft/melody/v3/application"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
)

func Configure(app *melodyapplication.Application) {
    moduleInstance := NewExampleModule(app.Configuration())

    /* @info observability module first so its metrics middleware wraps outermost, ahead of the example timing middleware. */
    app.RegisterModule(melodyopentelemetry.NewModule(melodyopentelemetry.ModuleConfig{
        Middlewares:      []melodyhttpcontract.Middleware{moduleInstance.metricsMiddleware},
        MetricsHandler:   moduleInstance.metricsHandler,
        MetricsPath:      "/metrics",
        MetricsRouteName: "example.metrics",
    }))

    app.RegisterModule(moduleInstance)

    /* @info opt-in OTLP tracing: when OTEL_EXPORTER_OTLP_ENDPOINT is set (see .env) the otlp module builds
       a TracerProvider, adds the tracing middleware and flushes spans on shutdown — plug-and-play, exactly
       like the other integration module facades. Unset ⇒ no tracing, no OTLP dependency cost at runtime. */
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

    /* @info the encrypt bulk command resolves its database through the factory at the first run — after
       Boot — so registering the command costs nothing in http or worker mode, and a boot without MYSQL_HOST
       stays clean (the first run then reports the missing database service). */
    app.RegisterModule(melodyencrypt.NewModule(melodyencrypt.ModuleConfig{
        DatabaseFactory: moduleInstance.encryptDatabaseFactory,
        Cipher:          moduleInstance.cipher,
    }))

    /* @info transactional-outbox module in the factory shape: the store and relay are registered as service
       providers that resolve the shared *bun.DB from the container at first use, and the module contributes
       the melody:outbox:relay command over the same lazily-resolved relay. */
    if nil != moduleInstance.database {
        app.RegisterModule(melodyoutbox.NewModule(melodyoutbox.ModuleConfig{
            StoreFactory: moduleInstance.outboxStoreFactory,
            RelayFactory: moduleInstance.outboxRelayFactory,
        }))
    }

    /* the db:* family is registered whether or not a database is configured, so the command surface does not
       change between environments; without one every db:* command fails at Run with the container refusal
       naming the registry service. No context family is declared: this major keeps the journal, the
       two-factor enrollment and the catalogue on one connection, so a single set covers the whole schema and
       the registry has a single manager for the unprefixed commands to reach. */
    app.RegisterModule(bunormmigrate.NewModule(bunormmigrate.ModuleConfig{
        Migrations: migration.Migrations,
        Options: bunormmigrate.Options{
            ManagerRegistryServiceId: serviceDatabaseRegistry,
        },
    }))

    /* @info cron's Configuration is kernel-dependent (reads parameters), so it is supplied as a factory evaluated at command-registration time. */
    app.RegisterModule(melodycron.NewModule(melodycron.ModuleConfig{
        ConfigurationFactory: newCronConfiguration,
        RunnerCommands:       cronRunnerCommands(),
    }))

    /* the SSE stream and the websocket handler both block on this hub; http.Server.Shutdown neither cancels an in-flight request's context nor tracks a hijacked connection, so without closing the hub a single connected client holds the whole shutdown timeout and is then cut mid-flight */
    app.OnHttpShutdown(moduleInstance.serverSentEventHub.Shutdown)

    app.RegisterModule(melodywebsocket.NewModule(melodywebsocket.ModuleConfig{
        Hub:       moduleInstance.serverSentEventHub,
        Path:      "/ws",
        RouteName: "example.websocket",
        /* @info IdleTimeout is required: the keepalive ping is the only thing that reaps a browser tab that went away without a fin, and 30s is a comfortable interval for one. */
        Options: melodywebsocket.Options{
            OriginPatterns: []string{"*"},
            IdleTimeout:    30 * time.Second,
        },
    }))

    if nil != moduleInstance.storageClient {
        app.RegisterModule(melodyawss3.NewModule(melodyawss3.ModuleConfig{
            Client: moduleInstance.storageClient,
            Bucket: moduleInstance.storageBucket,
        }))
    }

    if nil != moduleInstance.redisClient {
        app.RegisterModule(melodyrueidis.NewModule(melodyrueidis.ModuleConfig{
            Client:       moduleInstance.redisClient,
            AsTokenStore: true,
            TokenStoreOptions: []melodyrueidis.TokenStoreOption{
                melodyrueidis.WithTokenStorePrefix(redisTokenStoreKeyPrefix),
            },
        }))

        app.RegisterModule(melodyrueidiscache.NewModule(melodyrueidiscache.ModuleConfig{
            Client: moduleInstance.redisClient,
            Prefix: redisCacheKeyPrefix,
        }))
    }
}
