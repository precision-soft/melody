package opentelemetry

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
)

func TestNewPrometheusMetrics_TheMeterRecordsIntoWhatTheHandlerServes(t *testing.T) {
    prometheusMetrics, metricsErr := NewPrometheusMetrics(PrometheusConfig{MeterName: "example"})
    if nil != metricsErr {
        t.Fatalf("unexpected error: %v", metricsErr)
    }

    counter, counterErr := prometheusMetrics.Meter().Int64Counter("example_requests")
    if nil != counterErr {
        t.Fatalf("unexpected error: %v", counterErr)
    }

    counter.Add(context.Background(), 1)

    recorder := httptest.NewRecorder()
    prometheusMetrics.Handler().ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/metrics", nil))

    if false == strings.Contains(recorder.Body.String(), "example_requests") {
        t.Fatalf("expected the handler to serve the recorded counter, got %q", recorder.Body.String())
    }
}

func TestPrometheusMetrics_CloseShutsTheProviderDownAndASecondCloseAnswersNil(t *testing.T) {
    prometheusMetrics, metricsErr := NewPrometheusMetrics(PrometheusConfig{})
    if nil != metricsErr {
        t.Fatalf("unexpected error: %v", metricsErr)
    }

    if closeErr := prometheusMetrics.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if shutdownErr := prometheusMetrics.provider.Shutdown(context.Background()); nil == shutdownErr {
        t.Fatal("expected the provider already shut down")
    }

    if closeErr := prometheusMetrics.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("expected a second close to answer nil, got %v", closeErr)
    }
}

func TestPrometheusMetrics_ADoneCloseContextIsAnsweredItsErrorAndLeavesTheProviderOpen(t *testing.T) {
    prometheusMetrics, metricsErr := NewPrometheusMetrics(PrometheusConfig{})
    if nil != metricsErr {
        t.Fatalf("unexpected error: %v", metricsErr)
    }

    doneContext, cancel := context.WithCancel(context.Background())
    cancel()

    if closeErr := prometheusMetrics.CloseWithContext(doneContext); nil == closeErr || false == strings.Contains(closeErr.Error(), "already done") {
        t.Fatalf("expected the done context's refusal, got %v", closeErr)
    }

    if closeErr := prometheusMetrics.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("expected the provider still closable, got %v", closeErr)
    }
}
