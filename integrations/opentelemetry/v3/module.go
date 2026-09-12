package opentelemetry

import (
    nethttp "net/http"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const defaultMetricsRouteName = "melody.metrics"

type ModuleConfig struct {
    Middlewares []httpcontract.Middleware
    /* HandlerDecorators are registered as outermost wrappers around the kernel's nethttp.Handler (Application.RegisterHttpHandlerDecorator), so short-circuited requests — security denials, listener-written responses — are traced and counted too; build one with NewHandlerDecorator. */
    HandlerDecorators []applicationcontract.HttpHandlerDecorator
    MetricsHandler    nethttp.Handler
    MetricsRouteName  string
    MetricsPath       string
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

/* a nil entry in the configured lists is refused at boot rather than skipped: a skipped observability middleware has no later consumer to fail loudly — the app serves traffic uninstrumented and the operator reads an empty-but-healthy dashboard with nothing to distinguish "no traffic" from "not measured". The typical source is a discarded constructor error (`middleware, _ := NewMetricsMiddleware(meter)`), which is exactly a wiring mistake boot should name. */
func (instance *Module) RegisterHttpMiddlewares(kernelInstance kernelcontract.Kernel, registrar applicationcontract.HttpMiddlewareRegistrar) {
    for _, middleware := range instance.config.Middlewares {
        if nil == middleware {
            exception.Panic(exception.NewError("opentelemetry module received a nil middleware - a discarded constructor error leaves the app silently uninstrumented", nil, nil))
        }

        registrar.Use(middleware)
    }
}

func (instance *Module) RegisterHttpHandlerDecorators(kernelInstance kernelcontract.Kernel) []applicationcontract.HttpHandlerDecorator {
    decorators := make([]applicationcontract.HttpHandlerDecorator, 0, len(instance.config.HandlerDecorators))
    for _, decorator := range instance.config.HandlerDecorators {
        if nil == decorator {
            exception.Panic(exception.NewError("opentelemetry module received a nil handler decorator - a discarded constructor error leaves the app silently uninstrumented", nil, nil))
        }

        decorators = append(decorators, decorator)
    }

    return decorators
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
    _ applicationcontract.Module                     = (*Module)(nil)
    _ applicationcontract.HttpMiddlewareModule       = (*Module)(nil)
    _ applicationcontract.HttpModule                 = (*Module)(nil)
    _ applicationcontract.HttpHandlerDecoratorModule = (*Module)(nil)
)
