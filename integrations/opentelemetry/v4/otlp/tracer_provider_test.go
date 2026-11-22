package otlp

import (
    "context"
    "errors"
    "fmt"
    "math"
    "strings"
    "testing"
    "time"

    "go.opentelemetry.io/otel/trace"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
)

/* a missing endpoint is a configuration error, not a silent no-op exporter. */
func TestNewTracerProvider_RequiresEndpoint(t *testing.T) {
    provider, err := NewTracerProvider(tracerTestRuntime(), "", Config{})
    if nil == err {
        t.Fatalf("expected an error for an empty endpoint")
    }
    if nil != provider {
        t.Fatalf("expected no provider when the endpoint is missing")
    }
}

/* an unknown protocol is rejected rather than defaulting silently to the wrong transport. */
func TestNewTracerProvider_RejectsUnsupportedProtocol(t *testing.T) {
    provider, err := NewTracerProvider(tracerTestRuntime(), "collector:4317", Config{Protocol: "carrier-pigeon"})
    if nil == err {
        t.Fatalf("expected an error for an unsupported protocol")
    }
    if nil != provider {
        t.Fatalf("expected no provider for an unsupported protocol")
    }
}

/* grpc (default when unset) builds a provider without dialing — otlptracegrpc connects lazily. */
func TestNewTracerProvider_BuildsWithDefaultProtocol(t *testing.T) {
    provider, err := NewTracerProvider(tracerTestRuntime(), "collector:4317", Config{Insecure: true})
    if nil != err {
        t.Fatalf("unexpected error building the provider: %v", err)
    }
    if nil == provider {
        t.Fatalf("expected a provider")
    }

    _ = provider.Shutdown(context.Background())
}

func TestResolveSampler(t *testing.T) {
    cases := []struct {
        name     string
        sampling Sampling
        expected string
    }{
        {name: "left out", sampling: Sampling{}, expected: "AlwaysOnSampler"},
        {name: "a quarter", sampling: SampleRatio(0.25), expected: "TraceIDRatioBased{0.25}"},
        {name: "every trace", sampling: SampleRatio(1), expected: "AlwaysOnSampler"},
        {name: "none", sampling: SampleNone(), expected: "AlwaysOffSampler"},
    }

    for _, testCase := range cases {
        sampler, samplerErr := resolveSampler(testCase.sampling)
        if nil != samplerErr {
            t.Fatalf("%s: unexpected error: %v", testCase.name, samplerErr)
        }

        if testCase.expected != sampler.Description() {
            t.Fatalf("%s: wanted sampler %q, got %q", testCase.name, testCase.expected, sampler.Description())
        }
    }
}

func TestNewTracerProvider_RefusesAValueOutsideItsRuleNamingTheField(t *testing.T) {
    cases := []struct {
        name     string
        endpoint string
        config   Config
        key      string
        field    string
    }{
        {name: "empty endpoint", endpoint: "", config: Config{}, key: "argument", field: "endpoint"},
        {name: "zero ratio", endpoint: "collector:4317", config: Config{Sampling: SampleRatio(0)}, key: "field", field: "Config.Sampling"},
        {name: "negative ratio", endpoint: "collector:4317", config: Config{Sampling: SampleRatio(-1)}, key: "field", field: "Config.Sampling"},
        {name: "ratio above one", endpoint: "collector:4317", config: Config{Sampling: SampleRatio(1.5)}, key: "field", field: "Config.Sampling"},
        {name: "NaN ratio", endpoint: "collector:4317", config: Config{Sampling: SampleRatio(math.NaN())}, key: "field", field: "Config.Sampling"},
        {name: "zero batch timeout", endpoint: "collector:4317", config: Config{BatchTimeout: bound.Of(time.Duration(0))}, key: "field", field: "Config.BatchTimeout"},
        {name: "unlimited batch timeout", endpoint: "collector:4317", config: Config{BatchTimeout: bound.UnlimitedDuration()}, key: "field", field: "Config.BatchTimeout"},
    }

    for _, testCase := range cases {
        provider, providerErr := NewTracerProvider(tracerTestRuntime(), testCase.endpoint, testCase.config)
        if nil != provider {
            t.Fatalf("%s: expected no provider", testCase.name)
        }

        var refusal *exception.Error
        if false == errors.As(providerErr, &refusal) || testCase.field != refusal.Context()[testCase.key] {
            t.Fatalf("%s: expected a refusal naming %s, got %v", testCase.name, testCase.field, providerErr)
        }
    }
}

