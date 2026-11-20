package runtime_test

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
)

type lazyRuntimeKey struct{}

type lazyRuntimeDependency string

type lazyRuntimeHolder struct {
    handle *container.LazyService[*lazyRuntimeCreated]
}

type lazyRuntimeCreated struct {
    kind    containercontract.Kind
    value   any
    scope   containercontract.Scope
    created int64
}

/* a hand-written runtime, so a test can hand a kind or a scope no constructor would bind */
type lazyHandRuntime struct {
    ctx   context.Context
    scope containercontract.Scope
    kind  containercontract.Kind
}

func (instance *lazyHandRuntime) Context() context.Context       { return instance.ctx }
func (instance *lazyHandRuntime) Scope() containercontract.Scope { return instance.scope }
func (instance *lazyHandRuntime) Kind() containercontract.Kind   { return instance.kind }

const (
    lazyRuntimeCreatedName = "app.lazy.runtime.created"
    lazyRuntimeHolderName  = "app.lazy.runtime.holder"
)

func registerLazyRuntimeCreated(registrar containercontract.Registrar, creations *atomic.Int64, failFirst bool) {
    container.MustRegister[*lazyRuntimeCreated](registrar, lazyRuntimeCreatedName, func(resolver containercontract.Resolver) (*lazyRuntimeCreated, error) {
        created := creations.Add(1)
        if true == failFirst && 1 == created {
            return nil, errors.New("the first creation fails")
        }

        providerRuntime := resolver.Runtime()

        return &lazyRuntimeCreated{
            kind:    providerRuntime.Kind(),
            value:   providerRuntime.Context().Value(lazyRuntimeKey{}),
            scope:   providerRuntime.Scope(),
            created: created,
        }, nil
    }, containercontract.RegisterOptions{})
}

func newContainerOwnedHolder(t *testing.T, failFirst bool) (containercontract.Container, *lazyRuntimeHolder, *atomic.Int64) {
    t.Helper()

    return newContainerOwnedHolderWith(t, failFirst, func(containercontract.Container) {})
}

/* newContainerOwnedHolderWith registers what register adds before the holder's resolution freezes the registrations */
func newContainerOwnedHolderWith(t *testing.T, failFirst bool, register func(serviceContainer containercontract.Container)) (containercontract.Container, *lazyRuntimeHolder, *atomic.Int64) {
    t.Helper()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    register(serviceContainer)
    creations := &atomic.Int64{}
    registerLazyRuntimeCreated(serviceContainer, creations, failFirst)
    container.MustRegister[*lazyRuntimeHolder](serviceContainer, lazyRuntimeHolderName, func(resolver containercontract.Resolver) (*lazyRuntimeHolder, error) {
        return &lazyRuntimeHolder{handle: container.Lazy[*lazyRuntimeCreated](resolver, lazyRuntimeCreatedName)}, nil
    }, containercontract.RegisterOptions{})

    holder := container.MustFromResolver[*lazyRuntimeHolder](t.Context(), serviceContainer, lazyRuntimeHolderName)
    if 0 != creations.Load() {
        t.Fatalf("the holder's provider created the lazy service")
    }

    return serviceContainer, holder, creations
}

func lazyRuntimeContext(t *testing.T, value string) context.Context {
    return context.WithValue(t.Context(), lazyRuntimeKey{}, value)
}

func TestLazy_AContainerOwnedHandleCreatesUnderTheCallersKindAndContext(t *testing.T) {
    for _, testCase := range []struct {
        name string
        make func(ctx context.Context, scope containercontract.Scope) containercontract.Runtime
        kind containercontract.Kind
    }{
        {name: "request", make: runtime.NewRequest, kind: containercontract.KindRequest},
        {name: "run", make: runtime.NewRun, kind: containercontract.KindRun},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            serviceContainer, holder, _ := newContainerOwnedHolder(t, false)
            callerRuntime := testCase.make(lazyRuntimeContext(t, testCase.name), serviceContainer.NewScope())

            created := holder.handle.Get(callerRuntime)
            if testCase.kind != created.kind {
                t.Fatalf("the created provider read %s, wanted the caller's %s", created.kind, testCase.kind)
            }

            if testCase.name != created.value {
                t.Fatalf("the created provider read the context value %v, wanted the caller's %q", created.value, testCase.name)
            }
        })
    }
}

