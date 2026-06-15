package config

import (
    melodyawss3 "github.com/precision-soft/melody/integrations/awss3/v3"
    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
    melodycron "github.com/precision-soft/melody/integrations/cron/v3"
    melodyopentelemetry "github.com/precision-soft/melody/integrations/opentelemetry/v3"
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    melodyrueidiscache "github.com/precision-soft/melody/integrations/rueidis/v3/cache"
    melodywebsocket "github.com/precision-soft/melody/integrations/websocket/v3"
    melodyapplication "github.com/precision-soft/melody/v3/application"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
)

func Configure(app *melodyapplication.Application) {
    moduleInstance := NewExampleModule()

    /** @info observability module first so its metrics middleware wraps outermost, ahead of the example timing middleware. */
    app.RegisterModule(melodyopentelemetry.NewModule(melodyopentelemetry.ModuleConfig{
        Middlewares:      []melodyhttpcontract.Middleware{moduleInstance.metricsMiddleware},
        MetricsHandler:   moduleInstance.metricsHandler,
        MetricsPath:      "/metrics",
        MetricsRouteName: "example.metrics",
    }))

    app.RegisterModule(moduleInstance)

    app.RegisterModule(melodyencrypt.NewModule(melodyencrypt.ModuleConfig{
        Database: moduleInstance.database,
        Cipher:   moduleInstance.cipher,
    }))

    /** @info cron's Configuration is kernel-dependent (reads parameters), so it is supplied as a factory evaluated at command-registration time. */
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
