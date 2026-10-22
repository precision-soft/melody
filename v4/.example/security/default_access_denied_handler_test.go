package security

import (
    "errors"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    melodyhttp "github.com/precision-soft/melody/v4/http"
)

func TestDefaultAccessDeniedHandlerAnswersABrowserWithAnHtmlForbidden(t *testing.T) {
    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/admin/", nil)
    httpRequest.Header.Set("Accept", "text/html")

    response, handleErr := NewDefaultAccessDeniedHandler("/catalogue/", "/logout/").Handle(nil, melodyhttp.NewRequest(httpRequest, nil, nil, nil), errors.New("denied"))
    if nil != handleErr {
        t.Fatalf("the handler failed: %v", handleErr)
    }

    if nethttp.StatusForbidden != response.StatusCode() {
        t.Fatalf("expected 403, got %d", response.StatusCode())
    }

    if "text/html; charset=utf-8" != response.Headers().Get("Content-Type") {
        t.Fatalf("expected an html body for a browser, got %q", response.Headers().Get("Content-Type"))
    }

    body, readErr := io.ReadAll(response.BodyReader())
    if nil != readErr {
        t.Fatalf("the body could not be read: %v", readErr)
    }

    /* a signed-in account refused a page is offered the way out, each exit under its own label: Back, Home to the landing page and Log out, rather than left on a bare refusal */
    for _, expected := range []string{`href="javascript:history.back()">Back<`, `href="/catalogue/">Home<`, `href="/logout/">Log out<`} {
        if false == strings.Contains(string(body), expected) {
            t.Fatalf("expected the refusal page to offer %s, got %q", expected, string(body))
        }
    }
}

func TestDefaultAccessDeniedHandlerAnswersAnApiClientWithAJsonForbidden(t *testing.T) {
    response, handleErr := NewDefaultAccessDeniedHandler("/", "/logout/").Handle(nil, plainRequest(t), errors.New("denied"))
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