func TestLazy_AHandleOverTheContainerCreatesUnderTheCallersKindAndContext(t *testing.T) {
    for _, testCase := range []struct {
        name string
        make func(ctx context.Context, scope containercontract.Scope) containercontract.Runtime
        kind containercontract.Kind
    }{
        {name: "request", make: runtime.NewRequest, kind: containercontract.KindRequest},
        {name: "run", make: runtime.NewRun, kind: containercontract.KindRun},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            serviceContainer := container.NewContainer(container.ContainerConfig{})
            creations := &atomic.Int64{}
            registerLazyRuntimeCreated(serviceContainer, creations, false)
            handle := container.Lazy[*lazyRuntimeCreated](serviceContainer, lazyRuntimeCreatedName)

            created := handle.Get(testCase.make(lazyRuntimeContext(t, testCase.name), serviceContainer.NewScope()))
            if testCase.kind != created.kind || testCase.name != created.value {
                t.Fatalf("the created provider read %s / %v, wanted %s / %q", created.kind, created.value, testCase.kind, testCase.name)
            }

            /* resolved through the container, never the caller's scope: the provider's scope is suspended */
            if registerErr := created.scope.RegisterScoped("app.lazy.runtime.other", func(resolver containercontract.Resolver) (string, error) { return "", nil }, containercontract.RegisterOptions{}); nil == registerErr {
                t.Fatalf("the created provider's scope accepted a registration: the handle resolved through the caller's scope")
            }
        })
    }
}

func TestLazy_TheCreatedProvidersScopeIsSuspended(t *testing.T) {
    serviceContainer, holder, _ := newContainerOwnedHolderWith(t, false, func(serviceContainer containercontract.Container) {
        container.MustRegisterScoped[string](serviceContainer, "app.lazy.runtime.scoped", func(resolver containercontract.Resolver) (string, error) {
            return "scoped", nil
        }, containercontract.RegisterOptions{})
    })
    requestScope := serviceContainer.NewScope()

    created := holder.handle.Get(runtime.NewRequest(t.Context(), requestScope))

    if registerErr := created.scope.RegisterScoped("app.lazy.runtime.other", func(resolver containercontract.Resolver) (string, error) { return "", nil }, containercontract.RegisterOptions{}); nil == registerErr {
        t.Fatalf("expected the suspended scope to refuse a registration")
    }

    if overrideErr := created.scope.OverrideInstance(lazyRuntimeCreatedName, "x", containercontract.OverrideOptions{}); nil == overrideErr {
        t.Fatalf("expected the suspended scope to refuse an override")
    }

    if closeErr := created.scope.Close(); nil != closeErr {
        t.Fatalf("the suspended scope's close answered %v", closeErr)
    }

    if true == created.scope.Has("app.lazy.runtime.scoped") {
        t.Fatalf("the caller's scoped service is reachable through the created provider's scope")
    }

    if _, scopedErr := container.FromResolver[string](t.Context(), requestScope, "app.lazy.runtime.scoped"); nil != scopedErr {
        t.Fatalf("the caller's scope was closed through the suspended scope: %v", scopedErr)
    }
}

