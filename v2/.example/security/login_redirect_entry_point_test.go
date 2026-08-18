package security

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v2/.example/route"
)

func TestLoginRedirectEntryPointSendsABrowserToTheLoginPage(t *testing.T) {
    entryPoint := NewLoginRedirectEntryPoint(route.LoginPagePattern)

    response, err := entryPoint.Start(nil, requestAccepting(t, "text/html,application/xhtml+xml"))
    if nil != err {
        t.Fatalf("start the entry point: %v", err)
    }

    if nethttp.StatusFound != response.StatusCode() {
        t.Fatalf("expected a redirect, got %d", response.StatusCode())
    }

    if route.LoginPagePattern != response.Headers().Get("Location") {
        t.Fatalf("unexpected location: %q", response.Headers().Get("Location"))
    }
}

func TestLoginRedirectEntryPointRefusesAnApiClientWithoutRedirecting(t *testing.T) {
    entryPoint := NewLoginRedirectEntryPoint(route.LoginPagePattern)

    response, err := entryPoint.Start(nil, requestAccepting(t, "application/json"))
    if nil != err {
        t.Fatalf("start the entry point: %v", err)
    }

    if nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected a refusal, got %d", response.StatusCode())
    }
}

/* A client that ranks html BELOW json is asking for json. Reading the header by substring found "text/html" anywhere in it and answered a redirect, so an api client that merely tolerates html was sent to a login page it cannot render — and the 302 arrived where the caller was waiting for a status it could branch on. */
func TestLoginRedirectEntryPointHonoursTheWeightTheClientGaveHtml(t *testing.T) {
    entryPoint := NewLoginRedirectEntryPoint(route.LoginPagePattern)

    response, err := entryPoint.Start(nil, requestAccepting(t, "application/json, text/html;q=0.1"))
    if nil != err {
        t.Fatalf("start the entry point: %v", err)
    }

    if nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected a refusal for a client that down-weighted html, got %d", response.StatusCode())
    }
}

/* q=0 is an explicit refusal of the type, not a mention of it. */
func TestLoginRedirectEntryPointHonoursAnExplicitHtmlRefusal(t *testing.T) {
    entryPoint := NewLoginRedirectEntryPoint(route.LoginPagePattern)

    response, err := entryPoint.Start(nil, requestAccepting(t, "text/html;q=0, application/json"))
    if nil != err {
        t.Fatalf("start the entry point: %v", err)
    }

    if nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected a refusal for a client that refused html, got %d", response.StatusCode())
    }
}

/* The Accept field is list-typed, so a client may send it as several lines. Reading only the first line answered a browser with json whenever it put html on the second one. */
func TestLoginRedirectEntryPointReadsEveryAcceptLine(t *testing.T) {
    entryPoint := NewLoginRedirectEntryPoint(route.LoginPagePattern)

    response, err := entryPoint.Start(nil, requestAcceptingLines(t, "application/json;q=0.2", "text/html"))
    if nil != err {
        t.Fatalf("start the entry point: %v", err)
    }

    if nethttp.StatusFound != response.StatusCode() {
        t.Fatalf("expected a redirect for a browser that spelled Accept over two lines, got %d", response.StatusCode())
    }
}

func TestLoginRedirectEntryPointRefusesARequestWithoutAnAcceptHeader(t *testing.T) {
    entryPoint := NewLoginRedirectEntryPoint(route.LoginPagePattern)

    response, err := entryPoint.Start(nil, requestAccepting(t, ""))
    if nil != err {
        t.Fatalf("start the entry point: %v", err)
    }

    if nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected a refusal, got %d", response.StatusCode())
    }
}

func TestLoginRedirectEntryPointRefusesANilRequest(t *testing.T) {
    entryPoint := NewLoginRedirectEntryPoint(route.LoginPagePattern)

    response, err := entryPoint.Start(nil, nil)
    if nil != err {
        t.Fatalf("start the entry point: %v", err)
    }

    if nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected a refusal, got %d", response.StatusCode())
    }
}
