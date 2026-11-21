package http

import (
    "context"
    "crypto/tls"
    "errors"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "reflect"
    "regexp"
    "strings"
    "syscall"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/session"
    "github.com/precision-soft/melody/v4/session/contract"
)

type stubSessionManager struct {
    saveCalled         int
    deleteCalled       int
    saveErr            error
    deleteErr          error
    storageTimeout     bound.Duration
    saveContext        context.Context
    saveRuntime        containercontract.Runtime
    deleteContext      context.Context
    saveDeadline       time.Duration
    savePanic          any
    deletePanic        any
    timeoutAsked       int
}

func (instance *stubSessionManager) Session(runtimeInstance containercontract.Runtime, sessionId string) (contract.Session, error) {
    return nil, nil
}

func (instance *stubSessionManager) NewSession(runtimeInstance containercontract.Runtime) (contract.Session, error) {
    return nil, nil
}

func (instance *stubSessionManager) RegenerateSession(runtimeInstance containercontract.Runtime, sessionInstance contract.Session) (contract.Session, error) {
    return nil, nil
}

func (instance *stubSessionManager) SaveSession(runtimeInstance containercontract.Runtime, sessionInstance contract.Session) error {
    ctx := runtimeInstance.Context()
    instance.saveCalled++
    instance.saveContext = ctx
    instance.saveRuntime = runtimeInstance

    if deadline, hasDeadline := ctx.Deadline(); true == hasDeadline {
        instance.saveDeadline = time.Until(deadline)
    }

    if nil != instance.savePanic {
        panic(instance.savePanic)
    }

    return instance.saveErr
}

func (instance *stubSessionManager) DeleteSession(runtimeInstance containercontract.Runtime, sessionId string) error {
    ctx := runtimeInstance.Context()
    instance.deleteCalled++
    instance.deleteContext = ctx

    if nil != instance.deletePanic {
        panic(instance.deletePanic)
    }

    return instance.deleteErr
}

func (instance *stubSessionManager) StorageTimeout() bound.Duration {
    instance.timeoutAsked++

    return instance.storageTimeout
}

func (instance *stubSessionManager) CloseWithContext(closeContext context.Context) error { return nil }

type stubSession struct {
    id         string
    isModified bool
    isCleared  bool
}

func (instance *stubSession) Id() string { return instance.id }

func (instance *stubSession) Get(key string) any { return nil }

func (instance *stubSession) String(key string) string { return "" }

func (instance *stubSession) Set(key string, value any) {}

func (instance *stubSession) Has(key string) bool { return false }

func (instance *stubSession) Delete(key string) {}

func (instance *stubSession) Clear() {}

func (instance *stubSession) All() map[string]any { return map[string]any{} }

func (instance *stubSession) IsModified() bool { return instance.isModified }

func (instance *stubSession) IsCleared() bool { return instance.isCleared }

func (instance *stubSession) Snapshot() (map[string]any, bool, bool) {
    return instance.All(), instance.isModified, instance.isCleared
}

func TestIsRequestFromTrustedProxy_MatchesIpAndCidr(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "10.1.2.3:4567"

    if false == isRequestFromTrustedProxy(netRequest, []string{"10.0.0.0/8"}) {
        t.Fatalf("expected cidr match")
    }

    if false == isRequestFromTrustedProxy(netRequest, []string{"10.1.2.3"}) {
        t.Fatalf("expected ip match")
    }

    if true == isRequestFromTrustedProxy(netRequest, []string{"192.168.0.0/16"}) {
        t.Fatalf("expected no match")
    }
}

func TestIsRequestFromTrustedProxy_UnmapsIpv4MappedIpv6Peer(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "[::ffff:172.18.0.2]:5555"

    if false == isRequestFromTrustedProxy(netRequest, []string{"172.18.0.0/16"}) {
        t.Fatalf("expected mapped peer to match the ipv4 cidr")
    }

    exactRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    exactRequest.RemoteAddr = "10.0.0.5:5555"

    if false == isRequestFromTrustedProxy(exactRequest, []string{"::ffff:10.0.0.5"}) {
        t.Fatalf("expected unmapped peer to match the mapped trusted literal")
    }
}

func TestDetectSchemeWithForwardedHeadersPolicy_TrustsIpv4MappedIpv6Peer(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "[::ffff:172.18.0.2]:5555"
    netRequest.Header.Set("X-Forwarded-Proto", "https")

    scheme := detectSchemeWithForwardedHeadersPolicy(
        netRequest,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: true,
            TrustedProxyList:      []string{"172.18.0.0/16"},
        },
    )

    if "https" != scheme {
        t.Fatalf("expected https scheme for a mapped ipv4-in-ipv6 trusted proxy peer, got %q", scheme)
    }
}

func TestDetectSchemeWithForwardedHeadersPolicy_TrustsMappedFormCidr(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "10.0.0.5:5555"
    netRequest.Header.Set("X-Forwarded-Proto", "https")

    scheme := detectSchemeWithForwardedHeadersPolicy(
        netRequest,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: true,
            TrustedProxyList:      []string{"::ffff:10.0.0.0/104"},
        },
    )

    if "https" != scheme {
        t.Fatalf("expected https scheme when the trusted proxy list uses a mapped-form ipv4 cidr, got %q", scheme)
    }
}

func TestDetectSchemeWithForwardedHeadersPolicy_IgnoresForwardedProtoWhenUntrusted(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "10.1.2.3:4567"
    netRequest.Header.Set("X-Forwarded-Proto", "https")

    scheme := detectSchemeWithForwardedHeadersPolicy(
        netRequest,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      []string{"10.0.0.0/8"},
        },
    )

    if "http" != scheme {
        t.Fatalf("expected http scheme when forwarded headers are not trusted")
    }
}

func TestDetectSchemeWithForwardedHeadersPolicy_UsesForwardedProtoWhenTrustedProxy(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "10.1.2.3:4567"
    netRequest.Header.Set("X-Forwarded-Proto", "https")

    scheme := detectSchemeWithForwardedHeadersPolicy(
        netRequest,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: true,
            TrustedProxyList:      []string{"10.0.0.0/8"},
        },
    )

    if "https" != scheme {
        t.Fatalf("expected https scheme when forwarded headers are trusted and proxy matches")
    }
}

func TestDetectSchemeWithForwardedHeadersPolicy_TlsWinsOverForwarded(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "10.1.2.3:4567"
    netRequest.Header.Set("X-Forwarded-Proto", "http")
    netRequest.TLS = &tls.ConnectionState{}

    scheme := detectSchemeWithForwardedHeadersPolicy(
        netRequest,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: true,
            TrustedProxyList:      []string{"10.0.0.0/8"},
        },
    )

    if "https" != scheme {
        t.Fatalf("expected https scheme when tls is present")
    }
}

func TestWriteResponse_SetsSessionCookieWithSecureAndSameSite(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"
    netRequest.TLS = &tls.ConnectionState{}

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    response := EmptyResponse(nethttp.StatusOK)

    writer := httptest.NewRecorder()

    sessionManager := &stubSessionManager{}
    sessionInstance := &stubSession{
        id:         "session-123",
        isModified: true,
        isCleared:  false,
    }

    melodyRequest.Attributes().Set(requestAttributeSession, sessionInstance)

    writeResponse(
        sessionRuntime(),
        melodyRequest,
        writer,
        response,
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      []string{},
        },
        httpcontract.SessionCookiePolicy{
            Path:     "/",
            Domain:   "",
            SameSite: nethttp.SameSiteLaxMode,
        },
        0,
        nil,
    )

    if 1 != sessionManager.saveCalled {
        t.Fatalf("expected session to be saved once")
    }

    httpResponse := writer.Result()
    cookies := httpResponse.Cookies()

    if 1 != len(cookies) {
        t.Fatalf("expected one set-cookie")
    }

    cookie := cookies[0]

    if "session-123" != cookie.Value {
        t.Fatalf("expected cookie value to be session id")
    }

    if true != cookie.HttpOnly {
        t.Fatalf("expected cookie to be httpOnly")
    }

    if true != cookie.Secure {
        t.Fatalf("expected cookie to be secure when request is https")
    }

    if nethttp.SameSiteLaxMode != cookie.SameSite {
        t.Fatalf("expected cookie SameSite to be lax")
    }

    if "/" != cookie.Path {
        t.Fatalf("expected cookie path to be /")
    }
}

func TestWriteResponse_ClearsSessionCookieWithMaxAgeNegative(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"
    netRequest.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: "session-123"})

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    response := EmptyResponse(nethttp.StatusOK)

    writer := httptest.NewRecorder()

    sessionManager := &stubSessionManager{}
    sessionInstance := &stubSession{
        id:         "session-123",
        isModified: false,
        isCleared:  true,
    }

    melodyRequest.Attributes().Set(requestAttributeSession, sessionInstance)

    writeResponse(
        sessionRuntime(),
        melodyRequest,
        writer,
        response,
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      []string{},
        },
        httpcontract.SessionCookiePolicy{
            Path:     "/",
            Domain:   "",
            SameSite: nethttp.SameSiteLaxMode,
        },
        0,
        nil,
    )

    if 1 != sessionManager.deleteCalled {
        t.Fatalf("expected session to be deleted once")
    }

    httpResponse := writer.Result()
    cookies := httpResponse.Cookies()

    if 1 != len(cookies) {
        t.Fatalf("expected one set-cookie")
    }

    cookie := cookies[0]

    if "" != cookie.Value {
        t.Fatalf("expected cleared cookie value to be empty")
    }

    if false == (0 >= cookie.MaxAge) {
        t.Fatalf("expected cleared cookie MaxAge to be non-positive")
    }
}

func TestWriteResponse_ForcesTheSecureSessionCookieWhenThePolicyDemandsIt(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    writer := httptest.NewRecorder()

    sessionManager := &stubSessionManager{}
    sessionInstance := &stubSession{
        id:         "0123456789abcdef0123456789abcdef",
        isModified: true,
        isCleared:  false,
    }

    melodyRequest.Attributes().Set(requestAttributeSession, sessionInstance)

    writeResponse(
        newTestRuntime(),
        melodyRequest,
        writer,
        EmptyResponse(nethttp.StatusOK),
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      []string{},
        },
        httpcontract.SessionCookiePolicy{
            Path:     "/",
            Domain:   "",
            SameSite: nethttp.SameSiteLaxMode,
            Secure:   httpcontract.SessionCookieSecureAlways,
        },
        0,
        nil,
    )

    cookies := writer.Result().Cookies()
    if 1 != len(cookies) {
        t.Fatalf("expected one set-cookie, got %d", len(cookies))
    }

    if false == cookies[0].Secure {
        t.Fatalf("expected the session cookie to be secure when the policy forces it behind a plaintext hop")
    }
}

func TestWriteResponse_SessionCookieSecureNeverOverridesTheDetectedScheme(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"
    netRequest.TLS = &tls.ConnectionState{}

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    writer := httptest.NewRecorder()

    sessionManager := &stubSessionManager{}
    sessionInstance := &stubSession{
        id:         "0123456789abcdef0123456789abcdef",
        isModified: true,
        isCleared:  false,
    }

    melodyRequest.Attributes().Set(requestAttributeSession, sessionInstance)

    writeResponse(
        newTestRuntime(),
        melodyRequest,
        writer,
        EmptyResponse(nethttp.StatusOK),
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      []string{},
        },
        httpcontract.SessionCookiePolicy{
            Path:     "/",
            Domain:   "",
            SameSite: nethttp.SameSiteLaxMode,
            Secure:   httpcontract.SessionCookieSecureNever,
        },
        0,
        nil,
    )

    cookies := writer.Result().Cookies()
    if 1 != len(cookies) {
        t.Fatalf("expected one set-cookie, got %d", len(cookies))
    }

    if true == cookies[0].Secure {
        t.Fatalf("expected the policy to win over the detected https scheme")
    }
}

func TestWriteResponse_DoesNotStoreASessionTheDiscardedResponseCannotAdvertise(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/events", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    writer := newRecordingResponseWriter(httptest.NewRecorder())
    writer.WriteHeader(nethttp.StatusOK)

    sessionManager := &stubSessionManager{}
    sessionInstance := &stubSession{
        id:         "0123456789abcdef0123456789abcdef",
        isModified: true,
        isCleared:  false,
    }

    melodyRequest.Attributes().Set(requestAttributeSession, sessionInstance)

    writeResponse(
        newTestRuntime(),
        melodyRequest,
        writer,
        EmptyResponse(nethttp.StatusOK),
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      []string{},
        },
        httpcontract.SessionCookiePolicy{
            Path:     "/",
            Domain:   "",
            SameSite: nethttp.SameSiteLaxMode,
        },
        0,
        nil,
    )

    if 0 != sessionManager.saveCalled {
        t.Fatalf("expected no stored session for a response whose Set-Cookie can never reach the client, got %d saves", sessionManager.saveCalled)
    }

    if true == writer.SessionPersisted() {
        t.Fatalf("expected the session not to be marked persisted when it was never stored")
    }
}

