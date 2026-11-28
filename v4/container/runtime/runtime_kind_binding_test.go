package runtime

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

type phaseProbe struct {
    kind containercontract.Kind
}

func TestNewRequestAndNewRun_BindTheirKindSoAProviderEnteringThroughTheScopeReadsIt(t *testing.T) {
    for expectedKind, constructor := range map[containercontract.Kind]func(context.Context, containercontract.Scope) containercontract.Runtime{
        containercontract.KindRequest: NewRequest,
        containercontract.KindRun:     NewRun,
    } {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        container.MustRegister[*phaseProbe](serviceContainer, "app.phase", func(resolver containercontract.Resolver) (*phaseProbe, error) {
            return &phaseProbe{kind: resolver.Runtime().Kind()}, nil
        }, containercontract.RegisterOptions{})

        runtimeInstance := constructor(context.Background(), serviceContainer.NewScope())

        probe := MustFromRuntime[*phaseProbe](runtimeInstance, "app.phase")
        if expectedKind != probe.kind {
            t.Fatalf("expected the provider to read kind %s, got %s", expectedKind, probe.kind)
        }
    }
}

func TestNewRun_OverAScopeANewRequestBoundPanicsNamingBothKinds(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()

    NewRequest(context.Background(), scope)

    var recovered any
    func() {
        defer func() {
            recovered = recover()
        }()

        NewRun(context.Background(), scope)
    }()

    refusal, isError := recovered.(error)
    if false == isError {
        t.Fatalf("expected NewRun over a request scope to panic with an error, got %v", recovered)
    }

    var bindErr *exception.Error
    if false == errors.As(errors.Unwrap(refusal), &bindErr) || "request" != bindErr.Context()["boundKind"] || "run" != bindErr.Context()["kind"] {
        t.Fatalf("expected the refusal to name the bound kind request and the kind run, got %v", refusal)
    }
}

func TestNewRequest_TheSameKindTwiceAndWithContextBindNothingNew(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()

    first := NewRequest(context.Background(), scope)
    NewRequest(context.Background(), scope)

    derived := WithContext(first, context.Background())
    if containercontract.KindRequest != derived.Kind() {
        t.Fatalf("expected WithContext to keep kind request, got %s", derived.Kind())
    }

    testhelper.AssertPanicsWithError(t, func() {
        NewRun(context.Background(), derived.Scope())
    }, "scope cannot serve the runtime")
}

func TestNewRun_AScopeOfAnotherImplementationIsAcceptedUnbound(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    foreign := &nilableScope{Scope: serviceContainer.NewScope()}

    requestRuntime := NewRequest(context.Background(), foreign)
    runRuntime := NewRun(context.Background(), foreign)

    if containercontract.KindRequest != requestRuntime.Kind() || containercontract.KindRun != runRuntime.Kind() {
        t.Fatalf("expected each runtime over a foreign scope to answer its own kind")
    }
}

type cyclingScopedProbe struct{}

func TestFromRuntime_ACycleClosedThroughAProvidersDerivedRuntimeIsRefusedAsCircular(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegisterScoped[*cyclingScopedProbe](serviceContainer, "app.cycle", func(resolver containercontract.Resolver) (*cyclingScopedProbe, error) {
        derivedContext, cancel := context.WithTimeout(resolver.Runtime().Context(), 5*time.Second)
        defer cancel()

        return FromRuntime[*cyclingScopedProbe](WithContext(resolver.Runtime(), derivedContext), "app.cycle")
    }, containercontract.RegisterOptions{})

    runtimeInstance := NewRequest(context.Background(), serviceContainer.NewScope())

    started := time.Now()
    _, resolveErr := FromRuntime[*cyclingScopedProbe](runtimeInstance, "app.cycle")

    if false == errors.Is(resolveErr, container.ErrCircularDependency) {
        t.Fatalf("expected the cycle to be refused as circular, got %v", resolveErr)
    }

    if time.Second < time.Since(started) {
        t.Fatalf("expected the cycle to be refused at once, it took %s", time.Since(started))
    }
}
