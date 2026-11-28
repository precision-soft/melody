package websocket

import (
    "context"
    "errors"
    "fmt"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    coderwebsocket "github.com/coder/websocket"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

type spyRouter struct {
    httpcontract.Router
    handled  []string
    handlers []httpcontract.Handler
}

func (instance *spyRouter) Handle(pattern string, handler httpcontract.Handler, options httpcontract.RouteOptions) {
    instance.handled = append(instance.handled, options.Name+" "+strings.Join(options.Methods, ",")+" "+pattern)
    instance.handlers = append(instance.handlers, handler)
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

func TestModule_RegisterHttpRoutesRefusesAMissingHubAtBoot(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    assertRefusalNamesField(t, "ModuleConfig.Streams[0].Hub", func() {
        NewModule(ModuleConfig{Streams: []StreamConfig{{Path: "/stream"}}}).RegisterHttpRoutes(kernel)
    })
}

/* a valid IdleTimeout deliberately, so NewStreamHandler's own panic cannot answer for the path guard */
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
        Hub:     fixedHub(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})),
        Handler: StreamHandlerConfig{IdleTimeout: bound.Of(30 * time.Second)},
    }}}).RegisterHttpRoutes(kernel)
}

func TestModule_RegisterHttpRoutesUsesDefaultName(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    NewModule(ModuleConfig{Streams: []StreamConfig{{
        Hub:     fixedHub(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})),
        Path:    "/stream",
        Handler: StreamHandlerConfig{IdleTimeout: bound.Of(30 * time.Second)},
    }}}).RegisterHttpRoutes(kernel)

    if 1 != len(kernel.router.handled) || defaultStreamRouteName+" GET /stream" != kernel.router.handled[0] {
        t.Fatalf("expected the default stream route, got %v", kernel.router.handled)
    }
}

func TestModule_RegisterHttpRoutesHonoursCustomName(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    NewModule(ModuleConfig{Streams: []StreamConfig{{
        Hub:       fixedHub(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})),
        Path:      "/stream",
        RouteName: "custom.stream",
        Handler:   StreamHandlerConfig{IdleTimeout: bound.Of(30 * time.Second)},
    }}}).RegisterHttpRoutes(kernel)

    if 1 != len(kernel.router.handled) || "custom.stream GET /stream" != kernel.router.handled[0] {
        t.Fatalf("expected the custom stream route, got %v", kernel.router.handled)
    }
}

func TestModule_RegisterHttpRoutesAcceptsAnEmptyHandlerConfig(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    NewModule(ModuleConfig{Streams: []StreamConfig{{
        Hub:  fixedHub(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})),
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

        if false == strings.Contains(fmt.Sprintf("%v", exception.LogContext(recoveredErr, exception.LogContextOptions{})), "StreamHandlerConfig.IdleTimeout") {
            t.Fatalf("expected the refusal to name StreamHandlerConfig.IdleTimeout, got %v", recoveredErr)
        }
    }()

    kernel := &spyKernel{router: &spyRouter{}}

    NewModule(ModuleConfig{Streams: []StreamConfig{{
        Hub:     fixedHub(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})),
        Handler: StreamHandlerConfig{IdleTimeout: bound.Of(500 * time.Millisecond)},
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

func TestModule_RegisterHttpRoutesRefusesAStreamWithoutAPathNamingItsIndex(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    assertRefusalNamesField(t, "ModuleConfig.Streams[1].Path", func() {
        NewModule(ModuleConfig{Streams: []StreamConfig{
            {Hub: fixedHub(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})), Path: "/a", RouteName: "stream.a"},
            {Hub: fixedHub(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})), RouteName: "stream.b"},
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
            {Hub: fixedHub(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})), Path: "/a", RouteName: "stream.a"},
            {Hub: fixedHub(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})), Path: "/b"},
        }}).RegisterHttpRoutes(kernel)
    })

    if 0 != len(kernel.router.handled) {
        t.Fatalf("expected no route, got %v", kernel.router.handled)
    }
}

func TestModule_RegisterHttpRoutesRefusesALaterStreamsHubAndHandlerConfigBeforeAnyRoute(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    assertRefusalNamesField(t, "ModuleConfig.Streams[1].Hub", func() {
        NewModule(ModuleConfig{Streams: []StreamConfig{
            {Hub: fixedHub(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})), Path: "/a", RouteName: "stream.a"},
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
            {Hub: fixedHub(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})), Path: "/a", RouteName: "stream.a"},
            {Hub: fixedHub(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})), Path: "/b", RouteName: "stream.b", Handler: StreamHandlerConfig{IdleTimeout: bound.Of(500 * time.Millisecond)}},
        }}).RegisterHttpRoutes(kernel)
    }()

    if 0 != len(kernel.router.handled) {
        t.Fatalf("expected no route, got %v", kernel.router.handled)
    }
}

