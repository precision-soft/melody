package security

import (
    "context"
    "errors"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "reflect"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/cache"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

var strictLeeway = bound.Of(time.Duration(0))

func expectPanicNamingTheArgument(t *testing.T, argument string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatalf("expected a refusal naming the %s argument, got no error panic", argument)
        }

        if context := exception.LogContext(recoveredErr, exception.LogContextConfig{}); argument != context["argument"] {
            t.Fatalf("expected a refusal naming the %s argument, got %v: %v", argument, context, recoveredErr)
        }
    }()

    callback()
}

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

func (instance *recordingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *recordingLogger) CauseChainDepth() int {
    return 8
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

func runtimeWithRecordingLogger() (containercontract.Runtime, *recordingLogger) {
    logger := &recordingLogger{}

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger, container.OverrideOptions{})

    return runtime.NewRequest(context.Background(), scope), logger
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

func newSecurityTestRequest(method string, path string, headers map[string]string, runtimeInstance containercontract.Runtime) httpcontract.Request {
    request := httptest.NewRequest(method, "http://example.com"+path, nil)

    for key, value := range headers {
        request.Header.Set(key, value)
    }

    return http.NewRequest(
        request,
        runtimeInstance,
        http.RequestConfig{
            RequestContext: &securityTestRequestContext{
            requestIdValue: "test",
            startedAtValue: time.Now(),
        },
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
        http.RequestConfig{
            RequestContext: &firewallTestRequestContext{
            requestIdValue: "test",
            startedAtValue: time.Now(),
        },
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

func (instance *testScope) Runtime() containercontract.Runtime {
    return nil
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
        eventDispatcher:  event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()}),
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

func (instance *testKernel) BootRuntime() containercontract.Runtime {
    return nil
}

func (instance *testKernel) HttpKernel() httpcontract.Kernel {
    return instance.httpKernel
}

func (instance *testKernel) HttpRouter() httpcontract.Router {
    return instance.httpRouter
}

func (instance *testKernel) Clock() clockcontract.Clock { return nil }

var _ kernelcontract.Kernel = (*testKernel)(nil)

func newTestRuntime() containercontract.Runtime {
    scope := newTestScope()

    overrideErr := scope.OverrideInstance(
        logging.ServiceLogger,
        logging.NewNopLogger(),
        container.OverrideOptions{},
    )
    if nil != overrideErr {
        panic(overrideErr)
    }

    return runtime.NewRequest(context.Background(), scope)
}

func testRuntime() containercontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    return runtime.NewRequest(context.Background(), serviceContainer.NewScope())
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

func (instance *servedSecurityEnvironmentSource) Load(ctx context.Context) (map[string]string, error) {
    return map[string]string{config.EnvironmentNameKey: config.EnvironmentProduction}, nil
}

func servedSecurityContainer(t *testing.T, kernelInstance *testKernel, logger loggingcontract.Logger) containercontract.Container {
    t.Helper()

    serviceContainer := container.NewContainer(container.ContainerConfig{})

    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logger, nil
        },
        container.RegisterOptions{},
    )
    serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            environment, environmentErr := config.NewEnvironment(context.Background(), &servedSecurityEnvironmentSource{})
            if nil != environmentErr {
                return nil, environmentErr
            }

            return config.NewConfiguration(environment, t.TempDir(), config.ConfigurationConfig{})
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

    return serviceContainer
}

func serveSecurityRequest(t *testing.T, kernelInstance *testKernel, path string) *httptest.ResponseRecorder {
    t.Helper()

    return serveSecurityRequestThrough(
        t,
        kernelInstance,
        path,
        logging.NewNopLogger(),
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return http.TextResponse(nethttp.StatusOK, "served"), nil
        },
    )
}

func serveSecurityRequestThrough(
    t *testing.T,
    kernelInstance *testKernel,
    path string,
    logger loggingcontract.Logger,
    handler func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error),
) *httptest.ResponseRecorder {
    t.Helper()

    serviceContainer := servedSecurityContainer(t, kernelInstance, logger)

    kernelInstance.httpRouter.Handle(path, handler, http.RouteOptions{Methods: []string{nethttp.MethodGet}})

    request := httptest.NewRequest(nethttp.MethodGet, path, nil)
    request.Header.Set("Accept", "application/json")

    recorder := httptest.NewRecorder()
    kernelInstance.httpKernel.ServeHttp(serviceContainer).ServeHTTP(recorder, request)

    return recorder
}