func TestLazy_AScopedHandleKeepsItsScopesKind(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    creations := &atomic.Int64{}
    container.MustRegisterScoped[*lazyRuntimeCreated](serviceContainer, lazyRuntimeCreatedName, func(resolver containercontract.Resolver) (*lazyRuntimeCreated, error) {
        creations.Add(1)

        return &lazyRuntimeCreated{kind: resolver.Runtime().Kind(), value: resolver.Runtime().Context().Value(lazyRuntimeKey{})}, nil
    }, containercontract.RegisterOptions{})
    container.MustRegisterScoped[*lazyRuntimeHolder](serviceContainer, lazyRuntimeHolderName, func(resolver containercontract.Resolver) (*lazyRuntimeHolder, error) {
        return &lazyRuntimeHolder{handle: container.Lazy[*lazyRuntimeCreated](resolver, lazyRuntimeCreatedName)}, nil
    }, containercontract.RegisterOptions{})

    runScope := serviceContainer.NewScope()
    runRuntime := runtime.NewRun(t.Context(), runScope)
    holder := runtime.MustFromRuntime[*lazyRuntimeHolder](runRuntime, lazyRuntimeHolderName)

    created := holder.handle.Get(&lazyHandRuntime{ctx: lazyRuntimeContext(t, "hand"), scope: runScope, kind: containercontract.KindRequest})
    if containercontract.KindRun != created.kind {
        t.Fatalf("the scoped creation read %s, wanted its scope's run kind", created.kind)
    }

    if "hand" != created.value {
        t.Fatalf("the scoped creation read the context value %v, wanted the caller's", created.value)
    }
}

func TestLazy_AScopedHandleRefusesARuntimeOfAnotherScope(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    creations := &atomic.Int64{}
    container.MustRegisterScoped[*lazyRuntimeCreated](serviceContainer, lazyRuntimeCreatedName, func(resolver containercontract.Resolver) (*lazyRuntimeCreated, error) {
        creations.Add(1)

        return &lazyRuntimeCreated{kind: resolver.Runtime().Kind()}, nil
    }, containercontract.RegisterOptions{})
    container.MustRegisterScoped[*lazyRuntimeHolder](serviceContainer, lazyRuntimeHolderName, func(resolver containercontract.Resolver) (*lazyRuntimeHolder, error) {
        return &lazyRuntimeHolder{handle: container.Lazy[*lazyRuntimeCreated](resolver, lazyRuntimeCreatedName)}, nil
    }, containercontract.RegisterOptions{})

    ownScope := serviceContainer.NewScope()
    ownRuntime := runtime.NewRequest(t.Context(), ownScope)
    holder := runtime.MustFromRuntime[*lazyRuntimeHolder](ownRuntime, lazyRuntimeHolderName)
    scopeHandle := container.Lazy[*lazyRuntimeCreated](ownScope, lazyRuntimeCreatedName)

    otherRuntime := runtime.NewRun(t.Context(), serviceContainer.NewScope())
    for name, handle := range map[string]*container.LazyService[*lazyRuntimeCreated]{"provider view": holder.handle, "scope": scopeHandle} {
        _, resolveErr := handle.Resolve(otherRuntime)
        if nil == resolveErr || false == strings.Contains(resolveErr.Error(), "the runtime belongs to another scope than the lazy handle") {
            t.Fatalf("%s: expected the other scope's runtime refused, got %v", name, resolveErr)
        }

        for _, named := range []string{"handleScopeKind", "request", "runtimeScopeKind", "run"} {
            if false == strings.Contains(describeError(resolveErr), named) {
                t.Fatalf("%s: the refusal %q does not name %q", name, describeError(resolveErr), named)
            }
        }
    }

    if 0 != creations.Load() {
        t.Fatalf("a refused runtime created the service")
    }

    if _, ownErr := holder.handle.Resolve(runtime.WithContext(ownRuntime, t.Context())); nil != ownErr {
        t.Fatalf("the handle's own scope was refused: %v", ownErr)
    }

    _, containerHolder, _ := newContainerOwnedHolder(t, false)
    if _, containerErr := containerHolder.handle.Resolve(otherRuntime); nil != containerErr {
        t.Fatalf("a container service's handle refused a runtime of a scope: %v", containerErr)
    }
}

