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

/* A client that ranks html BELOW json is asking for json, so the entry point reads the weight rather than finding "text/html" anywhere in the header: an api client that merely tolerates html gets a status it can branch on, not a redirect to a login page it cannot render. */
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

/* The Accept field is list-typed, so a client may send it as several lines, and a browser that puts html on the second one is still redirected. */
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
