package config

import (
    melodyawss3 "github.com/precision-soft/melody/integrations/awss3/v3"
    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
    melodycron "github.com/precision-soft/melody/integrations/cron/v3"
    melodyopentelemetry "github.com/precision-soft/melody/integrations/opentelemetry/v3"
    melodyotlp "github.com/precision-soft/melody/integrations/opentelemetry/v3/otlp"
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    melodyrueidiscache "github.com/precision-soft/melody/integrations/rueidis/v3/cache"
    melodywebsocket "github.com/precision-soft/melody/integrations/websocket/v3"
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

    app.RegisterModule(melodyencrypt.NewModule(melodyencrypt.ModuleConfig{
        Database: moduleInstance.database,
        Cipher:   moduleInstance.cipher,
    }))

    /* @info cron's Configuration is kernel-dependent (reads parameters), so it is supplied as a factory evaluated at command-registration time. */
    app.RegisterModule(melodycron.NewModule(melodycron.ModuleConfig{
        ConfigurationFactory: newCronConfiguration,
    }))

    app.RegisterModule(melodywebsocket.NewModule(melodywebsocket.ModuleConfig{
        Hub:       moduleInstance.serverSentEventHub,
        Path:      "/ws",
        RouteName: "example.websocket",
        Options:   melodywebsocket.Options{OriginPatterns: []string{"*"}},
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
        }))

        app.RegisterModule(melodyrueidiscache.NewModule(melodyrueidiscache.ModuleConfig{
            Client: moduleInstance.redisClient,
            Prefix: "melody-example:",
        }))
    }
}