func TestWriteResponse_StillStoresASessionTheClientAlreadyHoldsOnADiscardedResponse(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/events", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"
    netRequest.AddCookie(
        &nethttp.Cookie{
            Name:  session.SessionCookieName,
            Value: "0123456789abcdef0123456789abcdef",
        },
    )

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    writer := newRecordingResponseWriter(httptest.NewRecorder())
    writer.WriteHeader(nethttp.StatusOK)

    sessionManager := &stubSessionManager{}
    sessionInstance := &stubSession{
        id:         "0123456789abcdef0123456789abcdef",
        isModified: true,
        isCleared:  false,
    }

    melodyRequest.Attributes().Set(requestAttributeSession, sessionInstance)

    writeResponse(
        newTestRuntime(),
        melodyRequest,
        writer,
        EmptyResponse(nethttp.StatusOK),
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      []string{},
        },
        httpcontract.SessionCookiePolicy{
            Path:     "/",
            Domain:   "",
            SameSite: nethttp.SameSiteLaxMode,
        },
        0,
        nil,
    )

    if 1 != sessionManager.saveCalled {
        t.Fatalf("expected the session the client already holds to be stored once, got %d saves", sessionManager.saveCalled)
    }
}

func TestWriteResponse_StillDeletesAClearedSessionOnADiscardedResponse(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodPost, "http://example.com/logout", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"
    netRequest.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: "0123456789abcdef0123456789abcdef"})

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    writer := newRecordingResponseWriter(httptest.NewRecorder())
    writer.WriteHeader(nethttp.StatusOK)

    sessionManager := &stubSessionManager{}
    sessionInstance := &stubSession{
        id:         "0123456789abcdef0123456789abcdef",
        isModified: false,
        isCleared:  true,
    }

    melodyRequest.Attributes().Set(requestAttributeSession, sessionInstance)

    writeResponse(
        newTestRuntime(),
        melodyRequest,
        writer,
        EmptyResponse(nethttp.StatusOK),
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      []string{},
        },
        httpcontract.SessionCookiePolicy{
            Path:     "/",
            Domain:   "",
            SameSite: nethttp.SameSiteLaxMode,
        },
        0,
        nil,
    )

    if 1 != sessionManager.deleteCalled {
        t.Fatalf("expected a cleared session to be destroyed even when the response is discarded, got %d deletes", sessionManager.deleteCalled)
    }
}

type dependencyA struct {
    Value string
}

type testScope struct {
    calledTypes []reflect.Type
    values      map[reflect.Type]any
    returnErr   error
}

func (instance *testScope) Get(ctx context.Context, serviceName string) (any, error) { return nil, nil }

func (instance *testScope) MustGet(ctx context.Context, serviceName string) any { return nil }

func (instance *testScope) GetByType(ctx context.Context, targetType reflect.Type) (any, error) {
    instance.calledTypes = append(instance.calledTypes, targetType)

    if nil != instance.returnErr {
        return nil, instance.returnErr
    }

    if nil == instance.values {
        return nil, nil
    }

    value, exists := instance.values[targetType]
    if false == exists {
        return nil, nil
    }

    return value, nil
}

func (instance *testScope) MustGetByType(ctx context.Context, targetType reflect.Type) any { return nil }

func (instance *testScope) Has(serviceName string) bool { return false }

func (instance *testScope) HasType(targetType reflect.Type) bool { return false }

func (instance *testScope) Runtime() containercontract.Runtime {
    return nil
}

func (instance *testScope) OverrideInstance(serviceName string, value any, options containercontract.OverrideOptions) error { return nil }

func (instance *testScope) MustOverrideInstance(serviceName string, value any, options containercontract.OverrideOptions) {}

func (instance *testScope) OverrideProtectedInstance(serviceName string, value any, options containercontract.OverrideOptions) error { return nil }

func (instance *testScope) MustOverrideProtectedInstance(serviceName string, value any, options containercontract.OverrideOptions) {}

func (instance *testScope) RegisterScoped(serviceName string, provider any, options containercontract.RegisterOptions) error {
    return nil
}

func (instance *testScope) MustRegisterScoped(serviceName string, provider any, options containercontract.RegisterOptions) {
}

func (instance *testScope) Close() error { return nil }

func (instance *testScope) CloseWithContext(closeContext context.Context) error { return nil }

var _ containercontract.Scope = (*testScope)(nil)

type testRuntime struct {
    scope containercontract.Scope
}

func (instance *testRuntime) Context() context.Context { return context.Background() }

func (instance *testRuntime) Scope() containercontract.Scope { return instance.scope }

func (instance *testRuntime) Kind() containercontract.Kind { return containercontract.KindRequest }

var _ containercontract.Runtime = (*testRuntime)(nil)

func assertPanicWithExceptionMessage(t *testing.T, callback func(), expectedMessage string) {
    t.Helper()

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic")
        }

        panicErr, ok := recoveredValue.(*exception.Error)
        if false == ok {
            t.Fatalf("expected panic to be *exception.Error, got %T", recoveredValue)
        }

        if panicErr.Message() != expectedMessage {
            t.Fatalf("expected panic message %q, got %q", expectedMessage, panicErr.Message())
        }
    }()

    callback()
}

func TestControllerHandler_ResolvesTheControllersArgumentsFromTheContainer(t *testing.T) {
    dependencyInstance := &dependencyA{Value: "ok"}

    scope := &testScope{
        values: map[reflect.Type]any{
            reflect.TypeOf((*dependencyA)(nil)): dependencyInstance,
        },
    }

    runtimeInstance := &testRuntime{scope: scope}

    var received *dependencyA

    controller := func(request *Request, dep *dependencyA) (*Response, error) {
        received = dep
        return EmptyResponse(200), nil
    }

    handler := ControllerHandler(controller)

    netRequest := httptest.NewRequest("GET", "http://example.com/", nil)
    request := NewRequest(netRequest, nil, runtimeInstance, nil)

    response, err := handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil != err {
        t.Fatalf("expected nil error, got %v", err)
    }

    if nil == response {
        t.Fatalf("expected non-nil response")
    }

    if dependencyInstance != received {
        t.Fatalf("expected dependency to be injected")
    }

    if 1 != len(scope.calledTypes) {
        t.Fatalf("expected GetByType to be called once, got %d", len(scope.calledTypes))
    }

    if reflect.TypeOf((*dependencyA)(nil)) != scope.calledTypes[0] {
        t.Fatalf("expected GetByType to be called with dependencyA type")
    }
}

func TestControllerHandler_InsertsRuntimeWhenControllerRequestsRuntimeParameter(t *testing.T) {
    dependencyInstance := &dependencyA{Value: "ok"}

    scope := &testScope{
        values: map[reflect.Type]any{
            reflect.TypeOf((*dependencyA)(nil)): dependencyInstance,
        },
    }

    runtimeInstance := &testRuntime{scope: scope}

    var receivedRuntime containercontract.Runtime
    var receivedDependency *dependencyA

    controller := func(request *Request, runtimeInstance containercontract.Runtime, dep *dependencyA) (*Response, error) {
        receivedRuntime = runtimeInstance
        receivedDependency = dep

        return EmptyResponse(200), nil
    }

    handler := ControllerHandler(controller)

    netRequest := httptest.NewRequest("GET", "http://example.com/", nil)
    request := NewRequest(netRequest, nil, runtimeInstance, nil)

    response, err := handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil != err {
        t.Fatalf("expected nil error, got %v", err)
    }

    if nil == response {
        t.Fatalf("expected non-nil response")
    }

    if runtimeInstance != receivedRuntime {
        t.Fatalf("expected runtime to be injected")
    }

    if dependencyInstance != receivedDependency {
        t.Fatalf("expected dependency to be injected")
    }

    if 1 != len(scope.calledTypes) {
        t.Fatalf("expected GetByType to be called once, got %d", len(scope.calledTypes))
    }

    if reflect.TypeOf((*dependencyA)(nil)) != scope.calledTypes[0] {
        t.Fatalf("expected GetByType to be called with dependencyA type")
    }
}

func TestControllerHandler_ReturnsErrorWhenRuntimeIsNil(t *testing.T) {
    controller := func(request *Request) (*Response, error) {
        return EmptyResponse(200), nil
    }

    handler := ControllerHandler(controller)

    netRequest := httptest.NewRequest("GET", "http://example.com/", nil)
    request := NewRequest(netRequest, nil, nil, nil)

    _, err := handler(nil, httptest.NewRecorder(), request)
    if nil == err {
        t.Fatalf("expected non-nil error")
    }

    if "runtime instance is nil in controller handler" != err.Error() {
        t.Fatalf("unexpected error: %v", err)
    }
}

func TestControllerHandler_PropagatesScopeGetByTypeError(t *testing.T) {
    expectedErr := errors.New("scope error")

    scope := &testScope{
        returnErr: expectedErr,
    }

    runtimeInstance := &testRuntime{scope: scope}

    controller := func(request *Request, dep *dependencyA) (*Response, error) {
        return EmptyResponse(200), nil
    }

    handler := ControllerHandler(controller)

    netRequest := httptest.NewRequest("GET", "http://example.com/", nil)
    request := NewRequest(netRequest, nil, runtimeInstance, nil)

    _, err := handler(runtimeInstance, httptest.NewRecorder(), request)
    if expectedErr != err {
        t.Fatalf("expected scope error to be returned")
    }

    if 1 != len(scope.calledTypes) {
        t.Fatalf("expected GetByType to be called once, got %d", len(scope.calledTypes))
    }
}

func TestControllerHandler_ReturnsControllerErrorAndNilResponse(t *testing.T) {
    expectedErr := errors.New("controller failed")

    controller := func(request *Request) (*Response, error) {
        return nil, expectedErr
    }

    handler := ControllerHandler(controller)

    runtimeInstance := &testRuntime{scope: &testScope{}}

    netRequest := httptest.NewRequest("GET", "http://example.com/", nil)
    request := NewRequest(netRequest, nil, runtimeInstance, nil)

    response, err := handler(runtimeInstance, httptest.NewRecorder(), request)
    if expectedErr != err {
        t.Fatalf("expected controller error to be returned")
    }

    if nil != response {
        t.Fatalf("expected nil response")
    }
}

func TestControllerHandler_ReturnsNilResponseWhenControllerReturnsNilResponseAndNilError(t *testing.T) {
    controller := func(request *Request) (*Response, error) {
        return nil, nil
    }

    handler := ControllerHandler(controller)

    runtimeInstance := &testRuntime{scope: &testScope{}}

    netRequest := httptest.NewRequest("GET", "http://example.com/", nil)
    request := NewRequest(netRequest, nil, runtimeInstance, nil)

    response, err := handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil != err {
        t.Fatalf("expected nil error, got %v", err)
    }

    if nil != response {
        t.Fatalf("expected nil response")
    }
}

func TestControllerHandler_PanicsWhenControllerIsNotAFunction(t *testing.T) {
    assertPanicWithExceptionMessage(
        t,
        func() {
            _ = ControllerHandler(123)
        },
        "controller must be a function",
    )
}

func TestControllerHandler_PanicsWhenControllerIsNil(t *testing.T) {
    assertPanicWithExceptionMessage(
        t,
        func() {
            _ = ControllerHandler(nil)
        },
        "controller must be a function",
    )
}

func TestControllerHandler_PanicsWhenControllerIsANilFunction(t *testing.T) {
    var controller func(request *Request) (*Response, error)

    assertPanicWithExceptionMessage(
        t,
        func() {
            _ = ControllerHandler(controller)
        },
        "controller is a nil function",
    )
}

func TestControllerHandler_PanicsWhenControllerHasNoArguments(t *testing.T) {
    assertPanicWithExceptionMessage(
        t,
        func() {
            _ = ControllerHandler(func() (*Response, error) { return EmptyResponse(200), nil })
        },
        "controller must have at least one argument",
    )
}

func TestControllerHandler_PanicsWhenFirstArgumentIsNotRequest(t *testing.T) {
    assertPanicWithExceptionMessage(
        t,
        func() {
            _ = ControllerHandler(func(value string) (*Response, error) { return EmptyResponse(200), nil })
        },
        "first controller argument must implement http request contract",
    )
}

func TestControllerHandler_PanicsWhenControllerDoesNotReturnTwoResults(t *testing.T) {
    assertPanicWithExceptionMessage(
        t,
        func() {
            _ = ControllerHandler(func(request *Request) *Response { return EmptyResponse(200) })
        },
        "controller must return two results, a response and an error",
    )
}

func TestControllerHandler_PanicsWhenFirstResultIsNotResponsePointer(t *testing.T) {
    assertPanicWithExceptionMessage(
        t,
        func() {
            _ = ControllerHandler(func(request *Request) (int, error) { return 1, nil })
        },
        "controller must return response contract as first result",
    )
}

