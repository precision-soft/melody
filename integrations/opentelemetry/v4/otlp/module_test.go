package otlp

import (
    "context"
    "strings"
    "testing"
    "time"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type moduleSpyRegistrar struct {
    names     []string
    providers []any
}

func (instance *moduleSpyRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.names = append(instance.names, serviceName)
    instance.providers = append(instance.providers, provider)
}

func (instance *moduleSpyRegistrar) Register(serviceName string, provider any, options containercontract.RegisterOptions) error {
    instance.RegisterService(serviceName, provider, options)

    return nil
}

func (instance *moduleSpyRegistrar) MustRegister(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.RegisterService(serviceName, provider, options)
}

/* the service NAME is what RegisterHttpMiddlewares resolves the provider back by, and the two sit in different methods with nothing between them: a drift makes the middleware registration panic at boot on a service it just registered itself */
func TestServiceTracerProvider_IsTheNameTheModuleRegistersUnder(t *testing.T) {
    if "opentelemetry.otlp.tracer_provider" != ServiceTracerProvider {
        t.Fatalf("expected the registered service name, got %q", ServiceTracerProvider)
    }

    registrar := &moduleSpyRegistrar{}
    NewModule(ModuleConfig{}).RegisterServices(registrar)

    if 1 != len(registrar.names) || ServiceTracerProvider != registrar.names[0] {
        t.Fatalf("expected the provider to be registered under %q, got %v", ServiceTracerProvider, registrar.names)
    }
}

func TestModule_NamesAndDescribesItself(t *testing.T) {
    moduleInstance := NewModule(ModuleConfig{})

    if "opentelemetry-otlp" != moduleInstance.Name() {
        t.Fatalf("expected the module name, got %q", moduleInstance.Name())
    }

    if false == strings.Contains(moduleInstance.Description(), "OTLP") {
        t.Fatalf("expected the description to name what it does, got %q", moduleInstance.Description())
    }
}

/* the registered provider is built LAZILY: the module is registered at boot, long before an endpoint is reachable, so the exporter must not be constructed while the registration is being recorded */
func TestModule_RegisterServicesDoesNotBuildTheExporter(t *testing.T) {
    registrar := &moduleSpyRegistrar{}

    NewModule(ModuleConfig{Config: Config{Endpoint: ""}}).RegisterServices(registrar)

    if 1 != len(registrar.names) {
        t.Fatalf("expected the registration to be recorded without building anything, got %v", registrar.names)
    }
}

/* the handle exists only to give the container the CloseWithContext door it closes services by: without it the batch span processor is never flushed, and the spans of the last seconds before shutdown are lost with the process */
func TestProviderHandle_ClosesTheTracerProviderItWraps(t *testing.T) {
    provider := sdktrace.NewTracerProvider()

    handle := &providerHandle{provider: provider}

    if closeErr := handle.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    /* a second shutdown is a no-op rather than a failure, which is what makes the handle safe under a teardown that runs twice */
    if closeErr := handle.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("unexpected error on a second close: %v", closeErr)
    }
}

/* recordingSpanProcessor answers the two questions the probes below ask of the vendor: whether the provider reached its span processors at all, and what deadline it reached them under. It is the only observable — TracerProvider.Shutdown answers nil for a provider it has already latched shut, so its return value cannot tell a flush that happened from one that was skipped. */
type recordingSpanProcessor struct {
    shutdownCalls    int
    shutdownDeadline time.Time
    shutdownBounded  bool
}

func (instance *recordingSpanProcessor) OnStart(parentContext context.Context, span sdktrace.ReadWriteSpan) {
}

func (instance *recordingSpanProcessor) OnEnd(span sdktrace.ReadOnlySpan) {
}

func (instance *recordingSpanProcessor) Shutdown(shutdownContext context.Context) error {
    instance.shutdownCalls = instance.shutdownCalls + 1
    instance.shutdownDeadline, instance.shutdownBounded = shutdownContext.Deadline()

    return nil
}

func (instance *recordingSpanProcessor) ForceFlush(flushContext context.Context) error {
    return nil
}

/* a close reached with the teardown budget already spent must leave the provider CLOSABLE. Driven under a spent context, the vendor latches isShutdown before its loop and returns at the head of the first iteration, so no span processor is shut down and every later Shutdown answers nil on the strength of that latch: the batch goroutine, the ticker and the exporter connection are then beyond every door there is. The assertion that carries this is the POSITIVE one — that the second close reaches the processor — because the refusal alone reads the same whether the provider survived it or not. */
func TestProviderHandle_ASpentDeadlineIsRefusedRatherThanLockingTheProviderShut(t *testing.T) {
    processor := &recordingSpanProcessor{}
    handle := &providerHandle{provider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor))}

    spentContext, cancelSpent := context.WithTimeout(context.Background(), time.Nanosecond)
    defer cancelSpent()

    time.Sleep(5 * time.Millisecond)

    closeErr := handle.CloseWithContext(spentContext)
    if nil == closeErr {
        t.Fatalf("a close reached with a spent deadline reported success")
    }

    if 0 != processor.shutdownCalls {
        t.Fatalf("the span processor was shut down %d times under a spent deadline, wanted none", processor.shutdownCalls)
    }

    /* the load-bearing assertion, asked before the wording of the refusal: a form that let the vendor refuse with its own "context deadline exceeded" would refuse too, so a probe reading the message first could not tell whether the latch was exercised */
    if closeErr := handle.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("the provider was left unclosable by the refused close: %v", closeErr)
    }

    if 1 != processor.shutdownCalls {
        t.Fatalf("the second close reached the span processor %d times, wanted once: the refused close had locked the provider shut", processor.shutdownCalls)
    }

    if false == strings.Contains(closeErr.Error(), "was not shut down") {
        t.Fatalf("expected the refusal to say the provider was not shut down, got %q", closeErr.Error())
    }
}

