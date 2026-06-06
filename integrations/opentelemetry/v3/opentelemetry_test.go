package opentelemetry_test

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/sdk/trace/tracetest"

    opentelemetry "github.com/precision-soft/melody/integrations/opentelemetry/v3"
    "github.com/precision-soft/melody/v3/container"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func testRequestAndRuntime() (httpcontract.Request, runtimecontract.Runtime) {
    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/orders/42", nil)
    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, nil)

    return request, runtimeInstance
}

func okHandler() httpcontract.Handler {
    return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return melodyhttp.JsonResponse(nethttp.StatusOK, map[string]any{"ok": true})
    }
}

func TestMetricsMiddleware_RecordsRequestMetrics(t *testing.T) {
    meter, registry, meterErr := opentelemetry.NewPrometheusMeter("melody-test")
    if nil != meterErr {
        t.Fatalf("meter: %v", meterErr)
    }

    middleware, middlewareErr := opentelemetry.NewMetricsMiddleware(meter)
    if nil != middlewareErr {
        t.Fatalf("middleware: %v", middlewareErr)
    }

    request, runtimeInstance := testRequestAndRuntime()
    handler := middleware(okHandler())

    if _, handlerErr := handler(runtimeInstance, httptest.NewRecorder(), request); nil != handlerErr {
        t.Fatalf("handler: %v", handlerErr)
    }

    families, gatherErr := registry.Gather()
    if nil != gatherErr {
        t.Fatalf("gather: %v", gatherErr)
    }

    found := false
    for _, family := range families {
        if true == strings.Contains(family.GetName(), "http_server_request") {
            found = true
        }
    }

    if false == found {
        t.Fatalf("expected an http_server_request metric to be recorded")
    }
}

func TestTracingMiddleware_RecordsServerSpan(t *testing.T) {
    recorder := tracetest.NewSpanRecorder()
    provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
    tracer := provider.Tracer("melody-test")

    middleware := opentelemetry.NewTracingMiddleware(tracer, nil)

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
