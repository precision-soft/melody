package security

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "reflect"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

var strictLeeway = bound.Of(time.Duration(0))

func expectPanicNamingField(t *testing.T, field string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatalf("expected a refusal naming %s, got no error panic", field)
        }

        expectErrorNamingField(t, recoveredErr, field)
    }()

    callback()
}

func expectPanicNamingArgument(t *testing.T, door string, argument string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatalf("expected %s to refuse %s, got no error panic", door, argument)
        }

        context := exception.LogContext(recoveredErr, exception.LogContextConfig{})
        if door != context["door"] || argument != context["argument"] {
            t.Fatalf("expected %s to refuse %s, got %v: %v", door, argument, context, recoveredErr)
        }
    }()

    callback()
}

func expectErrorNamingField(t *testing.T, err error, field string) {
    t.Helper()

    if nil == err {
        t.Fatalf("expected a refusal naming %s, got none", field)
    }

    if field != exception.LogContext(err, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected the refusal to name %s, got %v: %v", field, exception.LogContext(err, exception.LogContextConfig{}), err)
    }
}

type recordedLogRecord struct {
    level   loggingcontract.Level
    message string
    context loggingcontract.Context
}

/* recordingLogger captures every record with its level, so a test can assert the SEVERITY a path logged at, not only that it logged. */
type recordingLogger struct {
    mutex   sync.Mutex
    records []recordedLogRecord
}

func (instance *recordingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.records = append(instance.records, recordedLogRecord{level: level, message: message, context: context})
}

func (instance *recordingLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *recordingLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *recordingLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *recordingLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *recordingLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

func (instance *recordingLogger) recordsAtLevel(level loggingcontract.Level) []recordedLogRecord {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    matched := []recordedLogRecord{}
    for _, record := range instance.records {
        if level == record.level {
            matched = append(matched, record)
        }
    }

    return matched
}

func runtimeWithRecordingLogger() (runtimecontract.Runtime, *recordingLogger) {
    logger := &recordingLogger{}

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger, container.OverrideOptions{})

    return runtime.New(context.Background(), scope, serviceContainer), logger
}

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
    request := httptest.NewRequest(method, "http://example.com"+path, nil)

    for key, value := range headers {
        request.Header.Set(key, value)
    }

    return http.NewRequest(
        request,
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
    request := httptest.NewRequest(nethttp.MethodGet, "http://example.com"+path, nil)

    return http.NewRequest(
        request,
        nil,
        nil,
        &firewallTestRequestContext{
            requestIdValue: "test",
            startedAtValue: time.Now(),
        },
    )
}

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

func (instance *testScope) Get(ctx context.Context, serviceName string) (any, error) {
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

func (instance *testScope) MustGet(ctx context.Context, serviceName string) any {
    value, err := instance.Get(ctx, serviceName)
    if nil != err {
        panic(err)
    }

    return value
}

func (instance *testScope) GetByType(ctx context.Context, targetType reflect.Type) (any, error) {
    return nil, errors.New("not implemented")
}

func (instance *testScope) MustGetByType(ctx context.Context, targetType reflect.Type) any {
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

func (instance *testScope) OverrideInstance(serviceName string, value any, options containercontract.OverrideOptions) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return errors.New("scope is closed")
    }

    instance.valueByKey[serviceName] = value
    return nil
}

func (instance *testScope) MustOverrideInstance(serviceName string, value any, options containercontract.OverrideOptions) {
    err := instance.OverrideInstance(serviceName, value, options)
    if nil != err {
        panic(err)
    }
}

func (instance *testScope) OverrideProtectedInstance(serviceName string, value any, options containercontract.OverrideOptions) error {
    return instance.OverrideInstance(serviceName, value, options)
}

func (instance *testScope) MustOverrideProtectedInstance(serviceName string, value any, options containercontract.OverrideOptions) {
    err := instance.OverrideProtectedInstance(serviceName, value, options)
    if nil != err {
        panic(err)
    }
}

func (instance *testScope) RegisterScoped(serviceName string, provider any, options containercontract.RegisterOptions) error {
    return errors.New("not implemented")
}