func TestLazy_AScopedHolderUsesItsHandleUnderItsProvidersRuntime(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegisterScoped[lazyRuntimeDependency](serviceContainer, "app.lazy.runtime.dependency", func(resolver containercontract.Resolver) (lazyRuntimeDependency, error) {
        return "dependency", nil
    }, containercontract.RegisterOptions{})
    container.MustRegisterScoped[string](serviceContainer, "app.lazy.runtime.user", func(resolver containercontract.Resolver) (string, error) {
        handle := container.Lazy[lazyRuntimeDependency](resolver, "app.lazy.runtime.dependency")
        dependency, resolveErr := handle.Resolve(resolver.Runtime())

        return string(dependency), resolveErr
    }, containercontract.RegisterOptions{})

    answered := runtime.MustFromRuntime[string](runtime.NewRequest(t.Context(), serviceContainer.NewScope()), "app.lazy.runtime.user")
    if "dependency" != answered {
        t.Fatalf("the holder's own provider answered %q", answered)
    }
}

func TestLazy_AMemoizedHandleIgnoresALaterRuntimeAndAFailureIsRetriedUnderTheNextCaller(t *testing.T) {
    serviceContainer, holder, creations := newContainerOwnedHolder(t, true)

    if _, firstErr := holder.handle.Resolve(runtime.NewRequest(lazyRuntimeContext(t, "first"), serviceContainer.NewScope())); nil == firstErr {
        t.Fatalf("expected the first creation to fail")
    }

    second := holder.handle.Get(runtime.NewRun(lazyRuntimeContext(t, "second"), serviceContainer.NewScope()))
    if containercontract.KindRun != second.kind || "second" != second.value {
        t.Fatalf("the retry read %s / %v, wanted the second caller's run / %q", second.kind, second.value, "second")
    }

    third := holder.handle.Get(runtime.NewRequest(lazyRuntimeContext(t, "third"), serviceContainer.NewScope()))
    if third != second || 2 != creations.Load() {
        t.Fatalf("a memoized handle created again: %d creations", creations.Load())
    }
}

func TestLazy_ANilRuntimeIsRefused(t *testing.T) {
    _, holder, creations := newContainerOwnedHolder(t, false)

    var typedNil *lazyHandRuntime
    for name, runtimeInstance := range map[string]containercontract.Runtime{
        "nil":         nil,
        "typed nil":   typedNil,
        "nil context": &lazyHandRuntime{ctx: nil, kind: containercontract.KindRun},
    } {
        if _, resolveErr := holder.handle.Resolve(runtimeInstance); nil == resolveErr || false == strings.Contains(resolveErr.Error(), "the runtime is nil or carries no context") {
            t.Fatalf("%s: expected the nil runtime refused, got %v", name, resolveErr)
        }

        func() {
            defer func() {
                recovered := recover()
                if nil == recovered || false == strings.Contains(describeRecovered(recovered), "the runtime is nil or carries no context") {
                    t.Fatalf("%s: expected Get to panic with the nil runtime refusal, got %v", name, recovered)
                }
            }()

            holder.handle.Get(runtimeInstance)
        }()
    }

    if 0 != creations.Load() {
        t.Fatalf("a nil runtime created the service")
    }
}

func TestLazy_ARuntimeWhoseContextIsDoneStartsNoCreation(t *testing.T) {
    serviceContainer, holder, creations := newContainerOwnedHolder(t, false)

    bootRuntime, finishBoot := runtime.NewBoot(t.Context(), serviceContainer)
    if finishErr := finishBoot(); nil != finishErr {
        t.Fatalf("finish the boot: %v", finishErr)
    }

    _, resolveErr := holder.handle.Resolve(bootRuntime)
    if false == errors.Is(resolveErr, containercontract.ErrBootFinished) {
        t.Fatalf("expected the finished boot in the cause chain, got %v", resolveErr)
    }

    if 0 != creations.Load() {
        t.Fatalf("a done context created the service")
    }

    if _, retryErr := holder.handle.Resolve(runtime.NewRun(t.Context(), serviceContainer.NewScope())); nil != retryErr {
        t.Fatalf("the done-context failure was memoized: %v", retryErr)
    }
}