func TestControllerHandler_PanicsWhenSecondResultIsNotError(t *testing.T) {
    assertPanicWithExceptionMessage(
        t,
        func() {
            _ = ControllerHandler(func(request *Request) (*Response, string) { return EmptyResponse(200), "" })
        },
        "controller must return error as second result",
    )
}

func TestMatchPath_WildcardLocale_SetsLocaleParameter(t *testing.T) {
    routeDefinition := route{
        pattern:      "/*_locale...",
        parts:        splitPath("/*_locale..."),
        requirements: map[string]*regexp.Regexp{},
    }

    pathSegments := splitPath("/en")
    parameters, matched := matchPath(routeDefinition, pathSegments)

    if false == matched {
        t.Fatalf("expected path to match")
    }

    value, exists := parameters[RouteAttributeLocale]
    if false == exists {
        t.Fatalf("expected _locale param to exist")
    }

    if "en" != value {
        t.Fatalf("expected _locale param to be 'en', got: %s", value)
    }
}

func TestMatchPath_WildcardLocale_CatchAll_SetsLocaleParameter(t *testing.T) {
    routeDefinition := route{
        pattern:      "/prefix/*_locale...",
        parts:        splitPath("/prefix/*_locale..."),
        requirements: map[string]*regexp.Regexp{},
    }

    pathSegments := splitPath("/prefix/en/extra")
    parameters, matched := matchPath(routeDefinition, pathSegments)

    if false == matched {
        t.Fatalf("expected path to match")
    }

    if "en/extra" != parameters[RouteAttributeLocale] {
        t.Fatalf("expected _locale catch-all to be 'en/extra', got: %s", parameters[RouteAttributeLocale])
    }
}

func TestMatchPath_WildcardNonLocale_DoesNotSetLocaleParameter(t *testing.T) {
    routeDefinition := route{
        pattern:      "/*path...",
        parts:        splitPath("/*path..."),
        requirements: map[string]*regexp.Regexp{},
    }

    pathSegments := splitPath("/some/path")
    parameters, matched := matchPath(routeDefinition, pathSegments)

    if false == matched {
        t.Fatalf("expected path to match")
    }

    _, exists := parameters[RouteAttributeLocale]
    if true == exists {
        t.Fatalf("expected _locale param not to be set for non-locale wildcard")
    }

    if "some/path" != parameters["path"] {
        t.Fatalf("expected path param to be 'some/path', got: %s", parameters["path"])
    }
}

func TestMatchPath_ParameterLocale_SetsLocaleParameter(t *testing.T) {
    routeDefinition := route{
        pattern:      "/page/:_locale",
        parts:        splitPath("/page/:_locale"),
        requirements: map[string]*regexp.Regexp{},
    }

    pathSegments := splitPath("/page/fr")
    parameters, matched := matchPath(routeDefinition, pathSegments)

    if false == matched {
        t.Fatalf("expected path to match")
    }

    if "fr" != parameters[RouteAttributeLocale] {
        t.Fatalf("expected _locale param to be 'fr', got: %s", parameters[RouteAttributeLocale])
    }
}

func TestControllerHandler_PanicsWhenDependencyIsNilFromScope(t *testing.T) {
    scope := &testScope{
        values: map[reflect.Type]any{
            reflect.TypeOf((*dependencyA)(nil)): nil,
        },
    }

    runtimeInstance := &testRuntime{scope: scope}

    controller := func(request *Request, dep *dependencyA) (*Response, error) {
        if nil == dep {
            return nil, errors.New("unexpected nil dependency")
        }

        return EmptyResponse(200), nil
    }

    handler := ControllerHandler(controller)

    netRequest := httptest.NewRequest("GET", "http://example.com/", nil)
    request := NewRequest(netRequest, nil, runtimeInstance, nil)

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic when dependency is nil")
        }
    }()

    _, _ = handler(runtimeInstance, httptest.NewRecorder(), request)
}

func TestDetectSchemeWithForwardedHeadersPolicy_UsesTheClientFacingProtoOfAChain(t *testing.T) {
    for _, headerValue := range []string{"https, http", "https,http", " https , http "} {
        netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
        netRequest.RemoteAddr = "10.1.2.3:4567"
        netRequest.Header.Set("X-Forwarded-Proto", headerValue)

        scheme := detectSchemeWithForwardedHeadersPolicy(
            netRequest,
            httpcontract.ForwardedHeadersPolicy{
                TrustForwardedHeaders: true,
                TrustedProxyList:      []string{"10.0.0.0/8"},
            },
        )

        if "https" != scheme {
            t.Fatalf("expected https from %q, got %q", headerValue, scheme)
        }
    }
}

func TestWriteResponse_NilResponsePersistsSessionAndWritesNoContent(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodPost, "http://example.com/logout", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"
    netRequest.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: "session-123"})

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    writer := httptest.NewRecorder()

    sessionManager := &stubSessionManager{}
    sessionInstance := &stubSession{
        id:         "session-123",
        isModified: false,
        isCleared:  true,
    }

    melodyRequest.Attributes().Set(requestAttributeSession, sessionInstance)

    writeResponse(
        sessionRuntime(),
        melodyRequest,
        writer,
        nil,
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      []string{},
        },
        httpcontract.SessionCookiePolicy{
            Path:     "/",
            Domain:   "",
            SameSite: nethttp.SameSiteLaxMode,
        },
        0,
        nil,
    )

    if 1 != sessionManager.deleteCalled {
        t.Fatalf("nil response dropped the session: expected DeleteSession to be called once, got %d", sessionManager.deleteCalled)
    }

    httpResponse := writer.Result()

    if nethttp.StatusNoContent != httpResponse.StatusCode {
        t.Fatalf("expected 204 No Content for a nil response, got %d", httpResponse.StatusCode)
    }

    cookies := httpResponse.Cookies()
    if 1 != len(cookies) {
        t.Fatalf("nil response dropped the clearing Set-Cookie: expected one cookie, got %d", len(cookies))
    }

    if -1 != cookies[0].MaxAge {
        t.Fatalf("expected the session cookie to be cleared (MaxAge -1), got %d", cookies[0].MaxAge)
    }
}

func TestWriteResponse_SkipsPersistenceForATypedNilSession(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    response := EmptyResponse(nethttp.StatusOK)
    writer := httptest.NewRecorder()

    sessionManager := &stubSessionManager{}

    var typedNilSession *stubSession

    defer func() {
        if recovered := recover(); nil != recovered {
            t.Fatalf("expected a typed nil session to be skipped, not dereferenced: %v", recovered)
        }
    }()

    melodyRequest.Attributes().Set(requestAttributeSession, typedNilSession)

    writeResponse(
        sessionRuntime(),
        melodyRequest,
        writer,
        response,
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
        0,
        nil,
    )

    if 0 != sessionManager.saveCalled {
        t.Fatalf("expected no save for a typed nil session")
    }

    if 0 != len(writer.Result().Cookies()) {
        t.Fatalf("expected no session cookie for a typed nil session")
    }
}

func TestWriteResponse_SkipsPersistenceForATypedNilManager(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    response := EmptyResponse(nethttp.StatusOK)
    writer := httptest.NewRecorder()

    var typedNilManager *stubSessionManager

    sessionInstance := &stubSession{id: "session-123", isModified: true}

    defer func() {
        if recovered := recover(); nil != recovered {
            t.Fatalf("expected a typed nil manager to be skipped, not called: %v", recovered)
        }
    }()

    melodyRequest.Attributes().Set(requestAttributeSession, sessionInstance)

    writeResponse(
        sessionRuntime(),
        melodyRequest,
        writer,
        response,
        typedNilManager,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
        0,
        nil,
    )

    if 0 != len(writer.Result().Cookies()) {
        t.Fatalf("expected no session cookie when there is no manager to persist through")
    }
}

func TestWriteResponse_ADeletedSessionExpiresTheCookieAndKeepsTheResponse(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    response := TextResponse(nethttp.StatusOK, "handler body")
    writer := httptest.NewRecorder()

    sessionManager := &stubSessionManager{
        saveErr: exception.NewError("session was deleted and cannot be saved again", nil, session.ErrSessionDeleted),
    }
    sessionInstance := &stubSession{id: "session-123", isModified: true}

    melodyRequest.Attributes().Set(requestAttributeSession, sessionInstance)

    writeResponse(
        sessionRuntime(),
        melodyRequest,
        writer,
        response,
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
        0,
        nil,
    )

    httpResponse := writer.Result()

    if nethttp.StatusOK != httpResponse.StatusCode {
        t.Fatalf("expected the handler's own response to be served, got %d", httpResponse.StatusCode)
    }

    cookies := httpResponse.Cookies()
    if 1 != len(cookies) {
        t.Fatalf("expected the clearing cookie, got %d cookies", len(cookies))
    }

    if "" != cookies[0].Value || 0 <= cookies[0].MaxAge {
        t.Fatalf("expected an expiring session cookie, got value %q maxAge %d", cookies[0].Value, cookies[0].MaxAge)
    }
}

func TestWriteResponse_AnExpiringSessionCookieMarksTheResponsePrivate(t *testing.T) {
    arms := []struct {
        name            string
        sessionManager  *stubSessionManager
        sessionInstance *stubSession
    }{
        {
            name:            "cleared",
            sessionManager:  &stubSessionManager{},
            sessionInstance: &stubSession{id: "session-123", isModified: false, isCleared: true},
        },
        {
            name:            "deleted while the request was in flight",
            sessionManager:  &stubSessionManager{saveErr: exception.NewError("session was deleted and cannot be saved again", nil, session.ErrSessionDeleted)},
            sessionInstance: &stubSession{id: "session-123", isModified: true},
        },
    }

    for _, arm := range arms {
        netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
        netRequest.RemoteAddr = "127.0.0.1:1234"

        melodyRequest := NewRequest(netRequest, nil, nil, nil)

        response := TextResponse(nethttp.StatusOK, "handler body")
        response.Headers().Set("Cache-Control", "public, max-age=60")
        writer := httptest.NewRecorder()

        melodyRequest.Attributes().Set(requestAttributeSession, arm.sessionInstance)

        writeResponse(
            sessionRuntime(),
            melodyRequest,
            writer,
            response,
            arm.sessionManager,
            httpcontract.ForwardedHeadersPolicy{},
            httpcontract.SessionCookiePolicy{Path: "/"},
            0,
            nil,
        )

        httpResponse := writer.Result()

        cookies := httpResponse.Cookies()
        if 1 != len(cookies) || "" != cookies[0].Value || 0 <= cookies[0].MaxAge {
            t.Fatalf("%s: expected the expiring session cookie, got %+v", arm.name, cookies)
        }

        cacheControl := httpResponse.Header.Get("Cache-Control")
        if true == strings.Contains(strings.ToLower(cacheControl), "public") || false == strings.Contains(strings.ToLower(cacheControl), "private") {
            t.Fatalf("%s: expected the response carrying the expiring cookie to be marked private, got Cache-Control %q", arm.name, cacheControl)
        }
    }
}

func TestWriteResponse_ARotatedAwaySessionKeepsTheCookieAndTheResponse(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    response := TextResponse(nethttp.StatusOK, "handler body")
    writer := httptest.NewRecorder()

    sessionManager := &stubSessionManager{
        saveErr: exception.NewError(
            "session was rotated away and cannot be saved again",
            nil,
            errors.Join(session.ErrSessionDeleted, session.ErrSessionRotated),
        ),
    }
    sessionInstance := &stubSession{id: "session-123", isModified: true}

    melodyRequest.Attributes().Set(requestAttributeSession, sessionInstance)

    writeResponse(
        sessionRuntime(),
        melodyRequest,
        writer,
        response,
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
        0,
        nil,
    )

    httpResponse := writer.Result()

    if nethttp.StatusOK != httpResponse.StatusCode {
        t.Fatalf("expected the handler's own response to be served, got %d", httpResponse.StatusCode)
    }

    if 0 != len(httpResponse.Cookies()) {
        t.Fatalf("expected no cookie at all for a rotated-away session, got %d", len(httpResponse.Cookies()))
    }
}

func TestWriteResponse_ASaveOutageAnswersServiceUnavailableWithoutACookie(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    response := TextResponse(nethttp.StatusOK, "welcome")
    writer := httptest.NewRecorder()

    sessionManager := &stubSessionManager{
        saveErr: exception.NewError("storage is unreachable", nil, exception.MarkInfrastructureFailure(errors.New("connection refused"))),
    }
    sessionInstance := &stubSession{id: "session-123", isModified: true}

    melodyRequest.Attributes().Set(requestAttributeSession, sessionInstance)

    writeResponse(
        sessionRuntime(),
        melodyRequest,
        writer,
        response,
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
        0,
        nil,
    )

    httpResponse := writer.Result()

    if nethttp.StatusServiceUnavailable != httpResponse.StatusCode || "5" != httpResponse.Header.Get("Retry-After") {
        t.Fatalf("expected a storage outage to answer 503 with the default Retry-After, got %d and %q", httpResponse.StatusCode, httpResponse.Header.Get("Retry-After"))
    }

    if 0 != len(httpResponse.Cookies()) {
        t.Fatalf("expected no session cookie when nothing was persisted")
    }
}

