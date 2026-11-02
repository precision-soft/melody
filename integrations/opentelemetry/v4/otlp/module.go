package otlp

import (
    "context"
    "time"

    "go.opentelemetry.io/otel/propagation"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"

    opentelemetry "github.com/precision-soft/melody/integrations/opentelemetry/v4"
    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

/* ServiceTracerProvider is the container name the OTLP tracer provider is registered under. It is registered wrapped in a closeable handle so the application's container shutdown flushes pending spans. */
const ServiceTracerProvider = "opentelemetry.otlp.tracer_provider"

const defaultTracerName = "melody"

/* ModuleConfig is the plug-and-play wiring for OTLP tracing: given the exporter Config, the Module builds the TracerProvider, adds the tracing middleware and flushes on shutdown. TracerName is melody when empty and Propagator W3C TraceContext when nil. */
type ModuleConfig struct {
    Config     Config
    TracerName string
    Propagator propagation.TextMapPropagator

    /* ShutdownTimeout bounds the provider's shutdown when its close is reached with no deadline of the teardown's: 5s by default, at least 1ms, unlimited refused. A close that carries a deadline is honoured whole; an operator who needs the export finished raises the teardown budget. It is refused at RegisterServices, naming the field. */
    ShutdownTimeout bound.Duration
}

var shutdownTimeoutRule = bound.Rule[time.Duration]{
    Name:    "ModuleConfig.ShutdownTimeout",
    Default: 5 * time.Second,
    Minimum: bound.TimeoutMinimum,
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

/* RegisterServices registers the tracer provider as a container service, built once and closed by the container's shutdown, which flushes the batch span processor. The container reaches the handle through CloseWithContext, so the flush runs under the teardown's deadline. */
func (instance *Module) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    shutdownTimeout, shutdownTimeoutErr := bound.Resolve(instance.config.ShutdownTimeout, shutdownTimeoutRule)
    if nil != shutdownTimeoutErr {
        exception.Panic(exception.FromError(shutdownTimeoutErr))
    }

    registrar.RegisterService(
        ServiceTracerProvider,
        func(resolver containercontract.Resolver) (*providerHandle, error) {
            provider, providerErr := NewTracerProvider(context.Background(), instance.config.Config)
            if nil != providerErr {
                return nil, providerErr
            }

            return &providerHandle{provider: provider, shutdownTimeout: shutdownTimeout.Value}, nil
        },
        container.RegisterOptions{},
    )
}

/* RegisterHttpMiddlewares resolves the tracer provider, which instantiates it so the container tracks it for shutdown, and installs the tracing middleware; the boot calls every module's RegisterServices before any module's RegisterHttpMiddlewares, so the service is registered here. */
func (instance *Module) RegisterHttpMiddlewares(kernelInstance kernelcontract.Kernel, registrar applicationcontract.HttpMiddlewareRegistrar) {
    handle := container.MustFromResolver[*providerHandle](kernelInstance.Context(), kernelInstance.ServiceContainer(), ServiceTracerProvider)

    tracerName := instance.config.TracerName
    if "" == tracerName {
        tracerName = defaultTracerName
    }

    registrar.Use(applicationcontract.MiddlewareRegistration{
        Middlewares: []httpcontract.Middleware{opentelemetry.NewTracingMiddleware(handle.provider.Tracer(tracerName), instance.config.Propagator)},
    })
}

/* providerHandle adapts the OTLP TracerProvider's Shutdown to CloseWithContext, the one door the container's teardown closes a service through. */
type providerHandle struct {
    provider *sdktrace.TracerProvider
    /* the bound of a close reached with no deadline at all, on whichever door */
    shutdownTimeout time.Duration
}

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
        boundedContext, cancel := context.WithTimeout(closeContext, instance.shutdownTimeout)
        defer cancel()

        return instance.provider.Shutdown(boundedContext)
    }

    return instance.provider.Shutdown(closeContext)
}

var (
    _ applicationcontract.Module               = (*Module)(nil)
    _ applicationcontract.ServiceModule        = (*Module)(nil)
    _ applicationcontract.HttpMiddlewareModule = (*Module)(nil)
)