func TestModule_RegisterHttpRoutesRegistersEveryStream(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    NewModule(ModuleConfig{Streams: []StreamConfig{
        {Hub: fixedHub(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})), Path: "/a", RouteName: "stream.a"},
        {Hub: fixedHub(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})), Path: "/b", RouteName: "stream.b"},
    }}).RegisterHttpRoutes(kernel)

    if 2 != len(kernel.router.handled) || "stream.a GET /a" != kernel.router.handled[0] || "stream.b GET /b" != kernel.router.handled[1] {
        t.Fatalf("expected both stream routes, got %v", kernel.router.handled)
    }
}

func TestModule_TheHubIsResolvedPerConnectionThroughTheConnectionsRuntime(t *testing.T) {
    kernel := &spyKernel{router: &spyRouter{}}

    var calls atomic.Int64
    var seenRuntime atomic.Pointer[containercontract.Runtime]
    var builtHub atomic.Pointer[melodyhttp.ServerSentEventHub]

    NewModule(ModuleConfig{Streams: []StreamConfig{{
        Hub: func(runtimeInstance containercontract.Runtime) (*melodyhttp.ServerSentEventHub, error) {
            calls.Add(1)
            seenRuntime.Store(&runtimeInstance)

            return builtHub.Load(), nil
        },
        Path:    "/stream",
        Handler: StreamHandlerConfig{TopicResolver: func(request httpcontract.Request) string { return "demo" }, OriginPatterns: []string{"*"}},
    }}}).RegisterHttpRoutes(kernel)

    if 0 != calls.Load() {
        t.Fatalf("expected no hub resolution at the route registration, got %d", calls.Load())
    }

    /* the hub exists only after the routes registered, as a provider built at boot builds it */
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    builtHub.Store(hub)

    var serveRuntime atomic.Pointer[containercontract.Runtime]
    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        runtimeInstance := runtime.NewRequest(request.Context(), container.NewContainer(container.ContainerConfig{}).NewScope())
        serveRuntime.Store(&runtimeInstance)
        kernel.router.handlers[0](runtimeInstance, writer, melodyhttp.NewRequest(request, runtimeInstance, melodyhttp.RequestConfig{}))
    }))
    defer server.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    connection, _, dialErr := coderwebsocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
    if nil != dialErr {
        t.Fatalf("dial: %v", dialErr)
    }
    defer connection.CloseNow()

    subscribeDeadline := time.Now().Add(2 * time.Second)
    for 1 > hub.SubscriberCount("demo") {
        if true == time.Now().After(subscribeDeadline) {
            t.Fatal("the stream did not subscribe to the hub its provider answered")
        }
        time.Sleep(time.Millisecond)
    }

    if 1 != hub.Broadcast(broadcastTestRuntime(), "demo", melodyhttp.ServerSentEvent{Data: "hello"}) {
        t.Fatal("expected the broadcast to reach the connection")
    }

    _, payload, readErr := connection.Read(ctx)
    if nil != readErr || "hello" != string(payload) {
        t.Fatalf("expected the broadcast frame, got %q %v", payload, readErr)
    }

    if 1 != calls.Load() || *seenRuntime.Load() != *serveRuntime.Load() {
        t.Fatalf("expected one resolution under the connection's runtime, got %d", calls.Load())
    }

    connection.Close(coderwebsocket.StatusNormalClosure, "")
}

func TestModule_AHubProviderErrorIsTheRequestsAndANilHubIsRefused(t *testing.T) {
    providerErr := errors.New("the hub provider failed")

    for _, testCase := range []struct {
        name     string
        provider func(containercontract.Runtime) (*melodyhttp.ServerSentEventHub, error)
        expected string
    }{
        {"error", func(containercontract.Runtime) (*melodyhttp.ServerSentEventHub, error) { return nil, providerErr }, "the hub provider failed"},
        {"nil hub", func(containercontract.Runtime) (*melodyhttp.ServerSentEventHub, error) { return nil, nil }, "answered no hub"},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            kernel := &spyKernel{router: &spyRouter{}}

            NewModule(ModuleConfig{Streams: []StreamConfig{{Hub: testCase.provider, Path: "/stream"}}}).RegisterHttpRoutes(kernel)

            runtimeInstance := runtime.NewRequest(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope())
            recorder := httptest.NewRecorder()
            request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "/stream", nil), runtimeInstance, melodyhttp.RequestConfig{})

            _, handlerErr := kernel.router.handlers[0](runtimeInstance, recorder, request)
            if nil == handlerErr || false == strings.Contains(handlerErr.Error(), testCase.expected) {
                t.Fatalf("expected the refusal %q, got %v", testCase.expected, handlerErr)
            }

            if nethttp.StatusSwitchingProtocols == recorder.Code {
                t.Fatal("expected the refusal before the upgrade")
            }
        })
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

        if field != exception.LogContext(recoveredErr, exception.LogContextOptions{})["field"] {
            t.Fatalf("expected the refusal to name %s, got %v", field, exception.LogContext(recoveredErr, exception.LogContextOptions{}))
        }
    }()

    register()
}
