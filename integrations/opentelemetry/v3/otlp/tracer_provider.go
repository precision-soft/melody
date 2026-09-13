package otlp

import (
    "context"
    "fmt"
    "time"

    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
    "go.opentelemetry.io/otel/sdk/resource"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "google.golang.org/grpc"

    "github.com/precision-soft/melody/v3/exception"
)

const (
    /* ProtocolGrpc and ProtocolHttp select the OTLP transport; grpc (default) talks to the collector's 4317 receiver, http/protobuf to 4318. */
    ProtocolGrpc = "grpc"
    ProtocolHttp = "http"

    defaultBatchTimeout = 5 * time.Second
)

/* Config describes how to export spans to an OTLP collector. Endpoint is host:port without a scheme (for example "otel-collector:4317"); the deployment owns these values, so an app typically fills them from a parameter / .env. SampleRatio in (0,1) keeps that fraction of traces; 0 or >=1 samples everything; a negative or NaN ratio is refused at construction, because both used to fall through to AlwaysSample. */
type Config struct {
    Endpoint       string
    Protocol       string
    ServiceName    string
    ServiceVersion string
    SampleRatio    float64
    Headers        map[string]string
    Insecure       bool
    BatchTimeout   time.Duration
}

/* String and Format keep Headers out of every rendering fmt can reach. Headers is an EXPORTED field, so a plain %v walks it and prints the collector auth token an app puts there in the clear. Format is what makes this complete: fmt consults a Formatter before Stringer and GoStringer and for every verb, so Format answers %#v and the numeric verbs (%d, %o, …) that would otherwise reflection-walk the struct, and it delegates to String for the one rendering. A separate GoString would be dead — Format shadows it. The receivers are values so that both a Config and a pointer to it redact, and Headers is reduced to its count — enough to see whether any were set without naming one. This mirrors the redaction the encrypt key provider carries for the same reason. */
func (instance Config) String() string {
    return fmt.Sprintf(
        "otlp.Config{Endpoint:%q, Protocol:%q, ServiceName:%q, ServiceVersion:%q, SampleRatio:%v, Insecure:%v, BatchTimeout:%v, Headers:[redacted %d]}",
        instance.Endpoint,
        instance.Protocol,
        instance.ServiceName,
        instance.ServiceVersion,
        instance.SampleRatio,
        instance.Insecure,
        instance.BatchTimeout,
        len(instance.Headers),
    )
}

func (instance Config) Format(state fmt.State, verb rune) {
    _, _ = state.Write([]byte(instance.String()))
}

/* NewTracerProvider builds a batching TracerProvider wired to an OTLP exporter. The caller owns the returned provider's lifecycle — Shutdown must run on application exit to flush pending spans; the plug-and- play Module in this package does that for you by registering it as a container-managed service. */
func NewTracerProvider(ctx context.Context, config Config) (*sdktrace.TracerProvider, error) {
    if "" == config.Endpoint {
        return nil, exception.NewError("otlp tracer provider endpoint is required", nil, nil)
    }

    /* the documented contract covers 0 (sample everything) and the open interval; a negative ratio — the natural "tracing off" sentinel — and a NaN from a failed parse both fell through samplerFor's window check to AlwaysSample, inverting "minimum tracing" into 100% export exactly when the operator asked for none. Both are refused at construction instead. */
    if 0 > config.SampleRatio || config.SampleRatio != config.SampleRatio {
        return nil, exception.NewError(
            "otlp tracer provider sample ratio must not be negative or NaN: use a ratio in (0,1) to sample that fraction, or 0 / >=1 to sample everything",
            map[string]any{"sampleRatio": config.SampleRatio},
            nil,
        )
    }

    exporter, exporterErr := newExporter(ctx, config)
    if nil != exporterErr {
        return nil, exporterErr
    }

    resourceInstance, resourceErr := resource.New(
        ctx,
        resource.WithAttributes(
            attribute.String("service.name", serviceNameOrDefault(config.ServiceName)),
            attribute.String("service.version", config.ServiceVersion),
        ),
    )
    if nil != resourceErr {
        return nil, exception.NewError("failed to build otlp resource", nil, resourceErr)
    }

    batchTimeout := config.BatchTimeout
    if 0 >= batchTimeout {
        batchTimeout = defaultBatchTimeout
    }

    tracerProvider := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(batchTimeout)),
        sdktrace.WithResource(resourceInstance),
        sdktrace.WithSampler(sdktrace.ParentBased(samplerFor(config.SampleRatio))),
    )

    return tracerProvider, nil
}

func newExporter(ctx context.Context, config Config) (*otlptrace.Exporter, error) {
    protocol := config.Protocol
    if "" == protocol {
        protocol = ProtocolGrpc
    }

    switch protocol {
    case ProtocolGrpc:
        options := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(config.Endpoint)}
        if true == config.Insecure {
            options = append(options, otlptracegrpc.WithInsecure())
        }
        if 0 < len(config.Headers) {
            options = append(options, otlptracegrpc.WithHeaders(config.Headers))
        }

        /* the channel is dialled with the service config lookup disabled: grpc's dns resolver otherwise asks for a TXT record (_grpc_config.<host>) beside the address records of every new channel, and melody publishes no service config that way, so the lookup is cost alone. The channel opens lazily, on the first export; on a resolver that answers the address at once and the TXT record never — docker's embedded dns for a container name is one — that first export waits the resolver's whole timeout with the channel still connecting, and a shutdown flush that is the one to open it spends the entire close budget on a record it does not need, reporting a collector it could have reached as unreachable. */
        options = append(options, otlptracegrpc.WithDialOption(grpc.WithDisableServiceConfig()))

        return otlptracegrpc.New(ctx, options...)

    case ProtocolHttp:
        options := []otlptracehttp.Option{otlptracehttp.WithEndpoint(config.Endpoint)}
        if true == config.Insecure {
            options = append(options, otlptracehttp.WithInsecure())
        }
        if 0 < len(config.Headers) {
            options = append(options, otlptracehttp.WithHeaders(config.Headers))
        }

        return otlptracehttp.New(ctx, options...)

    default:
        return nil, exception.NewError(
            "unsupported otlp protocol",
            map[string]any{"protocol": protocol},
            nil,
        )
    }
}

func samplerFor(ratio float64) sdktrace.Sampler {
    if 0 < ratio && 1 > ratio {
        return sdktrace.TraceIDRatioBased(ratio)
    }

    return sdktrace.AlwaysSample()
}

func serviceNameOrDefault(serviceName string) string {
    if "" == serviceName {
        return "melody"
    }

    return serviceName
}
