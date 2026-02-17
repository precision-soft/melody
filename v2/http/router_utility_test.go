package http

import (
	"context"
	"crypto/tls"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	containercontract "github.com/precision-soft/melody/v2/container/contract"
	"github.com/precision-soft/melody/v2/exception"
	httpcontract "github.com/precision-soft/melody/v2/http/contract"
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
	"github.com/precision-soft/melody/v2/session/contract"
)

type stubSessionManager struct {
	saveCalled   int
	deleteCalled int
}

func (instance *stubSessionManager) Session(sessionId string) contract.Session { return nil }

func (instance *stubSessionManager) NewSession() contract.Session { return nil }

func (instance *stubSessionManager) SaveSession(sessionInstance contract.Session) error {
	instance.saveCalled++

	return nil
}

func (instance *stubSessionManager) DeleteSession(sessionId string) error {
	instance.deleteCalled++

	return nil
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

func (instance *testScope) Close() error { return nil }

var _ containercontract.Scope = (*testScope)(nil)

type testRuntime struct {
	scope containercontract.Scope
}

func (instance *testRuntime) Context() context.Context { return context.Background() }

func (instance *testRuntime) Scope() containercontract.Scope { return instance.scope }

func (instance *testRuntime) Container() containercontract.Container { return nil }

var _ runtimecontract.Runtime = (*testRuntime)(nil)

func assertPanicWithExceptionMessage(t *testing.T, fn func(), expectedMessage string) {
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

	fn()
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
