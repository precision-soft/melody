package opentelemetry

import (
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

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

/** @info middleware metrics */

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
