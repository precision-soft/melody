package opentelemetry

import (
    "context"
    nethttp "net/http"
    "strings"
    "testing"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

type spyMiddlewareRegistrar struct {
    registrations []applicationcontract.MiddlewareRegistration
}

func (instance *spyMiddlewareRegistrar) Use(registration applicationcontract.MiddlewareRegistration) {
    instance.registrations = append(instance.registrations, registration)
}

type spyRouter struct {
    httpcontract.Router
    handled []string
}

func (instance *spyRouter) HandleWithOptions(pattern string, handler httpcontract.Handler, options httpcontract.RouteOptions) {
    instance.handled = append(instance.handled, options.Name+" "+strings.Join(options.Methods, ",")+" "+pattern)
}

type spyKernel struct {
    kernelcontract.Kernel
    router *spyRouter
}

func (instance *spyKernel) Context() context.Context {
    return context.Background()
}

func (instance *spyKernel) HttpRouter() httpcontract.Router {
    return instance.router
}

func passthroughMiddleware(next httpcontract.Handler) httpcontract.Handler {
    return next
}

func TestModule_NameAndDescription(t *testing.T) {
    module := NewModule(ModuleConfig{})

    if "opentelemetry" != module.Name() {
        t.Fatalf("Name() = %q, want %q", module.Name(), "opentelemetry")
    }

    if "" == module.Description() {
        t.Fatal("Description() must not be empty")
    }
}

func TestModule_RegisterHttpMiddlewaresInstallsTheConfiguredMiddlewaresInOneRegistration(t *testing.T) {
    registrar := &spyMiddlewareRegistrar{}

    NewModule(ModuleConfig{
        Middlewares: []httpcontract.Middleware{passthroughMiddleware, passthroughMiddleware},
    }).RegisterHttpMiddlewares(nil, registrar)

    if 1 != len(registrar.registrations) || 2 != len(registrar.registrations[0].Middlewares) || 0 != registrar.registrations[0].Priority {
        t.Fatalf("expected one registration carrying the two configured middlewares at the default priority, got %+v", registrar.registrations)
    }
}

func TestModule_RegisterHttpMiddlewaresRefusesANilEntryAtBoot(t *testing.T) {
    registrar := &spyMiddlewareRegistrar{}

    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError || false == strings.Contains(recoveredErr.Error(), "opentelemetry module received a nil middleware") {
            t.Fatalf("expected a nil middleware entry to be refused at boot instead of silently disarming instrumentation, got %v", recoveredErr)
        }

        if 0 != len(registrar.registrations) {
            t.Fatalf("expected nothing registered before the refusal, got %+v", registrar.registrations)
        }
    }()

    NewModule(ModuleConfig{
        Middlewares: []httpcontract.Middleware{passthroughMiddleware, nil, passthroughMiddleware},
    }).RegisterHttpMiddlewares(nil, registrar)
}

func TestModule_RegisterHttpHandlerDecoratorsRefusesANilEntryAtBoot(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatal("expected a nil handler decorator entry to be refused at boot instead of silently disarming instrumentation")
        }
    }()

    NewModule(ModuleConfig{
        HandlerDecorators: []applicationcontract.HttpHandlerDecorator{nil},
    }).RegisterHttpHandlerDecorators(nil)
}

func TestModule_RegisterHttpRoutesWithNeitherHandlerNorPathRegistersNothing(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    NewModule(ModuleConfig{}).RegisterHttpRoutes(kernel)
    if 0 != len(kernel.router.handled) {
        t.Fatalf("expected no route, got %v", kernel.router.handled)
    }
}

func TestModule_RegisterHttpRoutesRefusesAHalfDeclaredMetricsRouteNamingTheField(t *testing.T) {
    for _, testCase := range []struct {
        name   string
        config ModuleConfig
        field  string
    }{
        {"a path with no handler", ModuleConfig{MetricsPath: "/metrics"}, "ModuleConfig.MetricsPath"},
        {"a handler with no path", ModuleConfig{MetricsHandler: nethttp.NewServeMux()}, "ModuleConfig.MetricsHandler"},
        {"a route name with neither", ModuleConfig{MetricsRouteName: "metrics"}, "ModuleConfig.MetricsRouteName"},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            kernel := &spyKernel{router: &spyRouter{}}

            defer func() {
                recoveredErr, isError := recover().(error)
                if false == isError {
                    t.Fatalf("expected %s to be refused at boot", testCase.name)
                }

                if testCase.field != exception.LogContext(recoveredErr, exception.LogContextConfig{})["field"] {
                    t.Fatalf("expected the refusal to name %s, got %v", testCase.field, exception.LogContext(recoveredErr, exception.LogContextConfig{}))
                }

                if 0 != len(kernel.router.handled) {
                    t.Fatalf("expected no route, got %v", kernel.router.handled)
                }
            }()

            NewModule(testCase.config).RegisterHttpRoutes(kernel)
        })
    }
}

func TestModule_RegisterHttpRoutesUsesDefaultName(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    NewModule(ModuleConfig{
        MetricsHandler: nethttp.NewServeMux(),
        MetricsPath:    "/metrics",
    }).RegisterHttpRoutes(kernel)

    if 1 != len(kernel.router.handled) {
        t.Fatalf("expected one route, got %v", kernel.router.handled)
    }
    if defaultMetricsRouteName+" GET /metrics" != kernel.router.handled[0] {
        t.Fatalf("expected the default metrics route, got %q", kernel.router.handled[0])
    }
}

func TestModule_RegisterHttpRoutesHonoursCustomName(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    NewModule(ModuleConfig{
        MetricsHandler:   nethttp.NewServeMux(),
        MetricsPath:      "/metrics",
        MetricsRouteName: "custom.metrics",
    }).RegisterHttpRoutes(kernel)

    if 1 != len(kernel.router.handled) || "custom.metrics GET /metrics" != kernel.router.handled[0] {
        t.Fatalf("expected the custom metrics route, got %v", kernel.router.handled)
    }
}
