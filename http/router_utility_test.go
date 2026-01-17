package http

import (
	"crypto/tls"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	httpcontract "github.com/precision-soft/melody/http/contract"
	"github.com/precision-soft/melody/session/contract"
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
