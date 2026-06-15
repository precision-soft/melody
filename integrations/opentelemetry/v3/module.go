package opentelemetry

import (
    nethttp "net/http"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const defaultMetricsRouteName = "melody.metrics"

type ModuleConfig struct {
    Middlewares      []httpcontract.Middleware
    MetricsHandler   nethttp.Handler
    MetricsRouteName string
    MetricsPath      string
}

func NewModule(config ModuleConfig) *Module {
    return &Module{config: config}
}

type Module struct {
    config ModuleConfig
}

func (instance *Module) Name() string {
    return "opentelemetry"
}

func (instance *Module) Description() string {
    return "registers the tracing and metrics middlewares plus the prometheus metrics route"
}

func (instance *Module) RegisterHttpMiddlewares(kernelInstance kernelcontract.Kernel, registrar applicationcontract.HttpMiddlewareRegistrar) {
    for _, middleware := range instance.config.Middlewares {
        if nil == middleware {
            continue
        }

        registrar.Use(middleware)
    }
}

func (instance *Module) RegisterHttpRoutes(kernelInstance kernelcontract.Kernel) {
    if nil == instance.config.MetricsHandler || "" == instance.config.MetricsPath {
        return
    }

    routeName := instance.config.MetricsRouteName
    if "" == routeName {
        routeName = defaultMetricsRouteName
    }

    kernelInstance.HttpRouter().HandleNamed(
        routeName,
        "GET",
        instance.config.MetricsPath,
        MetricsRouteHandler(instance.config.MetricsHandler),
    )
}

func MetricsRouteHandler(handler nethttp.Handler) httpcontract.Handler {
    return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        handler.ServeHTTP(writer, request.HttpRequest())

        return nil, nil
    }
}

var (
    _ applicationcontract.Module               = (*Module)(nil)
    _ applicationcontract.HttpMiddlewareModule = (*Module)(nil)
    _ applicationcontract.HttpModule           = (*Module)(nil)
)
