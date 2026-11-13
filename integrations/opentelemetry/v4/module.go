package opentelemetry

import (
    nethttp "net/http"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

const defaultMetricsRouteName = "melody.metrics"

type ModuleConfig struct {
    Middlewares []httpcontract.Middleware
    /* HandlerDecorators are registered as outermost wrappers around the kernel's nethttp.Handler (Application.RegisterHttpHandlerDecorator), so short-circuited requests — security denials, listener-written responses — are traced too, and counted when the decorator was built with HandlerDecoratorConfig.Meter; build one with NewHandlerDecorator. */
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

/* a nil entry, typically a discarded constructor error, is refused at boot: a skipped one would leave the application silently uninstrumented */
func (instance *Module) RegisterHttpMiddlewares(kernelInstance kernelcontract.Kernel, registrar applicationcontract.HttpMiddlewareRegistrar) {
    for _, middleware := range instance.config.Middlewares {
        if nil == middleware {
            exception.Panic(exception.NewError("opentelemetry module received a nil middleware - a discarded constructor error leaves the app silently uninstrumented", nil, nil))
        }
    }

    registrar.Use(applicationcontract.MiddlewareRegistration{Middlewares: instance.config.Middlewares})
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
    hasHandler := nil != instance.config.MetricsHandler
    hasPath := "" != instance.config.MetricsPath

    if false == hasHandler && false == hasPath {
        if "" != instance.config.MetricsRouteName {
            exception.Panic(exception.NewError(
                "the opentelemetry module is handed a metrics route name and no metrics route",
                map[string]any{"field": "ModuleConfig.MetricsRouteName", "requires": "ModuleConfig.MetricsHandler and ModuleConfig.MetricsPath"},
                nil,
            ))
        }

        return
    }

    if false == hasHandler {
        exception.Panic(exception.NewError(
            "the opentelemetry module is handed a metrics path and no metrics handler",
            map[string]any{"field": "ModuleConfig.MetricsPath", "requires": "ModuleConfig.MetricsHandler"},
            nil,
        ))
    }

    if false == hasPath {
        exception.Panic(exception.NewError(
            "the opentelemetry module is handed a metrics handler and no metrics path",
            map[string]any{"field": "ModuleConfig.MetricsHandler", "requires": "ModuleConfig.MetricsPath"},
            nil,
        ))
    }

    routeName := instance.config.MetricsRouteName
    if "" == routeName {
        routeName = defaultMetricsRouteName
    }

    kernelInstance.HttpRouter().HandleWithOptions(
        instance.config.MetricsPath,
        MetricsRouteHandler(instance.config.MetricsHandler),
        httpcontract.RouteOptions{Name: routeName, Methods: []string{"GET"}},
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
