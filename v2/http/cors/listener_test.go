package cors

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/v2/clock"
    clockcontract "github.com/precision-soft/melody/v2/clock/contract"
    "github.com/precision-soft/melody/v2/config"
    configcontract "github.com/precision-soft/melody/v2/config/contract"
    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/event"
    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    "github.com/precision-soft/melody/v2/http"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    "github.com/precision-soft/melody/v2/runtime"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    "github.com/precision-soft/melody/v2/security"
    "github.com/precision-soft/melody/v2/session"
    sessioncontract "github.com/precision-soft/melody/v2/session/contract"
)

func newTestRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logging.NewNopLogger())
    return runtime.New(context.Background(), scope, serviceContainer)
}

func TestRegisterResponseListener_AppliesHeadersToErrorResponse(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterResponseListener(dispatcher, DefaultService())

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    request.Header.Set("Origin", "https://example.com")
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    errorResponse := http.EmptyResponse(nethttp.StatusInternalServerError)
    payload := http.NewKernelResponseEvent(melodyRequest, errorResponse)

    _, err := dispatcher.DispatchName(newTestRuntime(), kernelcontract.EventKernelResponse, payload)
    if nil != err {
        t.Fatalf("unexpected dispatch error: %v", err)
    }

    if "" == errorResponse.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected CORS headers applied to error response by listener")
    }

    if 1 != len(errorResponse.Headers().Values("Vary")) {
        t.Fatalf("expected a single Vary header, got: %v", errorResponse.Headers().Values("Vary"))
    }
}

func TestRegisterResponseListener_SkipsWhenOriginMissing(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterResponseListener(dispatcher, DefaultService())

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    response := http.EmptyResponse(200)
    payload := http.NewKernelResponseEvent(melodyRequest, response)

    _, err := dispatcher.DispatchName(newTestRuntime(), kernelcontract.EventKernelResponse, payload)
    if nil != err {
        t.Fatalf("unexpected dispatch error: %v", err)
    }

    if "" != response.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected no CORS headers when Origin absent")
    }

    if "Origin" != response.Headers().Get("Vary") {
        t.Fatalf("expected Vary: Origin when Origin absent, got: %q", response.Headers().Get("Vary"))
    }
}

func TestRegisterResponseListener_SkipsDisallowedOrigin(t *testing.T) {
    service := NewService(Config{AllowOrigins: []string{"https://allowed.example.com"}})

    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    RegisterResponseListener(dispatcher, service)

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    request.Header.Set("Origin", "https://blocked.example.com")
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    response := http.EmptyResponse(200)
    payload := http.NewKernelResponseEvent(melodyRequest, response)

    _, err := dispatcher.DispatchName(newTestRuntime(), kernelcontract.EventKernelResponse, payload)
    if nil != err {
        t.Fatalf("unexpected dispatch error: %v", err)
    }

    if "" != response.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected no CORS headers for disallowed origin")
    }

    if "Origin" != response.Headers().Get("Vary") {
        t.Fatalf("expected Vary: Origin for disallowed origin, got: %q", response.Headers().Get("Vary"))
    }
}

func newPreflightTestRequest(path string) *nethttp.Request {
    request := httptest.NewRequest(nethttp.MethodOptions, path, nil)
    request.Header.Set("Origin", "https://app.example.com")
    request.Header.Set("Access-Control-Request-Method", nethttp.MethodPost)

    return request
}

func TestRegisterRequestListener_AnswersAWellFormedPreflightFromAnAllowedOrigin(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterRequestListener(dispatcher, RestrictiveService([]string{"https://app.example.com"}))

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(newPreflightTestRequest("/api/item"))
    requestEvent := http.NewKernelRequestEvent(newTestRuntime(), melodyRequest)

    _, err := dispatcher.DispatchName(newTestRuntime(), kernelcontract.EventKernelRequest, requestEvent)
    if nil != err {
        t.Fatalf("unexpected dispatch error: %v", err)
    }

    response := requestEvent.Response()
    if nil == response || nethttp.StatusNoContent != response.StatusCode() {
        t.Fatalf("expected the preflight to be answered 204, got %#v", response)
    }

    if "" == response.Headers().Get("Access-Control-Allow-Methods") {
        t.Fatalf("expected the preflight answer to carry the allow-methods header")
    }
}

