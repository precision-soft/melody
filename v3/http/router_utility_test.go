package http

import (
    "context"
    "crypto/tls"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "reflect"
    "regexp"
    "strings"
    "syscall"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/session"
    "github.com/precision-soft/melody/v3/session/contract"
)

type stubSessionManager struct {
    saveCalled   int
    deleteCalled int
    saveErr      error
    deleteErr    error
}

func (instance *stubSessionManager) Session(sessionId string) contract.Session { return nil }

func (instance *stubSessionManager) NewSession() contract.Session { return nil }

func (instance *stubSessionManager) RegenerateSession(sessionInstance contract.Session) (contract.Session, error) {
    return nil, nil
}

func (instance *stubSessionManager) SaveSession(sessionInstance contract.Session) error {
    instance.saveCalled++

    return instance.saveErr
}

func (instance *stubSessionManager) DeleteSession(sessionId string) error {
    instance.deleteCalled++

    return instance.deleteErr
}

func (instance *stubSessionManager) Close() error { return nil }

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

    writeResponse(
        nil,
        melodyRequest,
        writer,
        response,
        sessionManager,
        sessionInstance,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      []string{},
        },
        httpcontract.SessionCookiePolicy{
            Path:     "/",
            Domain:   "",
            SameSite: nethttp.SameSiteLaxMode,
        },
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

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    response := EmptyResponse(nethttp.StatusOK)

    writer := httptest.NewRecorder()

    sessionManager := &stubSessionManager{}
    sessionInstance := &stubSession{
        id:         "session-123",
        isModified: false,
        isCleared:  true,
    }

    writeResponse(
        nil,
        melodyRequest,
        writer,
        response,
        sessionManager,
        sessionInstance,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      []string{},
        },
        httpcontract.SessionCookiePolicy{
            Path:     "/",
            Domain:   "",
            SameSite: nethttp.SameSiteLaxMode,
        },
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

    writeResponse(
        newTestRuntime(),
        melodyRequest,
        writer,
        EmptyResponse(nethttp.StatusOK),
        sessionManager,
        sessionInstance,
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

    writeResponse(
        newTestRuntime(),
        melodyRequest,
        writer,
        EmptyResponse(nethttp.StatusOK),
        sessionManager,
        sessionInstance,
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

    writeResponse(
        newTestRuntime(),
        melodyRequest,
        writer,
        EmptyResponse(nethttp.StatusOK),
        sessionManager,
        sessionInstance,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      []string{},
        },
        httpcontract.SessionCookiePolicy{
            Path:     "/",
            Domain:   "",
            SameSite: nethttp.SameSiteLaxMode,
        },
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

    writeResponse(
        newTestRuntime(),
        melodyRequest,
        writer,
        EmptyResponse(nethttp.StatusOK),
        sessionManager,
        sessionInstance,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      []string{},
        },
        httpcontract.SessionCookiePolicy{
            Path:     "/",
            Domain:   "",
            SameSite: nethttp.SameSiteLaxMode,
        },
    )

    if 1 != sessionManager.saveCalled {
        t.Fatalf("expected the session the client already holds to be stored once, got %d saves", sessionManager.saveCalled)
    }
}

