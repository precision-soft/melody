package otlp

import (
    "context"
    "time"

    "go.opentelemetry.io/otel/propagation"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"

    opentelemetry "github.com/precision-soft/melody/integrations/opentelemetry/v3"
    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

/* ServiceTracerProvider is the container name the OTLP tracer provider is registered under. It is
registered wrapped in a Close()-able handle so the application's container shutdown flushes pending spans. */
const ServiceTracerProvider = "opentelemetry.otlp.tracer_provider"

const defaultTracerName = "melody"

/* ModuleConfig is the plug-and-play wiring for OTLP tracing: hand it the exporter Config and register the
Module; it builds the TracerProvider, adds the tracing middleware, and flushes on shutdown. Mirrors the
NewModule facade the other integrations expose. */
type ModuleConfig struct {
    Config     Config
    TracerName string
    Propagator propagation.TextMapPropagator
}

func NewModule(config ModuleConfig) *Module {
    return &Module{config: config}
}

type Module struct {
    config ModuleConfig
}

func (instance *Module) Name() string {
    return "opentelemetry-otlp"
}

func (instance *Module) Description() string {
    return "exports traces to an OTLP collector and registers the tracing middleware"
}

/* RegisterServices registers the tracer provider as a container service so that (a) it is built once and
(b) the container's shutdown closes it — the handle's Close flushes the batch span processor. */
func (instance *Module) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    registrar.RegisterService(
        ServiceTracerProvider,
        func(resolver containercontract.Resolver) (*providerHandle, error) {
            provider, providerErr := NewTracerProvider(context.Background(), instance.config.Config)
            if nil != providerErr {
                return nil, providerErr
            }

            return &providerHandle{provider: provider}, nil
        },
    )
}

/* RegisterHttpMiddlewares resolves the tracer provider (which instantiates it, so the container tracks it
for shutdown) and installs the tracing middleware around the request pipeline. bootContainer runs before
bootHttp, so the service registered above is available here. */
func (instance *Module) RegisterHttpMiddlewares(kernelInstance kernelcontract.Kernel, registrar applicationcontract.HttpMiddlewareRegistrar) {
    handle := container.MustFromResolver[*providerHandle](kernelInstance.ServiceContainer(), ServiceTracerProvider)

    tracerName := instance.config.TracerName
    if "" == tracerName {
        tracerName = defaultTracerName
    }

    registrar.Use(opentelemetry.NewTracingMiddleware(handle.provider.Tracer(tracerName), instance.config.Propagator))
}

/* providerHandle adapts the OTLP TracerProvider's Shutdown to the container's Close() error contract. */
type providerHandle struct {
    provider *sdktrace.TracerProvider
}

func (instance *providerHandle) Close() error {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    return instance.provider.Shutdown(ctx)
}

var (
    _ applicationcontract.Module               = (*Module)(nil)
    _ applicationcontract.ServiceModule        = (*Module)(nil)
    _ applicationcontract.HttpMiddlewareModule = (*Module)(nil)
)