func TestCloseDiscardedResponseBody_ReadsATypedNilResponseAsAbsent(t *testing.T) {
    var unassignedResponse *Response

    closeDiscardedResponseBody(unassignedResponse, nil)
}

func TestWriteResponse_ReadsATypedNilResponseAsTheEmptyDefault(t *testing.T) {
    var unassignedResponse *Response

    recorder := httptest.NewRecorder()

    writeResponse(
        sessionRuntime(),
        nil,
        recorder,
        unassignedResponse,
        nil,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{},
        0,
        nil,
    )

    if nethttp.StatusNoContent != recorder.Code {
        t.Fatalf("expected %d, got %d", nethttp.StatusNoContent, recorder.Code)
    }
}

func TestWriteResponse_ReturnsTheReplacedServiceUnavailableOnASaveOutage(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    response := TextResponse(nethttp.StatusOK, "welcome")
    writer := httptest.NewRecorder()

    sessionManager := &stubSessionManager{
        saveErr: exception.NewError("storage is unreachable", nil, exception.MarkInfrastructureFailure(errors.New("connection refused"))),
    }
    sessionInstance := &stubSession{id: "session-123", isModified: true}

    melodyRequest.Attributes().Set(requestAttributeSession, sessionInstance)

    writtenResponse := writeResponse(
        sessionRuntime(),
        melodyRequest,
        writer,
        response,
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
        0,
        nil,
    )

    if nil == writtenResponse || nethttp.StatusServiceUnavailable != writtenResponse.StatusCode() {
        t.Fatalf("expected the returned response to be the replaced 503, got %#v", writtenResponse)
    }

    if response == writtenResponse {
        t.Fatalf("expected the returned response to differ from the replaced original")
    }
}

func TestWriteResponse_ReturnsTheCallersResponseWhenNothingReplacedIt(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    response := TextResponse(nethttp.StatusOK, "welcome")
    writer := httptest.NewRecorder()

    writtenResponse := writeResponse(
        sessionRuntime(),
        melodyRequest,
        writer,
        response,
        nil,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
        0,
        nil,
    )

    if response != writtenResponse {
        t.Fatalf("expected the caller's response back, got %#v", writtenResponse)
    }
}

type snapshotDivergentSession struct {
    stubSession
}

func (instance *snapshotDivergentSession) Snapshot() (map[string]any, bool, bool) {
    return map[string]any{}, true, true
}

func TestWriteResponse_TheBranchDecisionFollowsTheSnapshotNotTheAccessors(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"
    netRequest.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: "1234567890abcdef1234567890abcdef"})

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    writer := httptest.NewRecorder()

    sessionManager := &stubSessionManager{}
    sessionInstance := &snapshotDivergentSession{
        stubSession{
            id:         "1234567890abcdef1234567890abcdef",
            isModified: true,
            isCleared:  false,
        },
    }

    melodyRequest.Attributes().Set(requestAttributeSession, sessionInstance)

    writeResponse(
        sessionRuntime(),
        melodyRequest,
        writer,
        EmptyResponse(nethttp.StatusOK),
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      []string{},
        },
        httpcontract.SessionCookiePolicy{
            Path:     "/",
            Domain:   "",
            SameSite: nethttp.SameSiteLaxMode,
        },
        0,
        nil,
    )

    if 1 != sessionManager.deleteCalled || 0 != sessionManager.saveCalled {
        t.Fatalf("expected the snapshot's cleared flag to take the delete branch, got %d deletes and %d saves", sessionManager.deleteCalled, sessionManager.saveCalled)
    }
}

func TestWriteResponse_AnOutOfRangeStatusCodeAnswersTheRenderedServerError(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/bad",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(99), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainer()
    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    server := httptest.NewServer(handler)
    defer server.Close()

    response, requestErr := nethttp.Get(server.URL + "/bad")
    if nil != requestErr {
        t.Fatalf("request failed: %v", requestErr)
    }
    defer func() { _ = response.Body.Close() }()

    if nethttp.StatusInternalServerError != response.StatusCode {
        t.Fatalf("expected the out-of-range status code to answer 500, got %d", response.StatusCode)
    }
}

func TestWriteResponse_RefusesTheOutOfRangeCodeBeforeTheDelegate(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    recorder := httptest.NewRecorder()

    written := writeResponse(
        sessionRuntime(),
        melodyRequest,
        recorder,
        EmptyResponse(99),
        nil,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{},
        0,
        nil,
    )

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the refusal ahead of the delegate to write 500, got %d", recorder.Code)
    }

    if nethttp.StatusInternalServerError != written.StatusCode() {
        t.Fatalf("expected the returned response to be the rendered 500, got %d", written.StatusCode())
    }
}

func TestIsClientAbortWriteError_ClassifiesTheBrokenPipeAndTheCancelledRequest(t *testing.T) {
    liveRequest := NewRequest(httptest.NewRequest(nethttp.MethodGet, "/download", nil), nil, nil, nil)

    if false == isClientAbortWriteError(liveRequest, syscall.EPIPE) {
        t.Fatal("expected a broken pipe to classify as the client's abort")
    }

    if false == isClientAbortWriteError(liveRequest, syscall.ECONNRESET) {
        t.Fatal("expected a connection reset to classify as the client's abort")
    }

    if true == isClientAbortWriteError(liveRequest, errors.New("disk full")) {
        t.Fatal("expected a server-side failure to stay one")
    }

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()
    cancelledRequest := NewRequest(httptest.NewRequest(nethttp.MethodGet, "/download", nil).WithContext(cancelledContext), nil, nil, nil)

    if false == isClientAbortWriteError(cancelledRequest, errors.New("any write failure")) {
        t.Fatal("expected a cancelled request context to classify the write failure as the client's abort")
    }
}

func TestIsClientAbortWriteError_KeepsAFailureUnderAnApplicationDeadlineOrCauseServerSide(t *testing.T) {
    expiredContext, cancelExpired := context.WithDeadline(context.Background(), time.Unix(0, 0))
    defer cancelExpired()
    expiredRequest := NewRequest(httptest.NewRequest(nethttp.MethodGet, "/download", nil).WithContext(expiredContext), nil, nil, nil)

    if true == isClientAbortWriteError(expiredRequest, errors.New("disk full")) {
        t.Fatal("expected a write failure under an expired application deadline to stay a server-side failure")
    }

    if false == isClientAbortWriteError(expiredRequest, syscall.EPIPE) {
        t.Fatal("expected a broken pipe to classify as the client's abort whatever the context carries")
    }

    causedContext, cancelCaused := context.WithCancelCause(context.Background())
    cancelCaused(errors.New("request budget spent"))
    causedRequest := NewRequest(httptest.NewRequest(nethttp.MethodGet, "/download", nil).WithContext(causedContext), nil, nil, nil)

    if true == isClientAbortWriteError(causedRequest, errors.New("disk full")) {
        t.Fatal("expected a write failure under an application cancellation with a cause to stay a server-side failure")
    }
}

func TestCloseResponseBodySafely_ContainsAPanickingClose(t *testing.T) {
    closeErr := closeResponseBodySafely(&panickingCloser{})
    if nil == closeErr {
        t.Fatal("expected the contained panic to answer as an error")
    }

    if false == strings.Contains(closeErr.Error(), "close panicked") {
        t.Fatalf("expected the error to name the contained panic, got %q", closeErr.Error())
    }
}

type panickingCloser struct{}

func (instance *panickingCloser) Close() error {
    panic("close died on the state the panic invalidated")
}

func TestWriteResponse_ADiscardedResponseReportsTheCommittedStatus(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/stream", nil)
    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    writer := newRecordingResponseWriter(httptest.NewRecorder())
    writer.WriteHeader(nethttp.StatusOK)
    _, _ = writer.Write([]byte("data: hello\n\n"))

    reported := writeResponse(
        sessionRuntime(),
        melodyRequest,
        writer,
        EmptyResponse(nethttp.StatusNoContent),
        nil,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{},
        0,
        nil,
    )

    if nethttp.StatusOK != reported.StatusCode() {
        t.Fatalf("expected the committed 200 reported to the access log, got %d", reported.StatusCode())
    }
}

func TestWriteResponse_AHijackedConnectionKeepsTheSubstituteStatus(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/upgrade", nil)
    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    writer := newRecordingResponseWriter(&hijackableResponseWriter{ResponseWriter: httptest.NewRecorder()})
    _, _, hijackErr := writer.Hijack()
    if nil != hijackErr {
        t.Fatalf("expected the hijack to succeed, got %v", hijackErr)
    }

    reported := writeResponse(
        sessionRuntime(),
        melodyRequest,
        writer,
        EmptyResponse(nethttp.StatusNoContent),
        nil,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{},
        0,
        nil,
    )

    if nethttp.StatusNoContent != reported.StatusCode() {
        t.Fatalf("expected the substitute kept for the statusless hijack, got %d", reported.StatusCode())
    }
}

type sessionPersistenceCaptureRecord struct {
    level   loggingcontract.Level
    message string
    context map[string]any
}

type sessionPersistenceCaptureLogger struct {
    entries []sessionPersistenceCaptureRecord
}

func (instance *sessionPersistenceCaptureLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.entries = append(instance.entries, sessionPersistenceCaptureRecord{level: level, message: message, context: context})
}

func (instance *sessionPersistenceCaptureLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *sessionPersistenceCaptureLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *sessionPersistenceCaptureLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *sessionPersistenceCaptureLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *sessionPersistenceCaptureLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

var _ loggingcontract.Logger = (*sessionPersistenceCaptureLogger)(nil)

func writeResponseWithSessionOutcome(
    t *testing.T,
    logger loggingcontract.Logger,
    sessionManager contract.Manager,
    sessionInstance contract.Session,
) {
    t.Helper()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger, container.OverrideOptions{})
    runtimeInstance := runtime.NewRequest(context.Background(), scope)

    netRequest := httptest.NewRequest(nethttp.MethodPost, "http://example.com/account/settings", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"
    netRequest.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: sessionInstance.Id()})

    sessionRequest := NewRequest(netRequest, nil, nil, nil)
    sessionRequest.Attributes().Set(requestAttributeSession, sessionInstance)

    writeResponse(
        runtimeInstance,
        sessionRequest,
        httptest.NewRecorder(),
        TextResponse(nethttp.StatusOK, "handler body"),
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
        0,
        nil,
    )
}

func TestWriteResponse_ADeletedSessionIsRecordedAtWarningWithTheRequestCoordinates(t *testing.T) {
    capture := &sessionPersistenceCaptureLogger{}

    writeResponseWithSessionOutcome(
        t,
        capture,
        &stubSessionManager{
            saveErr: exception.NewError(
                "session was deleted and cannot be saved again",
                map[string]any{"sessionReference": sessionIdLogReference("session-123")},
                session.ErrSessionDeleted,
            ),
        },
        &stubSession{id: "session-123", isModified: true},
    )

    if 1 != len(capture.entries) {
        t.Fatalf("expected exactly one record, got %d: %v", len(capture.entries), capture.entries)
    }

    record := capture.entries[0]

    if loggingcontract.LevelWarning != record.level {
        t.Fatalf("expected the session ending at warning, got %v", record.level)
    }

    if "session was deleted while the request was in flight" != record.message {
        t.Fatalf("unexpected message: %q", record.message)
    }

    if nil != record.context["sessionId"] {
        t.Fatalf("the raw session id must not reach the log, got %v", record.context["sessionId"])
    }

    for key, expected := range map[string]any{"sessionReference": sessionIdLogReference("session-123"), "method": nethttp.MethodPost, "path": "/account/settings"} {
        if expected != record.context[key] {
            t.Fatalf("expected %q to be %v, got %v in %v", key, expected, record.context[key], record.context)
        }
    }

    if _, hasRetiredKey := record.context["sessionRef"]; true == hasRetiredKey {
        t.Fatalf("expected the reference under sessionReference only, got %v", record.context)
    }
}

