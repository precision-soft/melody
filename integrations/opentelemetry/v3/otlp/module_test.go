package otlp

import (
    "context"
    "strings"
    "testing"
    "time"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type moduleSpyRegistrar struct {
    names []string
}

func (instance *moduleSpyRegistrar) RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.names = append(instance.names, serviceName)
}

func (instance *moduleSpyRegistrar) Register(serviceName string, provider any, options ...containercontract.RegisterOption) error {
    instance.RegisterService(serviceName, provider, options...)

    return nil
}

func (instance *moduleSpyRegistrar) MustRegister(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.RegisterService(serviceName, provider, options...)
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

/* the handle exists only to give the container the Close() error contract it closes services by: without it the batch span processor is never flushed, and the spans of the last seconds before shutdown are lost with the process */
func TestProviderHandle_ClosesTheTracerProviderItWraps(t *testing.T) {
    provider := sdktrace.NewTracerProvider()

    handle := &providerHandle{provider: provider}

    if closeErr := handle.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    /* a second shutdown is a no-op rather than a failure, which is what makes the handle safe under a teardown that runs twice */
    if closeErr := handle.Close(); nil != closeErr {
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

    /* the load-bearing assertion, and it is asked BEFORE the wording of the refusal: the old form refused too — with the vendor's own "context deadline exceeded" — so a probe that reads the message first is killed by every mutant without the latch ever being exercised */
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

/* the container reaches this handle through CloseWithContext, so a teardown that declares no deadline is where the package reserve has to be applied. Applied on Close() alone it was a reserve the teardown never spent, and the flush the reserve exists for ran with no bound of any kind. */
func TestProviderHandle_ACloseWithoutADeadlineDerivesThePackageReserve(t *testing.T) {
    processor := &recordingSpanProcessor{}
    handle := &providerHandle{provider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor))}

    before := time.Now()

    if closeErr := handle.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if false == processor.shutdownBounded {
        t.Fatalf("a close with no deadline handed the span processor a context with none either")
    }

    /* measured from an instant taken BEFORE the call, so the reserve reads a shade longer than the constant; the window only has to separate the package grace from any other figure, and from the absence checked above */
    reserve := processor.shutdownDeadline.Sub(before)
    if reserve > unbudgetedShutdownGrace+time.Second || reserve < unbudgetedShutdownGrace/2 {
        t.Fatalf("the derived reserve was %s, wanted about the package grace %s", reserve, unbudgetedShutdownGrace)
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
