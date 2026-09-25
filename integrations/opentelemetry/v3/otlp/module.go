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

/* ModuleConfig is the plug-and-play wiring for OTLP tracing: given the exporter Config, the Module builds the TracerProvider, adds the tracing middleware and flushes on shutdown. */
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

/* RegisterServices registers the tracer provider as a container service, built once and closed by the container's shutdown, which flushes the batch span processor. The container reaches the handle through CloseWithContext, so the flush runs under the teardown's deadline; Close is the door for a caller outside a container. */
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

/* RegisterHttpMiddlewares resolves the tracer provider, which instantiates it so the container tracks it for shutdown, and installs the tracing middleware; bootContainer runs before bootHttp, so the service is available here. */
func (instance *Module) RegisterHttpMiddlewares(kernelInstance kernelcontract.Kernel, registrar applicationcontract.HttpMiddlewareRegistrar) {
    handle := container.MustFromResolver[*providerHandle](kernelInstance.ServiceContainer(), ServiceTracerProvider)

    tracerName := instance.config.TracerName
    if "" == tracerName {
        tracerName = defaultTracerName
    }

    registrar.Use(opentelemetry.NewTracingMiddleware(handle.provider.Tracer(tracerName), instance.config.Propagator))
}

/* providerHandle adapts the OTLP TracerProvider's Shutdown to the two closing doors the container knows: it prefers CloseWithContext on any service that carries one and falls back to Close() error, so this handle offers both and the teardown takes the first. */
type providerHandle struct {
    provider *sdktrace.TracerProvider
}

/* unbudgetedShutdownGrace bounds the shutdown of a handle closed with no deadline at all. It is the fallback: a caller that declares a budget is honoured whole, an operator who needs the export finished raises the teardown budget, and it applies on whichever door is reached without a deadline. */
const unbudgetedShutdownGrace = 5 * time.Second

/* CloseWithContext hands the teardown's own deadline to the provider's Shutdown. A context already spent is not handed on: the sdk's Shutdown (go.opentelemetry.io/otel/sdk trace/provider.go) latches isShutdown by compare-and-swap before its loop, returns at the loop's first ctx.Done() read with the span processors still registered, and answers nil to every later Shutdown, which would leave the provider unclosable with its batch goroutine, ticker and exporter connection running. Refusing the call keeps it closable by whatever comes next, and the container's failure map names this service either way. A remainder too small for one processor to shut down inside still latches. */
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