func TestRegisterRequestListener_FallsThroughForADisallowedOrigin(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterRequestListener(dispatcher, RestrictiveService([]string{"https://app.example.com"}))

    request := newPreflightTestRequest("/api/item")
    request.Header.Set("Origin", "https://evil.example.com")

    requestEvent := http.NewKernelRequestEvent(newTestRuntime(), testhelper.NewHttpTestRequestFromHttpRequest(request))

    _, err := dispatcher.DispatchName(newTestRuntime(), kernelcontract.EventKernelRequest, requestEvent)
    if nil != err {
        t.Fatalf("unexpected dispatch error: %v", err)
    }

    if nil != requestEvent.Response() {
        t.Fatalf("expected a disallowed origin to fall through to security, got %#v", requestEvent.Response())
    }
}

func TestRegisterRequestListener_FallsThroughForAnOptionsRequestWithoutARequestedMethod(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterRequestListener(dispatcher, RestrictiveService([]string{"https://app.example.com"}))

    request := httptest.NewRequest(nethttp.MethodOptions, "/api/item", nil)
    request.Header.Set("Origin", "https://app.example.com")

    requestEvent := http.NewKernelRequestEvent(newTestRuntime(), testhelper.NewHttpTestRequestFromHttpRequest(request))

    _, err := dispatcher.DispatchName(newTestRuntime(), kernelcontract.EventKernelRequest, requestEvent)
    if nil != err {
        t.Fatalf("unexpected dispatch error: %v", err)
    }

    if nil != requestEvent.Response() {
        t.Fatalf("expected an OPTIONS request that is not a preflight to fall through, got %#v", requestEvent.Response())
    }
}

/* a preflight from an allowed origin that a listener ahead of this one already answered — the rate limiter's 429 among them — is decorated with the cross-origin headers rather than left opaque: the earlier listener's status stands, but a browser can now read the refusal as a rate limit instead of a bare cors failure. */
func TestRegisterRequestListener_DecoratesAnAlreadyAnsweredPreflight(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterRequestListener(dispatcher, RestrictiveService([]string{"https://app.example.com"}))

    requestEvent := http.NewKernelRequestEvent(newTestRuntime(), testhelper.NewHttpTestRequestFromHttpRequest(newPreflightTestRequest("/api/item")))

    alreadySetResponse := http.JsonErrorResponse(nethttp.StatusTooManyRequests, "rate limited")
    requestEvent.SetResponse(alreadySetResponse)

    _, err := dispatcher.DispatchName(newTestRuntime(), kernelcontract.EventKernelRequest, requestEvent)
    if nil != err {
        t.Fatalf("unexpected dispatch error: %v", err)
    }

    if alreadySetResponse != requestEvent.Response() {
        t.Fatalf("expected the earlier listener's response object to stay, got %#v", requestEvent.Response())
    }

    if nethttp.StatusTooManyRequests != requestEvent.Response().StatusCode() {
        t.Fatalf("expected the earlier listener's 429 status to stand, got %d", requestEvent.Response().StatusCode())
    }

    if "" == requestEvent.Response().Headers().Get("Access-Control-Allow-Methods") {
        t.Fatalf("expected the already-answered preflight to be decorated with the allow-methods header")
    }
    if "" == requestEvent.Response().Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected the already-answered preflight to be decorated with the allow-origin header")
    }
}

type corsListenerTestEnvironmentSource struct {
    values map[string]string
}

func (instance *corsListenerTestEnvironmentSource) Load() (map[string]string, error) {
    return instance.values, nil
}

func newCorsListenerTestContainer() containercontract.Container {
    serviceContainer := container.NewContainer()

    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logging.NewNopLogger(), nil
        },
    )

    serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            environment, err := config.NewEnvironment(
                &corsListenerTestEnvironmentSource{
                    values: map[string]string{
                        config.EnvKey: config.EnvDevelopment,
                    },
                },
            )
            if nil != err {
                return nil, err
            }

            return config.NewConfiguration(environment, "/tmp/melody")
        },
    )

    serviceContainer.MustRegister(
        session.ServiceSessionManager,
        func(resolver containercontract.Resolver) (sessioncontract.Manager, error) {
            return session.NewManager(session.NewInMemoryStorage(), 30*time.Minute), nil
        },
    )

    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return event.NewEventDispatcher(clock.NewSystemClock()), nil
        },
    )

    return serviceContainer
}