func TestWriteResponse_ASaveOutageStaysAtErrorAndNamesTheSessionAndTheRoute(t *testing.T) {
    capture := &sessionPersistenceCaptureLogger{}

    writeResponseWithSessionOutcome(
        t,
        capture,
        &stubSessionManager{saveErr: errors.New("redis: connection refused")},
        &stubSession{id: "session-456", isModified: true},
    )

    if 1 != len(capture.entries) {
        t.Fatalf("expected exactly one record, got %d: %v", len(capture.entries), capture.entries)
    }

    record := capture.entries[0]

    if loggingcontract.LevelError != record.level {
        t.Fatalf("expected a storage outage at error, got %v", record.level)
    }

    if "failed to save session" != record.message {
        t.Fatalf("unexpected message: %q", record.message)
    }

    if nil != record.context["sessionId"] {
        t.Fatalf("the raw session id must not reach the log, got %v", record.context["sessionId"])
    }

    for key, expected := range map[string]any{"sessionReference": sessionIdLogReference("session-456"), "method": nethttp.MethodPost, "path": "/account/settings"} {
        if expected != record.context[key] {
            t.Fatalf("expected %q to be %v, got %v in %v", key, expected, record.context[key], record.context)
        }
    }
}

func TestWriteResponse_ADeleteOutageStaysAtErrorAndNamesTheSessionAndTheRoute(t *testing.T) {
    capture := &sessionPersistenceCaptureLogger{}

    writeResponseWithSessionOutcome(
        t,
        capture,
        &stubSessionManager{deleteErr: errors.New("redis: connection refused")},
        &stubSession{id: "session-789", isCleared: true},
    )

    if 1 != len(capture.entries) {
        t.Fatalf("expected exactly one record, got %d: %v", len(capture.entries), capture.entries)
    }

    record := capture.entries[0]

    if loggingcontract.LevelError != record.level {
        t.Fatalf("expected a storage outage at error, got %v", record.level)
    }

    if "failed to delete session" != record.message {
        t.Fatalf("unexpected message: %q", record.message)
    }

    if nil != record.context["sessionId"] {
        t.Fatalf("the raw session id must not reach the log, got %v", record.context["sessionId"])
    }

    for key, expected := range map[string]any{"sessionReference": sessionIdLogReference("session-789"), "method": nethttp.MethodPost, "path": "/account/settings"} {
        if expected != record.context[key] {
            t.Fatalf("expected %q to be %v, got %v in %v", key, expected, record.context[key], record.context)
        }
    }
}

func TestRequestPathIsCanonical_RefusesFoldsAndAllowsTrailingSlash(t *testing.T) {
    canonicalPaths := []string{
        "/",
        "/login",
        "/admin",
        "/admin/",
        "/admin/secret",
        "/admin/secret/",
        "/admin//",
        "/.well-known/acme-challenge/token",
        "/assets/app.css",
        "/public /",
        "/a b/c",
    }

    for _, canonicalPath := range canonicalPaths {
        if false == requestPathIsCanonical(canonicalPath) {
            t.Fatalf("expected %q to be accepted as canonical", canonicalPath)
        }
    }

    foldedPaths := []string{
        "/admin/x/../../login",
        "/admin/..",
        "/admin/../login",
        "//admin/secret",
        "/a//b",
        "/admin/./x",
        "/./login",
        "/admin/.",
        "/../etc/passwd",
        "/public ",
        "/public\t",
        "/public\u00a0",
        "/ ",
        " /public",
        "\t/public",
        "\u00a0/public",
        " ",
    }

    for _, foldedPath := range foldedPaths {
        if true == requestPathIsCanonical(foldedPath) {
            t.Fatalf("expected %q to be refused as non-canonical", foldedPath)
        }
    }
}

func TestRequestPathIsCanonical_LeavesNonPathTargetsToTheRouter(t *testing.T) {
    for _, target := range []string{"*", "example.com:443", ""} {
        if false == requestPathIsCanonical(target) {
            t.Fatalf("expected non-path target %q to be left to the router", target)
        }
    }
}

func TestRequestPathIsCanonical_JudgesASlashLessPathAsTheMatchersReadIt(t *testing.T) {
    for _, foldedPath := range []string{"admin/../public", "admin/./x", "admin//x", "./admin", "admin ", "..", "../public"} {
        if true == requestPathIsCanonical(foldedPath) {
            t.Fatalf("expected slash-less path %q that folds to be refused", foldedPath)
        }
    }

    for _, canonicalPath := range []string{"admin", "admin/x", "admin/x/"} {
        if false == requestPathIsCanonical(canonicalPath) {
            t.Fatalf("expected slash-less canonical path %q to pass", canonicalPath)
        }
    }
}

func TestRequestPathAsRouted_KeepsAnEncodedSeparatorInsideItsSegmentAndDecodesTheRest(t *testing.T) {
    for escapedPath, routed := range map[string]string{
        "/":               "/",
        "/public":         "/public",
        "/public/":        "/public/",
        "/caf%C3%A9":      "/café",
        "/a%20b/c":        "/a b/c",
        "/a%25b":          "/a%b",
        "/public%2F":      "/public%2F",
        "/public%2f":      "/public%2F",
        "/admin%2Fusers":  "/admin%2Fusers",
        "/files/a%2Fb/c":  "/files/a%2Fb/c",
        "/public%252F":    "/public%2F",
        "/admin/users":    "/admin/users",
        "/a+b":            "/a+b",
        "/a%zz/b":         "/a%zz/b",
        "/a%2/b":          "/a%2/b",
        "*":               "*",
        "example.com:443": "example.com:443",
        "":                "",
    } {
        if routed != RequestPathAsRouted(escapedPath) {
            t.Fatalf("expected %q to be read as %q, got %q", escapedPath, routed, RequestPathAsRouted(escapedPath))
        }
    }
}

func TestRequestPathAsRouted_AgreesWithTheRoutersOwnSegments(t *testing.T) {
    for _, escapedPath := range []string{"/caf%C3%A9", "/a%20b/c", "/a+b", "/a%zz/b", "/files/a%2Fb/c", "/public%252F", "/x/%2E%2E/y", "/a%00b"} {
        routedSegments := strings.Split(RequestPathAsRouted(escapedPath), "/")
        routerSegments := splitRequestPath(escapedPath)

        if len(routedSegments) != len(routerSegments) {
            t.Fatalf("expected %q to have the router's %d segments, got %d: %q", escapedPath, len(routerSegments), len(routedSegments), routedSegments)
        }

        for index, routerSegment := range routerSegments {
            if strings.ReplaceAll(routerSegment, "/", "%2F") != routedSegments[index] {
                t.Fatalf("expected segment %d of %q to read %q as the router does, got %q", index, escapedPath, routerSegment, routedSegments[index])
            }
        }
    }
}

func TestMarkResponsePrivateForSessionCookie(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        existing string
        expected string
    }{
        {"public becomes private keeping max-age", "public, max-age=3600", "max-age=3600, private"},
        {"absent becomes private", "", "private"},
        {"already private is left", "private, max-age=60", "private, max-age=60"},
        {"no-store is left, public dropped", "public, no-store", "no-store"},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            response := NewResponse(nethttp.StatusOK, nil)
            if "" != testCase.existing {
                response.Headers().Set("Cache-Control", testCase.existing)
            }

            markResponsePrivateForSessionCookie(response)

            got := response.Headers().Get("Cache-Control")
            if testCase.expected != got {
                t.Fatalf("expected Cache-Control %q, got %q", testCase.expected, got)
            }

            lower := strings.ToLower(got)
            if true == strings.Contains(lower, "public") {
                t.Fatalf("a session-cookie response must never stay publicly cacheable, got %q", got)
            }
        })
    }
}

func TestMatchesHost_FoldsCase(t *testing.T) {
    if false == matchesHost("api.example.com", "API.Example.COM") {
        t.Fatalf("expected the host comparison to fold case, as the header definition requires")
    }
}

func TestMatchesHost_IgnoresThePortWhenTheRouteDeclaredNone(t *testing.T) {
    if false == matchesHost("api.example.com", "api.example.com:8443") {
        t.Fatalf("expected a route bound to a host to be reachable behind a non-default port")
    }
}

func TestMatchesHost_ComparesThePortWhenTheRouteDeclaredOne(t *testing.T) {
    if false == matchesHost("api.example.com:8443", "api.example.com:8443") {
        t.Fatalf("expected the declared port to match itself")
    }

    if true == matchesHost("api.example.com:8443", "api.example.com:9999") {
        t.Fatalf("expected a route that declares a port to discriminate on it")
    }

    if true == matchesHost("api.example.com:8443", "api.example.com") {
        t.Fatalf("expected a route that declares a port not to match a request without one")
    }
}

func TestMatchesHost_ReachesABracketedIpv6RouteBehindAPort(t *testing.T) {
    if false == matchesHost("[::1]", "[::1]:8080") {
        t.Fatalf("expected a route bound to a bracketed ipv6 literal to be reachable behind a port")
    }

    if false == matchesHost("[2001:db8::1]", "[2001:DB8::1]:8443") {
        t.Fatalf("expected the bracketed literal to fold case the way every other host does")
    }
}

func TestMatchesHost_ComparesThePortWhenTheBracketedRouteDeclaredOne(t *testing.T) {
    if false == matchesHost("[::1]:8080", "[::1]:8080") {
        t.Fatalf("expected the declared port of a bracketed route to match itself")
    }

    if true == matchesHost("[::1]:8080", "[::1]:9999") {
        t.Fatalf("expected a bracketed route that declares a port to discriminate on it")
    }

    if true == matchesHost("[::1]:8080", "[::1]") {
        t.Fatalf("expected a bracketed route that declares a port not to match a request without one")
    }
}

func TestMatchesHost_StillRefusesADifferentIpv6Host(t *testing.T) {
    if true == matchesHost("[::1]", "[::2]:8080") {
        t.Fatalf("expected a different ipv6 host to be refused")
    }

    if true == matchesHost("[::1]", "api.example.com:8080") {
        t.Fatalf("expected a name host to be refused by a route bound to an ipv6 literal")
    }
}

func TestMatchesHost_StillRefusesADifferentHost(t *testing.T) {
    if true == matchesHost("api.example.com", "evil.example.com") {
        t.Fatalf("expected a different host to be refused")
    }

    if true == matchesHost("api.example.com", "api.example.com.evil.test") {
        t.Fatalf("expected a suffix-extended host to be refused")
    }
}

func TestRequestPathIsRoutable_RefusesTheAsteriskForm(t *testing.T) {
    if true == requestPathIsRoutable("*") {
        t.Fatalf("expected the asterisk-form target not to be path-routed")
    }

    if false == requestPathIsRoutable("/admin") {
        t.Fatalf("expected an origin-form target to be path-routed")
    }

    if false == requestPathIsRoutable("") {
        t.Fatalf("expected an empty target to be path-routed as the root")
    }
}

func TestSplitRequestPath_KeepsAnEncodedSeparatorInsideItsSegment(t *testing.T) {
    segments := splitRequestPath("/admin%2Fusers")

    if 2 != len(segments) {
        t.Fatalf("expected the encoded separator not to split the path, got %v", segments)
    }

    if "admin/users" != segments[1] {
        t.Fatalf("expected the segment to be unescaped in place, got %q", segments[1])
    }
}

func TestSplitRequestPath_ReadsAnEmptyPathAsTheRoot(t *testing.T) {
    if false == slicesEqualForTest(splitRequestPath(""), splitRequestPath("/")) {
        t.Fatalf("expected an empty path to split exactly as the root does")
    }
}

func slicesEqualForTest(left []string, right []string) bool {
    if len(left) != len(right) {
        return false
    }

    for index := range left {
        if left[index] != right[index] {
            return false
        }
    }

    return true
}

type closeTrackingResponseBodyReader struct {
    reader *strings.Reader
    closed bool
}

func (instance *closeTrackingResponseBodyReader) Read(destination []byte) (int, error) {
    return instance.reader.Read(destination)
}

func (instance *closeTrackingResponseBodyReader) Close() error {
    instance.closed = true

    return nil
}

func TestWriteResponse_ClosesTheBodyItDiscardsForAnOutOfRangeStatus(t *testing.T) {
    bodyReader := &closeTrackingResponseBodyReader{reader: strings.NewReader("file bytes")}

    response := NewResponse(nethttp.StatusOK, nil)
    response.SetBodyReader(bodyReader)
    response.SetStatusCode(1200)

    request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "/download", nil), nil, nil, nil)

    written := writeResponse(nil, request, httptest.NewRecorder(), response, nil, httpcontract.ForwardedHeadersPolicy{}, httpcontract.SessionCookiePolicy{}, 0, nil)

    if nethttp.StatusInternalServerError != written.StatusCode() {
        t.Fatalf("expected the rendered 500, got %d", written.StatusCode())
    }

    if false == bodyReader.closed {
        t.Fatal("expected the discarded response body to be closed")
    }
}