/* a caller that declares a deadline keeps it whole: the reserve stands in for a budget nobody declared and must not shorten or lengthen one that was. */
func TestProviderHandle_ADeclaredDeadlineIsHandedOnUntouched(t *testing.T) {
    processor := &recordingSpanProcessor{}
    handle := &providerHandle{provider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor))}

    declaredContext, cancelDeclared := context.WithTimeout(context.Background(), 90*time.Millisecond)
    defer cancelDeclared()

    declaredDeadline, _ := declaredContext.Deadline()

    if closeErr := handle.CloseWithContext(declaredContext); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if false == processor.shutdownDeadline.Equal(declaredDeadline) {
        t.Fatalf("the declared deadline %s reached the span processor as %s", declaredDeadline, processor.shutdownDeadline)
    }
}

var _ sdktrace.SpanProcessor = (*recordingSpanProcessor)(nil)

var _ applicationcontract.ServiceRegistrar = (*moduleSpyRegistrar)(nil)

type otlpSpyKernel struct {
    kernelcontract.Kernel
    serviceContainer containercontract.Container
    bootRuntime      containercontract.Runtime
}

func (instance *otlpSpyKernel) BootRuntime() containercontract.Runtime {
    if nil == instance.bootRuntime {
        instance.bootRuntime, _ = runtime.NewBoot(context.Background(), instance.serviceContainer)
    }

    return instance.bootRuntime
}

func (instance *otlpSpyKernel) ServiceContainer() containercontract.Container {
    return instance.serviceContainer
}

type otlpSpyMiddlewareRegistrar struct {
    registrations []applicationcontract.MiddlewareRegistration
}

func (instance *otlpSpyMiddlewareRegistrar) Use(registration applicationcontract.MiddlewareRegistration) {
    instance.registrations = append(instance.registrations, registration)
}

func TestModule_RegisterHttpMiddlewaresInstallsTheTracingMiddleware(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        ServiceTracerProvider,
        func(resolver containercontract.Resolver) (*providerHandle, error) {
            return &providerHandle{provider: sdktrace.NewTracerProvider()}, nil
        },
        containercontract.RegisterOptions{},
    )

    registrar := &otlpSpyMiddlewareRegistrar{}
    NewModule(ModuleConfig{}).RegisterHttpMiddlewares(&otlpSpyKernel{serviceContainer: serviceContainer}, registrar)

    if 1 != len(registrar.registrations) || 1 != len(registrar.registrations[0].Middlewares) || nil == registrar.registrations[0].Middlewares[0] {
        t.Fatalf("expected one registration carrying the tracing middleware, got %+v", registrar.registrations)
    }

    if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }
}


func TestProviderHandle_ACloseWithoutADeadlineHandsShutdownNoDeadline(t *testing.T) {
    processor := &recordingSpanProcessor{}
    handle := &providerHandle{provider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor))}

    if closeErr := handle.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if true == processor.shutdownBounded {
        t.Fatalf("expected a close with no deadline to hand the span processor none, got one at %s", processor.shutdownDeadline)
    }
}

func (instance *moduleSpyRegistrar) ReservePhaseService(serviceName string, kind containercontract.Kind) error {
    return nil
}

func TestModule_RegisterHttpMiddlewaresBuildsTheProviderUnderTheBootRuntime(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    seenKind := containercontract.KindUnspecified
    serviceContainer.MustRegister(
        ServiceTracerProvider,
        func(resolver containercontract.Resolver) (*providerHandle, error) {
            seenKind = resolver.Runtime().Kind()

            return &providerHandle{provider: sdktrace.NewTracerProvider()}, nil
        },
        containercontract.RegisterOptions{},
    )

    NewModule(ModuleConfig{}).RegisterHttpMiddlewares(&otlpSpyKernel{serviceContainer: serviceContainer}, &otlpSpyMiddlewareRegistrar{})

    if containercontract.KindBoot != seenKind {
        t.Fatalf("expected the tracer provider built under the boot runtime, got %s", seenKind)
    }

    _ = serviceContainer.CloseWithContext(context.Background())
}

func TestModule_TheModulesProviderBuildsUnderTheRuntimeItIsResolvedThrough(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    registrar := &containerServiceRegistrar{Container: serviceContainer}
    NewModule(ModuleConfig{Config: Config{Endpoint: "collector:4317"}}).RegisterServices(registrar)

    doneContext, cancel := context.WithCancel(context.Background())
    cancel()

    bootRuntime, finish := runtime.NewBoot(doneContext, serviceContainer)
    defer func() {
        _ = finish()
    }()

    _, resolveErr := bootRuntime.Scope().Get(bootRuntime.Context(), ServiceTracerProvider)
    if nil == resolveErr || false == strings.Contains(strings.Join(exception.BuildCauseChain(resolveErr, 8), " "), "before the otlp exporter is dialled") {
        t.Fatalf("expected the module's provider to dial under the resolving runtime and refuse its done context, got %v", resolveErr)
    }
}

/* containerServiceRegistrar hands a module's registrations to a real container */
type containerServiceRegistrar struct {
    containercontract.Container
}

func (instance *containerServiceRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.Container.MustRegister(serviceName, provider, options)
}