func TestNewTracerProvider_SampleNoneSamplesNoRootSpan(t *testing.T) {
    provider, providerErr := NewTracerProvider(tracerTestRuntime(), "collector:4317", Config{Insecure: true, Sampling: SampleNone()})
    if nil != providerErr {
        t.Fatalf("unexpected error: %v", providerErr)
    }
    defer func() { _ = provider.Shutdown(context.Background()) }()

    _, span := provider.Tracer("test").Start(context.Background(), "root")
    defer span.End()

    if true == span.SpanContext().IsSampled() {
        t.Fatal("expected SampleNone to leave the root span unsampled")
    }

    /* the sampler is parent-based: a sampled inbound parent still decides for its trace */
    parent := trace.NewSpanContext(trace.SpanContextConfig{
        TraceID:    trace.TraceID{1},
        SpanID:     trace.SpanID{1},
        TraceFlags: trace.FlagsSampled,
        Remote:     true,
    })

    _, childSpan := provider.Tracer("test").Start(trace.ContextWithRemoteSpanContext(context.Background(), parent), "child")
    defer childSpan.End()

    if false == childSpan.SpanContext().IsSampled() {
        t.Fatal("expected a sampled inbound parent to keep deciding for its trace under SampleNone")
    }

    everyProvider, everyErr := NewTracerProvider(tracerTestRuntime(), "collector:4317", Config{Insecure: true})
    if nil != everyErr {
        t.Fatalf("unexpected error: %v", everyErr)
    }
    /* the sampled span would be exported to a collector that does not exist, so the shutdown is bounded */
    defer func() {
        shutdownContext, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
        defer cancel()

        _ = everyProvider.Shutdown(shutdownContext)
    }()

    _, everySpan := everyProvider.Tracer("test").Start(context.Background(), "root")
    defer everySpan.End()

    if false == everySpan.SpanContext().IsSampled() {
        t.Fatal("expected the zero sampling to sample the root span")
    }
}

func TestConfig_RedactsHeadersOnEveryFmtVerb(t *testing.T) {
    config := Config{
        Protocol: ProtocolGrpc,
        Headers:  map[string]string{"authorization": "Bearer super-secret-token"},
    }

    for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%d"} {
        rendered := fmt.Sprintf(verb, config)
        if true == strings.Contains(rendered, "super-secret-token") {
            t.Fatalf("verb %s leaked the header credential: %s", verb, rendered)
        }
        if false == strings.Contains(rendered, ProtocolGrpc) {
            t.Fatalf("verb %s dropped the safe Protocol field: %s", verb, rendered)
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

func tracerTestRuntime() containercontract.Runtime {
    return runtime.NewRun(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope())
}

func TestNewTracerProvider_RefusesANilRuntimeAndADoneContextBeforeTheDial(t *testing.T) {
    if _, nilErr := NewTracerProvider(nil, "collector:4317", Config{}); nil == nilErr {
        t.Fatalf("expected a nil runtime to be refused")
    }

    doneContext, cancel := context.WithCancel(context.Background())
    cancel()

    doneRuntime := runtime.NewRun(doneContext, container.NewContainer(container.ContainerConfig{}).NewScope())
    if _, doneErr := NewTracerProvider(doneRuntime, "collector:4317", Config{}); nil == doneErr || false == strings.Contains(doneErr.Error(), "before the otlp exporter is dialled") {
        t.Fatalf("expected a done runtime context to be refused before the dial, got %v", doneErr)
    }
}