func TestMarkResponsePrivateForSessionCookie_KeepsEveryFieldLineAndQuotedList(t *testing.T) {
    t.Run("directives on a second field line survive", func(t *testing.T) {
        response := NewResponse(nethttp.StatusOK, nil)
        response.Headers().Add("Cache-Control", "public")
        response.Headers().Add("Cache-Control", "max-age=60")

        markResponsePrivateForSessionCookie(response)

        got := response.Headers().Get("Cache-Control")
        if false == strings.Contains(got, "max-age=60") {
            t.Fatalf("expected the second field line's directive to survive, got %q", got)
        }

        if true == strings.Contains(strings.ToLower(got), "public") {
            t.Fatalf("a session-cookie response must never stay publicly cacheable, got %q", got)
        }

        if false == strings.Contains(got, "private") {
            t.Fatalf("expected the response to be marked private, got %q", got)
        }
    })

    t.Run("a quoted field-name list is not cut", func(t *testing.T) {
        response := NewResponse(nethttp.StatusOK, nil)
        response.Headers().Set("Cache-Control", `no-cache="X-One, Public, X-Two", max-age=60`)

        markResponsePrivateForSessionCookie(response)

        got := response.Headers().Get("Cache-Control")
        if false == strings.Contains(got, `no-cache="X-One, Public, X-Two"`) {
            t.Fatalf("expected the quoted field-name list to survive whole, got %q", got)
        }
    })
}

func TestWriteResponse_AnEarlyHintLeavesTheReturnedResponseToBeWritten(t *testing.T) {
    server := httptest.NewServer(nethttp.HandlerFunc(func(rawWriter nethttp.ResponseWriter, rawRequest *nethttp.Request) {
        writer := newRecordingResponseWriter(rawWriter)
        writer.WriteHeader(nethttp.StatusEarlyHints)

        writeResponse(
            newTestRuntime(),
            NewRequest(rawRequest, nil, nil, nil),
            writer,
            EmptyResponse(nethttp.StatusCreated),
            nil,
            httpcontract.ForwardedHeadersPolicy{
                TrustForwardedHeaders: false,
                TrustedProxyList:      []string{},
            },
            httpcontract.SessionCookiePolicy{
                Path:     "/",
                Domain:   "",
                SameSite: nethttp.SameSiteLaxMode,
            },
            0,
            nil,
        )
    }))
    defer server.Close()

    response, getErr := nethttp.Get(server.URL)
    if nil != getErr {
        t.Fatalf("expected the request to succeed, got %v", getErr)
    }
    _ = response.Body.Close()

    if nethttp.StatusCreated != response.StatusCode {
        t.Fatalf("expected the returned 201 after the early hint, got %d", response.StatusCode)
    }
}

func TestWriteResponse_AClearedSessionTheRequestDoesNotNameIsNotDeleted(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodPost, "http://example.com/logout", nil)
    netRequest.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: "fedcba9876543210fedcba9876543210"})

    sessionManager := &stubSessionManager{}
    writer := httptest.NewRecorder()

    sessionRequest := NewRequest(netRequest, nil, nil, nil)
    sessionRequest.Attributes().Set(requestAttributeSession, &stubSession{id: "0123456789abcdef0123456789abcdef", isCleared: true})

    writeResponse(
        sessionRuntime(),
        sessionRequest,
        writer,
        EmptyResponse(nethttp.StatusOK),
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
        0,
        nil,
    )

    if 0 != sessionManager.deleteCalled {
        t.Fatalf("expected no delete for a session the request does not name, got %d", sessionManager.deleteCalled)
    }

    cookies := writer.Result().Cookies()
    if 1 != len(cookies) || -1 != cookies[0].MaxAge {
        t.Fatalf("expected the session cookie expired all the same, got %v", cookies)
    }
}

type deleteCountingSessionStorage struct {
    inner   *session.InMemoryStorage
    deletes int
}

func (instance *deleteCountingSessionStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    return instance.inner.Load(runtimeInstance, sessionId)
}

func (instance *deleteCountingSessionStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    return instance.inner.Save(runtimeInstance, sessionId, data, ttl)
}

func (instance *deleteCountingSessionStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    instance.deletes++

    return instance.inner.Delete(runtimeInstance, sessionId)
}

func (instance *deleteCountingSessionStorage) CloseWithContext(closeContext context.Context) error {
    return instance.inner.CloseWithContext(context.Background())
}

func serveALogout(t *testing.T, storage *deleteCountingSessionStorage, cookieValue string) *httptest.ResponseRecorder {
    t.Helper()

    serviceContainer := newHttpTestContainerWithSessionStorage(storage)

    router := NewRouter()
    router.Handle(
        "/logout",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, _ := RequestSession(request)
            sessionInstance.Clear()

            return EmptyResponse(nethttp.StatusNoContent), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodPost}},
    )

    request := httptest.NewRequest(nethttp.MethodPost, "/logout", nil)
    request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: cookieValue})

    recorder := httptest.NewRecorder()
    NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, request)

    return recorder
}

func TestKernel_ALogoutWhoseCookieNamesNoStoredSessionDeletesNothing(t *testing.T) {
    storage := &deleteCountingSessionStorage{inner: session.NewInMemoryStorage(session.InMemoryStorageConfig{})}

    recorder := serveALogout(t, storage, "0123456789abcdef0123456789abcdef")

    if 0 != storage.deletes {
        t.Fatalf("expected no removal for an invented cookie, got %d", storage.deletes)
    }

    cookies := recorder.Result().Cookies()
    if 1 != len(cookies) || -1 != cookies[0].MaxAge {
        t.Fatalf("expected the cookie expired, got %v", cookies)
    }
}

func TestKernel_ALogoutWhoseCookieNamesAStoredSessionDeletesIt(t *testing.T) {
    storage := &deleteCountingSessionStorage{inner: session.NewInMemoryStorage(session.InMemoryStorageConfig{})}

    storedId := "0123456789abcdef0123456789abcdef"
    if saveErr := storage.inner.Save(sessionRuntime(), storedId, map[string]any{"userId": "u-1"}, bound.Of(time.Minute)); nil != saveErr {
        t.Fatalf("unexpected error seeding the session: %v", saveErr)
    }

    serveALogout(t, storage, storedId)

    if 1 != storage.deletes {
        t.Fatalf("expected the stored session removed once, got %d", storage.deletes)
    }

    if _, held, _ := storage.inner.Load(sessionRuntime(), storedId); true == held {
        t.Fatalf("expected the stored session gone")
    }
}

func sessionCookieWrittenOverPlainHttp(t *testing.T, sameSite nethttp.SameSite, secure httpcontract.SessionCookieSecurePolicy) *nethttp.Cookie {
    t.Helper()

    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"

    writer := httptest.NewRecorder()

    sessionRequest := NewRequest(netRequest, nil, nil, nil)
    sessionRequest.Attributes().Set(requestAttributeSession, &stubSession{id: "0123456789abcdef0123456789abcdef", isModified: true})

    writeResponse(
        newTestRuntime(),
        sessionRequest,
        writer,
        EmptyResponse(nethttp.StatusOK),
        &stubSessionManager{},
        httpcontract.ForwardedHeadersPolicy{TrustedProxyList: []string{}},
        httpcontract.SessionCookiePolicy{Path: "/", SameSite: sameSite, Secure: secure},
        0,
        nil,
    )

    cookies := writer.Result().Cookies()
    if 1 != len(cookies) {
        t.Fatalf("expected one set-cookie, got %d", len(cookies))
    }

    return cookies[0]
}

func TestWriteResponse_ASameSiteNoneSessionCookieIsSecureOverPlainHttpUnderFromScheme(t *testing.T) {
    if cookie := sessionCookieWrittenOverPlainHttp(t, nethttp.SameSiteNoneMode, httpcontract.SessionCookieSecureFromScheme); false == cookie.Secure {
        t.Fatalf("expected a SameSite=None session cookie written Secure, got %+v", cookie)
    }
}

func TestWriteResponse_ALaxSessionCookieOverPlainHttpIsNotSecure(t *testing.T) {
    if cookie := sessionCookieWrittenOverPlainHttp(t, nethttp.SameSiteLaxMode, httpcontract.SessionCookieSecureFromScheme); true == cookie.Secure {
        t.Fatalf("expected a Lax session cookie over plain http not Secure, got %+v", cookie)
    }
}

func TestMarkResponsePrivateForSessionCookie_AnUnbalancedQuoteInCacheControlIsReplacedByNoStorePrivate(t *testing.T) {
    for _, cacheControl := range []string{`no-cache="set-cookie`, `no-store, x="open`, `max-age=60, x="open`} {
        response := EmptyResponse(nethttp.StatusOK)
        response.Headers().Set("Cache-Control", cacheControl)

        markResponsePrivateForSessionCookie(response)

        if "no-store, private" != response.Headers().Get("Cache-Control") {
            t.Fatalf("expected %q replaced by no-store, private, got %q", cacheControl, response.Headers().Get("Cache-Control"))
        }
    }
}

func TestMarkResponsePrivateForSessionCookie_ABalancedQuoteKeepsItsMembers(t *testing.T) {
    response := EmptyResponse(nethttp.StatusOK)
    response.Headers().Set("Cache-Control", `no-cache="set-cookie, a\"b", max-age=0`)

    markResponsePrivateForSessionCookie(response)

    if `no-cache="set-cookie, a\"b", max-age=0, private` != response.Headers().Get("Cache-Control") {
        t.Fatalf("expected the members kept and private appended, got %q", response.Headers().Get("Cache-Control"))
    }
}

func TestControllerHandler_SignatureRefusalsCarryTheSignature(t *testing.T) {
    t.Run("first argument", func(t *testing.T) {
        defer func() {
            panicErr, isPanicErr := recover().(*exception.Error)
            if false == isPanicErr {
                t.Fatalf("expected an exception error")
            }

            if "func(string) (*http.Response, error)" != panicErr.Context()["type"] {
                t.Fatalf("expected the refusal to carry the signature, got %v", panicErr.Context())
            }
        }()

        _ = ControllerHandler(func(value string) (*Response, error) { return EmptyResponse(200), nil })
    })

    t.Run("result count", func(t *testing.T) {
        defer func() {
            panicErr, isPanicErr := recover().(*exception.Error)
            if false == isPanicErr {
                t.Fatalf("expected an exception error")
            }

            if "func(*http.Request) *http.Response" != panicErr.Context()["type"] || 1 != panicErr.Context()["resultCount"] {
                t.Fatalf("expected the refusal to carry the signature and the count, got %v", panicErr.Context())
            }
        }()

        _ = ControllerHandler(func(request *Request) *Response { return EmptyResponse(200) })
    })

    signatureCases := map[string]struct {
        controller any
        signature  string
        countKey   string
        count      int
    }{
        "argument count": {controller: func() (*Response, error) { return EmptyResponse(200), nil }, signature: "func() (*http.Response, error)", countKey: "argumentCount", count: 0},
        "first result":   {controller: func(request *Request) (string, error) { return "", nil }, signature: "func(*http.Request) (string, error)"},
        "second result":  {controller: func(request *Request) (*Response, string) { return EmptyResponse(200), "" }, signature: "func(*http.Request) (*http.Response, string)"},
    }

    for name, signatureCase := range signatureCases {
        t.Run(name, func(t *testing.T) {
            defer func() {
                panicErr, isPanicErr := recover().(*exception.Error)
                if false == isPanicErr {
                    t.Fatalf("expected an exception error")
                }

                if signatureCase.signature != panicErr.Context()["type"] {
                    t.Fatalf("expected the refusal to carry the signature, got %v", panicErr.Context())
                }

                if "" != signatureCase.countKey && signatureCase.count != panicErr.Context()[signatureCase.countKey] {
                    t.Fatalf("expected the refusal to count under %s, got %v", signatureCase.countKey, panicErr.Context())
                }
            }()

            _ = ControllerHandler(signatureCase.controller)
        })
    }
}

type cyclicClosingBody struct{}

func (instance *cyclicClosingBody) Read(buffer []byte) (int, error) {
    return 0, io.EOF
}

func (instance *cyclicClosingBody) Close() error {
    return newCyclicChainError()
}

func TestIsClientAbortWriteError_ReturnsOnACyclicChain(t *testing.T) {
    request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "/download", nil), nil, nil, nil)

    assertReturnsOnACyclicChain(t, func() {
        if true == isClientAbortWriteError(request, newCyclicChainError()) {
            t.Error("expected a cyclic chain not to read as a client abort")
        }
    })
}

func TestCloseDiscardedResponseBody_ReturnsOnACyclicChain(t *testing.T) {
    response := TextResponse(nethttp.StatusOK, "discarded")
    response.SetBodyReader(&cyclicClosingBody{})

    assertReturnsOnACyclicChain(t, func() {
        closeDiscardedResponseBody(response, logging.NewNopLogger())
    })
}

