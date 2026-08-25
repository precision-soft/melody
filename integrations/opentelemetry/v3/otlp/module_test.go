package otlp

import (
    "strings"
    "testing"

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

var _ applicationcontract.ServiceRegistrar = (*moduleSpyRegistrar)(nil)
