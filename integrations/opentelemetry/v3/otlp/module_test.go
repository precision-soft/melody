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

func TestModule_RegisterServicesDoesNotBuildTheExporter(t *testing.T) {
    registrar := &moduleSpyRegistrar{}

    NewModule(ModuleConfig{Config: Config{Endpoint: ""}}).RegisterServices(registrar)

    if 1 != len(registrar.names) {
        t.Fatalf("expected the registration to be recorded without building anything, got %v", registrar.names)
    }
}

func TestProviderHandle_ClosesTheTracerProviderItWraps(t *testing.T) {
    provider := sdktrace.NewTracerProvider()

    handle := &providerHandle{provider: provider}

    if closeErr := handle.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if closeErr := handle.Close(); nil != closeErr {
        t.Fatalf("unexpected error on a second close: %v", closeErr)
    }
}

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

    reserve := processor.shutdownDeadline.Sub(before)
    if reserve > unbudgetedShutdownGrace+time.Second || reserve < unbudgetedShutdownGrace/2 {
        t.Fatalf("the derived reserve was %s, wanted about the package grace %s", reserve, unbudgetedShutdownGrace)
    }
}

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