func expectUnavailableToken(t *testing.T, token securitycontract.Token) error {
    t.Helper()

    cause, unavailable := AuthenticationUnavailableCause(token)
    if false == unavailable {
        t.Fatalf("expected an unavailable token, got %T", token)
    }

    if true == token.IsAuthenticated() {
        t.Fatal("expected an unavailable token not authenticated")
    }

    if false == exception.IsInfrastructureFailure(cause) {
        t.Fatalf("expected the cause to carry the infrastructure mark, got %v", cause)
    }

    return cause
}

func expectAnonymousToken(t *testing.T, token securitycontract.Token) {
    t.Helper()

    if _, unavailable := AuthenticationUnavailableCause(token); true == unavailable {
        t.Fatalf("expected an anonymous token, got an unavailable one")
    }

    if _, anonymous := token.(*AnonymousToken); false == anonymous {
        t.Fatalf("expected *AnonymousToken, got %T", token)
    }
}

/* mustSession hands on the session a manager door answered, panicking with the error it answered instead */
func mustSession(sessionInstance sessioncontract.Session, sessionErr error) sessioncontract.Session {
    if nil != sessionErr {
        panic(sessionErr)
    }

    return sessionInstance
}

type fallibleTestVoter struct {
    attribute string
    result    securitycontract.VoteResult
    err       error
    runtimes  []containercontract.Runtime
}

func (instance *fallibleTestVoter) Supports(attribute string, subject any) bool {
    return instance.attribute == attribute
}

func (instance *fallibleTestVoter) Vote(runtimeInstance containercontract.Runtime, token securitycontract.Token, attribute string, subject any) (securitycontract.VoteResult, error) {
    instance.runtimes = append(instance.runtimes, runtimeInstance)

    return instance.result, instance.err
}

var _ securitycontract.Voter = (*fallibleTestVoter)(nil)

/* withItsRuntime hands a door the request together with the runtime the request carries */
func withItsRuntime(request httpcontract.Request) (containercontract.Runtime, httpcontract.Request) {
    return request.RuntimeInstance(), request
}

func sessionRuntime() containercontract.Runtime {
    return sessionRuntimeUnder(context.Background())
}

func sessionRuntimeUnder(ctx context.Context) containercontract.Runtime {
    return runtime.NewRun(ctx, container.NewContainer(container.ContainerConfig{}).NewScope())
}

/* contextlessRuntime answers no context, the third form RefuseNilRuntime refuses beside nil and a typed nil */
type contextlessRuntime struct {
    containercontract.Runtime
}

func (instance *contextlessRuntime) Context() context.Context {
    return nil
}

func refusedRuntimes() map[string]containercontract.Runtime {
    var typedNil *contextlessRuntime

    return map[string]containercontract.Runtime{
        "nil":        nil,
        "typed nil":  typedNil,
        "no context": &contextlessRuntime{},
    }
}

func assertRefusedAsNilRuntime(t *testing.T, err error) {
    t.Helper()

    if nil == err || "the runtime is nil or carries no context" != err.Error() {
        t.Fatalf("expected the nil runtime refusal, got %v", err)
    }
}

const (
    csrfAccountPath = "/account"
    csrfUploadPath  = "/upload"
    csrfCappedPath  = "/capped"
    csrfTokenPath   = "/csrf-token"
)

type csrfStatelessTokenSource struct{}

func (instance *csrfStatelessTokenSource) Name() string { return "stateless" }

func (instance *csrfStatelessTokenSource) Stateless() bool { return true }

func (instance *csrfStatelessTokenSource) Resolve(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
    return NewAnonymousToken(), nil
}