func (instance *testScope) MustRegisterScoped(serviceName string, provider any, options containercontract.RegisterOptions) {
    panic(errors.New("not implemented"))
}

func (instance *testScope) CloseWithContext(closeContext context.Context) error {
    return instance.Close()
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
        serviceContainer: container.NewContainer(container.ContainerConfig{}),
        eventDispatcher:  event.NewEventDispatcher(clock.NewSystemClock()),
        httpKernel:       http.NewKernel(httpRouter, http.KernelConfig{}),
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

func (instance *testKernel) Context() context.Context {
    return context.Background()
}

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
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    overrideErr := scope.OverrideInstance(
        logging.ServiceLogger,
        logging.NewNopLogger(),
        container.OverrideOptions{},
    )
    if nil != overrideErr {
        panic(overrideErr)
    }

    return runtime.New(context.Background(), scope, serviceContainer)
}

func testRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func bearerRequest(tokenString string) httpcontract.Request {
    request := httptest.NewRequest("GET", "/api/resource", nil)
    if "" != tokenString {
        request.Header.Set("Authorization", "Bearer "+tokenString)
    }

    return testhelper.NewHttpTestRequestFromHttpRequest(request)
}

/* unauthenticatedRoledToken carries roles while answering IsAuthenticated false — the shape a "remembered" or half-logged-in token takes. It is what proves the voters refuse a token that reports roles it has not authenticated for; the in-package tokens never combine the two. */
type unauthenticatedRoledToken struct {
    roles []string
}

func (instance *unauthenticatedRoledToken) UserIdentifier() string     { return "u1" }
func (instance *unauthenticatedRoledToken) Roles() []string            { return instance.roles }
func (instance *unauthenticatedRoledToken) IsAuthenticated() bool      { return false }
func (instance *unauthenticatedRoledToken) Scope() map[string]any      { return map[string]any{} }
func (instance *unauthenticatedRoledToken) Attributes() map[string]any { return map[string]any{} }

var _ securitycontract.Token = (*unauthenticatedRoledToken)(nil)

func newTestCompiledFirewallWithRoleHierarchy(name string, roleHierarchy *RoleHierarchy) *CompiledFirewall {
    return NewCompiledFirewall(CompiledFirewallConfig{
        Name:               name,
        Matcher:            NewPathPrefixMatcher("/"),
        MatcherDescription: "prefix /",
        RoleHierarchy:      roleHierarchy,
    })
}

type servedSecurityEnvironmentSource struct{}

func (instance *servedSecurityEnvironmentSource) Load() (map[string]string, error) {
    return map[string]string{config.EnvKey: config.EnvProduction}, nil
}

func serveSecurityRequest(t *testing.T, kernelInstance *testKernel, path string) *httptest.ResponseRecorder {
    t.Helper()

    serviceContainer := container.NewContainer(container.ContainerConfig{})

    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logging.NewNopLogger(), nil
        },
        container.RegisterOptions{},
    )
    serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            environment, environmentErr := config.NewEnvironment(&servedSecurityEnvironmentSource{})
            if nil != environmentErr {
                return nil, environmentErr
            }

            return config.NewConfiguration(environment, t.TempDir())
        },
        container.RegisterOptions{},
    )
    serviceContainer.MustRegister(
        session.ServiceSessionManager,
        func(resolver containercontract.Resolver) (sessioncontract.Manager, error) {
            return session.NewManager(session.NewInMemoryStorage(session.InMemoryStorageConfig{}), session.ManagerConfig{Ttl: bound.Of(30*time.Minute)}), nil
        },
        container.RegisterOptions{},
    )
    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return kernelInstance.eventDispatcher, nil
        },
        container.RegisterOptions{},
    )

    kernelInstance.httpRouter.Handle(
        nethttp.MethodGet,
        path,
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return http.TextResponse(nethttp.StatusOK, "served"), nil
        },
    )

    request := httptest.NewRequest(nethttp.MethodGet, path, nil)
    request.Header.Set("Accept", "application/json")

    recorder := httptest.NewRecorder()
    kernelInstance.httpKernel.ServeHttp(serviceContainer).ServeHTTP(recorder, request)

    return recorder
}
