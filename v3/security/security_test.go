package security

import (
    "context"
    "errors"
    "net/http/httptest"
    nethttp "net/http"
    "reflect"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/event"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* @info request helper */

type securityTestRequestContext struct {
    requestIdValue string
    startedAtValue time.Time
}

func (instance *securityTestRequestContext) RequestId() string {
    return instance.requestIdValue
}

func (instance *securityTestRequestContext) StartedAt() time.Time {
    return instance.startedAtValue
}

func newSecurityTestRequest(method string, path string, headers map[string]string, runtimeInstance runtimecontract.Runtime) httpcontract.Request {
    req := httptest.NewRequest(method, "http://example.com"+path, nil)

    for key, value := range headers {
        req.Header.Set(key, value)
    }

    return http.NewRequest(
        req,
        nil,
        runtimeInstance,
        &securityTestRequestContext{
            requestIdValue: "test",
            startedAtValue: time.Now(),
        },
    )
}

type firewallTestRequestContext struct {
    requestIdValue string
    startedAtValue time.Time
}

func (instance *firewallTestRequestContext) RequestId() string    { return instance.requestIdValue }
func (instance *firewallTestRequestContext) StartedAt() time.Time { return instance.startedAtValue }

func newFirewallTestRequest(path string) httpcontract.Request {
    req := httptest.NewRequest(nethttp.MethodGet, "http://example.com"+path, nil)

    return http.NewRequest(
        req,
        nil,
        nil,
        &firewallTestRequestContext{
            requestIdValue: "test",
            startedAtValue: time.Now(),
        },
    )
}

/* @info kernel and runtime harness */

type testScope struct {
    mutex      sync.RWMutex
    valueByKey map[string]any
    closed     bool
}

func newTestScope() *testScope {
    return &testScope{
        valueByKey: make(map[string]any),
    }
}

func (instance *testScope) Get(serviceName string) (any, error) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    if true == instance.closed {
        return nil, errors.New("scope is closed")
    }

    value, exists := instance.valueByKey[serviceName]
    if false == exists {
        return nil, errors.New("service not found")
    }

    return value, nil
}

func (instance *testScope) MustGet(serviceName string) any {
    value, err := instance.Get(serviceName)
    if nil != err {
        panic(err)
    }

    return value
}

func (instance *testScope) GetByType(targetType reflect.Type) (any, error) {
    return nil, errors.New("not implemented")
}

func (instance *testScope) MustGetByType(targetType reflect.Type) any {
    panic(errors.New("not implemented"))
}

func (instance *testScope) Has(serviceName string) bool {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    if true == instance.closed {
        return false
    }

    _, exists := instance.valueByKey[serviceName]
    return true == exists
}

func (instance *testScope) HasType(targetType reflect.Type) bool {
    return false
}

func (instance *testScope) OverrideInstance(serviceName string, value any) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return errors.New("scope is closed")
    }

    instance.valueByKey[serviceName] = value
    return nil
}

func (instance *testScope) MustOverrideInstance(serviceName string, value any) {
    err := instance.OverrideInstance(serviceName, value)
    if nil != err {
        panic(err)
    }
}

func (instance *testScope) OverrideProtectedInstance(serviceName string, value any) error {
    return instance.OverrideInstance(serviceName, value)
}

func (instance *testScope) MustOverrideProtectedInstance(serviceName string, value any) {
    err := instance.OverrideProtectedInstance(serviceName, value)
    if nil != err {
        panic(err)
    }
}

func (instance *testScope) Close() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.closed = true
    return nil
}

var _ containercontract.Scope = (*testScope)(nil)

type testKernel struct {
    configuration    configcontract.Configuration
    serviceContainer containercontract.Container
    eventDispatcher  eventcontract.EventDispatcher
    httpKernel       httpcontract.Kernel
    httpRouter       httpcontract.Router
    clock            clockcontract.Clock
}

func newTestKernel() *testKernel {
    httpRouter := http.NewRouter()

    return &testKernel{
        configuration:    nil,
        serviceContainer: container.NewContainer(),
        eventDispatcher:  event.NewEventDispatcher(clock.NewSystemClock()),
        httpKernel:       http.NewKernel(httpRouter),
        httpRouter:       httpRouter,
        clock:            clock.NewSystemClock(),
    }
}

func (instance *testKernel) Environment() string {
    return "test"
}

func (instance *testKernel) DebugMode() bool { return true }

func (instance *testKernel) ServiceContainer() containercontract.Container {
    return nil
}

func (instance *testKernel) EventDispatcher() eventcontract.EventDispatcher {
    return instance.eventDispatcher
}

func (instance *testKernel) Config() configcontract.Configuration { return nil }

func (instance *testKernel) HttpKernel() httpcontract.Kernel {
    return instance.httpKernel
}

func (instance *testKernel) HttpRouter() httpcontract.Router {
    return instance.httpRouter
}

func (instance *testKernel) Clock() clockcontract.Clock { return nil }

var _ kernelcontract.Kernel = (*testKernel)(nil)

func newTestRuntime() runtimecontract.Runtime {
    scope := newTestScope()
    serviceContainer := container.NewContainer()

    overrideErr := scope.OverrideInstance(
        logging.ServiceLogger,
        logging.NewNopLogger(),
    )
    if nil != overrideErr {
        panic(overrideErr)
    }

    return runtime.New(context.Background(), scope, serviceContainer)
}

func registerTestKernelExceptionListener(kernelInstance *testKernel) {
    kernelInstance.EventDispatcher().AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            exceptionEvent, ok := eventValue.Payload().(*http.KernelExceptionEvent)
            if false == ok || nil == exceptionEvent {
                return nil
            }

            if nil != exceptionEvent.Response() {
                return nil
            }

            exceptionEvent.SetResponse(
                http.JsonErrorResponse(
                    500,
                    "internal_server_error",
                ),
            )

            return nil
        },
        0,
    )
}

/* @info bearer token request helpers */

func testRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func bearerRequest(tokenString string) httpcontract.Request {
    request := httptest.NewRequest("GET", "/api/resource", nil)
    if "" != tokenString {
        request.Header.Set("Authorization", "Bearer "+tokenString)
    }

    return testhelper.NewHttpTestRequestFromHttpRequest(request)
}
