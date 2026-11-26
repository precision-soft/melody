package container

import (
    "context"
    "errors"
    "reflect"
    "strings"
    "sync"
    "testing"
    "time"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/internal/runtimeseam"
    "github.com/precision-soft/melody/v4/exception"
)

type runtimeKindProbe struct {
    kind containercontract.Kind
}

type innerRuntimeKindProbe struct {
    kind containercontract.Kind
}

type lateRuntimeKindProbe struct {
    kind containercontract.Kind
}

type unboundRuntimeProbe struct{}

type runtimeContextKey struct{}

type scopedRuntimeProbe struct {
    value string
}

func boundScope(t *testing.T, serviceContainer containercontract.Container, kind containercontract.Kind) containercontract.Scope {
    t.Helper()

    scopeInstance := serviceContainer.NewScope()
    t.Cleanup(func() {
        _ = scopeInstance.CloseWithContext(context.Background())
    })

    if bindErr := scopeInstance.(runtimeseam.KindBinder).BindRuntimeKind(runtimeseam.Token{}, kind); nil != bindErr {
        t.Fatalf("unexpected bind error: %v", bindErr)
    }

    return scopeInstance
}

func TestRuntime_AContainerServiceFirstResolvedThroughABoundScopeReadsItsKindTheWaitingContextAndASuspendedScope(t *testing.T) {
    for _, kind := range []containercontract.Kind{containercontract.KindRequest, containercontract.KindRun} {
        serviceContainer := NewContainer(ContainerConfig{})

        MustRegisterScoped[*scopedRuntimeProbe](serviceContainer, "app.scoped.only", func(resolver containercontract.Resolver) (*scopedRuntimeProbe, error) {
            return &scopedRuntimeProbe{value: "scoped"}, nil
        }, RegisterOptions{})

        seen := make(chan containercontract.Runtime, 1)
        refusals := make(chan []error, 1)
        MustRegister[*testService](serviceContainer, "app.singleton", func(resolver containercontract.Resolver) (*testService, error) {
            runtimeInstance := resolver.Runtime()
            seen <- runtimeInstance

            suspended := runtimeInstance.Scope()
            _, scopedErr := suspended.Get(runtimeInstance.Context(), "app.scoped.only")
            registerErr := suspended.RegisterScoped("app.late.scoped", func(resolver containercontract.Resolver) (*scopedRuntimeProbe, error) {
                return &scopedRuntimeProbe{}, nil
            }, RegisterOptions{})
            overrideErr := suspended.OverrideInstance("app.scoped.only", &testService{}, containercontract.OverrideOptions{})
            protectedErr := suspended.OverrideProtectedInstance("app.scoped.only", &testService{}, containercontract.OverrideOptions{})
            refusals <- []error{scopedErr, registerErr, overrideErr, protectedErr, suspended.CloseWithContext(context.Background()), suspended.CloseWithContext(context.Background())}

            return &testService{Value: "singleton"}, nil
        }, RegisterOptions{})

        scopeInstance := boundScope(t, serviceContainer, kind)
        waitingContext := context.WithValue(context.Background(), runtimeContextKey{}, "waiting")

        if _, getErr := scopeInstance.Get(waitingContext, "app.singleton"); nil != getErr {
            t.Fatalf("unexpected error: %v", getErr)
        }

        runtimeInstance := <-seen
        if kind != runtimeInstance.Kind() {
            t.Fatalf("expected kind %s, got %s", kind, runtimeInstance.Kind())
        }

        if waitingContext != runtimeInstance.Context() {
            t.Fatalf("expected the context the starting call waits under")
        }

        if _, isSuspended := runtimeInstance.Scope().(*suspendedScope); false == isSuspended {
            t.Fatalf("expected a container-owned provider to read a suspended scope, got %T", runtimeInstance.Scope())
        }

        answers := <-refusals
        if nil == answers[0] || false == strings.Contains(answers[0].Error(), "not registered") {
            t.Fatalf("expected the suspended scope to refuse a scope-only service, got %v", answers[0])
        }

        for index, door := range []string{"RegisterScoped", "OverrideInstance", "OverrideProtectedInstance"} {
            refusal := answers[index+1]
            melodyErr, isMelody := refusal.(*exception.Error)
            if false == isMelody || door != melodyErr.Context()["door"] {
                t.Fatalf("expected %s to be refused naming the door, got %v", door, refusal)
            }
        }

        if nil != answers[4] || nil != answers[5] {
            t.Fatalf("expected closing the suspended scope to answer nil, got %v and %v", answers[4], answers[5])
        }

        if _, scopedErr := scopeInstance.Get(context.Background(), "app.scoped.only"); nil != scopedErr {
            t.Fatalf("expected closing the suspended scope to leave the real scope open, got %v", scopedErr)
        }
    }
}