func TestWriteResponse_AnUnmarkedCyclicChainFromTheSessionSaveIsAnsweredWithAnInternalServerError(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"

    melodyRequest := NewRequest(netRequest, nil, nil, nil)
    writer := httptest.NewRecorder()

    assertReturnsOnACyclicChain(t, func() {
        melodyRequest.Attributes().Set(requestAttributeSession, &stubSession{id: "session-123", isModified: true})

        writeResponse(
            sessionRuntime(),
            melodyRequest,
            writer,
            TextResponse(nethttp.StatusOK, "welcome"),
            &stubSessionManager{saveErr: newCyclicChainError()},
            httpcontract.ForwardedHeadersPolicy{},
            httpcontract.SessionCookiePolicy{Path: "/"},
            0,
            nil,
        )
    })

    if nethttp.StatusInternalServerError != writer.Result().StatusCode {
        t.Fatalf("expected 500, got %d", writer.Result().StatusCode)
    }
}

func TestWriteResponse_AnInformationalStatusIsAnsweredAsTheRenderedInternalServerError(t *testing.T) {
    melodyRequest := NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil), nil, nil, nil)

    body := &closeTrackingReader{}
    response := &Response{statusCode: nethttp.StatusEarlyHints, headers: make(nethttp.Header), bodyReader: body}
    response.Headers().Add("Set-Cookie", "leaked=1")

    writer := httptest.NewRecorder()
    written := writeResponse(nil, melodyRequest, writer, response, nil, httpcontract.ForwardedHeadersPolicy{}, httpcontract.SessionCookiePolicy{}, 0, nil)

    if nethttp.StatusInternalServerError != writer.Code || nethttp.StatusInternalServerError != written.StatusCode() {
        t.Fatalf("expected the rendered 500 written and answered, got %d and %d", writer.Code, written.StatusCode())
    }

    if "" != writer.Header().Get("Set-Cookie") || false == body.closed.Load() {
        t.Fatalf("expected the refused response's cookie dropped and its body closed, got %v, closed %v", writer.Header(), body.closed.Load())
    }
}

func TestKernel_AResponseObjectWithSwitchingProtocolsIsAnsweredAsTheRendered500(t *testing.T) {
    melodyRequest := NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil), nil, nil, nil)

    recordingLogger := &recordsCaptureLogger{}
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})
    runtimeInstance := runtime.NewRequest(context.Background(), scope)

    body := &closeTrackingReader{}
    response := &Response{statusCode: nethttp.StatusSwitchingProtocols, headers: make(nethttp.Header), bodyReader: body}

    writer := httptest.NewRecorder()
    written := writeResponse(runtimeInstance, melodyRequest, writer, response, nil, httpcontract.ForwardedHeadersPolicy{}, httpcontract.SessionCookiePolicy{}, 0, nil)

    if nethttp.StatusInternalServerError != writer.Code || nethttp.StatusInternalServerError != written.StatusCode() {
        t.Fatalf("expected the rendered 500 written and answered, got %d and %d", writer.Code, written.StatusCode())
    }

    if false == body.closed.Load() {
        t.Fatal("expected the refused response's body closed")
    }

    for _, record := range recordingLogger.failureRecords() {
        if loggingcontract.LevelError == record.level && true == strings.Contains(record.message, "not a final status") && nethttp.StatusSwitchingProtocols == record.context["statusCode"] {
            return
        }
    }

    t.Fatalf("expected an error record naming status 101, got %+v", recordingLogger.failureRecords())
}

type persistencePlantedKey struct{}

func writeResponseUnderRequestContext(
    t *testing.T,
    requestContext context.Context,
    sessionManager contract.Manager,
    sessionInstance contract.Session,
    writer nethttp.ResponseWriter,
) httpcontract.Response {
    t.Helper()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logging.NewNopLogger(), container.OverrideOptions{})
    runtimeInstance := runtime.NewRequest(requestContext, scope)

    netRequest := httptest.NewRequest(nethttp.MethodPost, "http://example.com/account", nil).WithContext(requestContext)
    netRequest.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: sessionInstance.Id()})

    sessionRequest := NewRequest(netRequest, nil, nil, nil)
    sessionRequest.Attributes().Set(requestAttributeSession, sessionInstance)

    return writeResponse(
        runtimeInstance,
        sessionRequest,
        writer,
        TextResponse(nethttp.StatusOK, "handler body"),
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
        0,
        nil,
    )
}

func cancelledRequestContext() context.Context {
    requestContext, cancel := context.WithCancel(context.WithValue(context.Background(), persistencePlantedKey{}, "planted"))
    cancel()

    return requestContext
}

func TestWriteResponse_PersistsUnderAContextTheRequestsCancellationDoesNotReach(t *testing.T) {
    sessionManager := &stubSessionManager{}

    writeResponseUnderRequestContext(t, cancelledRequestContext(), sessionManager, &stubSession{id: "session-1", isModified: true}, httptest.NewRecorder())

    if 1 != sessionManager.saveCalled || nil == sessionManager.saveContext {
        t.Fatalf("expected one save, got %d", sessionManager.saveCalled)
    }

    if "planted" != sessionManager.saveContext.Value(persistencePlantedKey{}) {
        t.Fatalf("expected the persistence context to keep the request's values")
    }

    if 0 >= sessionManager.saveDeadline || 5*time.Second < sessionManager.saveDeadline {
        t.Fatalf("expected a deadline within 5s, got %s", sessionManager.saveDeadline)
    }
}

func TestWriteResponse_PersistsUnderTheRuntimesContextOverTheNetRequests(t *testing.T) {
    sessionManager := &stubSessionManager{}
    sessionInstance := &stubSession{id: "session-1", isModified: true}

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logging.NewNopLogger(), container.OverrideOptions{})
    runtimeInstance := runtime.NewRequest(context.WithValue(context.Background(), persistencePlantedKey{}, "planted"), scope)

    netRequest := httptest.NewRequest(nethttp.MethodPost, "http://example.com/account", nil)
    netRequest.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: sessionInstance.Id()})

    sessionRequest := NewRequest(netRequest, nil, nil, nil)
    sessionRequest.Attributes().Set(requestAttributeSession, sessionInstance)

    writeResponse(runtimeInstance, sessionRequest, httptest.NewRecorder(), TextResponse(nethttp.StatusOK, "handler body"), sessionManager, httpcontract.ForwardedHeadersPolicy{}, httpcontract.SessionCookiePolicy{Path: "/"}, 0, nil)

    if 1 != sessionManager.saveCalled || "planted" != sessionManager.saveContext.Value(persistencePlantedKey{}) {
        t.Fatalf("expected one save under the runtime's context, got %d", sessionManager.saveCalled)
    }
}

func TestWriteResponse_DeletesUnderAContextTheRequestsCancellationDoesNotReach(t *testing.T) {
    sessionManager := &stubSessionManager{}

    writeResponseUnderRequestContext(t, cancelledRequestContext(), sessionManager, &stubSession{id: "session-1", isCleared: true}, httptest.NewRecorder())

    if 1 != sessionManager.deleteCalled || nil == sessionManager.deleteContext {
        t.Fatalf("expected one delete, got %d", sessionManager.deleteCalled)
    }

    if "planted" != sessionManager.deleteContext.Value(persistencePlantedKey{}) {
        t.Fatalf("expected the persistence context to keep the request's values")
    }

    if _, hasDeadline := sessionManager.deleteContext.Deadline(); false == hasDeadline {
        t.Fatalf("expected the delete bounded by the storage timeout")
    }
}

type errorAtCallContextManager struct {
    stubSessionManager
    errAtCall error
}

func (instance *errorAtCallContextManager) SaveSession(runtimeInstance containercontract.Runtime, sessionInstance contract.Session) error {
    ctx := runtimeInstance.Context()
    instance.errAtCall = ctx.Err()

    return instance.stubSessionManager.SaveSession(runtimeInstance, sessionInstance)
}

func TestWriteResponse_ThePersistenceContextIsLiveAtTheCall(t *testing.T) {
    sessionManager := &errorAtCallContextManager{}

    writeResponseUnderRequestContext(t, cancelledRequestContext(), sessionManager, &stubSession{id: "session-1", isModified: true}, httptest.NewRecorder())

    if 1 != sessionManager.saveCalled || nil != sessionManager.errAtCall {
        t.Fatalf("expected the save called under a live context, got %d calls and %v", sessionManager.saveCalled, sessionManager.errAtCall)
    }
}

func TestWriteResponse_AnUnlimitedStorageTimeoutSetsNoDeadline(t *testing.T) {
    sessionManager := &stubSessionManager{storageTimeout: bound.UnlimitedDuration()}

    writeResponseUnderRequestContext(t, context.Background(), sessionManager, &stubSession{id: "session-1", isModified: true}, httptest.NewRecorder())

    if _, hasDeadline := sessionManager.saveContext.Deadline(); true == hasDeadline || nil != sessionManager.saveContext.Err() {
        t.Fatalf("expected no deadline and a live context under an unlimited bound")
    }
}

func TestWriteResponse_AnAbsentBoundPersistsUnderTheDefault(t *testing.T) {
    sessionManager := &stubSessionManager{}

    writeResponseUnderRequestContext(t, context.Background(), sessionManager, &stubSession{id: "session-1", isModified: true}, httptest.NewRecorder())

    if 4*time.Second > sessionManager.saveDeadline || 5*time.Second < sessionManager.saveDeadline {
        t.Fatalf("expected the deadline about 5s away, got %s", sessionManager.saveDeadline)
    }
}

func TestWriteResponse_ADeclaredBoundIsTheDeadline(t *testing.T) {
    sessionManager := &stubSessionManager{storageTimeout: bound.Of(2 * time.Second)}

    writeResponseUnderRequestContext(t, context.Background(), sessionManager, &stubSession{id: "session-1", isModified: true}, httptest.NewRecorder())

    if time.Second > sessionManager.saveDeadline || 2*time.Second < sessionManager.saveDeadline {
        t.Fatalf("expected the deadline about 2s away, got %s", sessionManager.saveDeadline)
    }
}

func TestWriteResponse_ABoundTheRuleRefusesFailsThePersistence(t *testing.T) {
    sessionManager := &stubSessionManager{storageTimeout: bound.Of(time.Microsecond)}
    writer := httptest.NewRecorder()

    writeResponseUnderRequestContext(t, context.Background(), sessionManager, &stubSession{id: "session-1", isModified: true}, writer)

    if 0 != sessionManager.saveCalled {
        t.Fatalf("expected a refused bound never to reach the manager's save")
    }

    if nethttp.StatusInternalServerError != writer.Code || "" != writer.Header().Get("Retry-After") || 0 != len(writer.Result().Cookies()) {
        t.Fatalf("expected the failed persistence answered 500 without Retry-After or a cookie, got %d", writer.Code)
    }

    deleteManager := &stubSessionManager{storageTimeout: bound.Of(time.Microsecond)}
    deleteWriter := httptest.NewRecorder()
    writeResponseUnderRequestContext(t, context.Background(), deleteManager, &stubSession{id: "session-1", isCleared: true}, deleteWriter)

    if 0 != deleteManager.deleteCalled {
        t.Fatalf("expected a refused bound never to reach the manager's delete")
    }

    if cookies := deleteWriter.Result().Cookies(); nethttp.StatusInternalServerError != deleteWriter.Code || 1 != len(cookies) || 0 <= cookies[0].MaxAge {
        t.Fatalf("expected the failed delete answered 500 with the expiring cookie, got %d %v", deleteWriter.Code, cookies)
    }
}

type bodyObservingWriter struct {
    *httptest.ResponseRecorder
    sessionManager *stubSessionManager
    observed       bool
    errAtWrite     error
}

func (instance *bodyObservingWriter) Write(body []byte) (int, error) {
    if false == instance.observed && nil != instance.sessionManager.saveContext {
        instance.observed = true
        instance.errAtWrite = instance.sessionManager.saveContext.Err()
    }

    return instance.ResponseRecorder.Write(body)
}

func TestWriteResponse_ThePersistenceTimerIsReleasedBeforeTheBodyIsWritten(t *testing.T) {
    sessionManager := &stubSessionManager{}
    writer := &bodyObservingWriter{ResponseRecorder: httptest.NewRecorder(), sessionManager: sessionManager}

    writeResponseUnderRequestContext(t, context.Background(), sessionManager, &stubSession{id: "session-1", isModified: true}, writer)

    if false == writer.observed || false == errors.Is(writer.errAtWrite, context.Canceled) {
        t.Fatalf("expected the persistence context released before the body write, got %v", writer.errAtWrite)
    }
}

func TestWriteResponse_APanickingDoorLeavesNoPersistenceTimerArmed(t *testing.T) {
    for _, row := range []struct {
        name            string
        sessionManager  *stubSessionManager
        sessionInstance *stubSession
    }{
        {"save", &stubSessionManager{storageTimeout: bound.Of(time.Hour), savePanic: "save panicked"}, &stubSession{id: "session-1", isModified: true}},
        {"delete", &stubSessionManager{storageTimeout: bound.Of(time.Hour), deletePanic: "delete panicked"}, &stubSession{id: "session-1", isCleared: true}},
    } {
        t.Run(row.name, func(t *testing.T) {
            func() {
                defer func() { _ = recover() }()

                writeResponseUnderRequestContext(t, context.Background(), row.sessionManager, row.sessionInstance, httptest.NewRecorder())
            }()

            persistenceContext := row.sessionManager.saveContext
            if nil == persistenceContext {
                persistenceContext = row.sessionManager.deleteContext
            }

            if nil == persistenceContext || false == errors.Is(persistenceContext.Err(), context.Canceled) {
                t.Fatalf("expected the persistence context released through the panic, got %v", persistenceContext)
            }
        })
    }
}

