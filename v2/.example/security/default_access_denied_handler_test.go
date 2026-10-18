package security

import (
    "errors"
    "io"
    nethttp "net/http"
    "strings"
    "testing"

    melodyhttp "github.com/precision-soft/melody/v2/http"
)

func TestDefaultAccessDeniedHandlerAnswersABrowserWithHtml(t *testing.T) {
    handler := NewDefaultAccessDeniedHandler("/catalogue/", "/logout/")

    response, err := handler.Handle(nil, requestAccepting(t, "text/html"), errors.New("denied"))
    if nil != err {
        t.Fatalf("handle the refusal: %v", err)
    }

    if nethttp.StatusForbidden != response.StatusCode() {
        t.Fatalf("expected a refusal, got %d", response.StatusCode())
    }

    if melodyhttp.ContentTypeTextHtml != response.Headers().Get("Content-Type") {
        t.Fatalf("unexpected content type: %q", response.Headers().Get("Content-Type"))
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

func TestDefaultAccessDeniedHandlerAnswersAnApiClientWithJson(t *testing.T) {
    handler := NewDefaultAccessDeniedHandler("/", "/logout/")

    response, err := handler.Handle(nil, requestAccepting(t, "application/json"), errors.New("denied"))
    if nil != err {
        t.Fatalf("handle the refusal: %v", err)
    }

    if nethttp.StatusForbidden != response.StatusCode() {
        t.Fatalf("expected a refusal, got %d", response.StatusCode())
    }

    if melodyhttp.ContentTypeTextHtml == response.Headers().Get("Content-Type") {
        t.Fatalf("an api client was answered with html")
    }
}

/* The refusal and the entry point beside it read the client's preference through ONE door, so a request cannot be told it is a browser by the handler that refuses it and an api client by the one that challenges it. */
func TestDefaultAccessDeniedHandlerReadsThePreferenceTheEntryPointReads(t *testing.T) {
    handler := NewDefaultAccessDeniedHandler("/", "/logout/")
    entryPoint := NewLoginRedirectEntryPoint("/login/")

    for _, acceptHeader := range []string{
        "application/json, text/html;q=0.1",
        "text/html;q=0, application/json",
        "text/html,application/xhtml+xml",
        "*/*",
        "",
    } {
        deniedResponse, deniedErr := handler.Handle(nil, requestAccepting(t, acceptHeader), errors.New("denied"))
        if nil != deniedErr {
            t.Fatalf("handle the refusal for %q: %v", acceptHeader, deniedErr)
        }

        startResponse, startErr := entryPoint.Start(nil, requestAccepting(t, acceptHeader))
        if nil != startErr {
            t.Fatalf("start the entry point for %q: %v", acceptHeader, startErr)
        }

        deniedChoseHtml := melodyhttp.ContentTypeTextHtml == deniedResponse.Headers().Get("Content-Type")
        startChoseHtml := nethttp.StatusFound == startResponse.StatusCode()

        if deniedChoseHtml != startChoseHtml {
            t.Fatalf(
                "the two readers disagree about %q: refusal chose html=%v, entry point chose html=%v",
                acceptHeader,
                deniedChoseHtml,
                startChoseHtml,
            )
        }
    }
}
