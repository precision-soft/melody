package otlp

import (
    "context"
    "fmt"
    "math"
    "strings"
    "testing"

    sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

/* a missing endpoint is a configuration error, not a silent no-op exporter. */
func TestNewTracerProvider_RequiresEndpoint(t *testing.T) {
    provider, err := NewTracerProvider(context.Background(), Config{})
    if nil == err {
        t.Fatalf("expected an error for an empty endpoint")
    }
    if nil != provider {
        t.Fatalf("expected no provider when the endpoint is missing")
    }
}

/* an unknown protocol is rejected rather than defaulting silently to the wrong transport. */
func TestNewTracerProvider_RejectsUnsupportedProtocol(t *testing.T) {
    provider, err := NewTracerProvider(context.Background(), Config{Endpoint: "collector:4317", Protocol: "carrier-pigeon"})
    if nil == err {
        t.Fatalf("expected an error for an unsupported protocol")
    }
    if nil != provider {
        t.Fatalf("expected no provider for an unsupported protocol")
    }
}

/* grpc (default when unset) builds a provider without dialing — otlptracegrpc connects lazily. */
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

/* a ratio in (0,1) yields a ratio sampler; 0 and >=1 sample everything. */
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

func TestNewTracerProvider_RefusesANegativeSampleRatio(t *testing.T) {
    _, providerErr := NewTracerProvider(context.Background(), Config{Endpoint: "collector:4317", SampleRatio: -1})

    if nil == providerErr {
        t.Fatal("expected a negative sample ratio - the natural tracing-off sentinel - to be refused instead of inverting to AlwaysSample")
    }
}

func TestNewTracerProvider_RefusesANaNSampleRatio(t *testing.T) {
    _, providerErr := NewTracerProvider(context.Background(), Config{Endpoint: "collector:4317", SampleRatio: math.NaN()})

    if nil == providerErr {
        t.Fatal("expected a NaN sample ratio - the shape of a failed parse - to be refused instead of inverting to AlwaysSample")
    }
}

func TestConfig_RedactsHeadersOnEveryFmtVerb(t *testing.T) {
    config := Config{
        Endpoint: "otel-collector:4317",
        Protocol: ProtocolGrpc,
        Headers:  map[string]string{"authorization": "Bearer super-secret-token"},
    }

    for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%d"} {
        rendered := fmt.Sprintf(verb, config)
        if true == strings.Contains(rendered, "super-secret-token") {
            t.Fatalf("verb %s leaked the header credential: %s", verb, rendered)
        }
        if false == strings.Contains(rendered, "otel-collector:4317") {
            t.Fatalf("verb %s dropped the safe Endpoint field: %s", verb, rendered)
        }
        if false == strings.Contains(rendered, "redacted") {
            t.Fatalf("verb %s did not mark the headers redacted: %s", verb, rendered)
        }
    }

    /* a pointer to the config redacts too, since the value receiver is promoted */
    pointerRendered := fmt.Sprintf("%v", &config)
    if true == strings.Contains(pointerRendered, "super-secret-token") {
        t.Fatalf("a *Config leaked the header credential: %s", pointerRendered)
    }
}