func TestRuntime_AScopedProviderReadsAScopeThatHandsItsChangesToTheRealScope(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    seen := make(chan containercontract.Runtime, 1)
    MustRegisterScoped[*scopedRuntimeProbe](serviceContainer, "app.scoped", func(resolver containercontract.Resolver) (*scopedRuntimeProbe, error) {
        runtimeInstance := resolver.Runtime()
        seen <- runtimeInstance

        if overrideErr := runtimeInstance.Scope().OverrideInstance("app.scoped.override", &testService{Value: "installed"}, containercontract.OverrideOptions{}); nil != overrideErr {
            return nil, overrideErr
        }

        return &scopedRuntimeProbe{value: "scoped"}, nil
    }, RegisterOptions{})

    scopeInstance := boundScope(t, serviceContainer, containercontract.KindRun)

    if _, getErr := scopeInstance.Get(context.Background(), "app.scoped"); nil != getErr {
        t.Fatalf("unexpected error: %v", getErr)
    }

    runtimeInstance := <-seen
    if _, isProviderScope := runtimeInstance.Scope().(*providerScope); false == isProviderScope {
        t.Fatalf("expected the scoped provider to read a provider scope, got %T", runtimeInstance.Scope())
    }

    if containercontract.KindRun != runtimeInstance.Kind() {
        t.Fatalf("expected kind run, got %s", runtimeInstance.Kind())
    }

    installed, getErr := scopeInstance.Get(context.Background(), "app.scoped.override")
    if nil != getErr || "installed" != installed.(*testService).Value {
        t.Fatalf("expected the override made through the provider's runtime to reach the real scope, got %v, %v", installed, getErr)
    }

    if closeErr := runtimeInstance.Scope().CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if false == scopeInstance.(*scope).Closed() {
        t.Fatalf("expected closing the provider's runtime scope to close the real scope")
    }
}

func TestRuntime_ACycleClosedThroughAProvidersRuntimeScopeIsRefusedAsCircular(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegisterScoped[*scopedRuntimeProbe](serviceContainer, "app.scoped.cycle", func(resolver containercontract.Resolver) (*scopedRuntimeProbe, error) {
        runtimeInstance := resolver.Runtime()
        _, cycleErr := runtimeInstance.Scope().Get(runtimeInstance.Context(), "app.scoped.cycle")

        return nil, cycleErr
    }, RegisterOptions{})
    MustRegister[*testService](serviceContainer, "app.container.cycle", func(resolver containercontract.Resolver) (*testService, error) {
        runtimeInstance := resolver.Runtime()
        _, cycleErr := runtimeInstance.Scope().Get(runtimeInstance.Context(), "app.container.cycle")

        return nil, cycleErr
    }, RegisterOptions{})

    scopeInstance := boundScope(t, serviceContainer, containercontract.KindRequest)

    for _, name := range []string{"app.scoped.cycle", "app.container.cycle"} {
        deadlineContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)

        started := time.Now()
        _, getErr := scopeInstance.Get(deadlineContext, name)
        cancel()

        if false == errors.Is(getErr, ErrCircularDependency) {
            t.Fatalf("expected %s to be refused as circular, got %v", name, getErr)
        }

        if time.Second < time.Since(started) {
            t.Fatalf("expected %s to be refused at once, it took %s", name, time.Since(started))
        }
    }
}

func TestRuntime_ADeadlineHandedToGetIsTheDeadlineOfTheProvidersRuntimeContext(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    seen := make(chan context.Context, 1)
    MustRegister[*testService](serviceContainer, "app.deadline", func(resolver containercontract.Resolver) (*testService, error) {
        seen <- resolver.Runtime().Context()

        return &testService{}, nil
    }, RegisterOptions{})

    scopeInstance := boundScope(t, serviceContainer, containercontract.KindRequest)

    deadlineContext, cancel := context.WithTimeout(context.Background(), time.Hour)
    defer cancel()

    if _, getErr := scopeInstance.Get(deadlineContext, "app.deadline"); nil != getErr {
        t.Fatalf("unexpected error: %v", getErr)
    }

    expectedDeadline, _ := deadlineContext.Deadline()
    seenDeadline, hasDeadline := (<-seen).Deadline()
    if false == hasDeadline || false == expectedDeadline.Equal(seenDeadline) {
        t.Fatalf("expected the provider's runtime to carry the deadline handed to Get")
    }
}

