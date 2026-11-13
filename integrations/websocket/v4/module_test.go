package websocket

import (
    "context"
    "fmt"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

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

func TestModule_NameAndDescription(t *testing.T) {
    module := NewModule(ModuleConfig{})

    if "websocket" != module.Name() {
        t.Fatalf("Name() = %q, want %q", module.Name(), "websocket")
    }

    if "" == module.Description() {
        t.Fatal("Description() must not be empty")
    }
}

/* an unregistered route has no later consumer to fail loudly, since clients get 404 while boot reads healthy, so a missing hub is refused at boot; the module refuses it before NewStreamHandler's own guard, so the refusal names the stream's index */
func TestModule_RegisterHttpRoutesRefusesAMissingHubAtBoot(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    assertRefusalNamesField(t, "ModuleConfig.Streams[0].Hub", func() {
        NewModule(ModuleConfig{Streams: []StreamConfig{{Path: "/stream"}}}).RegisterHttpRoutes(kernel)
    })
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

    NewModule(ModuleConfig{Streams: []StreamConfig{{
        Hub:     melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}),
        Options: Options{IdleTimeout: bound.Of(30 * time.Second)},
    }}}).RegisterHttpRoutes(kernel)
}

func TestModule_RegisterHttpRoutesUsesDefaultName(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    NewModule(ModuleConfig{Streams: []StreamConfig{{
        Hub:     melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}),
        Path:    "/stream",
        Options: Options{IdleTimeout: bound.Of(30 * time.Second)},
    }}}).RegisterHttpRoutes(kernel)

    if 1 != len(kernel.router.handled) || defaultStreamRouteName+" GET /stream" != kernel.router.handled[0] {
        t.Fatalf("expected the default stream route, got %v", kernel.router.handled)
    }
}

func TestModule_RegisterHttpRoutesHonoursCustomName(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    NewModule(ModuleConfig{Streams: []StreamConfig{{
        Hub:       melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}),
        Path:      "/stream",
        RouteName: "custom.stream",
        Options:   Options{IdleTimeout: bound.Of(30 * time.Second)},
    }}}).RegisterHttpRoutes(kernel)

    if 1 != len(kernel.router.handled) || "custom.stream GET /stream" != kernel.router.handled[0] {
        t.Fatalf("expected the custom stream route, got %v", kernel.router.handled)
    }
}

func TestModule_RegisterHttpRoutesAcceptsEmptyOptions(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    NewModule(ModuleConfig{Streams: []StreamConfig{{
        Hub:  melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}),
        Path: "/stream",
    }}}).RegisterHttpRoutes(kernel)

    if 1 != len(kernel.router.handled) {
        t.Fatalf("expected the stream route registered, got %v", kernel.router.handled)
    }
}

func TestModule_RegisterHttpRoutesRefusesASubSecondIdleTimeout(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected a panic for a module wired with a sub-second IdleTimeout")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected the panic value to be an error, got %T", recovered)
        }

        if false == strings.Contains(fmt.Sprintf("%v", exception.LogContext(recoveredErr, exception.LogContextConfig{})), "Options.IdleTimeout") {
            t.Fatalf("expected the refusal to name Options.IdleTimeout, got %v", recoveredErr)
        }
    }()

    kernel := &spyKernel{router: &spyRouter{}}

    NewModule(ModuleConfig{Streams: []StreamConfig{{
        Hub:     melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}),
        Options: Options{IdleTimeout: bound.Of(500 * time.Millisecond)},
        Path:    "/stream",
    }}}).RegisterHttpRoutes(kernel)
}

func TestModule_RegisterHttpRoutesRefusesAModuleWithoutAStream(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    assertRefusalNamesField(t, "ModuleConfig.Streams", func() {
        NewModule(ModuleConfig{}).RegisterHttpRoutes(kernel)
    })

    if 0 != len(kernel.router.handled) {
        t.Fatalf("expected no route, got %v", kernel.router.handled)
    }
}

/* the second stream's defect is judged before the first stream registers, so a refused module leaves no route behind */
func TestModule_RegisterHttpRoutesRefusesAStreamWithoutAPathNamingItsIndex(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    assertRefusalNamesField(t, "ModuleConfig.Streams[1].Path", func() {
        NewModule(ModuleConfig{Streams: []StreamConfig{
            {Hub: melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), Path: "/a", RouteName: "stream.a"},
            {Hub: melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), RouteName: "stream.b"},
        }}).RegisterHttpRoutes(kernel)
    })

    if 0 != len(kernel.router.handled) {
        t.Fatalf("expected no route, got %v", kernel.router.handled)
    }
}

func TestModule_RegisterHttpRoutesRefusesAnUnnamedStreamAmongSeveral(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    assertRefusalNamesField(t, "ModuleConfig.Streams[1].RouteName", func() {
        NewModule(ModuleConfig{Streams: []StreamConfig{
            {Hub: melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), Path: "/a", RouteName: "stream.a"},
            {Hub: melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), Path: "/b"},
        }}).RegisterHttpRoutes(kernel)
    })

    if 0 != len(kernel.router.handled) {
        t.Fatalf("expected no route, got %v", kernel.router.handled)
    }
}

/* a later stream's nil hub and invalid option are refused before the first stream registers, the hub naming its index */
func TestModule_RegisterHttpRoutesRefusesALaterStreamsHubAndOptionsBeforeAnyRoute(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    assertRefusalNamesField(t, "ModuleConfig.Streams[1].Hub", func() {
        NewModule(ModuleConfig{Streams: []StreamConfig{
            {Hub: melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), Path: "/a", RouteName: "stream.a"},
            {Path: "/b", RouteName: "stream.b"},
        }}).RegisterHttpRoutes(kernel)
    })

    func() {
        defer func() {
            if nil == recover() {
                t.Fatal("expected the sub-second IdleTimeout refused")
            }
        }()

        NewModule(ModuleConfig{Streams: []StreamConfig{
            {Hub: melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), Path: "/a", RouteName: "stream.a"},
            {Hub: melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), Path: "/b", RouteName: "stream.b", Options: Options{IdleTimeout: bound.Of(500 * time.Millisecond)}},
        }}).RegisterHttpRoutes(kernel)
    }()

    if 0 != len(kernel.router.handled) {
        t.Fatalf("expected no route, got %v", kernel.router.handled)
    }
}

func TestModule_RegisterHttpRoutesRegistersEveryStream(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    NewModule(ModuleConfig{Streams: []StreamConfig{
        {Hub: melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), Path: "/a", RouteName: "stream.a"},
        {Hub: melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), Path: "/b", RouteName: "stream.b"},
    }}).RegisterHttpRoutes(kernel)

    if 2 != len(kernel.router.handled) || "stream.a GET /a" != kernel.router.handled[0] || "stream.b GET /b" != kernel.router.handled[1] {
        t.Fatalf("expected both stream routes, got %v", kernel.router.handled)
    }
}

func assertRefusalNamesField(t *testing.T, field string, register func()) {
    t.Helper()

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected a refusal naming %s", field)
        }

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected the panic value to be an error, got %T", recovered)
        }

        if field != exception.LogContext(recoveredErr, exception.LogContextConfig{})["field"] {
            t.Fatalf("expected the refusal to name %s, got %v", field, exception.LogContext(recoveredErr, exception.LogContextConfig{}))
        }
    }()

    register()
}
