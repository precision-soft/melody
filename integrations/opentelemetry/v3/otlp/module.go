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
    "github.com/precision-soft/melody/v3/exception"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

/* ServiceTracerProvider is the container name the OTLP tracer provider is registered under. It is registered wrapped in a closeable handle so the application's container shutdown flushes pending spans. */
const ServiceTracerProvider = "opentelemetry.otlp.tracer_provider"

const defaultTracerName = "melody"

/* ModuleConfig is the plug-and-play wiring for OTLP tracing: hand it the exporter Config and register the Module; it builds the TracerProvider, adds the tracing middleware, and flushes on shutdown. Mirrors the NewModule facade the other integrations expose. */
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

/* RegisterServices registers the tracer provider as a container service so that (a) it is built once and (b) the container's shutdown closes it — the flush of the batch span processor is what that close performs. The container reaches the handle through CloseWithContext, not through Close, so the teardown's own deadline is what the flush runs under; the plain door is what a caller outside a container gets. */
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

/* RegisterHttpMiddlewares resolves the tracer provider (which instantiates it, so the container tracks it for shutdown) and installs the tracing middleware around the request pipeline. bootContainer runs before bootHttp, so the service registered above is available here. */
func (instance *Module) RegisterHttpMiddlewares(kernelInstance kernelcontract.Kernel, registrar applicationcontract.HttpMiddlewareRegistrar) {
    handle := container.MustFromResolver[*providerHandle](kernelInstance.ServiceContainer(), ServiceTracerProvider)

    tracerName := instance.config.TracerName
    if "" == tracerName {
        tracerName = defaultTracerName
    }

    registrar.Use(opentelemetry.NewTracingMiddleware(handle.provider.Tracer(tracerName), instance.config.Propagator))
}

type providerHandle struct {
    provider *sdktrace.TracerProvider
}

const unbudgetedShutdownGrace = 5 * time.Second

/* CloseWithContext passes the caller’s deadline to provider shutdown, adding the package fallback when none is present. An already-spent context is refused before the provider can latch its shutdown state. A deadline that expires inside vendor shutdown can still leave processors unclosed; the method cannot guarantee their completion beyond the caller’s budget. */
func (instance *providerHandle) CloseWithContext(closeContext context.Context) error {
    if nil != closeContext.Err() {
        return exception.NewError(
            "otlp tracer provider was not shut down: its close was reached with the deadline already spent, and a shutdown driven under one is answered by locking the provider shut with every span processor still running",
            map[string]any{"service": ServiceTracerProvider},
            closeContext.Err(),
        )
    }

    if _, hasDeadline := closeContext.Deadline(); false == hasDeadline {
        boundedContext, cancel := context.WithTimeout(closeContext, unbudgetedShutdownGrace)
        defer cancel()

        return instance.provider.Shutdown(boundedContext)
    }

    return instance.provider.Shutdown(closeContext)
}

func (instance *providerHandle) Close() error {
    return instance.CloseWithContext(context.Background())
}

var (
    _ applicationcontract.Module               = (*Module)(nil)
    _ applicationcontract.ServiceModule        = (*Module)(nil)
    _ applicationcontract.HttpMiddlewareModule = (*Module)(nil)
)
