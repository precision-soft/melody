package security

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    melodyhttp "github.com/precision-soft/melody/v3/http"
)

func TestLoginRedirectEntryPointRedirectsABrowserToTheLoginPage(t *testing.T) {
    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/products/", nil)
    httpRequest.Header.Set("Accept", "text/html")

    response, startErr := NewLoginRedirectEntryPoint("/login").Start(nil, melodyhttp.NewRequest(httpRequest, nil, nil, nil))
    if nil != startErr {
        t.Fatalf("the entry point failed: %v", startErr)
    }

    if nethttp.StatusFound != response.StatusCode() {
        t.Fatalf("expected a redirect for a browser, got %d", response.StatusCode())
    }

    if "/login" != response.Headers().Get("Location") {
        t.Fatalf("expected the redirect to the login page, got %q", response.Headers().Get("Location"))
    }
}

func TestLoginRedirectEntryPointAnswersAnApiClientWithUnauthorized(t *testing.T) {
    response, startErr := NewLoginRedirectEntryPoint("/login").Start(nil, plainRequest(t))
    if nil != startErr {
        t.Fatalf("the entry point failed: %v", startErr)
    }

    if nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected 401 for a client that does not prefer html, got %d", response.StatusCode())
    }
}
