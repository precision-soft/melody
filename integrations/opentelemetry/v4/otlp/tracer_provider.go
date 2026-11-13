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

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

const (
    /* ProtocolGrpc and ProtocolHttp select the OTLP transport; grpc (default) talks to the collector's 4317 receiver, http/protobuf to 4318. */
    ProtocolGrpc = "grpc"
    ProtocolHttp = "http"
)

var batchTimeoutRule = bound.Rule[time.Duration]{
    Name:    "Config.BatchTimeout",
    Default: 5 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

/* Config describes how to export spans to an OTLP collector. Endpoint is required, host:port without a scheme, for example "otel-collector:4317", filled by the deployment from a parameter or .env; Protocol is grpc when empty; ServiceName is melody when empty; Sampling samples every trace when left out; Headers are the exporter's extra headers; Insecure skips transport security. BatchTimeout is the batch span processor's flush interval: 5s by default, at least 1ms, unlimited refused, since an unflushed batch is never exported. */
type Config struct {
    Endpoint       string
    Protocol       string
    ServiceName    string
    ServiceVersion string
    Sampling       Sampling
    Headers        map[string]string
    Insecure       bool
    BatchTimeout   bound.Duration
}

/* String and Format keep Headers out of every rendering fmt routes through this value's methods, Headers reduced to its count, since it is an exported field that carries the collector auth token. Format answers %#v and the numeric verbs too, and value receivers make a Config and a pointer to it redact alike. %p and %w never reach Format, and a Config held in an unexported field of another struct is walked by reflection with no method called. */
func (instance Config) String() string {
    return fmt.Sprintf(
        "otlp.Config{Endpoint:%q, Protocol:%q, ServiceName:%q, ServiceVersion:%q, Sampling:%v, Insecure:%v, BatchTimeout:%v, Headers:[redacted %d]}",
        instance.Endpoint,
        instance.Protocol,
        instance.ServiceName,
        instance.ServiceVersion,
        instance.Sampling.String(),
        instance.Insecure,
        instance.BatchTimeout.String(),
        len(instance.Headers),
    )
}

func (instance Config) Format(state fmt.State, verb rune) {
    _, _ = state.Write([]byte(instance.String()))
}

/* NewTracerProvider builds a batching TracerProvider wired to an OTLP exporter; an empty endpoint, a sampling or a batch timeout outside its rule is answered as the error, naming the field. The caller owns its lifecycle and runs Shutdown on exit to flush pending spans; the Module in this package does so by registering it as a container-managed service. */
func NewTracerProvider(ctx context.Context, config Config) (*sdktrace.TracerProvider, error) {
    if "" == config.Endpoint {
        return nil, exception.NewError("otlp tracer provider endpoint is required", exceptioncontract.Context{"field": "Config.Endpoint"}, nil)
    }

    sampler, samplerErr := resolveSampler(config.Sampling)
    if nil != samplerErr {
        return nil, samplerErr
    }

    batchTimeout, batchTimeoutErr := bound.Resolve(config.BatchTimeout, batchTimeoutRule)
    if nil != batchTimeoutErr {
        return nil, batchTimeoutErr
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

    tracerProvider := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(batchTimeout.Value)),
        sdktrace.WithResource(resourceInstance),
        sdktrace.WithSampler(sdktrace.ParentBased(sampler)),
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

        /* the channel is dialled with the service config lookup disabled: grpc's dns resolver otherwise asks for a TXT record (_grpc_config.<host>) beside the address records, which melody never publishes. The channel opens on the first export, and on a resolver that never answers the TXT record, docker's embedded dns for a container name being one, that export, possibly the shutdown flush, would wait the resolver's whole timeout */
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

func serviceNameOrDefault(serviceName string) string {
    if "" == serviceName {
        return "melody"
    }

    return serviceName
}
