package opentelemetry

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
)

func TestNewPrometheusMeter_TheMeterRecordsIntoTheRegistryTheHandlerServes(t *testing.T) {
    meter, registry, meterErr := NewPrometheusMeter("example")
    if nil != meterErr {
        t.Fatalf("unexpected error: %v", meterErr)
    }

    counter, counterErr := meter.Int64Counter("example_requests")
    if nil != counterErr {
        t.Fatalf("unexpected error: %v", counterErr)
    }

    counter.Add(context.Background(), 1)

    recorder := httptest.NewRecorder()
    MetricsHandler(registry).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/metrics", nil))

    if false == strings.Contains(recorder.Body.String(), "example_requests") {
        t.Fatalf("expected the handler to serve the recorded counter, got %q", recorder.Body.String())
    }
}
