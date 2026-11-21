package opentelemetry

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    sdkmetric "go.opentelemetry.io/otel/sdk/metric"
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

/* failingShutdownReader is a reader whose shutdown fails, so the provider's one shutdown answers an error */
type failingShutdownReader struct {
    *sdkmetric.ManualReader
}

func (instance *failingShutdownReader) Shutdown(context.Context) error {
    return errors.New("the reader could not shut down")
}

/* the sdk runs its shutdown once and answers every later call an error, so a close after a failed shutdown answers nil rather than that error on every retry of the container's teardown */
func TestPrometheusMetrics_ACloseAfterAFailedShutdownAnswersNil(t *testing.T) {
    prometheusMetrics := &PrometheusMetrics{
        provider: sdkmetric.NewMeterProvider(sdkmetric.WithReader(&failingShutdownReader{ManualReader: sdkmetric.NewManualReader()})),
    }

    firstErr := prometheusMetrics.CloseWithContext(context.Background())
    if nil == firstErr || false == strings.Contains(firstErr.Error(), "could not shut the prometheus meter provider down") {
        t.Fatalf("expected the failed shutdown answered, got %v", firstErr)
    }

    if secondErr := prometheusMetrics.CloseWithContext(context.Background()); nil != secondErr {
        t.Fatalf("expected a close after a failed shutdown to answer nil, got %v", secondErr)
    }
}
