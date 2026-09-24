package opentelemetry

import (
    "bufio"
    "net"
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/sdk/trace/tracetest"
    "go.opentelemetry.io/otel/trace"
)

func newDecoratorTestTracer(t *testing.T) (trace.Tracer, *tracetest.SpanRecorder) {
    t.Helper()

    recorder := tracetest.NewSpanRecorder()
    provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

    return provider.Tracer("melody-test"), recorder
}

func TestHandlerDecorator_TracesShortCircuitedRequest(t *testing.T) {
    tracer, recorder := newDecoratorTestTracer(t)

    decorator, decoratorErr := NewHandlerDecorator(HandlerDecoratorConfig{Tracer: tracer})
    if nil != decoratorErr {
        t.Fatalf("unexpected decorator error: %v", decoratorErr)
    }

    /* the inner handler stands in for the kernel writing a 401 short-circuit before any middleware ran — the exact request shape the middleware seam never observes */
    denied := nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        writer.WriteHeader(nethttp.StatusUnauthorized)
        _, _ = writer.Write([]byte(`{"error":"unauthorized"}`))
    })

    request := httptest.NewRequest(nethttp.MethodGet, "/secure/me", nil)
    responseRecorder := httptest.NewRecorder()

    decorator(denied).ServeHTTP(responseRecorder, request)

    spans := recorder.Ended()
    if 1 != len(spans) {
        t.Fatalf("expected exactly one lifecycle span for the denied request, got %d", len(spans))
    }

    statusFound := false
    for _, spanAttribute := range spans[0].Attributes() {
        if "http.response.status_code" == string(spanAttribute.Key) && nethttp.StatusUnauthorized == int(spanAttribute.Value.AsInt64()) {
            statusFound = true
        }
    }

    if false == statusFound {
        t.Fatalf("expected the lifecycle span to carry the 401 status attribute")
    }
}

func TestHandlerDecorator_InnerSpanParentsToLifecycleSpan(t *testing.T) {
    tracer, recorder := newDecoratorTestTracer(t)

    decorator, decoratorErr := NewHandlerDecorator(HandlerDecoratorConfig{Tracer: tracer})
    if nil != decoratorErr {
        t.Fatalf("unexpected decorator error: %v", decoratorErr)
    }

    /* the inner handler starts a child span from the request context, standing in for the routed tracing middleware */
    routed := nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        _, span := tracer.Start(request.Context(), "GET /hello")
        span.End()

        writer.WriteHeader(nethttp.StatusOK)
    })

    request := httptest.NewRequest(nethttp.MethodGet, "/hello", nil)

    decorator(routed).ServeHTTP(httptest.NewRecorder(), request)

    spans := recorder.Ended()
    if 2 != len(spans) {
        t.Fatalf("expected the lifecycle span plus the routed child span, got %d", len(spans))
    }

    /* Ended() lists the child first (it ends first); the child must parent to the lifecycle span */
    child := spans[0]
    lifecycle := spans[1]

    if child.Parent().SpanID() != lifecycle.SpanContext().SpanID() {
        t.Fatalf("expected the routed span to be a child of the lifecycle span")
    }
}

func TestHandlerDecorator_MarksServerErrorStatus(t *testing.T) {
    tracer, recorder := newDecoratorTestTracer(t)

    decorator, decoratorErr := NewHandlerDecorator(HandlerDecoratorConfig{Tracer: tracer})
    if nil != decoratorErr {
        t.Fatalf("unexpected decorator error: %v", decoratorErr)
    }

    failing := nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        writer.WriteHeader(nethttp.StatusInternalServerError)
    })

    request := httptest.NewRequest(nethttp.MethodGet, "/boom", nil)

    decorator(failing).ServeHTTP(httptest.NewRecorder(), request)

    spans := recorder.Ended()
    if 1 != len(spans) {
        t.Fatalf("expected exactly one span, got %d", len(spans))
    }

    if "Error" != spans[0].Status().Code.String() {
        t.Fatalf("expected the 500 to mark the span status Error, got %s", spans[0].Status().Code.String())
    }
}

func TestHandlerDecorator_RequiresTracer(t *testing.T) {
    if _, decoratorErr := NewHandlerDecorator(HandlerDecoratorConfig{}); nil == decoratorErr {
        t.Fatalf("expected a nil tracer to be rejected")
    }
}

/* a middleware wrapper between the recorder and the connection that forwards Unwrap alone */
type unwrapOnlyResponseWriter struct {
    nethttp.ResponseWriter
}

func (instance *unwrapOnlyResponseWriter) Unwrap() nethttp.ResponseWriter {
    return instance.ResponseWriter
}

type hijackableResponseWriter struct {
    *httptest.ResponseRecorder
    hijacked bool
}

func (instance *hijackableResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
    instance.hijacked = true

    return nil, nil, nil
}

func TestStatusRecordingResponseWriter_FlushReachesThroughAWrapperThatForwardsUnwrapAlone(t *testing.T) {
    inner := httptest.NewRecorder()
    recorder := &statusRecordingResponseWriter{ResponseWriter: &unwrapOnlyResponseWriter{ResponseWriter: inner}, statusCode: nethttp.StatusOK}

    recorder.Flush()

    if false == inner.Flushed {
        t.Fatalf("expected the flush to reach the writer behind the wrapper")
    }
}

func TestStatusRecordingResponseWriter_HijackReachesThroughAWrapperThatForwardsUnwrapAlone(t *testing.T) {
    inner := &hijackableResponseWriter{ResponseRecorder: httptest.NewRecorder()}
    recorder := &statusRecordingResponseWriter{ResponseWriter: &unwrapOnlyResponseWriter{ResponseWriter: inner}, statusCode: nethttp.StatusOK}

    if _, _, hijackErr := recorder.Hijack(); nil != hijackErr || false == inner.hijacked || false == recorder.hijacked {
        t.Fatalf("expected the hijack to reach the writer behind the wrapper, got %v", hijackErr)
    }
}
