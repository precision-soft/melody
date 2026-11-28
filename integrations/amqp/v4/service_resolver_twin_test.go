package amqp

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "testing"

    amqp091 "github.com/rabbitmq/amqp091-go"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

func TestConnectionMustFromRuntime_AnswersTheSiblingsInstanceThroughTheRuntimesScope(t *testing.T) {
    first := NewConnection(&amqp091.Connection{}, ConnectionConfig{})
    second := NewConnection(&amqp091.Connection{}, ConnectionConfig{})

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    registerConnectionService(&containerRegistrar{serviceContainer: serviceContainer}, func(resolver containercontract.Resolver) (*Connection, error) {
        return first, nil
    }, containercontract.RegisterOptions{})

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
        {"unregistered names the service", runtime.NewRequest(t.Context(), container.NewContainer(container.ContainerConfig{}).NewScope()), ServiceConnection},
    } {
        func() {
            defer func() {
                recovered := recover()
                if nil == recovered {
                    t.Fatalf("%s: expected a panic", probe.name)
                }

                if _, isError := recovered.(error); false == isError || false == strings.Contains(twinRecoveredText(recovered), probe.expected) {
                    t.Fatalf("%s: expected a panic naming %q, got %v", probe.name, probe.expected, recovered)
                }
            }()

            _ = ConnectionMustFromRuntime(probe.runtime)
        }()
    }
}

func twinRecoveredText(value any) string {
    builder := strings.Builder{}

    current, isError := value.(error)
    if false == isError {
        return fmt.Sprint(value)
    }

    for nil != current {
        builder.WriteString(current.Error())

        if contextual, ok := current.(interface{ Context() exceptioncontract.Context }); true == ok {
            builder.WriteString(fmt.Sprint(contextual.Context()))
        }

        builder.WriteString("\n")
        current = errors.Unwrap(current)
    }

    return builder.String()
}

type twinContextKey struct{}

func TestConnectionMustFromRuntime_ResolvesUnderTheRuntimesContext(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    var seen any
    connection := NewConnection(&amqp091.Connection{}, ConnectionConfig{})
    container.MustRegister[*Connection](serviceContainer, ServiceConnection, func(resolver containercontract.Resolver) (*Connection, error) {
        seen = resolver.Runtime().Context().Value(twinContextKey{})

        return connection, nil
    }, containercontract.RegisterOptions{TeardownReflectionDisabled: true})

    runtimeInstance := runtime.NewRun(context.WithValue(t.Context(), twinContextKey{}, "run"), serviceContainer.NewScope())
    if connection != ConnectionMustFromRuntime(runtimeInstance) || "run" != seen {
        t.Fatalf("expected the connection resolved under the runtime's context, the provider read %v", seen)
    }
}
