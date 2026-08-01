package http

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/precision-soft/melody/serializer"
    serializercontract "github.com/precision-soft/melody/serializer/contract"
)

func TestNormalizeResultToResponse_TypedNilResponseBecomesNilInterface(t *testing.T) {
    var typedNil *Response

    response, err := NormalizeResultToResponse(nil, nil, typedNil)
    if nil != err {
        t.Fatalf("NormalizeResultToResponse returned an error: %v", err)
    }

    if nil != response {
        t.Fatalf("a typed-nil *Response must normalize to a nil httpcontract.Response interface so the kernel takes the no-content path; a non-nil interface wrapping a nil *Response panics on Headers()")
    }
}

/* @info the accept header is list-typed, so its repeated lines are one header: reading only the first line dropped whatever the client sent on the second — here the refusal of json would be honoured while the acceptance of text/plain vanished, answering 406 to a client that named an available type */
func TestNormalizeResultToResponse_JoinsRepeatedAcceptLines(t *testing.T) {
    serviceContainer := container.NewContainer()

    registerErr := container.Register[*serializer.SerializerManager](
        serviceContainer,
        serializer.ServiceSerializerManager,
        func(resolver containercontract.Resolver) (*serializer.SerializerManager, error) {
            return serializer.NewSerializerManager(
                map[string]serializercontract.Serializer{
                    serializer.MimeApplicationJson: serializer.NewJsonSerializer(),
                    serializer.MimeTextPlain:       serializer.NewPlainTextSerializer(),
                },
            )
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/value", nil)
    httpRequest.Header.Add("Accept", "application/json;q=0")
    httpRequest.Header.Add("Accept", "text/plain")

    request := NewRequest(httpRequest, nil, runtimeInstance, NewRequestContext("test", time.Now()))

    response, err := NormalizeResultToResponse(runtimeInstance, request, map[string]any{"a": "b"})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if nil == response {
        t.Fatalf("expected a response")
    }

    if nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the second accept line to be honoured with 200, got %d", response.StatusCode())
    }

    if false == strings.HasPrefix(response.Headers().Get("Content-Type"), serializer.MimeTextPlain) {
        t.Fatalf("expected the representation accepted on the second line, got %s", response.Headers().Get("Content-Type"))
    }
}

/* @info WrapResultHandler is the adapter that lets a handler return a plain value — a struct, a string, a slice of bytes — and had never been entered. It normalizes through the same path the kernel uses, so a wrapper that skipped the normalization would hand the kernel a value it cannot write, and one that swallowed the error would answer 200 for a handler that failed. */

func TestWrapResultHandler_NormalizesAPlainStringResult(t *testing.T) {
    handler := WrapResultHandler(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (any, error) {
            return "plain text", nil
        },
    )

    response, err := handler(newTestRuntime(), httptest.NewRecorder(), newResultHandlerRequest())
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("unexpected status: %d", response.StatusCode())
    }

    if "plain text" != readResponseBody(t, response) {
        t.Fatalf("unexpected body: %q", readResponseBody(t, response))
    }
}

/* @info a handler that fails returns its error untouched and no response at all: normalizing an error into a response here would take the failure away from the exception listener, which is the one place a status and a log record are decided together. */

func TestWrapResultHandler_ReturnsTheHandlerErrorWithoutAResponse(t *testing.T) {
    failure := exception.NewHttpException(nethttp.StatusTeapot, "short and stout")

    handler := WrapResultHandler(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (any, error) {
            return "never rendered", failure
        },
    )

    response, err := handler(newTestRuntime(), httptest.NewRecorder(), newResultHandlerRequest())

    if failure != err {
        t.Fatalf("expected the handler's own error, got: %v", err)
    }

    if nil != response {
        t.Fatalf("expected no response beside the error, got: %v", response)
    }
}

/* @info a handler returning nothing produces no response, which is what lets the kernel synthesize its own empty answer and still dispatch the response event over it. */

func TestWrapResultHandler_ANilResultProducesNoResponse(t *testing.T) {
    handler := WrapResultHandler(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (any, error) {
            return nil, nil
        },
    )

    response, err := handler(newTestRuntime(), httptest.NewRecorder(), newResultHandlerRequest())
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if nil != response {
        t.Fatalf("expected no response for a handler that returned nothing, got: %v", response)
    }
}

/* @info a handler that already built a response has it passed through rather than re-normalized, so a status the handler chose survives the wrapper. */

func TestWrapResultHandler_PassesAResponseThrough(t *testing.T) {
    built := NewResponse(nethttp.StatusCreated, []byte("created"))

    handler := WrapResultHandler(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (any, error) {
            return built, nil
        },
    )

    response, err := handler(newTestRuntime(), httptest.NewRecorder(), newResultHandlerRequest())
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if built != response {
        t.Fatalf("expected the handler's own response to pass through untouched")
    }

    if nethttp.StatusCreated != response.StatusCode() {
        t.Fatalf("expected the status the handler chose to survive, got: %d", response.StatusCode())
    }
}

func newResultHandlerRequest() httpcontract.Request {
    return testhelper.NewHttpTestRequestFromHttpRequest(httptest.NewRequest(nethttp.MethodGet, "/articles", nil))
}