type corsListenerTestKernel struct {
    eventDispatcher eventcontract.EventDispatcher
}

func (instance *corsListenerTestKernel) Environment() string { return "test" }

func (instance *corsListenerTestKernel) DebugMode() bool { return true }

func (instance *corsListenerTestKernel) ServiceContainer() containercontract.Container { return nil }

func (instance *corsListenerTestKernel) EventDispatcher() eventcontract.EventDispatcher {
    return instance.eventDispatcher
}

func (instance *corsListenerTestKernel) Config() configcontract.Configuration { return nil }

func (instance *corsListenerTestKernel) HttpKernel() httpcontract.Kernel { return nil }

func (instance *corsListenerTestKernel) HttpRouter() httpcontract.Router { return nil }

func (instance *corsListenerTestKernel) Clock() clockcontract.Clock { return nil }

var _ kernelcontract.Kernel = (*corsListenerTestKernel)(nil)

func newAccessControlledCorsKernel(registerListenerDoors bool) nethttp.Handler {
    serviceContainer := newCorsListenerTestContainer()

    router := http.NewRouter()
    router.Handle(
        nethttp.MethodPost,
        "/api/item",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return http.TextResponse(nethttp.StatusCreated, "created"), nil
        },
    )

    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)

    registry := security.NewFirewallRegistry(
        security.NewCompiledConfiguration(
            nil,
            security.NewAccessControl(security.NewAccessControlRule("/api", "ROLE_USER")),
        ),
    )
    security.RegisterKernelAccessControlListener(&corsListenerTestKernel{eventDispatcher: dispatcher}, registry)

    service := RestrictiveService([]string{"https://app.example.com"})

    if true == registerListenerDoors {
        RegisterListeners(dispatcher, service)
    }

    kernelInstance := http.NewKernel(router)
    kernelInstance.Use(Middleware(service))

    return kernelInstance.ServeHttp(serviceContainer)
}

func TestRegisterListeners_AnswersThePreflightTheAccessControlWouldOtherwiseRefuse(t *testing.T) {
    withoutListenerDoors := newAccessControlledCorsKernel(false)

    refusedRecorder := httptest.NewRecorder()
    withoutListenerDoors.ServeHTTP(refusedRecorder, newPreflightTestRequest("/api/item"))

    if nethttp.StatusUnauthorized != refusedRecorder.Code {
        t.Fatalf("expected the middleware door alone to leave the preflight refused 401, got %d", refusedRecorder.Code)
    }

    withListenerDoors := newAccessControlledCorsKernel(true)

    answeredRecorder := httptest.NewRecorder()
    withListenerDoors.ServeHTTP(answeredRecorder, newPreflightTestRequest("/api/item"))

    if nethttp.StatusNoContent != answeredRecorder.Code {
        t.Fatalf("expected the request listener to answer the preflight ahead of access control, got %d", answeredRecorder.Code)
    }

    if "" == answeredRecorder.Header().Get("Access-Control-Allow-Methods") {
        t.Fatalf("expected the preflight answer to carry the allow-methods header")
    }
}

func TestRegisterListeners_WiresTheResponseDoorToo(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterListeners(dispatcher, RestrictiveService([]string{"https://app.example.com"}))

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    request.Header.Set("Origin", "https://app.example.com")
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    errorResponse := http.EmptyResponse(nethttp.StatusInternalServerError)
    responseEvent := http.NewKernelResponseEvent(melodyRequest, errorResponse)

    _, err := dispatcher.DispatchName(newTestRuntime(), kernelcontract.EventKernelResponse, responseEvent)
    if nil != err {
        t.Fatalf("unexpected dispatch error: %v", err)
    }

    if "https://app.example.com" != errorResponse.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected the response door to decorate the error response, got %q", errorResponse.Headers().Get("Access-Control-Allow-Origin"))
    }
}
