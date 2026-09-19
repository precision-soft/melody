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

/* providerHandle adapts the OTLP TracerProvider's Shutdown to the two closing doors the container knows: it prefers CloseWithContext on any service that carries one and falls back to Close() error, so this handle offers both and the teardown takes the first. */
type providerHandle struct {
    provider *sdktrace.TracerProvider
}

/* unbudgetedShutdownGrace bounds the shutdown of a handle closed with no deadline at all, where nobody said how long the flush of the pending spans may take. It is the fallback, not the figure: a caller that declares a budget is honoured whole, and an operator who needs the export finished raises the teardown budget rather than this. It is applied on whichever door is reached without a deadline, not on the plain one alone — the container calls CloseWithContext, so a reserve that lived on Close() was a reserve the teardown never spent. */
const unbudgetedShutdownGrace = 5 * time.Second

/* CloseWithContext hands the teardown's own deadline to the provider's Shutdown, which has taken a context since it was written — erasing that context is the only thing this handle ever did, and it did it because the container's teardown used to ask for Close() error and nothing else.

   A context that is already spent is NOT handed on, and that is the whole of this door's own judgement. Measured in the vendor (go.opentelemetry.io/otel/sdk trace/provider.go): Shutdown latches isShutdown through a compare-and-swap BEFORE its loop, the loop then reads ctx.Done() at the head of its first iteration and returns, the list of span processors is never emptied, and every later Shutdown answers nil on the strength of that latch. So an expired deadline does not merely drop the spans it could not export — it leaves the provider permanently unclosable, with its batch goroutine, its ticker and its exporter connection still running and no door left that can end them. Refusing to make the call keeps the provider closable by whatever comes next, and the container's failure map names this service either way, which is what an operator reads.

   What this does not close is the deadline that is nearly spent: a remainder too small for one processor to shut down inside still latches, because bounding that would mean spending time the caller's budget no longer has. The figure that would buy is a decision about every component of the teardown at once, not about this one. */
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
