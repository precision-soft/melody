package testhelper

import (
    "context"
    "net/http"
    "net/http/httptest"
    "testing"

    containercontract "github.com/precision-soft/melody/container/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

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

func TestNewHttpTestRequestWithAccept_PutsTheHeaderWhereTheNegotiationReadsIt(t *testing.T) {
    request := NewHttpTestRequestWithAccept(http.MethodGet, "/things", "application/json")

    if "application/json" != request.Header("Accept") {
        t.Fatalf("unexpected accept header: %q", request.Header("Accept"))
    }
}

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

func TestHttpTestRequest_ParamSeparatesAnAbsentParameterFromAnEmptyOne(t *testing.T) {
    request := NewHttpTestRequest(http.MethodGet, "/articles/7").(*HttpTestRequest)
    request.paramsValue["id"] = "7"
    request.paramsValue["slug"] = ""

    value, ok := request.Param("id")
    if false == ok || "7" != value {
        t.Fatalf("expected a seeded parameter to be found, got %q, %t", value, ok)
    }

    value, ok = request.Param("slug")
    if false == ok || "" != value {
        t.Fatalf("expected a seeded empty parameter to be found, got %q, %t", value, ok)
    }

    value, ok = request.Param("missing")
    if true == ok || "" != value {
        t.Fatalf("expected an absent parameter to be reported absent, got %q, %t", value, ok)
    }
}

func TestHttpTestRequest_ParamsHandsOutACopyRatherThanTheLiveMap(t *testing.T) {
    request := NewHttpTestRequest(http.MethodGet, "/articles/7").(*HttpTestRequest)
    request.paramsValue["id"] = "7"

    handedOut := request.Params()
    handedOut["id"] = "9"
    handedOut["injected"] = "value"

    if "7" != request.paramsValue["id"] {
        t.Fatalf("expected the request to keep its own value, got %q", request.paramsValue["id"])
    }

    if _, exists := request.paramsValue["injected"]; true == exists {
        t.Fatalf("expected a key added to the handed-out map to stay out of the request")
    }

    if "7" != request.Params()["id"] {
        t.Fatalf("expected a second read to answer the request's own value")
    }
}

func TestHttpTestRequest_RuntimeInstanceAnswersWhatWasSeeded(t *testing.T) {
    request := NewHttpTestRequest(http.MethodGet, "/articles").(*HttpTestRequest)

    if nil != request.RuntimeInstance() {
        t.Fatalf("expected a freshly built request to carry no runtime")
    }

    seeded := &stubRuntime{ctx: context.Background()}
    request.runtimeValue = seeded

    if seeded != request.RuntimeInstance() {
        t.Fatalf("expected the seeded runtime to be handed back")
    }
}

type stubRuntime struct {
    ctx context.Context
}

func (instance *stubRuntime) Context() context.Context { return instance.ctx }

func (instance *stubRuntime) Scope() containercontract.Scope { return nil }

func (instance *stubRuntime) Container() containercontract.Container { return nil }

var _ runtimecontract.Runtime = (*stubRuntime)(nil)