func TestLazy_ATerminalHandleAnswersItsClosedSentinelWhateverTheRuntime(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    creations := &atomic.Int64{}
    registerLazyRuntimeCreated(serviceContainer, creations, false)
    containerHandle := container.Lazy[*lazyRuntimeCreated](serviceContainer, lazyRuntimeCreatedName)

    scope := serviceContainer.NewScope()
    scopeHandle := container.Lazy[*lazyRuntimeCreated](scope, lazyRuntimeCreatedName)
    scopeRuntime := runtime.NewRequest(t.Context(), scope)
    if closeErr := scope.Close(); nil != closeErr {
        t.Fatalf("close the scope: %v", closeErr)
    }

    for _, runtimeInstance := range []containercontract.Runtime{scopeRuntime, runtime.NewRun(t.Context(), serviceContainer.NewScope())} {
        if _, resolveErr := scopeHandle.Resolve(runtimeInstance); false == errors.Is(resolveErr, container.ErrScopeClosed) {
            t.Fatalf("expected ErrScopeClosed, got %v", resolveErr)
        }
    }

    otherRuntime := runtime.NewRun(t.Context(), serviceContainer.NewScope())
    if closeErr := serviceContainer.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close the container: %v", closeErr)
    }

    if _, resolveErr := containerHandle.Resolve(otherRuntime); false == errors.Is(resolveErr, container.ErrContainerClosed) {
        t.Fatalf("expected ErrContainerClosed, got %v", resolveErr)
    }
}

type lazyForeignResolver struct {
    containercontract.Resolver
    seen any
}

func (instance *lazyForeignResolver) Get(ctx context.Context, serviceName string) (any, error) {
    instance.seen = ctx.Value(lazyRuntimeKey{})

    return "foreign", nil
}

func TestLazy_AForeignResolverIsAskedUnderTheRuntimesContext(t *testing.T) {
    foreign := &lazyForeignResolver{}
    handle := container.Lazy[string](foreign, "app.lazy.runtime.foreign")

    answered := handle.Get(&lazyHandRuntime{ctx: lazyRuntimeContext(t, "foreign"), kind: containercontract.KindRun})
    if "foreign" != answered || "foreign" != foreign.seen {
        t.Fatalf("the foreign resolver answered %q under %v", answered, foreign.seen)
    }
}

func describeError(err error) string {
    var described strings.Builder
    described.WriteString(err.Error())

    var melodyErr *exception.Error
    if true == exception.ChainAs(err, &melodyErr) && nil != melodyErr {
        for key, value := range melodyErr.Context() {
            described.WriteString(fmt.Sprintf(" %s=%v", key, value))
        }
    }

    return described.String()
}

func describeRecovered(recovered any) string {
    if err, isErr := recovered.(error); true == isErr {
        return err.Error()
    }

    if stringer, isStringer := recovered.(interface{ String() string }); true == isStringer {
        return stringer.String()
    }

    return ""
}

func TestLazy_AContainerHandleDoesNotAnswerTheCallersScopeOverride(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    creations := &atomic.Int64{}
    registerLazyRuntimeCreated(serviceContainer, creations, false)
    handle := container.Lazy[*lazyRuntimeCreated](serviceContainer, lazyRuntimeCreatedName)

    callerScope := serviceContainer.NewScope()
    override := &lazyRuntimeCreated{value: "override"}
    if overrideErr := callerScope.OverrideInstance(lazyRuntimeCreatedName, override, containercontract.OverrideOptions{}); nil != overrideErr {
        t.Fatalf("override in the caller's scope: %v", overrideErr)
    }

    created := handle.Get(runtime.NewRequest(t.Context(), callerScope))
    if override == created || 1 != creations.Load() {
        t.Fatalf("the container handle answered the caller's scope override")
    }
}

type lazyRuntimeForeignScope struct {
    containercontract.Scope
}

