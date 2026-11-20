package outbox

import (
    "context"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
)

type twinContextKey struct{}

func TestStoreMustFromRuntime_AnswersTheSiblingsInstanceThroughTheRuntimesScope(t *testing.T) {
    first := &Store{}
    second := &Store{}

    seen := any(nil)

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[*Store](serviceContainer, ServiceStore, func(resolver containercontract.Resolver) (*Store, error) {
        seen = resolver.Runtime().Context().Value(twinContextKey{})

        return first, nil
    }, containercontract.RegisterOptions{})

    runtimeInstance := runtime.NewRequest(context.WithValue(t.Context(), twinContextKey{}, "the caller's"), serviceContainer.NewScope())
    if first != StoreMustFromRuntime(runtimeInstance) || first != StoreMustFromResolver(runtimeInstance.Context(), runtimeInstance.Scope()) {
        t.Fatalf("expected the runtime form to answer the resolver form's instance")
    }

    if "the caller's" != seen {
        t.Fatalf("expected the provider to run under the runtime's context, read %v", seen)
    }

    overridden := serviceContainer.NewScope()
    overridden.MustOverrideProtectedInstance(ServiceStore, second, containercontract.OverrideOptions{})

    if second != StoreMustFromRuntime(runtime.NewRun(t.Context(), overridden)) {
        t.Fatalf("expected the runtime form to resolve through the runtime's scope")
    }
}

func TestStoreMustFromRuntime_PanicsOnANilRuntimeAndAnUnregisteredService(t *testing.T) {
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

            _ = StoreMustFromRuntime(probe.runtime)
        }()
    }
}