/* csrfFixture serves a session firewall over a route of every method and a route that turned the check off */
type csrfFixture struct {
    registry  *FirewallRegistry
    kernel    *testKernel
    container containercontract.Container
    manager   sessioncontract.Manager
    logger    *recordingLogger
    served    *int
}

type csrfProbe struct {
    method      string
    path        string
    sessionId   string
    headers     map[string]string
    body        string
    contentType string
    /* bodyReads, when set, counts the reads the body answers */
    bodyReads *atomic.Int64
}

const csrfCappedMaxBodyBytes = 128

type csrfCountingBody struct {
    reader io.Reader
    reads  *atomic.Int64
}

func (instance *csrfCountingBody) Read(buffer []byte) (int, error) {
    instance.reads.Add(1)

    return instance.reader.Read(buffer)
}

func newCsrfFixture(t *testing.T, mutate func(*CompiledFirewallConfig)) *csrfFixture {
    t.Helper()

    served := 0
    handler := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        served++

        return http.TextResponse(nethttp.StatusOK, "served"), nil
    }

    kernel := newTestKernel()
    kernel.httpRouter.Handle(csrfAccountPath, handler, http.RouteOptions{})
    kernel.httpRouter.Handle(csrfUploadPath, handler, http.RouteOptions{Methods: []string{nethttp.MethodPost}, Protection: http.RouteProtectionConfig{CsrfDisabled: true}})
    kernel.httpRouter.Handle(csrfCappedPath, handler, http.RouteOptions{Methods: []string{nethttp.MethodPost}, Protection: http.RouteProtectionConfig{MaxBodyBytes: bound.Of(csrfCappedMaxBodyBytes)}})

    config := CompiledFirewallConfig{
        Name:          "main",
        Matcher:       NewPathPrefixMatcher("/"),
        TokenSource:   &doorTokenSource{},
        LoginPath:     doorLoginPath,
        LoginHandler:  &doorLoginHandler{},
        LogoutPath:    doorLogoutPath,
        LogoutHandler: &doorLogoutHandler{},
    }

    if nil != mutate {
        mutate(&config)
    }

    registry := NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{NewCompiledFirewall(config)}, nil))
    RegisterKernelSecurityResolutionListener(kernel, registry)

    logger := &recordingLogger{}
    serviceContainer := servedSecurityContainer(t, kernel, logger)

    return &csrfFixture{
        registry:  registry,
        kernel:    kernel,
        container: serviceContainer,
        manager:   session.SessionMustFromResolver(t.Context(), serviceContainer),
        logger:    logger,
        served:    &served,
    }
}

/* storedSession saves a session holding the values and a csrf secret, and answers its id and a token of the secret */
func (instance *csrfFixture) storedSession(t *testing.T, values map[string]any) (string, string) {
    t.Helper()

    sessionInstance := mustSession(instance.manager.NewSession(sessionRuntime()))
    for key, value := range values {
        sessionInstance.Set(key, value)
    }

    issuer := http.NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil), nil, http.RequestConfig{})
    if setErr := http.SetRequestSession(issuer, sessionInstance); nil != setErr {
        t.Fatalf("unexpected error: %v", setErr)
    }

    token, tokenErr := http.CsrfToken(issuer)
    if nil != tokenErr {
        t.Fatalf("unexpected error: %v", tokenErr)
    }

    if saveErr := instance.manager.SaveSession(sessionRuntime(), sessionInstance); nil != saveErr {
        t.Fatalf("unexpected error: %v", saveErr)
    }

    return sessionInstance.Id(), token
}

func (instance *csrfFixture) serve(t *testing.T, probe csrfProbe) *httptest.ResponseRecorder {
    t.Helper()

    method := probe.method
    if "" == method {
        method = nethttp.MethodPost
    }

    path := probe.path
    if "" == path {
        path = csrfAccountPath
    }

    var body io.Reader = strings.NewReader(probe.body)
    if nil != probe.bodyReads {
        body = &csrfCountingBody{reader: body, reads: probe.bodyReads}
    }

    request := httptest.NewRequest(method, path, body)
    request.Header.Set("Accept", "application/json")
    if "" != probe.contentType {
        request.Header.Set("Content-Type", probe.contentType)
    }

    for name, value := range probe.headers {
        request.Header.Set(name, value)
    }

    if "" != probe.sessionId {
        request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: probe.sessionId})
    }

    recorder := httptest.NewRecorder()
    instance.kernel.httpKernel.ServeHttp(instance.container).ServeHTTP(recorder, request)

    return recorder
}

