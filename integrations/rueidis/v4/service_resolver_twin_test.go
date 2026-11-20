package rueidis

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "testing"

    "github.com/redis/rueidis"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

type twinClient struct{ rueidis.Client }

type twinContextKey struct{}

func TestClientMustFromRuntime_AnswersTheSiblingsInstanceThroughTheRuntimesScope(t *testing.T) {
    first := &twinClient{}
    second := &twinClient{}

    seen := any(nil)

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[rueidis.Client](serviceContainer, ServiceClient, func(resolver containercontract.Resolver) (rueidis.Client, error) {
        seen = resolver.Runtime().Context().Value(twinContextKey{})

        return first, nil
    }, containercontract.RegisterOptions{})

    runtimeInstance := runtime.NewRequest(context.WithValue(t.Context(), twinContextKey{}, "the caller's"), serviceContainer.NewScope())
    if first != ClientMustFromRuntime(runtimeInstance) || first != ClientMustFromResolver(runtimeInstance.Context(), runtimeInstance.Scope()) {
        t.Fatalf("expected the runtime form to answer the resolver form's instance")
    }

    if "the caller's" != seen {
        t.Fatalf("expected the provider to run under the runtime's context, read %v", seen)
    }

    overridden := serviceContainer.NewScope()
    overridden.MustOverrideProtectedInstance(ServiceClient, second, containercontract.OverrideOptions{})

    if second != ClientMustFromRuntime(runtime.NewRun(t.Context(), overridden)) {
        t.Fatalf("expected the runtime form to resolve through the runtime's scope")
    }
}

func TestClientMustFromRuntime_PanicsOnANilRuntimeAndAnUnregisteredService(t *testing.T) {
    for _, probe := range []struct {
        name     string
        runtime  containercontract.Runtime
        expected string
    }{
        {"nil runtime", nil, "runtime may not be nil"},
        {"unregistered", runtime.NewRequest(t.Context(), container.NewContainer(container.ContainerConfig{}).NewScope()), "service is not registered"},
        {"unregistered names the service", runtime.NewRequest(t.Context(), container.NewContainer(container.ContainerConfig{}).NewScope()), ServiceClient},
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

            _ = ClientMustFromRuntime(probe.runtime)
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
