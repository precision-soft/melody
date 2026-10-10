package security

import (
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    melodyhttp "github.com/precision-soft/melody/v3/http"
)

func TestDefaultAccessDeniedHandlerAnswersABrowserWithAnHtmlForbidden(t *testing.T) {
    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/admin/", nil)
    httpRequest.Header.Set("Accept", "text/html")

    response, handleErr := NewDefaultAccessDeniedHandler().Handle(nil, melodyhttp.NewRequest(httpRequest, nil, nil, nil), errors.New("denied"))
    if nil != handleErr {
        t.Fatalf("the handler failed: %v", handleErr)
    }

    if nethttp.StatusForbidden != response.StatusCode() {
        t.Fatalf("expected 403, got %d", response.StatusCode())
    }

    if "text/html; charset=utf-8" != response.Headers().Get("Content-Type") {
        t.Fatalf("expected an html body for a browser, got %q", response.Headers().Get("Content-Type"))
    }
}

func TestDefaultAccessDeniedHandlerAnswersAnApiClientWithAJsonForbidden(t *testing.T) {
    response, handleErr := NewDefaultAccessDeniedHandler().Handle(nil, plainRequest(t), errors.New("denied"))
    if nil != handleErr {
        t.Fatalf("the handler failed: %v", handleErr)
    }

    if nethttp.StatusForbidden != response.StatusCode() {
        t.Fatalf("expected 403, got %d", response.StatusCode())
    }

    if "application/json; charset=utf-8" != response.Headers().Get("Content-Type") {
        t.Fatalf("expected a json body for an api client, got %q", response.Headers().Get("Content-Type"))
    }
}