func TestLazy_ACallerCancelledWhileAnotherGoroutineCreatesLeavesTheWaitUnmemoized(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    release := make(chan struct{})
    started := make(chan struct{})
    container.MustRegister[*lazyRuntimeCreated](serviceContainer, lazyRuntimeCreatedName, func(resolver containercontract.Resolver) (*lazyRuntimeCreated, error) {
        close(started)
        <-release

        return &lazyRuntimeCreated{kind: resolver.Runtime().Kind()}, nil
    }, containercontract.RegisterOptions{})
    handle := container.Lazy[*lazyRuntimeCreated](serviceContainer, lazyRuntimeCreatedName)

    creatorDone := make(chan *lazyRuntimeCreated, 1)
    go func() {
        creatorDone <- handle.Get(runtime.NewRun(t.Context(), serviceContainer.NewScope()))
    }()
    <-started

    waiterContext, cancelWaiter := context.WithCancel(t.Context())
    waiterDone := make(chan error, 1)
    go func() {
        _, waitErr := handle.Resolve(runtime.NewRequest(waiterContext, serviceContainer.NewScope()))
        waiterDone <- waitErr
    }()

    /* the cancel has to land on a waiter already parked on the creation, past the done-context check */
    time.Sleep(50 * time.Millisecond)
    cancelWaiter()
    if waitErr := <-waiterDone; false == errors.Is(waitErr, context.Canceled) {
        close(release)
        t.Fatalf("expected the parked waiter to leave with its context's error, got %v", waitErr)
    }

    close(release)
    created := <-creatorDone
    if containercontract.KindRun != created.kind {
        t.Fatalf("the creation read %s, wanted the creator's run", created.kind)
    }

    if again := handle.Get(runtime.NewRequest(t.Context(), serviceContainer.NewScope())); again != created {
        t.Fatalf("the cancelled waiter's failure was memoized or the value was not")
    }
}

func TestLazy_AByTypeHandleCreatesUnderTheCallersKind(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    creations := &atomic.Int64{}
    registerLazyRuntimeCreated(serviceContainer, creations, false)
    container.MustRegister[*lazyRuntimeHolder](serviceContainer, lazyRuntimeHolderName, func(resolver containercontract.Resolver) (*lazyRuntimeHolder, error) {
        return &lazyRuntimeHolder{handle: container.LazyByType[*lazyRuntimeCreated](resolver)}, nil
    }, containercontract.RegisterOptions{})
    holder := container.MustFromResolver[*lazyRuntimeHolder](t.Context(), serviceContainer, lazyRuntimeHolderName)

    created := holder.handle.Get(runtime.NewRequest(lazyRuntimeContext(t, "by type"), serviceContainer.NewScope()))
    if containercontract.KindRequest != created.kind || "by type" != created.value {
        t.Fatalf("the by-type view creation read %s / %v, wanted the caller's request", created.kind, created.value)
    }

    otherContainer := container.NewContainer(container.ContainerConfig{})
    registerLazyRuntimeCreated(otherContainer, &atomic.Int64{}, false)
    containerHandle := container.LazyByType[*lazyRuntimeCreated](otherContainer)
    if created := containerHandle.Get(runtime.NewRun(t.Context(), otherContainer.NewScope())); containercontract.KindRun != created.kind {
        t.Fatalf("the by-type container creation read %s, wanted the caller's run", created.kind)
    }
}

func TestLazy_AScopedByTypeHandleRefusesARuntimeOfAnotherScope(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegisterScoped[*lazyRuntimeCreated](serviceContainer, lazyRuntimeCreatedName, func(resolver containercontract.Resolver) (*lazyRuntimeCreated, error) {
        return &lazyRuntimeCreated{}, nil
    }, containercontract.RegisterOptions{})
    container.MustRegisterScoped[*lazyRuntimeHolder](serviceContainer, lazyRuntimeHolderName, func(resolver containercontract.Resolver) (*lazyRuntimeHolder, error) {
        return &lazyRuntimeHolder{handle: container.LazyByType[*lazyRuntimeCreated](resolver)}, nil
    }, containercontract.RegisterOptions{})

    holder := runtime.MustFromRuntime[*lazyRuntimeHolder](runtime.NewRequest(t.Context(), serviceContainer.NewScope()), lazyRuntimeHolderName)
    if _, resolveErr := holder.handle.Resolve(runtime.NewRun(t.Context(), serviceContainer.NewScope())); nil == resolveErr || false == strings.Contains(resolveErr.Error(), "the runtime belongs to another scope than the lazy handle") {
        t.Fatalf("expected the by-type handle to refuse another scope's runtime, got %v", resolveErr)
    }
}

