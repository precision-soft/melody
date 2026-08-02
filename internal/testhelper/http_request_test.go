package testhelper

import (
    "net/http"
    "net/http/httptest"
    "testing"
)

/* @info this is the request every http-facing test of the framework is built from — the kernel's, the security listeners', the middleware chain's — so a builder that dropped a header or handed back a shared bag would make a whole class of tests assert against something the real request never looks like. It had no mirror of its own: its correctness was only ever implied by the tests that used it. */
func TestNewHttpTestRequest_BuildsARequestCarryingTheMethodAndTheUrl(t *testing.T) {
    request := NewHttpTestRequest(http.MethodPost, "/things?page=2")

    if http.MethodPost != request.HttpRequest().Method {
        t.Fatalf("unexpected method: %q", request.HttpRequest().Method)
    }

    if "/things" != request.HttpRequest().URL.Path {
        t.Fatalf("unexpected path: %q", request.HttpRequest().URL.Path)
    }

    if nil == request.Query() || nil == request.Post() || nil == request.Attributes() {
        t.Fatalf("expected every bag to be built rather than left nil")
    }
}

/* @info the accept variant is what every content-negotiation test drives, so the header has to land where the negotiation reads it rather than beside it. */
func TestNewHttpTestRequestWithAccept_PutsTheHeaderWhereTheNegotiationReadsIt(t *testing.T) {
    request := NewHttpTestRequestWithAccept(http.MethodGet, "/things", "application/json")

    if "application/json" != request.Header("Accept") {
        t.Fatalf("unexpected accept header: %q", request.Header("Accept"))
    }
}

/* @info a nil request is a wiring mistake in the test itself, and it is refused by name rather than dereferenced — a nil-pointer panic inside a helper sends the reader looking at the framework instead of at the line that called it. */
func TestNewHttpTestRequestFromHttpRequest_ANilRequestIsRefusedByName(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected a nil request to be refused")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic value, got %#v", recoveredValue)
        }

        if "http request may not be nil" != recoveredErr.Error() {
            t.Fatalf("unexpected refusal message: %q", recoveredErr.Error())
        }
    }()

    _ = NewHttpTestRequestFromHttpRequest(nil)
}

/* @info two requests built by the helper must not share their bags: a test that seeds parameters into one and asserts the absence of them in another is the ordinary shape of an isolation test, and a shared bag would make it pass for the wrong reason. */
func TestNewHttpTestRequest_TwoRequestsDoNotShareTheirBags(t *testing.T) {
    first := NewHttpTestRequestFromHttpRequest(httptest.NewRequest(http.MethodGet, "/things", nil)).(*HttpTestRequest)
    second := NewHttpTestRequestFromHttpRequest(httptest.NewRequest(http.MethodGet, "/things", nil)).(*HttpTestRequest)

    if first.Query() == second.Query() {
        t.Fatalf("expected each built request to own its query bag")
    }

    if first.HttpRequest() == second.HttpRequest() {
        t.Fatalf("expected each built request to wrap its own net/http request")
    }
}
