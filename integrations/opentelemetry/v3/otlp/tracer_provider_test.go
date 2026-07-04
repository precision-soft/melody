package otlp

import (
    "context"
    "testing"

    sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

/** @info a missing endpoint is a configuration error, not a silent no-op exporter. */
func TestNewTracerProvider_RequiresEndpoint(t *testing.T) {
    provider, err := NewTracerProvider(context.Background(), Config{})
    if nil == err {
        t.Fatalf("expected an error for an empty endpoint")
    }
    if nil != provider {
        t.Fatalf("expected no provider when the endpoint is missing")
    }
}

/** @info an unknown protocol is rejected rather than defaulting silently to the wrong transport. */
func TestNewTracerProvider_RejectsUnsupportedProtocol(t *testing.T) {
    provider, err := NewTracerProvider(context.Background(), Config{Endpoint: "collector:4317", Protocol: "carrier-pigeon"})
    if nil == err {
        t.Fatalf("expected an error for an unsupported protocol")
    }
    if nil != provider {
        t.Fatalf("expected no provider for an unsupported protocol")
    }
}

/** @info grpc (default when unset) builds a provider without dialing — otlptracegrpc connects lazily. */
func TestNewTracerProvider_BuildsWithDefaultProtocol(t *testing.T) {
    provider, err := NewTracerProvider(context.Background(), Config{Endpoint: "collector:4317", Insecure: true})
    if nil != err {
        t.Fatalf("unexpected error building the provider: %v", err)
    }
    if nil == provider {
        t.Fatalf("expected a provider")
    }

    _ = provider.Shutdown(context.Background())
}

/** @info a ratio in (0,1) yields a ratio sampler; 0 and >=1 sample everything. */
func TestSamplerFor(t *testing.T) {
    cases := []struct {
        ratio    float64
        expected string
    }{
        {ratio: 0.25, expected: "TraceIDRatioBased{0.25}"},
        {ratio: 0, expected: "AlwaysOnSampler"},
        {ratio: 1, expected: "AlwaysOnSampler"},
        {ratio: 2, expected: "AlwaysOnSampler"},
    }

    for _, testCase := range cases {
        sampler := samplerFor(testCase.ratio)

        var _ sdktrace.Sampler = sampler

        if testCase.expected != sampler.Description() {
            t.Fatalf("ratio %v: wanted sampler %q, got %q", testCase.ratio, testCase.expected, sampler.Description())
        }
    }
}
