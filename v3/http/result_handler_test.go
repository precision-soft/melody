package http

import (
    "context"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    "github.com/precision-soft/melody/v3/logging"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/serializer"
    serializercontract "github.com/precision-soft/melody/v3/serializer/contract"
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

type contractOnlyResponse struct {
    statusCode int
    headers    nethttp.Header
    bodyReader io.Reader
}

func (instance *contractOnlyResponse) StatusCode() int {
    return instance.statusCode
}

func (instance *contractOnlyResponse) SetStatusCode(statusCode int) {
    instance.statusCode = statusCode
}

func (instance *contractOnlyResponse) Headers() nethttp.Header {
    return instance.headers
}

func (instance *contractOnlyResponse) SetHeaders(headers nethttp.Header) {
    instance.headers = headers
}

func (instance *contractOnlyResponse) BodyReader() io.Reader {
    return instance.bodyReader
}

func (instance *contractOnlyResponse) SetBodyReader(reader io.Reader) {
    instance.bodyReader = reader
}

var _ httpcontract.Response = (*contractOnlyResponse)(nil)

func TestNormalizeResultToResponse_ServesACallerResponseImplementation(t *testing.T) {
    custom := &contractOnlyResponse{
        statusCode: nethttp.StatusCreated,
        headers:    nethttp.Header{"X-Custom": []string{"yes"}},
        bodyReader: strings.NewReader("custom-body"),
    }

    normalized, err := NormalizeResultToResponse(nil, nil, custom)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if httpcontract.Response(custom) != normalized {
        t.Fatalf("expected the caller's response implementation to be served by identity")
    }
}

func TestNormalizeResultToResponse_TypedNilContractResponseBecomesNilInterface(t *testing.T) {
    normalized, err := NormalizeResultToResponse(nil, nil, (*contractOnlyResponse)(nil))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if nil != normalized {
        t.Fatalf("expected a typed nil contract response to normalize to the nil interface")
    }
}

/* the fallback that serves the default representation no longer swallows the resolution failure: the record is the only diagnostic a client that named an available type and received another will ever produce */
func TestNormalizeResultToResponse_ARefusedResolutionIsRecordedBeforeTheFallback(t *testing.T) {
    serviceContainer := container.NewContainer()

    registerErr := container.Register[*serializer.SerializerManager](
        serviceContainer,
        serializer.ServiceSerializerManager,
        func(resolver containercontract.Resolver) (*serializer.SerializerManager, error) {
            return serializer.NewSerializerManager(
                map[string]serializercontract.Serializer{
                    serializer.MimeApplicationJson: serializer.NewJsonSerializer(),
                },
            )
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scope := serviceContainer.NewScope()
    capture := &exceptionListenerCaptureLogger{}
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, capture)

    runtimeInstance := runtime.New(context.Background(), scope, serviceContainer)

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/value", nil)
    httpRequest.Header.Set("Accept", ",,,")

    request := NewRequest(httpRequest, nil, runtimeInstance, NewRequestContext("test", time.Now()))

    response, err := NormalizeResultToResponse(runtimeInstance, request, map[string]any{"a": "b"})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if nil == response || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the fallback representation to be served")
    }

    if 1 != capture.warningCalls {
        t.Fatalf("expected the refused resolution to be recorded at warning, got %d", capture.warningCalls)
    }
}
