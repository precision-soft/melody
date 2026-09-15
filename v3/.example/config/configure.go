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
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    melodyrueidiscache "github.com/precision-soft/melody/integrations/rueidis/v3/cache"
    melodywebsocket "github.com/precision-soft/melody/integrations/websocket/v3"
    "github.com/precision-soft/melody/v3/.example/migration"
    melodyapplication "github.com/precision-soft/melody/v3/application"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
)

/* Configure wires the application. The context is the one main handed the application — the signal context — because the module binds the database registry's lazy opens to it; the application does not publish its own. */
func Configure(ctx context.Context, app *melodyapplication.Application) {
    moduleInstance := NewExampleModule(ctx, app.Configuration())

    app.RegisterModule(melodyopentelemetry.NewModule(melodyopentelemetry.ModuleConfig{
        Middlewares:      []melodyhttpcontract.Middleware{moduleInstance.metricsMiddleware},
        MetricsHandler:   moduleInstance.metricsHandler,
        MetricsPath:      "/metrics",
        MetricsRouteName: "example.metrics",
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

    app.RegisterModule(melodyencrypt.NewModule(melodyencrypt.ModuleConfig{
        DatabaseFactory: moduleInstance.encryptDatabaseFactory,
        Cipher:          moduleInstance.cipher,
    }))

    if nil != moduleInstance.database {
        app.RegisterModule(melodyoutbox.NewModule(melodyoutbox.ModuleConfig{
            StoreFactory: moduleInstance.outboxStoreFactory,
            RelayFactory: moduleInstance.outboxRelayFactory,
        }))
    }

    app.RegisterModule(bunormmigrate.NewModule(bunormmigrate.ModuleConfig{
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
    }))

    app.RegisterModule(melodycron.NewModule(melodycron.ModuleConfig{
        ConfigurationFactory: newCronConfiguration,
        RunnerCommands:       cronRunnerCommands(),
    }))

    app.OnHttpShutdown(moduleInstance.serverSentEventHub.Shutdown)

    app.RegisterModule(melodywebsocket.NewModule(melodywebsocket.ModuleConfig{
        Hub:       moduleInstance.serverSentEventHub,
        Path:      "/ws",
        RouteName: "example.websocket",

        Options: melodywebsocket.Options{
            IdleTimeout: 30 * time.Second,
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
            Connection:   moduleInstance.redisConnection,
            AsTokenStore: true,
            TokenStoreOptions: []melodyrueidis.TokenStoreOption{
                melodyrueidis.WithTokenStorePrefix(redisTokenStoreKeyPrefix),
            },
        }))

        app.RegisterModule(melodyrueidiscache.NewModule(melodyrueidiscache.ModuleConfig{
            Client: moduleInstance.redisClient,
            Prefix: cacheKeyPrefix(),

            BackendOptions: []melodyrueidiscache.BackendOption{
                melodyrueidiscache.WithCommandTimeout(time.Second),
            },
        }))
    }
}