func TestWriteResponse_AnUnmodifiedSessionBuildsNoPersistenceContext(t *testing.T) {
    sessionManager := &stubSessionManager{}

    writeResponseUnderRequestContext(t, context.Background(), sessionManager, &stubSession{id: "session-1"}, httptest.NewRecorder())

    if 0 != sessionManager.timeoutAsked || 0 != sessionManager.saveCalled || 0 != sessionManager.deleteCalled {
        t.Fatalf("expected an unmodified session never to ask for the persistence bound, asked %d times", sessionManager.timeoutAsked)
    }

    writeResponseUnderRequestContext(t, context.Background(), sessionManager, &stubSession{id: "session-1", isModified: true}, httptest.NewRecorder())

    if 1 != sessionManager.timeoutAsked {
        t.Fatalf("expected a modified session to ask for the persistence bound once, asked %d times", sessionManager.timeoutAsked)
    }
}

func TestWriteResponse_ASaveOutageAnswersTheKernelsRetryAfter(t *testing.T) {
    sessionRequest := NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil), nil, nil, nil)
    sessionRequest.Attributes().Set(requestAttributeSession, &stubSession{id: "session-1", isModified: true})

    writer := httptest.NewRecorder()
    writeResponse(
        sessionRuntime(),
        sessionRequest,
        writer,
        TextResponse(nethttp.StatusOK, "welcome"),
        &stubSessionManager{saveErr: exception.MarkInfrastructureFailure(errors.New("storage outage"))},
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
        42*time.Second,
        nil,
    )

    if nethttp.StatusServiceUnavailable != writer.Code || "42" != writer.Header().Get("Retry-After") {
        t.Fatalf("expected 503 with the kernel's Retry-After, got %d and %q", writer.Code, writer.Header().Get("Retry-After"))
    }
}

/* hangingSessionManager waits on the context of its save or delete and answers its bare, unmarked error, as a replaced manager that forgets the mark would */
type hangingSessionManager struct {
    stubSessionManager
}

func (instance *hangingSessionManager) SaveSession(runtimeInstance containercontract.Runtime, sessionInstance contract.Session) error {
    ctx := runtimeInstance.Context()
    instance.saveCalled++
    return awaitStorageContext(ctx)
}

func (instance *hangingSessionManager) DeleteSession(runtimeInstance containercontract.Runtime, sessionId string) error {
    ctx := runtimeInstance.Context()
    instance.deleteCalled++
    return awaitStorageContext(ctx)
}

func writeResponseRecorded(
    t *testing.T,
    sessionManager contract.Manager,
    sessionInstance contract.Session,
    writer nethttp.ResponseWriter,
) (httpcontract.Response, *errorContextRecordingLogger) {
    t.Helper()

    recordingLogger := &errorContextRecordingLogger{}
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})
    runtimeInstance := runtime.NewRequest(context.Background(), scope)

    netRequest := httptest.NewRequest(nethttp.MethodPost, "http://example.com/account", nil)
    netRequest.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: sessionInstance.Id()})

    sessionRequest := NewRequest(netRequest, nil, nil, nil)
    sessionRequest.Attributes().Set(requestAttributeSession, sessionInstance)

    response := writeResponse(
        runtimeInstance,
        sessionRequest,
        writer,
        TextResponse(nethttp.StatusOK, "handler body"),
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
        7*time.Second,
        nil,
    )

    return response, recordingLogger
}

func TestWriteResponse_ASaveRefusedForAReasonThatIsNoOutageAnswers500WithoutRetryAfter(t *testing.T) {
    writer := httptest.NewRecorder()
    _, recordingLogger := writeResponseRecorded(t, &stubSessionManager{saveErr: errors.New("session id is invalid in save session")}, &stubSession{id: "session-1", isModified: true}, writer)

    if nethttp.StatusInternalServerError != writer.Code || "" != writer.Header().Get("Retry-After") || 0 != len(writer.Result().Cookies()) {
        t.Fatalf("expected 500 without Retry-After or a cookie, got %d %q %v", writer.Code, writer.Header().Get("Retry-After"), writer.Result().Cookies())
    }

    if _, recorded := recordingLogger.errorContextFor("failed to save session"); false == recorded {
        t.Fatalf("expected the failed save recorded at error")
    }
}

func TestWriteResponse_ASaveCutByTheStorageBoundIs503EvenUnmarked(t *testing.T) {
    sessionManager := &hangingSessionManager{stubSessionManager: stubSessionManager{storageTimeout: bound.Of(20 * time.Millisecond)}}
    writer := httptest.NewRecorder()

    _, recordingLogger := writeResponseRecorded(t, sessionManager, &stubSession{id: "session-1", isModified: true}, writer)

    if nethttp.StatusServiceUnavailable != writer.Code || "7" != writer.Header().Get("Retry-After") {
        t.Fatalf("expected the cut save answered 503 with the kernel's Retry-After, got %d %q", writer.Code, writer.Header().Get("Retry-After"))
    }

    recordContext, recorded := recordingLogger.errorContextFor("failed to save session")
    if false == recorded || session.StorageTimeoutRule.Name != recordContext["bound"] {
        t.Fatalf("expected the record to name the bound, got %v", recordContext)
    }
}

func TestWriteResponse_ADeleteOutageAnswers503WithRetryAfter(t *testing.T) {
    writer := httptest.NewRecorder()
    sessionManager := &stubSessionManager{deleteErr: exception.MarkInfrastructureFailure(errors.New("connection refused"))}

    _, recordingLogger := writeResponseRecorded(t, sessionManager, &stubSession{id: "session-1", isCleared: true}, writer)

    if nethttp.StatusServiceUnavailable != writer.Code || "7" != writer.Header().Get("Retry-After") {
        t.Fatalf("expected the delete outage answered 503 with Retry-After, got %d %q", writer.Code, writer.Header().Get("Retry-After"))
    }

    if cookies := writer.Result().Cookies(); 0 != len(cookies) {
        t.Fatalf("expected the cookie kept on the 503, got %v", cookies)
    }

    if "handler body" == writer.Body.String() {
        t.Fatalf("expected the handler's response replaced")
    }

    if _, recorded := recordingLogger.errorContextFor("failed to delete session"); false == recorded {
        t.Fatalf("expected the failed delete recorded at error")
    }
}

func TestWriteResponse_ADeleteCutByTheStorageBoundIs503EvenUnmarkedAndNamesTheBound(t *testing.T) {
    sessionManager := &hangingSessionManager{stubSessionManager: stubSessionManager{storageTimeout: bound.Of(20 * time.Millisecond)}}
    writer := httptest.NewRecorder()

    _, recordingLogger := writeResponseRecorded(t, sessionManager, &stubSession{id: "session-1", isCleared: true}, writer)

    if nethttp.StatusServiceUnavailable != writer.Code || 0 != len(writer.Result().Cookies()) {
        t.Fatalf("expected the cut delete answered 503 keeping the cookie, got %d %v", writer.Code, writer.Result().Cookies())
    }

    recordContext, recorded := recordingLogger.errorContextFor("failed to delete session")
    if false == recorded || session.StorageTimeoutRule.Name != recordContext["bound"] {
        t.Fatalf("expected the record to name the bound, got %v", recordContext)
    }
}

func TestWriteResponse_ADeleteFailureThatIsNoOutageAnswers500AndExpiresTheCookie(t *testing.T) {
    writer := httptest.NewRecorder()
    _, _ = writeResponseRecorded(t, &stubSessionManager{deleteErr: errors.New("session id is invalid")}, &stubSession{id: "session-1", isCleared: true}, writer)

    cookies := writer.Result().Cookies()
    if nethttp.StatusInternalServerError != writer.Code || "" != writer.Header().Get("Retry-After") || 1 != len(cookies) || 0 <= cookies[0].MaxAge {
        t.Fatalf("expected 500 with the expiring cookie and no Retry-After, got %d %q %v", writer.Code, writer.Header().Get("Retry-After"), cookies)
    }
}

func TestWriteResponse_ADeleteOutageOnACommittedResponseKeepsItsStatus(t *testing.T) {
    committed := newRecordingResponseWriter(httptest.NewRecorder())
    committed.WriteHeader(nethttp.StatusOK)

    response, recordingLogger := writeResponseRecorded(t, &stubSessionManager{deleteErr: exception.MarkInfrastructureFailure(errors.New("connection refused"))}, &stubSession{id: "session-1", isCleared: true}, committed)

    if nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the committed response kept, got %d", response.StatusCode())
    }

    if _, recorded := recordingLogger.errorContextFor("failed to delete session"); false == recorded {
        t.Fatalf("expected the failed delete still recorded")
    }
}

/* a hijacked connection records no status, so the response writeResponse answers is the one it keeps */
type hijackedHeadersWriter struct {
    *httptest.ResponseRecorder
}

func (instance *hijackedHeadersWriter) HeadersWritten() bool {
    return true
}

func TestWriteResponse_ADeleteOutageOnAHijackedConnectionKeepsTheHandlersResponse(t *testing.T) {
    response, _ := writeResponseRecorded(t, &stubSessionManager{deleteErr: exception.MarkInfrastructureFailure(errors.New("connection refused"))}, &stubSession{id: "session-1", isCleared: true}, &hijackedHeadersWriter{ResponseRecorder: httptest.NewRecorder()})

    if nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the handler's response kept on a committed connection, got %d", response.StatusCode())
    }
}

func TestWriteResponse_ThePersistenceRuntimeKeepsTheRequestsScopeAndKind(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"
    melodyRequest := NewRequest(netRequest, nil, nil, nil)
    melodyRequest.Attributes().Set(requestAttributeSession, &stubSession{id: "session-123", isModified: true})

    requestScope := container.NewContainer(container.ContainerConfig{}).NewScope()
    requestRuntime := runtime.NewRequest(netRequest.Context(), requestScope)
    sessionManager := &stubSessionManager{}

    writeResponse(
        requestRuntime,
        melodyRequest,
        httptest.NewRecorder(),
        TextResponse(nethttp.StatusOK, "body"),
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
        0,
        nil,
    )

    if 1 != sessionManager.saveCalled || nil == sessionManager.saveRuntime {
        t.Fatalf("expected the session saved once")
    }

    if containercontract.KindRequest != sessionManager.saveRuntime.Kind() || requestScope != sessionManager.saveRuntime.Scope() {
        t.Fatalf("expected the save under the request's scope and kind, got %s", sessionManager.saveRuntime.Kind())
    }
}

func TestWriteResponse_AnAbsentRuntimeRefusesThePersistenceWithoutAPanic(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"
    melodyRequest := NewRequest(netRequest, nil, nil, nil)
    melodyRequest.Attributes().Set(requestAttributeSession, &stubSession{id: "session-123", isModified: true})

    sessionManager := &stubSessionManager{}
    writer := httptest.NewRecorder()

    writeResponse(nil, melodyRequest, writer, TextResponse(nethttp.StatusOK, "body"), sessionManager, httpcontract.ForwardedHeadersPolicy{}, httpcontract.SessionCookiePolicy{Path: "/"}, 0, nil)

    if 0 != sessionManager.saveCalled || nethttp.StatusInternalServerError != writer.Result().StatusCode {
        t.Fatalf("expected no save and a 500 under an absent runtime, got %d saves and %d", sessionManager.saveCalled, writer.Result().StatusCode)
    }
}

func TestWriteResponse_ARefusedStorageTimeoutIsNotNamedTheBoundUnderAnExpiredRequest(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"
    melodyRequest := NewRequest(netRequest, nil, nil, nil)
    melodyRequest.Attributes().Set(requestAttributeSession, &stubSession{id: "session-123", isModified: true})

    expiredContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    sessionManager := &stubSessionManager{storageTimeout: bound.Of(time.Microsecond)}
    writer := httptest.NewRecorder()

    writeResponse(runtime.NewRequest(expiredContext, container.NewContainer(container.ContainerConfig{}).NewScope()), melodyRequest, writer, TextResponse(nethttp.StatusOK, "body"), sessionManager, httpcontract.ForwardedHeadersPolicy{}, httpcontract.SessionCookiePolicy{Path: "/"}, 0, nil)

    if 0 != sessionManager.saveCalled || nethttp.StatusInternalServerError != writer.Result().StatusCode {
        t.Fatalf("expected the refused bound answered 500 with no save, got %d saves and %d", sessionManager.saveCalled, writer.Result().StatusCode)
    }
}
