package opentelemetry

import (
    nethttp "net/http"
    "testing"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

type spyMiddlewareRegistrar struct {
    count int
}

func (instance *spyMiddlewareRegistrar) Use(middlewares ...httpcontract.Middleware) {
    instance.count += len(middlewares)
}

func (instance *spyMiddlewareRegistrar) UseWithPriority(priority int, middlewares ...httpcontract.Middleware) {
    instance.count += len(middlewares)
}

type spyRouter struct {
    httpcontract.Router
    handled []string
}

func (instance *spyRouter) HandleNamed(name string, method string, pattern string, handler httpcontract.Handler) {
    instance.handled = append(instance.handled, name+" "+method+" "+pattern)
}

type spyKernel struct {
    kernelcontract.Kernel
    router *spyRouter
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

/* inverted from the old skip-the-nil pin: a skipped observability middleware has no later consumer to fail loudly, so the wiring mistake (typically a discarded constructor error) must be refused at boot rather than served as an empty-but-healthy dashboard. */
func TestModule_RegisterHttpMiddlewaresRefusesANilEntryAtBoot(t *testing.T) {
    registrar := &spyMiddlewareRegistrar{}

    defer func() {
        if nil == recover() {
            t.Fatal("expected a nil middleware entry to be refused at boot instead of silently disarming instrumentation")
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

func TestModule_RegisterHttpRoutesNoHandler(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    NewModule(ModuleConfig{MetricsPath: "/metrics"}).RegisterHttpRoutes(kernel)
    if 0 != len(kernel.router.handled) {
        t.Fatalf("expected no route without a handler, got %v", kernel.router.handled)
    }

    kernel = &spyKernel{router: &spyRouter{}}
    NewModule(ModuleConfig{MetricsHandler: nethttp.NewServeMux()}).RegisterHttpRoutes(kernel)
    if 0 != len(kernel.router.handled) {
        t.Fatalf("expected no route without a path, got %v", kernel.router.handled)
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