func TestRuntime_ADependencyCreatedInsideAContainerOwnedProviderReadsTheSameKind(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister[*innerRuntimeKindProbe](serviceContainer, "app.inner", func(resolver containercontract.Resolver) (*innerRuntimeKindProbe, error) {
        return &innerRuntimeKindProbe{kind: resolver.Runtime().Kind()}, nil
    }, RegisterOptions{})

    MustRegister[*runtimeKindProbe](serviceContainer, "app.outer", func(resolver containercontract.Resolver) (*runtimeKindProbe, error) {
        inner := MustFromResolver[*innerRuntimeKindProbe](resolver.Runtime().Context(), resolver, "app.inner")

        return &runtimeKindProbe{kind: inner.kind}, nil
    }, RegisterOptions{})

    scopeInstance := boundScope(t, serviceContainer, containercontract.KindRequest)

    inner := MustFromResolver[*runtimeKindProbe](context.Background(), scopeInstance, "app.outer")
    if containercontract.KindRequest != inner.kind {
        t.Fatalf("expected the nested provider to read kind request, got %s", inner.kind)
    }
}

func TestRuntime_TheContainerDoorAndAnUnboundScopeReadADetachedRuntimeOverTheContextGiven(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    seen := make(chan containercontract.Runtime, 2)
    MustRegister[*testService](serviceContainer, "app.through.container", func(resolver containercontract.Resolver) (*testService, error) {
        seen <- resolver.Runtime()

        return &testService{}, nil
    }, RegisterOptions{})
    MustRegister[*unboundRuntimeProbe](serviceContainer, "app.through.unbound", func(resolver containercontract.Resolver) (*unboundRuntimeProbe, error) {
        seen <- resolver.Runtime()

        return &unboundRuntimeProbe{}, nil
    }, RegisterOptions{})

    givenContext := context.WithValue(context.Background(), runtimeContextKey{}, "given")

    boundScope(t, serviceContainer, containercontract.KindRequest)

    if _, getErr := serviceContainer.Get(givenContext, "app.through.container"); nil != getErr {
        t.Fatalf("unexpected error: %v", getErr)
    }

    unbound := serviceContainer.NewScope()
    defer func() {
        _ = unbound.CloseWithContext(context.Background())
    }()

    if _, getErr := unbound.Get(givenContext, "app.through.unbound"); nil != getErr {
        t.Fatalf("unexpected error: %v", getErr)
    }

    for _, door := range []string{"container", "unbound scope"} {
        runtimeInstance := <-seen
        if containercontract.KindDetached != runtimeInstance.Kind() {
            t.Fatalf("expected the %s door to read a detached runtime, got %s", door, runtimeInstance.Kind())
        }

        if givenContext != runtimeInstance.Context() {
            t.Fatalf("expected the %s door's runtime over the context given", door)
        }
    }
}

func TestRuntime_AGoroutineViewAndItsSuspendedScopeAnswerADetachedRuntime(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    seen := make(chan [2]containercontract.Runtime, 1)
    MustRegister[*testService](serviceContainer, "app.goroutine", func(resolver containercontract.Resolver) (*testService, error) {
        goroutineView := ForGoroutine(resolver)
        seen <- [2]containercontract.Runtime{goroutineView.Runtime(), resolver.Runtime().Scope().Runtime()}

        return &testService{}, nil
    }, RegisterOptions{})

    scopeInstance := boundScope(t, serviceContainer, containercontract.KindRequest)

    if _, getErr := scopeInstance.Get(context.WithValue(context.Background(), runtimeContextKey{}, "request"), "app.goroutine"); nil != getErr {
        t.Fatalf("unexpected error: %v", getErr)
    }

    answers := <-seen

    goroutineRuntime := answers[0]
    if nil == goroutineRuntime || containercontract.KindDetached != goroutineRuntime.Kind() || context.Background() != goroutineRuntime.Context() {
        t.Fatalf("expected a goroutine view to answer a detached runtime over context.Background()")
    }

    if _, isSuspended := goroutineRuntime.Scope().(*suspendedScope); false == isSuspended {
        t.Fatalf("expected a goroutine view's runtime to answer a suspended scope")
    }

    if containercontract.KindRequest != answers[1].Kind() {
        t.Fatalf("expected the suspended scope to answer its view's runtime while the provider runs, got %s", answers[1].Kind())
    }
}