func TestWriteResponse_StillDeletesAClearedSessionOnADiscardedResponse(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodPost, "http://example.com/logout", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    writer := newRecordingResponseWriter(httptest.NewRecorder())
    writer.WriteHeader(nethttp.StatusOK)

    sessionManager := &stubSessionManager{}
    sessionInstance := &stubSession{
        id:         "0123456789abcdef0123456789abcdef",
        isModified: false,
        isCleared:  true,
    }

    writeResponse(
        newTestRuntime(),
        melodyRequest,
        writer,
        EmptyResponse(nethttp.StatusOK),
        sessionManager,
        sessionInstance,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      []string{},
        },
        httpcontract.SessionCookiePolicy{
            Path:     "/",
            Domain:   "",
            SameSite: nethttp.SameSiteLaxMode,
        },
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

func (instance *testScope) Get(serviceName string) (any, error) { return nil, nil }

func (instance *testScope) MustGet(serviceName string) any { return nil }

func (instance *testScope) GetByType(targetType reflect.Type) (any, error) {
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

func (instance *testScope) MustGetByType(targetType reflect.Type) any { return nil }

func (instance *testScope) Has(serviceName string) bool { return false }

func (instance *testScope) HasType(targetType reflect.Type) bool { return false }

func (instance *testScope) OverrideInstance(serviceName string, value any) error { return nil }

func (instance *testScope) MustOverrideInstance(serviceName string, value any) {}

func (instance *testScope) OverrideProtectedInstance(serviceName string, value any) error { return nil }

func (instance *testScope) MustOverrideProtectedInstance(serviceName string, value any) {}

func (instance *testScope) RegisterScoped(serviceName string, provider any, options ...containercontract.RegisterOption) error {
    return nil
}

func (instance *testScope) MustRegisterScoped(serviceName string, provider any, options ...containercontract.RegisterOption) {
}

func (instance *testScope) Close() error { return nil }

var _ containercontract.Scope = (*testScope)(nil)

type testRuntime struct {
    scope containercontract.Scope
}

func (instance *testRuntime) Context() context.Context { return context.Background() }

func (instance *testRuntime) Scope() containercontract.Scope { return instance.scope }

func (instance *testRuntime) Container() containercontract.Container { return nil }

var _ runtimecontract.Runtime = (*testRuntime)(nil)

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

func TestWrapControllerWithContainer_AutowiresDependenciesByType(t *testing.T) {
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

    handler := wrapControllerWithContainer(controller)

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

func TestWrapControllerWithContainer_InsertsRuntimeWhenControllerRequestsRuntimeParameter(t *testing.T) {
    dependencyInstance := &dependencyA{Value: "ok"}

    scope := &testScope{
        values: map[reflect.Type]any{
            reflect.TypeOf((*dependencyA)(nil)): dependencyInstance,
        },
    }

    runtimeInstance := &testRuntime{scope: scope}

    var receivedRuntime runtimecontract.Runtime
    var receivedDependency *dependencyA

    controller := func(request *Request, runtimeInstance runtimecontract.Runtime, dep *dependencyA) (*Response, error) {
        receivedRuntime = runtimeInstance
        receivedDependency = dep

        return EmptyResponse(200), nil
    }

    handler := wrapControllerWithContainer(controller)

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

func TestWrapControllerWithContainer_ReturnsErrorWhenRuntimeIsNil(t *testing.T) {
    controller := func(request *Request) (*Response, error) {
        return EmptyResponse(200), nil
    }

    handler := wrapControllerWithContainer(controller)

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

func TestWrapControllerWithContainer_PropagatesScopeGetByTypeError(t *testing.T) {
    expectedErr := errors.New("scope error")

    scope := &testScope{
        returnErr: expectedErr,
    }

    runtimeInstance := &testRuntime{scope: scope}

    controller := func(request *Request, dep *dependencyA) (*Response, error) {
        return EmptyResponse(200), nil
    }

    handler := wrapControllerWithContainer(controller)

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

func TestWrapControllerWithContainer_ReturnsControllerErrorAndNilResponse(t *testing.T) {
    expectedErr := errors.New("controller failed")

    controller := func(request *Request) (*Response, error) {
        return nil, expectedErr
    }

    handler := wrapControllerWithContainer(controller)

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

func TestWrapControllerWithContainer_ReturnsNilResponseWhenControllerReturnsNilResponseAndNilError(t *testing.T) {
    controller := func(request *Request) (*Response, error) {
        return nil, nil
    }

    handler := wrapControllerWithContainer(controller)

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

func TestWrapControllerWithContainer_PanicsWhenControllerIsNotAFunction(t *testing.T) {
    assertPanicWithExceptionMessage(
        t,
        func() {
            _ = wrapControllerWithContainer(123)
        },
        "controller must be a function",
    )
}

func TestWrapControllerWithContainer_PanicsWhenControllerHasNoArguments(t *testing.T) {
    assertPanicWithExceptionMessage(
        t,
        func() {
            _ = wrapControllerWithContainer(func() (*Response, error) { return EmptyResponse(200), nil })
        },
        "controller must have at least one argument",
    )
}

func TestWrapControllerWithContainer_PanicsWhenFirstArgumentIsNotRequest(t *testing.T) {
    assertPanicWithExceptionMessage(
        t,
        func() {
            _ = wrapControllerWithContainer(func(value string) (*Response, error) { return EmptyResponse(200), nil })
        },
        "first controller argument must implement http request contract",
    )
}

func TestWrapControllerWithContainer_PanicsWhenControllerDoesNotReturnTwoResults(t *testing.T) {
    assertPanicWithExceptionMessage(
        t,
        func() {
            _ = wrapControllerWithContainer(func(request *Request) *Response { return EmptyResponse(200) })
        },
        "controller must return response",
    )
}

func TestWrapControllerWithContainer_PanicsWhenFirstResultIsNotResponsePointer(t *testing.T) {
    assertPanicWithExceptionMessage(
        t,
        func() {
            _ = wrapControllerWithContainer(func(request *Request) (int, error) { return 1, nil })
        },
        "controller must return response contract as first result",
    )
}

func TestWrapControllerWithContainer_PanicsWhenSecondResultIsNotError(t *testing.T) {
    assertPanicWithExceptionMessage(
        t,
        func() {
            _ = wrapControllerWithContainer(func(request *Request) (*Response, string) { return EmptyResponse(200), "" })
        },
        "controller must return error as second result",
    )
}

func TestMatchPath_WildcardLocale_SetsLocaleParam(t *testing.T) {
    routeDefinition := route{
        pattern:      "/*_locale...",
        parts:        splitPath("/*_locale..."),
        requirements: map[string]*regexp.Regexp{},
    }

    pathSegments := splitPath("/en")
    params, matched := matchPath(routeDefinition, pathSegments)

    if false == matched {
        t.Fatalf("expected path to match")
    }

    value, exists := params[RouteAttributeLocale]
    if false == exists {
        t.Fatalf("expected _locale param to exist")
    }

    if "en" != value {
        t.Fatalf("expected _locale param to be 'en', got: %s", value)
    }
}

func TestMatchPath_WildcardLocale_CatchAll_SetsLocaleParam(t *testing.T) {
    routeDefinition := route{
        pattern:      "/prefix/*_locale...",
        parts:        splitPath("/prefix/*_locale..."),
        requirements: map[string]*regexp.Regexp{},
    }

    pathSegments := splitPath("/prefix/en/extra")
    params, matched := matchPath(routeDefinition, pathSegments)

    if false == matched {
        t.Fatalf("expected path to match")
    }

    if "en/extra" != params[RouteAttributeLocale] {
        t.Fatalf("expected _locale catch-all to be 'en/extra', got: %s", params[RouteAttributeLocale])
    }
}

func TestMatchPath_WildcardNonLocale_DoesNotSetLocaleParam(t *testing.T) {
    routeDefinition := route{
        pattern:      "/*path...",
        parts:        splitPath("/*path..."),
        requirements: map[string]*regexp.Regexp{},
    }

    pathSegments := splitPath("/some/path")
    params, matched := matchPath(routeDefinition, pathSegments)

    if false == matched {
        t.Fatalf("expected path to match")
    }

    _, exists := params[RouteAttributeLocale]
    if true == exists {
        t.Fatalf("expected _locale param not to be set for non-locale wildcard")
    }

    if "some/path" != params["path"] {
        t.Fatalf("expected path param to be 'some/path', got: %s", params["path"])
    }
}

func TestMatchPath_ParamLocale_SetsLocaleParam(t *testing.T) {
    routeDefinition := route{
        pattern:      "/page/:_locale",
        parts:        splitPath("/page/:_locale"),
        requirements: map[string]*regexp.Regexp{},
    }

    pathSegments := splitPath("/page/fr")
    params, matched := matchPath(routeDefinition, pathSegments)

    if false == matched {
        t.Fatalf("expected path to match")
    }

    if "fr" != params[RouteAttributeLocale] {
        t.Fatalf("expected _locale param to be 'fr', got: %s", params[RouteAttributeLocale])
    }
}

func TestWrapControllerWithContainer_PanicsWhenDependencyIsNilFromScope(t *testing.T) {
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

    handler := wrapControllerWithContainer(controller)

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

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    writer := httptest.NewRecorder()

    sessionManager := &stubSessionManager{}
    sessionInstance := &stubSession{
        id:         "session-123",
        isModified: false,
        isCleared:  true,
    }

    writeResponse(
        nil,
        melodyRequest,
        writer,
        nil,
        sessionManager,
        sessionInstance,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      []string{},
        },
        httpcontract.SessionCookiePolicy{
            Path:     "/",
            Domain:   "",
            SameSite: nethttp.SameSiteLaxMode,
        },
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

    writeResponse(
        nil,
        melodyRequest,
        writer,
        response,
        sessionManager,
        typedNilSession,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
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

    writeResponse(
        nil,
        melodyRequest,
        writer,
        response,
        typedNilManager,
        sessionInstance,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
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

    writeResponse(
        nil,
        melodyRequest,
        writer,
        response,
        sessionManager,
        sessionInstance,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
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

    writeResponse(
        nil,
        melodyRequest,
        writer,
        response,
        sessionManager,
        sessionInstance,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
    )

    httpResponse := writer.Result()

    if nethttp.StatusOK != httpResponse.StatusCode {
        t.Fatalf("expected the handler's own response to be served, got %d", httpResponse.StatusCode)
    }

    if 0 != len(httpResponse.Cookies()) {
        t.Fatalf("expected no cookie at all for a rotated-away session, got %d", len(httpResponse.Cookies()))
    }
}

func TestWriteResponse_ASaveOutageAnswersFiveHundredWithoutACookie(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    response := TextResponse(nethttp.StatusOK, "welcome")
    writer := httptest.NewRecorder()

    sessionManager := &stubSessionManager{
        saveErr: exception.NewError("storage is unreachable", nil, nil),
    }
    sessionInstance := &stubSession{id: "session-123", isModified: true}

    writeResponse(
        nil,
        melodyRequest,
        writer,
        response,
        sessionManager,
        sessionInstance,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
    )

    httpResponse := writer.Result()

    if nethttp.StatusInternalServerError != httpResponse.StatusCode {
        t.Fatalf("expected a storage outage to answer 500, got %d", httpResponse.StatusCode)
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
        nil,
        nil,
        recorder,
        unassignedResponse,
        nil,
        nil,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{},
    )

    if nethttp.StatusNoContent != recorder.Code {
        t.Fatalf("expected %d, got %d", nethttp.StatusNoContent, recorder.Code)
    }
}

func TestWriteResponse_ReturnsTheReplacedFiveHundredOnASaveOutage(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"

    melodyRequest := NewRequest(netRequest, nil, nil, nil)

    response := TextResponse(nethttp.StatusOK, "welcome")
    writer := httptest.NewRecorder()

    sessionManager := &stubSessionManager{
        saveErr: exception.NewError("storage is unreachable", nil, nil),
    }
    sessionInstance := &stubSession{id: "session-123", isModified: true}

    writtenResponse := writeResponse(
        nil,
        melodyRequest,
        writer,
        response,
        sessionManager,
        sessionInstance,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
    )

    if nil == writtenResponse || nethttp.StatusInternalServerError != writtenResponse.StatusCode() {
        t.Fatalf("expected the returned response to be the replaced 500, got %#v", writtenResponse)
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
        nil,
        melodyRequest,
        writer,
        response,
        nil,
        nil,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
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

    writeResponse(
        nil,
        melodyRequest,
        writer,
        EmptyResponse(nethttp.StatusOK),
        sessionManager,
        sessionInstance,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      []string{},
        },
        httpcontract.SessionCookiePolicy{
            Path:     "/",
            Domain:   "",
            SameSite: nethttp.SameSiteLaxMode,
        },
    )

    if 1 != sessionManager.deleteCalled || 0 != sessionManager.saveCalled {
        t.Fatalf("expected the snapshot's cleared flag to take the delete branch, got %d deletes and %d saves", sessionManager.deleteCalled, sessionManager.saveCalled)
    }
}

func TestWriteResponse_AnOutOfRangeStatusCodeAnswersTheRenderedServerError(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/bad",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(99), nil
        },
    )

    serviceContainer := newHttpTestContainer()
    handler := NewKernel(router).ServeHttp(serviceContainer)

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
        nil,
        melodyRequest,
        recorder,
        EmptyResponse(99),
        nil,
        nil,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{},
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
        nil,
        melodyRequest,
        writer,
        EmptyResponse(nethttp.StatusNoContent),
        nil,
        nil,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{},
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
        nil,
        melodyRequest,
        writer,
        EmptyResponse(nethttp.StatusNoContent),
        nil,
        nil,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{},
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

    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger)
    runtimeInstance := runtime.New(context.Background(), scope, serviceContainer)

    netRequest := httptest.NewRequest(nethttp.MethodPost, "http://example.com/account/settings", nil)
    netRequest.RemoteAddr = "127.0.0.1:1234"

    writeResponse(
        runtimeInstance,
        NewRequest(netRequest, nil, nil, nil),
        httptest.NewRecorder(),
        TextResponse(nethttp.StatusOK, "handler body"),
        sessionManager,
        sessionInstance,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
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
                map[string]any{"sessionRef": sessionIdLogReference("session-123")},
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

    for key, expected := range map[string]any{"sessionRef": sessionIdLogReference("session-123"), "method": nethttp.MethodPost, "path": "/account/settings"} {
        if expected != record.context[key] {
            t.Fatalf("expected %q to be %v, got %v in %v", key, expected, record.context[key], record.context)
        }
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

    for key, expected := range map[string]any{"sessionRef": sessionIdLogReference("session-456"), "method": nethttp.MethodPost, "path": "/account/settings"} {
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

    for key, expected := range map[string]any{"sessionRef": sessionIdLogReference("session-789"), "method": nethttp.MethodPost, "path": "/account/settings"} {
        if expected != record.context[key] {
            t.Fatalf("expected %q to be %v, got %v in %v", key, expected, record.context[key], record.context)
        }
    }
}

func TestRequestPathIsCanonical_RefusesFoldsAndAllowsTrailingSlash(t *testing.T) {
    canonicalPaths := []string{
        "/public name",
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
        "/public ",
        " /public",
        "/public\t",
        "/public\u00a0",
        "/public\u2003",
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

func TestRequestPathAsRouted_KeepsAnEncodedSeparatorInsideItsSegmentAndDecodesTheRest(t *testing.T) {
    for escapedPath, routed := range map[string]string{
        "/":                    "/",
        "/public":              "/public",
        "/public/":             "/public/",
        "/caf%C3%A9":           "/café",
        "/a%20b/c":             "/a b/c",
        "/a%25b":               "/a%b",
        "/public%2F":           "/public%2F",
        "/public%2f":           "/public%2F",
        "/admin%2Fusers":       "/admin%2Fusers",
        "/files/a%2Fb/c":       "/files/a%2Fb/c",
        "/public%252F":         "/public%2F",
        "/admin/users":         "/admin/users",
        "/a+b":                 "/a+b",
        "/a%zz/b":              "/a%zz/b",
        "/a%2/b":               "/a%2/b",
        "*":                    "*",
        "example.com:443":      "example.com:443",
        "":                     "",
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

    written := writeResponse(nil, request, httptest.NewRecorder(), response, nil, nil, httpcontract.ForwardedHeadersPolicy{}, httpcontract.SessionCookiePolicy{})

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