func TestLazy_ARetainedViewHandleDoesNotAnswerTheCallersScopeOverride(t *testing.T) {
    serviceContainer, holder, creations := newContainerOwnedHolder(t, false)

    callerScope := serviceContainer.NewScope()
    override := &lazyRuntimeCreated{value: "override"}
    if overrideErr := callerScope.OverrideInstance(lazyRuntimeCreatedName, override, containercontract.OverrideOptions{}); nil != overrideErr {
        t.Fatalf("override in the caller's scope: %v", overrideErr)
    }

    if created := holder.handle.Get(runtime.NewRequest(t.Context(), callerScope)); override == created || 1 != creations.Load() {
        t.Fatalf("the retained view's handle answered the caller's scope override")
    }
}

func TestLazy_AHandleOverAProvidersRuntimeScopeCreatesUnderTheCallersKind(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    registerLazyRuntimeCreated(serviceContainer, &atomic.Int64{}, false)
    container.MustRegister[*lazyRuntimeHolder](serviceContainer, lazyRuntimeHolderName, func(resolver containercontract.Resolver) (*lazyRuntimeHolder, error) {
        return &lazyRuntimeHolder{handle: container.Lazy[*lazyRuntimeCreated](resolver.Runtime().Scope(), lazyRuntimeCreatedName)}, nil
    }, containercontract.RegisterOptions{})
    holder := container.MustFromResolver[*lazyRuntimeHolder](t.Context(), serviceContainer, lazyRuntimeHolderName)

    if created := holder.handle.Get(runtime.NewRun(t.Context(), serviceContainer.NewScope())); containercontract.KindRun != created.kind {
        t.Fatalf("the creation through the suspended scope read %s, wanted the caller's run", created.kind)
    }
}

func TestLazy_TheDoneContextIsJudgedAfterTheTerminalAndTheMemoizedAnswers(t *testing.T) {
    serviceContainer, holder, _ := newContainerOwnedHolder(t, false)
    created := holder.handle.Get(runtime.NewRun(t.Context(), serviceContainer.NewScope()))

    doneContext, cancel := context.WithCancel(t.Context())
    cancel()
    if memoized := holder.handle.Get(runtime.NewRun(doneContext, serviceContainer.NewScope())); memoized != created {
        t.Fatalf("a memoized handle refused a done context")
    }

    scope := serviceContainer.NewScope()
    scopeHandle := container.Lazy[*lazyRuntimeCreated](scope, lazyRuntimeCreatedName)
    if closeErr := scope.Close(); nil != closeErr {
        t.Fatalf("close the scope: %v", closeErr)
    }

    if _, closedErr := scopeHandle.Resolve(runtime.NewRun(doneContext, serviceContainer.NewScope())); false == errors.Is(closedErr, container.ErrScopeClosed) {
        t.Fatalf("expected a terminal handle to answer ErrScopeClosed under a done context, got %v", closedErr)
    }
}

func TestLazy_AScopedHandleNamesAForeignOrAContainerScope(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegisterScoped[*lazyRuntimeCreated](serviceContainer, lazyRuntimeCreatedName, func(resolver containercontract.Resolver) (*lazyRuntimeCreated, error) {
        return &lazyRuntimeCreated{}, nil
    }, containercontract.RegisterOptions{})
    scope := serviceContainer.NewScope()
    runtime.NewRequest(t.Context(), scope)
    handle := container.Lazy[*lazyRuntimeCreated](scope, lazyRuntimeCreatedName)

    for label, runtimeInstance := range map[string]containercontract.Runtime{
        "foreign":   &lazyHandRuntime{ctx: t.Context(), scope: &lazyRuntimeForeignScope{}, kind: containercontract.KindRequest},
        "container": serviceContainer.Runtime(),
    } {
        _, resolveErr := handle.Resolve(runtimeInstance)
        if nil == resolveErr || false == strings.Contains(describeError(resolveErr), "runtimeScopeKind="+label) {
            t.Fatalf("expected the refusal to name the %s scope, got %v", label, resolveErr)
        }
    }
}