func TestRuntime_ACreationStartedThroughARetainedViewReadsTheScopesKindOnlyWhileTheViewSeesTheScope(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister[*runtimeKindProbe](serviceContainer, "app.late.from.scoped", func(resolver containercontract.Resolver) (*runtimeKindProbe, error) {
        return &runtimeKindProbe{kind: resolver.Runtime().Kind()}, nil
    }, RegisterOptions{})
    MustRegister[*lateRuntimeKindProbe](serviceContainer, "app.late.from.singleton", func(resolver containercontract.Resolver) (*lateRuntimeKindProbe, error) {
        return &lateRuntimeKindProbe{kind: resolver.Runtime().Kind()}, nil
    }, RegisterOptions{})

    retained := make(chan containercontract.Resolver, 2)
    MustRegisterScoped[*scopedRuntimeProbe](serviceContainer, "app.scoped.holder", func(resolver containercontract.Resolver) (*scopedRuntimeProbe, error) {
        retained <- resolver

        return &scopedRuntimeProbe{}, nil
    }, RegisterOptions{})
    MustRegister[*testService](serviceContainer, "app.singleton.holder", func(resolver containercontract.Resolver) (*testService, error) {
        retained <- resolver

        return &testService{}, nil
    }, RegisterOptions{})

    scopeInstance := boundScope(t, serviceContainer, containercontract.KindRequest)

    for _, name := range []string{"app.scoped.holder", "app.singleton.holder"} {
        if _, getErr := scopeInstance.Get(context.Background(), name); nil != getErr {
            t.Fatalf("unexpected error: %v", getErr)
        }
    }

    scopedView := <-retained
    singletonView := <-retained

    fromScoped := MustFromResolver[*runtimeKindProbe](context.Background(), scopedView, "app.late.from.scoped")
    if containercontract.KindRequest != fromScoped.kind {
        t.Fatalf("expected a creation through a scoped service's retained view to read kind request, got %s", fromScoped.kind)
    }

    fromSingleton := MustFromResolver[*lateRuntimeKindProbe](context.Background(), singletonView, "app.late.from.singleton")
    if containercontract.KindDetached != fromSingleton.kind {
        t.Fatalf("expected a creation through a container-owned service's retained view to be detached, got %s", fromSingleton.kind)
    }
}

func TestBindRuntimeKind_TheFirstKindWinsTheSameKindAgainIsAdmittedAndAnotherIsRefusedNamingBoth(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    scopeInstance := serviceContainer.NewScope()
    defer func() {
        _ = scopeInstance.CloseWithContext(context.Background())
    }()

    binder := scopeInstance.(runtimeseam.KindBinder)

    if bindErr := binder.BindRuntimeKind(runtimeseam.Token{}, containercontract.KindRequest); nil != bindErr {
        t.Fatalf("unexpected error on the first bind: %v", bindErr)
    }

    if bindErr := binder.BindRuntimeKind(runtimeseam.Token{}, containercontract.KindRequest); nil != bindErr {
        t.Fatalf("expected the same kind again to be admitted, got %v", bindErr)
    }

    bindErr := binder.BindRuntimeKind(runtimeseam.Token{}, containercontract.KindRun)
    melodyErr, isMelody := bindErr.(*exception.Error)
    if false == isMelody {
        t.Fatalf("expected another kind to be refused, got %v", bindErr)
    }

    if "request" != melodyErr.Context()["boundKind"] || "run" != melodyErr.Context()["kind"] {
        t.Fatalf("expected the refusal to name both kinds, got %v", melodyErr.Context())
    }

    if containercontract.KindRequest != scopeInstance.(*scope).boundRuntimeKind() {
        t.Fatalf("expected the first kind to stay bound")
    }
}

func TestRuntime_TwoScopesRacingTheFirstCreationOfOneSingletonHandItOneContextAndKindPair(t *testing.T) {
    for attempt := 0; attempt < 50; attempt++ {
        serviceContainer := NewContainer(ContainerConfig{})

        seen := make(chan containercontract.Runtime, 2)
        MustRegister[*testService](serviceContainer, "app.raced", func(resolver containercontract.Resolver) (*testService, error) {
            seen <- resolver.Runtime()

            return &testService{}, nil
        }, RegisterOptions{})

        kinds := map[string]containercontract.Kind{"request": containercontract.KindRequest, "run": containercontract.KindRun}

        var group sync.WaitGroup
        for label, kind := range kinds {
            scopeInstance := boundScope(t, serviceContainer, kind)
            raceContext := context.WithValue(context.Background(), runtimeContextKey{}, label)

            group.Add(1)
            go func() {
                defer group.Done()

                if _, getErr := scopeInstance.Get(raceContext, "app.raced"); nil != getErr {
                    t.Errorf("unexpected error: %v", getErr)
                }
            }()
        }

        group.Wait()

        if 1 != len(seen) {
            t.Fatalf("expected one provider run, got %d", len(seen))
        }

        runtimeInstance := <-seen
        label, _ := runtimeInstance.Context().Value(runtimeContextKey{}).(string)
        if kinds[label] != runtimeInstance.Kind() {
            t.Fatalf("expected one caller's context and kind, got context %q with kind %s", label, runtimeInstance.Kind())
        }
    }
}

