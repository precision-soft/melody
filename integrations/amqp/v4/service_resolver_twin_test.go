package amqp

import (
    "strings"
    "testing"

    amqp091 "github.com/rabbitmq/amqp091-go"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
)

func TestConnectionMustFromRuntime_AnswersTheSiblingsInstanceThroughTheRuntimesScope(t *testing.T) {
    first := &amqp091.Connection{}
    second := &amqp091.Connection{}

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    RegisterConnectionService(&containerRegistrar{serviceContainer: serviceContainer}, first, ConnectionServiceConfig{})

    runtimeInstance := runtime.NewRequest(t.Context(), serviceContainer.NewScope())
    if first != ConnectionMustFromRuntime(runtimeInstance) || first != ConnectionMustFromResolver(runtimeInstance.Context(), runtimeInstance.Scope()) {
        t.Fatalf("expected the runtime form to answer the resolver form's instance")
    }

    overridden := serviceContainer.NewScope()
    overridden.MustOverrideProtectedInstance(ServiceConnection, second, containercontract.OverrideOptions{})

    if second != ConnectionMustFromRuntime(runtime.NewRun(t.Context(), overridden)) {
        t.Fatalf("expected the runtime form to resolve through the runtime's scope")
    }
}

func TestConnectionMustFromRuntime_PanicsOnANilRuntimeAndAnUnregisteredService(t *testing.T) {
    for _, probe := range []struct {
        name     string
        runtime  containercontract.Runtime
        expected string
    }{
        {"nil runtime", nil, "runtime may not be nil"},
        {"unregistered", runtime.NewRequest(t.Context(), container.NewContainer(container.ContainerConfig{}).NewScope()), "service is not registered"},
    } {
        func() {
            defer func() {
                recovered := recover()
                if nil == recovered {
                    t.Fatalf("%s: expected a panic", probe.name)
                }

                if recoveredErr, isError := recovered.(error); false == isError || false == strings.Contains(recoveredErr.Error(), probe.expected) {
                    t.Fatalf("%s: expected a panic naming %q, got %v", probe.name, probe.expected, recovered)
                }
            }()

            _ = ConnectionMustFromRuntime(probe.runtime)
        }()
    }
}