/* csrfRefusals collects the refusals the kernel.exception path sees */
func (instance *csrfFixture) refusals() *[]error {
    recorded := make([]error, 0)

    instance.kernel.eventDispatcher.AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            if exceptionEvent, isExceptionEvent := eventValue.Payload().(*http.KernelExceptionEvent); true == isExceptionEvent {
                recorded = append(recorded, exceptionEvent.Err())
            }

            return nil
        },
        eventcontract.ListenerConfig{Priority: 100},
    )

    return &recorded
}

/* recordingFailedAttemptStore counts in a map, answering the remainder it is told to and recording each call, so the throttle is read without a cache */
type recordingFailedAttemptStore struct {
    mutex           sync.Mutex
    countByKey      map[string]int
    windowRemaining time.Duration
    increments      []string
    resets          []string
    windows         []bound.Duration
    runtimes        []containercontract.Runtime
    resetErr        error
}

func newRecordingFailedAttemptStore() *recordingFailedAttemptStore {
    return &recordingFailedAttemptStore{countByKey: map[string]int{}}
}

func (instance *recordingFailedAttemptStore) Increment(runtimeInstance containercontract.Runtime, key string, window bound.Duration) (securitycontract.FailedAttemptCount, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.increments = append(instance.increments, key)
    instance.windows = append(instance.windows, window)
    instance.runtimes = append(instance.runtimes, runtimeInstance)
    instance.countByKey[key]++

    return securitycontract.FailedAttemptCount{Count: instance.countByKey[key], WindowRemaining: instance.windowRemaining}, nil
}

func (instance *recordingFailedAttemptStore) Reset(runtimeInstance containercontract.Runtime, key string) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.resets = append(instance.resets, key)
    instance.runtimes = append(instance.runtimes, runtimeInstance)
    if nil != instance.resetErr {
        return instance.resetErr
    }

    delete(instance.countByKey, key)

    return nil
}

func (instance *recordingFailedAttemptStore) incrementCount() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return len(instance.increments)
}

type failingFailedAttemptStore struct {
    failure error
}

func (instance *failingFailedAttemptStore) Increment(_ containercontract.Runtime, _ string, _ bound.Duration) (securitycontract.FailedAttemptCount, error) {
    return securitycontract.FailedAttemptCount{}, instance.failure
}

func (instance *failingFailedAttemptStore) Reset(_ containercontract.Runtime, _ string) error {
    return instance.failure
}

/* newTestCache is the application's default cache over a backend reading the clock, closed with the test */
func newTestCache(t *testing.T, clockInstance clockcontract.Clock) cachecontract.Cache {
    t.Helper()

    manager := cache.NewManager(
        cache.NewInMemoryBackend(cache.InMemoryBackendConfig{Clock: clockInstance}),
        cache.NewJsonSerializer(),
        cache.ManagerConfig{OwnsBackend: true},
    )
    t.Cleanup(func() {
        _ = manager.CloseWithContext(context.Background())
    })

    return manager
}

/* runtimeWithCache is a request runtime whose scope resolves the cache as the application's */
func runtimeWithCache(t *testing.T, cacheInstance cachecontract.Cache) (containercontract.Runtime, *recordingLogger) {
    t.Helper()

    logger := &recordingLogger{}

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        cache.ServiceCache,
        func(resolver containercontract.Resolver) (cachecontract.Cache, error) {
            return cacheInstance, nil
        },
        container.RegisterOptions{},
    )

    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger, container.OverrideOptions{})

    return runtime.NewRequest(context.Background(), scope), logger
}