type goroutineKindProbe struct {
    kind containercontract.Kind
}

type scopedGoroutineKindProbe struct {
    kind containercontract.Kind
}

func TestRuntime_ACreationStartedThroughAGoroutineViewReadsTheScopesKindOnlyWhileTheViewSeesTheScope(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister[*goroutineKindProbe](serviceContainer, "app.goroutine.from.singleton", func(resolver containercontract.Resolver) (*goroutineKindProbe, error) {
        return &goroutineKindProbe{kind: resolver.Runtime().Kind()}, nil
    }, RegisterOptions{})
    MustRegister[*scopedGoroutineKindProbe](serviceContainer, "app.goroutine.from.scoped", func(resolver containercontract.Resolver) (*scopedGoroutineKindProbe, error) {
        return &scopedGoroutineKindProbe{kind: resolver.Runtime().Kind()}, nil
    }, RegisterOptions{})

    views := make(chan containercontract.Resolver, 2)
    MustRegister[*testService](serviceContainer, "app.goroutine.singleton.holder", func(resolver containercontract.Resolver) (*testService, error) {
        views <- ForGoroutine(resolver)

        return &testService{}, nil
    }, RegisterOptions{})
    MustRegisterScoped[*scopedRuntimeProbe](serviceContainer, "app.goroutine.scoped.holder", func(resolver containercontract.Resolver) (*scopedRuntimeProbe, error) {
        views <- ForGoroutine(resolver)

        return &scopedRuntimeProbe{}, nil
    }, RegisterOptions{})

    scopeInstance := boundScope(t, serviceContainer, containercontract.KindRequest)

    for _, name := range []string{"app.goroutine.singleton.holder", "app.goroutine.scoped.holder"} {
        if _, getErr := scopeInstance.Get(context.Background(), name); nil != getErr {
            t.Fatalf("unexpected error: %v", getErr)
        }
    }

    singletonView := <-views
    scopedView := <-views

    fromSingleton := MustFromResolver[*goroutineKindProbe](context.Background(), singletonView, "app.goroutine.from.singleton")
    if containercontract.KindDetached != fromSingleton.kind {
        t.Fatalf("expected a creation through a container-owned provider's goroutine view to be detached, got %s", fromSingleton.kind)
    }

    fromScoped := MustFromResolver[*scopedGoroutineKindProbe](context.Background(), scopedView, "app.goroutine.from.scoped")
    if containercontract.KindRequest != fromScoped.kind {
        t.Fatalf("expected a creation through a scoped provider's goroutine view to read kind request, got %s", fromScoped.kind)
    }
}

type forwardedCapabilityProbe struct{}

func (instance *forwardedCapabilityProbe) Greet() string {
    return "forwarded"
}

