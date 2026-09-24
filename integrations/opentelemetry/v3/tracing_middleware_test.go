package opentelemetry

import (
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/sdk/trace/tracetest"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "go.opentelemetry.io/otel/codes"
)

func TestTracingMiddleware_RequiresTracer(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected a nil tracer to panic at construction")
        }
    }()

    NewTracingMiddleware(nil, nil)
}

func TestTracingMiddleware_RecordsServerSpan(t *testing.T) {
    recorder := tracetest.NewSpanRecorder()
    provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
    tracer := provider.Tracer("melody-test")

    middleware := NewTracingMiddleware(tracer, nil)

    request, runtimeInstance := testRequestAndRuntime()
    handler := middleware(okHandler())

    if _, handlerErr := handler(runtimeInstance, httptest.NewRecorder(), request); nil != handlerErr {
        t.Fatalf("handler: %v", handlerErr)
    }

    spans := recorder.Ended()
    if 1 != len(spans) {
        t.Fatalf("expected exactly one span, got %d", len(spans))
    }

    if false == strings.Contains(spans[0].Name(), nethttp.MethodGet) {
        t.Fatalf("unexpected span name: %s", spans[0].Name())
    }

    methodFound := false
    statusFound := false
    for _, attribute := range spans[0].Attributes() {
        if "http.request.method" == string(attribute.Key) && nethttp.MethodGet == attribute.Value.AsString() {
            methodFound = true
        }
        if "http.response.status_code" == string(attribute.Key) && 200 == int(attribute.Value.AsInt64()) {
            statusFound = true
        }
    }

    if false == methodFound || false == statusFound {
        t.Fatalf("expected method and status attributes on the span")
    }
}

func TestTracingMiddleware_ATypedNilResponseIsReadAsAbsentInsteadOfPanicking(t *testing.T) {
    recorder := tracetest.NewSpanRecorder()
    provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

    middleware := NewTracingMiddleware(provider.Tracer("melody-typed-nil-test"), nil)

    typedNilHandler := func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        var response *typedNilProneResponse

        return response, nil
    }

    request, runtimeInstance := testRequestAndRuntime()

    if _, handlerErr := middleware(typedNilHandler)(runtimeInstance, httptest.NewRecorder(), request); nil != handlerErr {
        t.Fatalf("handler: %v", handlerErr)
    }

    spans := recorder.Ended()
    if 1 != len(spans) {
        t.Fatalf("expected the span to end cleanly over a typed-nil response, got %d spans", len(spans))
    }
}

func TestTracingMiddleware_RecordsTheClientStatusWhenTheHandlerReturnsAnHttpExceptionError(t *testing.T) {
    recorder := tracetest.NewSpanRecorder()
    provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
    tracer := provider.Tracer("melody-error-test")

    middleware := NewTracingMiddleware(tracer, nil)

    request, runtimeInstance := testRequestAndRuntime()
    handler := middleware(func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return nil, exception.NewHttpException(nethttp.StatusNotFound, "not found")
    })

    if _, handlerErr := handler(runtimeInstance, httptest.NewRecorder(), request); nil == handlerErr {
        t.Fatalf("expected the error to propagate")
    }

    spans := recorder.Ended()
    if 1 != len(spans) {
        t.Fatalf("expected exactly one span, got %d", len(spans))
    }

    statusCode := int64(-1)
    for _, attribute := range spans[0].Attributes() {
        if "http.response.status_code" == string(attribute.Key) {
            statusCode = attribute.Value.AsInt64()
        }
    }

    if 404 != statusCode {
        t.Fatalf("expected the span to carry the client-facing 404 rather than omitting the status on error, got %d", statusCode)
    }
}

func tracedHandlerSpan(t *testing.T, handlerErr error) sdktrace.ReadOnlySpan {
    t.Helper()

    recorder := tracetest.NewSpanRecorder()
    provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
    middleware := NewTracingMiddleware(provider.Tracer("melody-status-test"), nil)

    request, runtimeInstance := testRequestAndRuntime()
    handler := middleware(func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return nil, handlerErr
    })

    _, _ = handler(runtimeInstance, httptest.NewRecorder(), request)

    spans := recorder.Ended()
    if 1 != len(spans) {
        t.Fatalf("expected exactly one span, got %d", len(spans))
    }

    return spans[0]
}

/* a handled sub-500 is the client's error: the server span keeps its status unset, and the error stays recorded as the span's exception event */
func TestTracingMiddleware_AHandledSubFiveHundredLeavesTheSpanStatusUnset(t *testing.T) {
    span := tracedHandlerSpan(t, exception.NewHttpException(nethttp.StatusNotFound, "not found"))

    if codes.Unset != span.Status().Code || 1 != len(span.Events()) {
        t.Fatalf("expected an unset status and the error recorded as one event, got %v with %d events", span.Status(), len(span.Events()))
    }
}

func TestTracingMiddleware_AServerErrorSetsTheSpanStatusToError(t *testing.T) {
    span := tracedHandlerSpan(t, errors.New("database is gone"))

    if codes.Error != span.Status().Code || "database is gone" != span.Status().Description {
        t.Fatalf("expected the error status carrying the message, got %v", span.Status())
    }
}

type tracingTypedNilError struct {
    message *string
}

func (instance *tracingTypedNilError) Error() string {
    return *instance.message
}

/* a typed nil answers Error() with a panic: the middleware reads the message through the exception package and records the span without panicking on the handler's value */
func TestTracingMiddleware_ATypedNilHandlerErrorIsRecordedWithoutPanicking(t *testing.T) {
    var typedNil *tracingTypedNilError

    span := tracedHandlerSpan(t, typedNil)

    if codes.Error != span.Status().Code || 1 != len(span.Events()) {
        t.Fatalf("expected the typed nil recorded as a server error, got %v with %d events", span.Status(), len(span.Events()))
    }
}
