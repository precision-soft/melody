package websocket

import (
    "testing"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

/* @info spies */

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

/* @info tests */

func TestModule_NameAndDescription(t *testing.T) {
    module := NewModule(ModuleConfig{})

    if "websocket" != module.Name() {
        t.Fatalf("Name() = %q, want %q", module.Name(), "websocket")
    }

    if "" == module.Description() {
        t.Fatal("Description() must not be empty")
    }
}

func TestModule_RegisterHttpRoutesGuards(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}
    NewModule(ModuleConfig{Path: "/stream"}).RegisterHttpRoutes(kernel)
    if 0 != len(kernel.router.handled) {
        t.Fatalf("expected no route without a hub, got %v", kernel.router.handled)
    }

    kernel = &spyKernel{router: &spyRouter{}}
    NewModule(ModuleConfig{Hub: melodyhttp.NewServerSentEventHub()}).RegisterHttpRoutes(kernel)
    if 0 != len(kernel.router.handled) {
        t.Fatalf("expected no route without a path, got %v", kernel.router.handled)
    }
}

func TestModule_RegisterHttpRoutesUsesDefaultName(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    NewModule(ModuleConfig{
        Hub:  melodyhttp.NewServerSentEventHub(),
        Path: "/stream",
    }).RegisterHttpRoutes(kernel)

    if 1 != len(kernel.router.handled) || defaultStreamRouteName+" GET /stream" != kernel.router.handled[0] {
        t.Fatalf("expected the default stream route, got %v", kernel.router.handled)
    }
}

func TestModule_RegisterHttpRoutesHonoursCustomName(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    NewModule(ModuleConfig{
        Hub:       melodyhttp.NewServerSentEventHub(),
        Path:      "/stream",
        RouteName: "custom.stream",
    }).RegisterHttpRoutes(kernel)

    if 1 != len(kernel.router.handled) || "custom.stream GET /stream" != kernel.router.handled[0] {
        t.Fatalf("expected the custom stream route, got %v", kernel.router.handled)
    }
}
