package opentelemetry

import (
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/sdk/trace/tracetest"
)

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