func TestRuntime_AProvidersRuntimeScopeForwardsTheCapabilitiesOfItsResolver(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    type answers struct {
        goroutineView      containercontract.Resolver
        runtimeScope       containercontract.Scope
        readsContainer     bool
        resolvingReference bool
    }

    seen := make(chan answers, 2)
    MustRegisterType[*forwardedCapabilityProbe](serviceContainer, func(resolver containercontract.Resolver) (*forwardedCapabilityProbe, error) {
        runtimeScope := resolver.Runtime().Scope()
        reference := containercontract.ServiceReference{ServiceName: defaultServiceNameForType(reflect.TypeOf((*forwardedCapabilityProbe)(nil))), ServiceType: reflect.TypeOf((*forwardedCapabilityProbe)(nil))}

        _ = Lazy[*testService](runtimeScope, "app.forwarded.missing")

        seen <- answers{
            goroutineView:      ForGoroutine(runtimeScope),
            runtimeScope:       runtimeScope,
            readsContainer:     runtimeScope.(containerReadingSource).readsContainer(),
            resolvingReference: runtimeScope.(referenceResolutionChecker).isResolvingReference(reference),
        }

        return &forwardedCapabilityProbe{}, nil
    }, RegisterOptions{})

    MustRegisterScoped[*scopedRuntimeProbe](serviceContainer, "app.forwarded.scoped", func(resolver containercontract.Resolver) (*scopedRuntimeProbe, error) {
        runtimeScope := resolver.Runtime().Scope()
        seen <- answers{
            goroutineView:  ForGoroutine(runtimeScope),
            runtimeScope:   runtimeScope,
            readsContainer: runtimeScope.(containerReadingSource).readsContainer(),
        }

        return &scopedRuntimeProbe{}, nil
    }, RegisterOptions{})

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.GetByType(context.Background(), reflect.TypeOf((*forwardedCapabilityProbe)(nil))); nil != getErr {
        t.Fatalf("unexpected error: %v", getErr)
    }

    if _, getErr := scopeInstance.Get(context.Background(), "app.forwarded.scoped"); nil != getErr {
        t.Fatalf("unexpected error: %v", getErr)
    }

    containerOwned := <-seen
    scoped := <-seen

    if containerOwned.goroutineView == containercontract.Resolver(containerOwned.runtimeScope) {
        t.Fatalf("expected ForGoroutine over a runtime scope to answer the resolver's goroutine view, not the scope itself")
    }

    if _, isView := containerOwned.goroutineView.(*resolverContext); false == isView {
        t.Fatalf("expected the goroutine view of the resolver behind, got %T", containerOwned.goroutineView)
    }

    if false == containerOwned.readsContainer || true == scoped.readsContainer {
        t.Fatalf("expected the container-owned provider's runtime scope to read the container and the scoped one's the scope")
    }

    if false == containerOwned.resolvingReference {
        t.Fatalf("expected the runtime scope to report the reference its provider is resolving")
    }

    if verifyErr := serviceContainer.(*container).VerifyLazyNames(); nil == verifyErr {
        t.Fatalf("expected a lazy handle built over a container-owned provider's runtime scope to be recorded and refused")
    }

    if true == scoped.runtimeScope.(interface{ Closed() bool }).Closed() || true == scoped.runtimeScope.(closedScopeChecker).isScopeClosed() {
        t.Fatalf("expected the scoped provider's runtime scope to report an open scope")
    }

    _ = scopeInstance.CloseWithContext(context.Background())

    if false == scoped.runtimeScope.(interface{ Closed() bool }).Closed() || false == scoped.runtimeScope.(closedScopeChecker).isScopeClosed() {
        t.Fatalf("expected the scoped provider's runtime scope to report its scope closed")
    }
}

func TestBindRuntimeKind_AProvidersRuntimeScopeAdmitsOnlyTheKindItsCreationsRead(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    if bindErr := serviceContainer.Runtime().Scope().(runtimeseam.KindBinder).BindRuntimeKind(runtimeseam.Token{}, containercontract.KindRequest); nil == bindErr {
        t.Fatalf("expected the container's suspended scope to refuse a request kind")
    }

    if bindErr := serviceContainer.Runtime().Scope().(runtimeseam.KindBinder).BindRuntimeKind(runtimeseam.Token{}, containercontract.KindDetached); nil != bindErr {
        t.Fatalf("expected the container's suspended scope to admit the detached kind, got %v", bindErr)
    }

    bindErrs := make(chan [2]error, 1)
    MustRegisterScoped[*scopedRuntimeProbe](serviceContainer, "app.bind.scoped", func(resolver containercontract.Resolver) (*scopedRuntimeProbe, error) {
        binder := resolver.Runtime().Scope().(runtimeseam.KindBinder)
        bindErrs <- [2]error{binder.BindRuntimeKind(runtimeseam.Token{}, containercontract.KindRun), binder.BindRuntimeKind(runtimeseam.Token{}, containercontract.KindRequest)}

        return &scopedRuntimeProbe{}, nil
    }, RegisterOptions{})

    scopeInstance := boundScope(t, serviceContainer, containercontract.KindRun)
    if _, getErr := scopeInstance.Get(context.Background(), "app.bind.scoped"); nil != getErr {
        t.Fatalf("unexpected error: %v", getErr)
    }

    answers := <-bindErrs
    if nil != answers[0] || nil == answers[1] {
        t.Fatalf("expected a scoped provider's runtime scope to admit its scope's kind and refuse another, got %v and %v", answers[0], answers[1])
    }
}
