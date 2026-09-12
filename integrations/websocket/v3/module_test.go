package websocket

import (
    "strings"
    "testing"
    "time"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

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

func TestModule_NameAndDescription(t *testing.T) {
    module := NewModule(ModuleConfig{})

    if "websocket" != module.Name() {
        t.Fatalf("Name() = %q, want %q", module.Name(), "websocket")
    }

    if "" == module.Description() {
        t.Fatal("Description() must not be empty")
    }
}

/* inverted from the old skip-silently pin: an unregistered route has no later consumer to fail loudly — clients get 404 while boot reads healthy — so the missing hub or path is refused at boot instead. The nil hub is refused by NewStreamHandler's own guard, which registration reaches at this same boot moment; the module deliberately carries no shadowed sister in front of it. */
func TestModule_RegisterHttpRoutesRefusesAMissingHubAtBoot(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    defer func() {
        if nil == recover() {
            t.Fatal("expected the nil hub to be refused at boot instead of silently registering no route")
        }
    }()

    NewModule(ModuleConfig{Path: "/stream"}).RegisterHttpRoutes(kernel)
}

/* the options carry a VALID IdleTimeout deliberately: with the zero value, NewStreamHandler's own panic would answer for a disarmed path guard and the mutant would survive shadowed — the empty path must be the only refusal candidate. */
func TestModule_RegisterHttpRoutesRefusesAnEmptyPathAtBoot(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected the empty path to be refused at boot instead of silently registering no route")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError || false == strings.Contains(recoveredErr.Error(), "path is empty") {
            t.Fatalf("expected the refusal to name the empty path, got %v", recovered)
        }
    }()

    NewModule(ModuleConfig{
        Hub:     melodyhttp.NewServerSentEventHub(),
        Options: Options{IdleTimeout: 30 * time.Second},
    }).RegisterHttpRoutes(kernel)
}

func TestModule_RegisterHttpRoutesUsesDefaultName(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    NewModule(ModuleConfig{
        Hub:     melodyhttp.NewServerSentEventHub(),
        Path:    "/stream",
        Options: Options{IdleTimeout: 30 * time.Second},
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
        Options:   Options{IdleTimeout: 30 * time.Second},
    }).RegisterHttpRoutes(kernel)

    if 1 != len(kernel.router.handled) || "custom.stream GET /stream" != kernel.router.handled[0] {
        t.Fatalf("expected the custom stream route, got %v", kernel.router.handled)
    }
}

/* the module hands Options straight through, so a module wired with a zero IdleTimeout must fail its route registration at boot rather than serve connections nothing can reap. */
func TestModule_RegisterHttpRoutesRefusesAZeroIdleTimeout(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected a panic for a module wired with a zero IdleTimeout")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected the panic value to be an error, got %T", recovered)
        }

        if false == strings.Contains(recoveredErr.Error(), "IdleTimeout") {
            t.Fatalf("expected the diagnostic to name IdleTimeout, got %v", recoveredErr)
        }
    }()

    kernel := &spyKernel{router: &spyRouter{}}

    NewModule(ModuleConfig{
        Hub:  melodyhttp.NewServerSentEventHub(),
        Path: "/stream",
    }).RegisterHttpRoutes(kernel)
}
