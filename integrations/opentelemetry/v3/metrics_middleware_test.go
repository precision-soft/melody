package opentelemetry

import (
    "errors"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/prometheus/client_golang/prometheus"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func TestNormalizedMethod(t *testing.T) {
    standard := []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "CONNECT", "OPTIONS", "TRACE"}
    for _, method := range standard {
        if normalized := normalizedMethod(method); method != normalized {
            t.Fatalf("expected standard method %q to be preserved, got %q", method, normalized)
        }
    }

    for _, method := range []string{"BREW", "XYZZY", "M0001", "get", ""} {
        if normalized := normalizedMethod(method); "_OTHER" != normalized {
            t.Fatalf("expected non-standard method %q to normalize to _OTHER, got %q", method, normalized)
        }
    }
}

func TestMetricsMiddleware_RequiresMeter(t *testing.T) {
    if _, middlewareErr := NewMetricsMiddleware(nil); nil == middlewareErr {
        t.Fatalf("expected a nil meter to be rejected")
    }
}

func TestMetricsMiddleware_RecordsRequestMetrics(t *testing.T) {
    meter, registry, meterErr := NewPrometheusMeter("melody-test")
    if nil != meterErr {
        t.Fatalf("meter: %v", meterErr)
    }

    middleware, middlewareErr := NewMetricsMiddleware(meter)
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

func TestMetricsMiddleware_RecordsServerErrorStatusWhenHandlerReturnsError(t *testing.T) {
    meter, registry, meterErr := NewPrometheusMeter("melody-test-error")
    if nil != meterErr {
        t.Fatalf("meter: %v", meterErr)
    }

    middleware, middlewareErr := NewMetricsMiddleware(meter)
    if nil != middlewareErr {
        t.Fatalf("middleware: %v", middlewareErr)
    }

    request, runtimeInstance := testRequestAndRuntime()
    handler := middleware(func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return nil, errors.New("boom")
    })

    if _, handlerErr := handler(runtimeInstance, httptest.NewRecorder(), request); nil == handlerErr {
        t.Fatalf("expected the handler error to propagate")
    }

    families, gatherErr := registry.Gather()
    if nil != gatherErr {
        t.Fatalf("gather: %v", gatherErr)
    }

    statusFound := ""
    for _, family := range families {
        if false == strings.Contains(family.GetName(), "http_server_request") {
            continue
        }
        for _, metricInstance := range family.GetMetric() {
            for _, label := range metricInstance.GetLabel() {
                if "http_response_status_code" == label.GetName() {
                    statusFound = label.GetValue()
                }
            }
        }
    }

    if "500" != statusFound {
        t.Fatalf("expected the status_code label to be 500 for an errored request, got %q", statusFound)
    }
}

func TestMetricsMiddleware_ReadsTheStatusADirectWriterCommitted(t *testing.T) {
    meter, registry, meterErr := NewPrometheusMeter("melody-direct-writer-test")
    if nil != meterErr {
        t.Fatalf("meter: %v", meterErr)
    }

    middleware, middlewareErr := NewMetricsMiddleware(meter)
    if nil != middlewareErr {
        t.Fatalf("middleware: %v", middlewareErr)
    }

    directWriter := func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        writer.WriteHeader(nethttp.StatusServiceUnavailable)

        return nil, nil
    }

    request, runtimeInstance := testRequestAndRuntime()

    if _, handlerErr := middleware(directWriter)(runtimeInstance, httptest.NewRecorder(), request); nil != handlerErr {
        t.Fatalf("handler: %v", handlerErr)
    }

    if false == gatheredStatusLabels(t, registry)["503"] {
        t.Fatalf("expected the directly committed 503 to be recorded instead of the constructor's 200, got %v", gatheredStatusLabels(t, registry))
    }
}

/* typedNilProneResponse exists so a test can hand the middleware the typed-nil shape a userland error branch produces. Its accessors DEREFERENCE the receiver, like every real response's do: a method body that ignores the receiver would run happily on a nil pointer, and the guard's mutant would survive against a fixture that cannot reproduce the panic it guards against. */
type typedNilProneResponse struct {
    statusCode int
    headers    nethttp.Header
    body       io.Reader
}

func (instance *typedNilProneResponse) StatusCode() int                   { return instance.statusCode }
func (instance *typedNilProneResponse) SetStatusCode(statusCode int)      { instance.statusCode = statusCode }
func (instance *typedNilProneResponse) Headers() nethttp.Header           { return instance.headers }
func (instance *typedNilProneResponse) SetHeaders(headers nethttp.Header) { instance.headers = headers }
func (instance *typedNilProneResponse) BodyReader() io.Reader             { return instance.body }
func (instance *typedNilProneResponse) SetBodyReader(reader io.Reader)    { instance.body = reader }

func TestMetricsMiddleware_ATypedNilResponseIsReadAsAbsentInsteadOfPanicking(t *testing.T) {
    meter, registry, meterErr := NewPrometheusMeter("melody-typed-nil-test")
    if nil != meterErr {
        t.Fatalf("meter: %v", meterErr)
    }

    middleware, middlewareErr := NewMetricsMiddleware(meter)
    if nil != middlewareErr {
        t.Fatalf("middleware: %v", middlewareErr)
    }

    typedNilHandler := func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        var response *typedNilProneResponse

        return response, nil
    }

    request, runtimeInstance := testRequestAndRuntime()

    if _, handlerErr := middleware(typedNilHandler)(runtimeInstance, httptest.NewRecorder(), request); nil != handlerErr {
        t.Fatalf("handler: %v", handlerErr)
    }

    if false == gatheredStatusLabels(t, registry)["200"] {
        t.Fatalf("expected the typed-nil response to be read as absent and the committed 200 recorded, got %v", gatheredStatusLabels(t, registry))
    }
}

func gatheredStatusLabels(t *testing.T, registry *prometheus.Registry) map[string]bool {
    t.Helper()

    families, gatherErr := registry.Gather()
    if nil != gatherErr {
        t.Fatalf("gather: %v", gatherErr)
    }

    statuses := map[string]bool{}
    for _, family := range families {
        for _, metricInstance := range family.GetMetric() {
            for _, label := range metricInstance.GetLabel() {
                if "http_response_status_code" == label.GetName() {
                    statuses[label.GetValue()] = true
                }
            }
        }
    }

    return statuses
}
