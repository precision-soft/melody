package http

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    httpcontract "github.com/precision-soft/melody/http/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func TestNewRequestContext_ReportsTheIdAndTheStartItWasGiven(t *testing.T) {
    startedAt := time.Date(2026, time.August, 1, 10, 0, 0, 0, time.UTC)

    requestContext := NewRequestContext("request-1", startedAt)

    if "request-1" != requestContext.RequestId() {
        t.Fatalf("unexpected request id: %q", requestContext.RequestId())
    }

    if false == startedAt.Equal(requestContext.StartedAt()) {
        t.Fatalf("unexpected start: %v", requestContext.StartedAt())
    }
}

func TestRequestContext_DistinguishesTwoRequests(t *testing.T) {
    first := NewRequestContext("request-1", time.Now())
    second := NewRequestContext("request-2", time.Now())

    if first.RequestId() == second.RequestId() {
        t.Fatalf("expected two contexts to carry different ids, both answered %q", first.RequestId())
    }

    if "" == first.RequestId() || "" == second.RequestId() {
        t.Fatalf("expected both contexts to carry a non-empty id")
    }
}

func TestRequestContext_ReachesTheHandlerThroughTheRequest(t *testing.T) {
    observedRequestId := ""

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/articles",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            if nil != request.RequestContext() {
                observedRequestId = request.RequestContext().RequestId()
            }

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())
    handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/articles", nil))

    if "" == observedRequestId {
        t.Fatalf("expected the handler to read a non-empty request id off the context the kernel published")
    }
}
