package container

import (
    "context"
    "errors"
    "fmt"
    "reflect"
    "runtime"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

func TestScope_GetDelegatesToContainerAndCachesPerScope(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    calls := 0

    err := serviceContainer.Register(
        "service.test",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            calls++
            return &scopeTestService{value: "ok"}, nil
        },
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    scope := serviceContainer.NewScope()

    _, err = scope.Get(t.Context(), "service.test")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    _, err = scope.Get(t.Context(), "service.test")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    if 1 != calls {
        t.Fatalf("expected provider to be called once per container singleton")
    }
}

func TestScope_OverrideInstance_IsolatedFromResolver(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    err := serviceContainer.Register(
        "service.test",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "container"}, nil
        },
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    scope := serviceContainer.NewScope()

    err = scope.OverrideProtectedInstance(
        "service.test",
        &scopeTestService{value: "scope"},
        OverrideOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected override error: %v", err)
    }

    valueAny, err := scope.Get(t.Context(), "service.test")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    scopeValue := valueAny.(*scopeTestService)
    if "scope" != scopeValue.value {
        t.Fatalf("expected scope override value")
    }

    containerValueAny, err := serviceContainer.Get(t.Context(), "service.test")
    if nil != err {
        t.Fatalf("unexpected container get error: %v", err)
    }

    containerValue := containerValueAny.(*scopeTestService)
    if "container" != containerValue.value {
        t.Fatalf("expected container value to remain unchanged")
    }
}

func TestScopeOverride_OfOneNameDoesNotAnswerAnotherServicesGetByType(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    interfaceRegisterErr := serviceContainer.Register(
        "scope.poison.contract",
        func(resolver containercontract.Resolver) (testInterface, error) {
            return &testImplementation{name: "contract owner"}, nil
        },
        RegisterOptions{},
    )
    if nil != interfaceRegisterErr {
        t.Fatalf("unexpected register error: %v", interfaceRegisterErr)
    }

    concreteRegisterErr := serviceContainer.Register(
        "scope.poison.concrete",
        func(resolver containercontract.Resolver) (*testImplementation, error) {
            return &testImplementation{name: "concrete owner"}, nil
        },
        RegisterOptions{},
    )
    if nil != concreteRegisterErr {
        t.Fatalf("unexpected register error: %v", concreteRegisterErr)
    }

    scopeInstance := serviceContainer.NewScope()

    overrideErr := scopeInstance.OverrideProtectedInstance("scope.poison.contract", &testImplementation{name: "override"}, OverrideOptions{})
    if nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    concreteValue, concreteErr := scopeInstance.GetByType(t.Context(), reflect.TypeOf((*testImplementation)(nil)))
    if nil != concreteErr {
        t.Fatalf("unexpected get by type error: %v", concreteErr)
    }
    if implementation, isTyped := concreteValue.(*testImplementation); false == isTyped || "concrete owner" != implementation.name {
        t.Fatalf("expected the concrete service to keep answering its own type through the scope, got %#v", concreteValue)
    }
}

func TestScope_AnOverrideWinsOverAnInstanceTheContainerHasAlreadyBuilt(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    if err := serviceContainer.Register(
        "service.test",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "container"}, nil
        },
        RegisterOptions{},
    ); nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    if _, err := serviceContainer.Get(t.Context(), "service.test"); nil != err {
        t.Fatalf("unexpected container get error: %v", err)
    }

    scope := serviceContainer.NewScope()

    if err := scope.OverrideProtectedInstance(
        "service.test",
        &scopeTestService{value: "scope"},
        OverrideOptions{},
    ); nil != err {
        t.Fatalf("unexpected override error: %v", err)
    }

    valueAny, err := scope.Get(t.Context(), "service.test")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    if "scope" != valueAny.(*scopeTestService).value {
        t.Fatalf("expected the scope override to answer, got the container's own instance")
    }
}

func TestScope_CloseReturnsErrorOnGet(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    scope := serviceContainer.NewScope()
    _ = scope.CloseWithContext(context.Background())

    _, getErr := scope.Get(t.Context(), "service.test")
    if nil == getErr {
        t.Fatalf("expected closed-scope error")
    }

    if false == errors.Is(getErr, ErrScopeClosed) {
        t.Fatalf("expected the refusal to classify as ErrScopeClosed")
    }
}

func TestScope_CloseKeepsMustGetPanicking(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    /* registered so a live scope answers: on an empty container MustGet panics whatever the scope's state */
    registerErr := serviceContainer.Register(
        "service.test",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "live"}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()
    _ = scopeInstance.CloseWithContext(context.Background())

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected panic from MustGet on a closed scope")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError || false == errors.Is(recoveredErr, ErrScopeClosed) {
            t.Fatalf("expected the panic to carry the closed-scope refusal, got %#v", recovered)
        }
    }()

    _ = scopeInstance.MustGet(t.Context(), "service.test")
}

func TestScope_HasReturnsFalseWhenClosed(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "a",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "live"}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if false == scopeInstance.Has("a") {
        t.Fatalf("expected the live scope to answer for the registered name")
    }

    _ = scopeInstance.CloseWithContext(context.Background())

    if true == scopeInstance.Has("a") {
        t.Fatalf("expected false")
    }
}

func TestScope_CloseIsIdempotent(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    closeCalls := int32(0)

    registerErr := serviceContainer.RegisterScoped(
        "app.idempotent",
        func(resolver containercontract.Resolver) (*closeCountingScopeService, error) {
            return &closeCountingScopeService{value: "scoped", closeCalls: &closeCalls}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get(t.Context(), "app.idempotent"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if err := scopeInstance.CloseWithContext(context.Background()); nil != err {
        t.Fatalf("unexpected first close error: %v", err)
    }

    if err := scopeInstance.CloseWithContext(context.Background()); nil != err {
        t.Fatalf("unexpected second close error: %v", err)
    }

    if 1 != atomic.LoadInt32(&closeCalls) {
        t.Fatalf("expected the built service to be closed exactly once across both passes, got %d", atomic.LoadInt32(&closeCalls))
    }
}

func TestScope_OverrideAfterCloseReturnsError(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    scopeInstance := serviceContainer.NewScope()
    _ = scopeInstance.CloseWithContext(context.Background())

    overrideErr := scopeInstance.OverrideProtectedInstance("service.after_close", &scopeTestService{value: "late"}, OverrideOptions{})
    if nil == overrideErr {
        t.Fatalf("expected closed-scope error on override after close")
    }

    if false == errors.Is(overrideErr, ErrScopeClosed) {
        t.Fatalf("expected the refusal to classify as ErrScopeClosed")
    }

    if "entry" != refusalStageOf(t, overrideErr) {
        t.Fatalf("expected the entry guard to answer, got %q", refusalStageOf(t, overrideErr))
    }
}

func TestScope_OverrideAfterCloseKeepsMustPanicking(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    scopeInstance := serviceContainer.NewScope()
    _ = scopeInstance.CloseWithContext(context.Background())

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected panic from MustOverrideProtectedInstance on a closed scope")
        }

        /* an unqualified recover accepts any panic, so the cause names the guard under test */
        recoveredErr, isError := recovered.(error)
        if false == isError || false == errors.Is(recoveredErr, ErrScopeClosed) {
            t.Fatalf("expected the panic to carry the closed-scope refusal, got %#v", recovered)
        }
    }()

    scopeInstance.MustOverrideProtectedInstance("service.after_close", &scopeTestService{value: "late"}, OverrideOptions{})
}

func TestScope_GetByTypeAfterCloseReturnsError(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    scopeInstance := serviceContainer.NewScope()
    _ = scopeInstance.CloseWithContext(context.Background())

    _, getByTypeErr := scopeInstance.GetByType(t.Context(), reflect.TypeOf((*scopeTestService)(nil)))
    if nil == getByTypeErr {
        t.Fatalf("expected closed-scope error on get-by-type after close")
    }

    if false == errors.Is(getByTypeErr, ErrScopeClosed) {
        t.Fatalf("expected the refusal to classify as ErrScopeClosed")
    }
}

func TestScope_GetByTypeAfterCloseKeepsMustPanicking(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "service.getbytype",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "live"}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()
    _ = scopeInstance.CloseWithContext(context.Background())

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected panic from MustGetByType on a closed scope")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError || false == errors.Is(recoveredErr, ErrScopeClosed) {
            t.Fatalf("expected the panic to carry the closed-scope refusal, got %#v", recovered)
        }
    }()

    /* registered so a live scope answers: on an empty container the by-type read panics whatever the scope's state */
    _ = scopeInstance.MustGetByType(t.Context(), reflect.TypeOf((*scopeTestService)(nil)))
}

func TestScope_HasTypeReturnsFalseWhenClosed(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "service.hastype",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "live"}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if false == scopeInstance.HasType(reflect.TypeOf((*scopeTestService)(nil))) {
        t.Fatalf("expected the live scope to answer for the registered type")
    }

    _ = scopeInstance.CloseWithContext(context.Background())

    if true == scopeInstance.HasType(reflect.TypeOf((*scopeTestService)(nil))) {
        t.Fatalf("expected false after close")
    }
}

func TestScope_ConcurrentGetAndClose(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    err := serviceContainer.Register(
        "service.concurrent",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "ok"}, nil
        },
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    scopeInstance := serviceContainer.NewScope()

    var waitGroup sync.WaitGroup
    var closeOnce sync.Once
    closeSignal := make(chan struct{})

    readerCount := 32
    panics := atomic.Int64{}

    for readerIndex := 0; readerIndex < readerCount; readerIndex++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            defer func() {
                if nil != recover() {
                    panics.Add(1)
                }
            }()

            for iteration := 0; iteration < 200; iteration++ {
                _, getErr := scopeInstance.Get(t.Context(), "service.concurrent")
                if nil != getErr {
                    return
                }

                if 50 == iteration {
                    closeOnce.Do(func() {
                        close(closeSignal)
                    })
                }

                select {
                case <-closeSignal:
                default:
                }
            }
        }()
    }

    waitGroup.Add(1)
    go func() {
        defer waitGroup.Done()
        <-closeSignal
        _ = scopeInstance.CloseWithContext(context.Background())
    }()

    waitGroup.Wait()

    if 0 != panics.Load() {
        t.Fatalf("expected no reader to panic while the scope closed under them, got %d", panics.Load())
    }
}

func TestScope_ConcurrentOverrideAndGet(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    err := serviceContainer.Register(
        "service.mutable",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "base"}, nil
        },
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    scopeInstance := serviceContainer.NewScope()

    var waitGroup sync.WaitGroup

    for writerIndex := 0; writerIndex < 8; writerIndex++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            for iteration := 0; iteration < 200; iteration++ {
                _ = scopeInstance.OverrideProtectedInstance(
                    "service.mutable",
                    &scopeTestService{value: "override"},
                    OverrideOptions{},
                )
            }
        }()
    }

    for readerIndex := 0; readerIndex < 8; readerIndex++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            for iteration := 0; iteration < 200; iteration++ {
                _, _ = scopeInstance.Get(t.Context(), "service.mutable")
                _ = scopeInstance.Has("service.mutable")
            }
        }()
    }

    waitGroup.Wait()
}

func TestScope_ConcurrentHasAndClose(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    scopeInstance := serviceContainer.NewScope()

    var waitGroup sync.WaitGroup
    var closeOnce sync.Once
    closeSignal := make(chan struct{})

    for readerIndex := 0; readerIndex < 16; readerIndex++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            for iteration := 0; iteration < 500; iteration++ {
                _ = scopeInstance.Has("service.any")
                _ = scopeInstance.HasType(reflect.TypeOf((*scopeTestService)(nil)))

                if 100 == iteration {
                    closeOnce.Do(func() {
                        close(closeSignal)
                    })
                }
            }
        }()
    }

    waitGroup.Add(1)
    go func() {
        defer waitGroup.Done()
        <-closeSignal
        _ = scopeInstance.CloseWithContext(context.Background())
    }()

    waitGroup.Wait()
}

func TestScope_MustGetByTypeNilTypePanicsDescriptively(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    scope := serviceContainer.NewScope()

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected MustGetByType(nil) to panic")
        }
        if _, isRuntimeError := recoveredValue.(runtime.Error); true == isRuntimeError {
            t.Fatalf("expected a descriptive panic carrying the GetByType cause, got a runtime error: %v", recoveredValue)
        }
    }()

    _ = scope.MustGetByType(t.Context(), nil)
}

func TestScope_ServiceThatReadsNothingFromTheScopeStaysASingleton(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    calls := 0

    registerErr := serviceContainer.Register(
        "app.shared",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            calls = calls + 1

            return &scopeTestService{value: "shared"}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    firstScope := serviceContainer.NewScope()
    first, getErr := firstScope.Get(t.Context(), "app.shared")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }
    _ = firstScope.CloseWithContext(context.Background())

    secondScope := serviceContainer.NewScope()
    second, getErr := secondScope.Get(t.Context(), "app.shared")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }
    _ = secondScope.CloseWithContext(context.Background())

    if 1 != calls {
        t.Fatalf("expected the provider to run once for the whole process, ran %d times", calls)
    }

    if first != second {
        t.Fatalf("expected both scopes to be handed the same singleton")
    }

    fromContainer, getErr := serviceContainer.Get(t.Context(), "app.shared")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if first != fromContainer {
        t.Fatalf("expected the container itself to hold the singleton the scopes built")
    }
}

func TestScope_RootContainerDoesNotSeeTheScopeBoundInstance(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "app.consumer",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            tag, getErr := resolver.Get(resolver.Runtime().Context(), "app.tag")
            if nil != getErr {
                return &scopeTestService{value: "no-tag"}, nil
            }

            return &scopeTestService{value: tag.(*scopeTestService).value}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    overrideErr := scopeInstance.OverrideInstance("app.tag", &scopeTestService{value: "request-1"}, OverrideOptions{})
    if nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    _, getErr := scopeInstance.Get(t.Context(), "app.consumer")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    _ = scopeInstance.CloseWithContext(context.Background())

    fromContainer, getErr := serviceContainer.Get(t.Context(), "app.consumer")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if "no-tag" != fromContainer.(*scopeTestService).value {
        t.Fatalf("expected the container to build its own instance, got the request-bound %q", fromContainer.(*scopeTestService).value)
    }
}

type closeCountingScopeService struct {
    value      string
    closeCalls *int32
    closeErr   error
}

func (instance *closeCountingScopeService) CloseWithContext(closeContext context.Context) error {
    atomic.AddInt32(instance.closeCalls, 1)

    return instance.closeErr
}

func TestScope_CloseLeavesOverridesAndRootSingletonsAlone(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    var singletonCloseCalls int32
    var overrideCloseCalls int32

    registerErr := serviceContainer.Register(
        "app.singleton",
        func(resolver containercontract.Resolver) (*closeCountingScopeService, error) {
            return &closeCountingScopeService{
                value:      "singleton",
                closeCalls: &singletonCloseCalls,
            }, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    overrideErr := scopeInstance.OverrideInstance(
        "app.override",
        &closeCountingScopeService{
            value:      "override",
            closeCalls: &overrideCloseCalls,
        },
        OverrideOptions{},
    )
    if nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    _, getErr := scopeInstance.Get(t.Context(), "app.singleton")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    _, getErr = scopeInstance.Get(t.Context(), "app.override")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    closeErr := scopeInstance.CloseWithContext(context.Background())
    if nil != closeErr {
        t.Fatalf("unexpected scope close error: %v", closeErr)
    }

    if 0 != atomic.LoadInt32(&singletonCloseCalls) {
        t.Fatalf("expected the root container's singleton to survive the scope, got %d close calls", atomic.LoadInt32(&singletonCloseCalls))
    }

    if 0 != atomic.LoadInt32(&overrideCloseCalls) {
        t.Fatalf("expected an installed override to survive the scope, got %d close calls", atomic.LoadInt32(&overrideCloseCalls))
    }
}

type scopeLifetimeProbe struct {
    closed *atomic.Int64
}

func (instance *scopeLifetimeProbe) CloseWithContext(closeContext context.Context) error {
    instance.closed.Add(1)

    return nil
}

func TestScope_AContainerServiceStaysASingletonWhenResolvedThroughAScope(t *testing.T) {
    var buildCount atomic.Int64
    var closeCount atomic.Int64

    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "infrastructure.logger",
        func(resolver containercontract.Resolver) (string, error) {
            return "container-logger", nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    seenLogger := ""
    registerErr = serviceContainer.Register(
        "app.pool",
        func(resolver containercontract.Resolver) (*scopeLifetimeProbe, error) {
            buildCount.Add(1)

            logger, getErr := resolver.Get(resolver.Runtime().Context(), "infrastructure.logger")
            if nil != getErr {
                return nil, getErr
            }
            seenLogger, _ = logger.(string)

            return &scopeLifetimeProbe{closed: &closeCount}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    for requestIndex := 0; requestIndex < 3; requestIndex = requestIndex + 1 {
        requestScope := serviceContainer.NewScope()

        if overrideErr := requestScope.OverrideProtectedInstance("infrastructure.logger", "request-logger", OverrideOptions{}); nil != overrideErr {
            t.Fatalf("request %d: unexpected override error: %v", requestIndex, overrideErr)
        }

        if _, getErr := requestScope.Get(t.Context(), "app.pool"); nil != getErr {
            t.Fatalf("request %d: unexpected get error: %v", requestIndex, getErr)
        }

        if closeErr := requestScope.CloseWithContext(context.Background()); nil != closeErr {
            t.Fatalf("request %d: unexpected close error: %v", requestIndex, closeErr)
        }
    }

    if 1 != buildCount.Load() {
        t.Fatalf("a container singleton was built %d times for 3 requests", buildCount.Load())
    }

    if 0 != closeCount.Load() {
        t.Fatalf("a container singleton was closed %d times by requests ending; nothing outside the process may end its life", closeCount.Load())
    }

    if "container-logger" != seenLogger {
        t.Fatalf("the provider read %q: a process-lifetime service must be assembled from the container's own values, not from one request's substitutes", seenLogger)
    }
}

func TestScope_AContainerProviderCannotReachAScopeOnlyEntry(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "app.reporter",
        func(resolver containercontract.Resolver) (string, error) {
            requestContext, getErr := resolver.Get(resolver.Runtime().Context(), "request.context")
            if nil != getErr {
                return "", getErr
            }

            return requestContext.(string), nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    requestScope := serviceContainer.NewScope()
    defer requestScope.CloseWithContext(context.Background())

    if overrideErr := requestScope.OverrideProtectedInstance("request.context", "the-request", OverrideOptions{}); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if value, getErr := requestScope.Get(t.Context(), "request.context"); nil != getErr || "the-request" != value {
        t.Fatalf("expected the scope to answer for its own entry, got %v / %v", value, getErr)
    }

    _, getErr := requestScope.Get(t.Context(), "app.reporter")
    if nil == getErr {
        t.Fatal("a container provider reached a scope-only entry: it would hold one request for the life of the process")
    }
}

func TestScopeClose_TypeAliasClosesAfterDependent(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    recorder := &scopedCloseRecorder{}

    registerTransactionErr := serviceContainer.RegisterScoped(
        "app.alias.transaction",
        func(resolver containercontract.Resolver) (*recordingScopedService, error) {
            return &recordingScopedService{name: "transaction", recorder: recorder}, nil
        },
        RegisterOptions{},
    )
    if nil != registerTransactionErr {
        t.Fatalf("unexpected register error: %v", registerTransactionErr)
    }

    registerRepositoryErr := serviceContainer.RegisterScoped(
        "app.alias.repository",
        func(resolver containercontract.Resolver) (*aliasRepositoryService, error) {
            _, getErr := resolver.GetByType(resolver.Runtime().Context(), reflect.TypeOf((*recordingScopedService)(nil)))
            if nil != getErr {
                return nil, getErr
            }

            return &aliasRepositoryService{recorder: recorder}, nil
        },
        RegisterOptions{},
    )
    if nil != registerRepositoryErr {
        t.Fatalf("unexpected register error: %v", registerRepositoryErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get(t.Context(), "app.alias.repository"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    if closeErr := scopeInstance.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    recorded := recorder.recorded()
    if 2 != len(recorded) {
        t.Fatalf("expected exactly two closes, got %v", recorded)
    }

    if "repository" != recorded[0] || "transaction" != recorded[1] {
        t.Fatalf("expected the repository to close before the transaction it holds, got %v", recorded)
    }
}

type aliasRepositoryService struct {
    recorder *scopedCloseRecorder
}

func (instance *aliasRepositoryService) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record("repository")

    return nil
}

type dualFiledValueService struct {
    closeCount *int32
    padding    []string
}

func (instance dualFiledValueService) CloseWithContext(closeContext context.Context) error {
    atomic.AddInt32(instance.closeCount, 1)

    return nil
}

func TestScopeClose_DualFiledUncomparableValue_ClosedOnce(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    closeCount := int32(0)

    registerErr := serviceContainer.RegisterScoped(
        "app.alias.value",
        func(resolver containercontract.Resolver) (dualFiledValueService, error) {
            return dualFiledValueService{
                closeCount: &closeCount,
                padding:    []string{"uncomparable"},
            }, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.GetByType(t.Context(), reflect.TypeOf(dualFiledValueService{})); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    if closeErr := scopeInstance.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != atomic.LoadInt32(&closeCount) {
        t.Fatalf("expected exactly one close for the dual-filed value, got %d", atomic.LoadInt32(&closeCount))
    }
}

func TestScopeClose_EvictedCreatedInstanceClosedAtTeardown(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    recorder := &scopedCloseRecorder{}

    registerErr := serviceContainer.RegisterScoped(
        "app.evicted.service",
        func(resolver containercontract.Resolver) (*recordingScopedService, error) {
            return &recordingScopedService{name: "created", recorder: recorder}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get(t.Context(), "app.evicted.service"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    overrideErr := scopeInstance.OverrideProtectedInstance(
        "app.evicted.service",
        &recordingScopedService{name: "override", recorder: recorder},
        OverrideOptions{ClosedWithScope: true},
    )
    if nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if closeErr := scopeInstance.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    recorded := recorder.recorded()

    createdCloses := 0
    overrideCloses := 0
    for _, name := range recorded {
        if "created" == name {
            createdCloses = createdCloses + 1
        }
        if "override" == name {
            overrideCloses = overrideCloses + 1
        }
    }

    if 1 != createdCloses {
        t.Fatalf("expected the evicted created instance to be closed exactly once, got %v", recorded)
    }

    if 1 != overrideCloses {
        t.Fatalf("expected the ClosedWithScope override to be closed exactly once, got %v", recorded)
    }
}

type secondEvictedFailingService struct {
    failure error
}

func (instance *secondEvictedFailingService) CloseWithContext(closeContext context.Context) error {
    return instance.failure
}

func TestScopeClose_TwoFailingEvictedInstancesAreBothRecorded(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerFirstErr := serviceContainer.RegisterScoped(
        "app.evicted.failing.first",
        func(resolver containercontract.Resolver) (*scopeCloseFailingService, error) {
            return &scopeCloseFailingService{failure: errors.New("refusing to close app.evicted.failing.first")}, nil
        },
        RegisterOptions{},
    )
    if nil != registerFirstErr {
        t.Fatalf("unexpected register error: %v", registerFirstErr)
    }

    registerSecondErr := serviceContainer.RegisterScoped(
        "app.evicted.failing.second",
        func(resolver containercontract.Resolver) (*secondEvictedFailingService, error) {
            return &secondEvictedFailingService{failure: errors.New("refusing to close app.evicted.failing.second")}, nil
        },
        RegisterOptions{},
    )
    if nil != registerSecondErr {
        t.Fatalf("unexpected register error: %v", registerSecondErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get(t.Context(), "app.evicted.failing.first"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    if _, getErr := scopeInstance.Get(t.Context(), "app.evicted.failing.second"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    firstOverrideErr := scopeInstance.OverrideProtectedInstance(
        "app.evicted.failing.first",
        &scopeCloseFailingService{},
        OverrideOptions{ClosedWithScope: true},
    )
    if nil != firstOverrideErr {
        t.Fatalf("unexpected override error: %v", firstOverrideErr)
    }

    secondOverrideErr := scopeInstance.OverrideProtectedInstance(
        "app.evicted.failing.second",
        &secondEvictedFailingService{},
        OverrideOptions{ClosedWithScope: true},
    )
    if nil != secondOverrideErr {
        t.Fatalf("unexpected override error: %v", secondOverrideErr)
    }

    closeErr := scopeInstance.CloseWithContext(context.Background())
    if nil == closeErr {
        t.Fatalf("expected the two failing evicted closes to be reported")
    }

    typedError, isTyped := closeErr.(*exception.Error)
    if false == isTyped {
        t.Fatalf("expected an exception error, got %T", closeErr)
    }

    failures, hasFailures := typedError.Context()["failures"].(map[string]string)
    if false == hasFailures {
        t.Fatalf("expected a failures map in the close error context, got %+v", typedError.Context())
    }

    if "refusing to close app.evicted.failing.first" != failures["scope.evictedInstance[0]"] {
        t.Fatalf("expected the first evicted failure under its own key, got %+v", failures)
    }

    if "refusing to close app.evicted.failing.second" != failures["scope.evictedInstance[1]"] {
        t.Fatalf("expected the second evicted failure under its own key, got %+v", failures)
    }
}

func TestScope_MustOverrideInstance_InstallsAndNamesItsOwnFailure(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    scopeInstance := serviceContainer.NewScope()

    scopeInstance.MustOverrideInstance("app.request.substitute", &scopeTestService{value: "installed"}, OverrideOptions{})

    value, getErr := scopeInstance.Get(t.Context(), "app.request.substitute")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    service, isService := value.(*scopeTestService)
    if false == isService || "installed" != service.value {
        t.Fatalf("expected the installed override to answer, got %#v", value)
    }

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the protected name to panic")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic value, got %#v", recoveredValue)
        }

        if "failed to override service instance" != recoveredErr.Error() {
            t.Fatalf("unexpected panic message: %q", recoveredErr.Error())
        }
    }()

    scopeInstance.MustOverrideInstance("service.protected", &scopeTestService{value: "installed"}, OverrideOptions{})
}

func TestScope_MustOverrideProtectedInstance_InstallsAndNamesItsOwnFailure(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    scopeInstance := serviceContainer.NewScope()

    scopeInstance.MustOverrideProtectedInstance("service.protected", &scopeTestService{value: "installed"}, OverrideOptions{})

    value, getErr := scopeInstance.Get(t.Context(), "service.protected")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    service, isService := value.(*scopeTestService)
    if false == isService || "installed" != service.value {
        t.Fatalf("expected the protected door to install the override, got %#v", value)
    }

    if closeErr := scopeInstance.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the closed scope to panic")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic value, got %#v", recoveredValue)
        }

        if "failed to override protected service instance" != recoveredErr.Error() {
            t.Fatalf("unexpected panic message: %q", recoveredErr.Error())
        }
    }()

    scopeInstance.MustOverrideProtectedInstance("service.protected", &scopeTestService{value: "late"}, OverrideOptions{})
}

func TestScope_OverrideInstance_ProtectedNameRefused(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    scopeInstance := serviceContainer.NewScope()

    overrideErr := scopeInstance.OverrideInstance("service.protected", &scopeTestService{value: "substitute"}, OverrideOptions{})
    if nil == overrideErr {
        t.Fatalf("expected the protected name to be refused by the plain scope override door")
    }

    if "service is protected and cannot be overridden" != overrideErr.Error() {
        t.Fatalf("unexpected refusal message: %q", overrideErr.Error())
    }
}

func TestScope_OverrideProtectedInstance_EmptyNameRefused(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    scopeInstance := serviceContainer.NewScope()

    overrideErr := scopeInstance.OverrideProtectedInstance("", &scopeTestService{value: "installed"}, OverrideOptions{})
    if nil == overrideErr {
        t.Fatalf("expected an empty name to be refused")
    }

    if "service name is empty in override instance" != overrideErr.Error() {
        t.Fatalf("unexpected refusal message: %q", overrideErr.Error())
    }
}

func TestScope_OverrideProtectedInstance_NilValueRefusedInBothSpellings(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    scopeInstance := serviceContainer.NewScope()

    untypedErr := scopeInstance.OverrideProtectedInstance("app.request.substitute", nil, OverrideOptions{})
    if nil == untypedErr {
        t.Fatalf("expected an untyped nil override to be refused")
    }

    if "value is nil in override instance" != untypedErr.Error() {
        t.Fatalf("unexpected refusal message: %q", untypedErr.Error())
    }

    var typedNil *scopeTestService

    typedErr := scopeInstance.OverrideProtectedInstance("app.request.substitute", typedNil, OverrideOptions{})
    if nil == typedErr {
        t.Fatalf("expected a typed nil override to be refused")
    }

    if "value is nil in override instance" != typedErr.Error() {
        t.Fatalf("unexpected refusal message: %q", typedErr.Error())
    }
}

func TestScope_Get_EmptyNameRefused(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    scopeInstance := serviceContainer.NewScope()

    _, getErr := scopeInstance.Get(t.Context(), "")
    if nil == getErr {
        t.Fatalf("expected an empty service name to be refused")
    }

    if "service name is empty in get" != getErr.Error() {
        t.Fatalf("unexpected refusal message: %q", getErr.Error())
    }
}

func TestScope_HasAndHasType_AnswerFalseForTheEmptyNameAndTheNilType(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "app.thing",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "thing"}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if true == scopeInstance.Has("") {
        t.Fatalf("expected the empty name to be absent from a scope")
    }

    if true == scopeInstance.HasType(nil) {
        t.Fatalf("expected the nil type to be absent from a scope")
    }

    if false == scopeInstance.Has("app.thing") {
        t.Fatalf("expected the scope to see the container name")
    }

    if false == scopeInstance.HasType(reflect.TypeOf((*scopeTestService)(nil))) {
        t.Fatalf("expected the scope to see the container type")
    }
}

type scopeCloseFailingService struct {
    failure error
}

func (instance *scopeCloseFailingService) CloseWithContext(closeContext context.Context) error {
    return instance.failure
}

func TestScope_Close_ReportsAFailingServiceClose(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.scoped.failing",
        func(resolver containercontract.Resolver) (*scopeCloseFailingService, error) {
            return &scopeCloseFailingService{failure: errors.New("the connection refused to close")}, nil
        },
        RegisterOptions{},
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get(t.Context(), "app.scoped.failing"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    closeErr := scopeInstance.CloseWithContext(context.Background())
    if nil == closeErr {
        t.Fatalf("expected the failing service close to be reported")
    }

    if "failed to close scope services" != closeErr.Error() {
        t.Fatalf("unexpected close failure message: %q", closeErr.Error())
    }

    var typedError *exception.Error
    if false == errors.As(closeErr, &typedError) {
        t.Fatalf("expected a melody error, got %T", closeErr)
    }

    failures, hasFailures := typedError.Context()["failures"].(map[string]string)
    if false == hasFailures {
        t.Fatalf("expected the failures map in the scope close error context")
    }

    if 1 != len(failures) {
        t.Fatalf("expected exactly the one failing service to be recorded, got %v", failures)
    }

    for nodeKey, failureText := range failures {
        if false == strings.Contains(failureText, "the connection refused to close") {
            t.Fatalf("expected the report to carry the service's own failure, got %q", failureText)
        }

        if false == strings.Contains(nodeKey, "app.scoped.failing") {
            t.Fatalf("expected the failure to be filed under the service that produced it, got %q", nodeKey)
        }
    }
}

func TestNewScope_ANilPlanBecomesAnEmptyPlanRatherThanANilDereference(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    scopeInstance := newScope(serviceContainer, nil).(*scope)

    if nil == scopeInstance.plan {
        t.Fatalf("expected the nil plan to be replaced by an empty one")
    }

    if 0 != len(scopeInstance.plan.providers) || 0 != len(scopeInstance.plan.typeProviders) || 0 != len(scopeInstance.plan.typeRegistrationNamesByType) {
        t.Fatalf("expected the fallback plan to declare nothing")
    }

    if true == scopeInstance.Has("app.anything") {
        t.Fatalf("expected a scope on an empty plan to hold nothing")
    }

    if true == scopeInstance.HasType(reflect.TypeOf((*scopeTestService)(nil))) {
        t.Fatalf("expected a scope on an empty plan to hold no type")
    }

    if closeErr := scopeInstance.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }
}

type scopeOverrideGreeter interface {
    Greet() string
}

type containerScopeGreeter struct{}

func (instance *containerScopeGreeter) Greet() string {
    return "container"
}

type overrideScopeGreeter struct{}

func (instance *overrideScopeGreeter) Greet() string {
    return "override"
}

type scopeOverrideOutsider struct{}

func TestScope_OverridePropagatesToEveryRegisteredTypeOfTheName(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "app.greeter",
        func(resolver containercontract.Resolver) (scopeOverrideGreeter, error) {
            return &containerScopeGreeter{}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    _ = serviceContainer.MustGet(t.Context(), "app.greeter")

    requestScope := serviceContainer.NewScope()
    defer requestScope.CloseWithContext(context.Background())

    override := &overrideScopeGreeter{}
    if overrideErr := requestScope.OverrideInstance("app.greeter", override, OverrideOptions{}); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    byName := requestScope.MustGet(t.Context(), "app.greeter")
    if scopeOverrideGreeter(override) != byName {
        t.Fatalf("expected the name to answer the override, got %T", byName)
    }

    byType := requestScope.MustGetByType(t.Context(), reflect.TypeOf((*scopeOverrideGreeter)(nil)).Elem())
    if scopeOverrideGreeter(override) != byType {
        t.Fatalf("expected the type-keyed resolution to answer the override, got %T", byType)
    }
}

func TestScope_OverridePropagationDoesNotPolluteTheContainer(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "app.greeter",
        func(resolver containercontract.Resolver) (scopeOverrideGreeter, error) {
            return &containerScopeGreeter{}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    containerOwn := serviceContainer.MustGet(t.Context(), "app.greeter")

    requestScope := serviceContainer.NewScope()

    override := &overrideScopeGreeter{}
    if overrideErr := requestScope.OverrideInstance("app.greeter", override, OverrideOptions{}); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    _ = requestScope.MustGet(t.Context(), "app.greeter")
    _ = requestScope.MustGetByType(t.Context(), reflect.TypeOf((*scopeOverrideGreeter)(nil)).Elem())

    greeterType := reflect.TypeOf((*scopeOverrideGreeter)(nil)).Elem()

    if containerOwn != serviceContainer.MustGetByType(t.Context(), greeterType) {
        t.Fatalf("expected the container's type-keyed answer to stay its own instance")
    }

    otherScope := serviceContainer.NewScope()
    defer otherScope.CloseWithContext(context.Background())

    if containerOwn != otherScope.MustGetByType(t.Context(), greeterType) {
        t.Fatalf("expected a sibling scope to keep resolving the container's instance")
    }

    if closeErr := requestScope.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if containerOwn != serviceContainer.MustGetByType(t.Context(), greeterType) {
        t.Fatalf("expected the container untouched after the overriding scope closed")
    }
}

func TestScope_OverrideRefusesAValueARegisteredTypeCannotHold(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "app.greeter",
        func(resolver containercontract.Resolver) (scopeOverrideGreeter, error) {
            return &containerScopeGreeter{}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    containerOwn := serviceContainer.MustGet(t.Context(), "app.greeter")

    requestScope := serviceContainer.NewScope()
    defer requestScope.CloseWithContext(context.Background())

    overrideErr := requestScope.OverrideInstance("app.greeter", &scopeOverrideOutsider{}, OverrideOptions{})
    if nil == overrideErr {
        t.Fatalf("expected the unassignable override to be refused")
    }

    if false == strings.Contains(overrideErr.Error(), "override value is not assignable to the registered service type") {
        t.Fatalf("expected the assignability refusal, got %q", overrideErr.Error())
    }

    if containerOwn != requestScope.MustGet(t.Context(), "app.greeter") {
        t.Fatalf("expected the refused override to have written nothing")
    }
}

func TestScope_AClosedWithScopeOverrideUnderARegisteredTypeClosesOnce(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "app.probe",
        func(resolver containercontract.Resolver) (*scopeLifetimeProbe, error) {
            return &scopeLifetimeProbe{}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    requestScope := serviceContainer.NewScope()

    var closeCount atomic.Int64
    override := &scopeLifetimeProbe{closed: &closeCount}

    if overrideErr := requestScope.OverrideInstance("app.probe", override, OverrideOptions{ClosedWithScope: true}); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if closeErr := requestScope.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != closeCount.Load() {
        t.Fatalf("expected the override closed exactly once by the teardown, got %d", closeCount.Load())
    }
}

func TestScope_ClosedAnswersTheLifecycle(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    requestScope := serviceContainer.NewScope()

    closedChecker, isChecker := requestScope.(interface{ Closed() bool })
    if false == isChecker {
        t.Fatalf("expected the concrete scope to answer the closed question")
    }

    if true == closedChecker.Closed() {
        t.Fatalf("expected a fresh scope to report open")
    }

    if closeErr := requestScope.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if false == closedChecker.Closed() {
        t.Fatalf("expected a closed scope to report closed")
    }
}

func TestScope_OverridePropagationCoversTheScopedPlanLayer(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.RegisterScoped(
        "app.greeter",
        func(resolver containercontract.Resolver) (scopeOverrideGreeter, error) {
            return &containerScopeGreeter{}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    requestScope := serviceContainer.NewScope()
    defer requestScope.CloseWithContext(context.Background())

    greeterType := reflect.TypeOf((*scopeOverrideGreeter)(nil)).Elem()

    built := requestScope.MustGetByType(t.Context(), greeterType)
    if nil == built {
        t.Fatalf("expected the scoped registration to build")
    }

    override := &overrideScopeGreeter{}
    if overrideErr := requestScope.OverrideInstance("app.greeter", override, OverrideOptions{}); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if scopeOverrideGreeter(override) != requestScope.MustGetByType(t.Context(), greeterType) {
        t.Fatalf("expected the override to outrank the built scoped instance on the type key")
    }
}

func TestScope_OverridePropagationCoversTheScopeOwnRegistrations(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    requestScope := serviceContainer.NewScope()
    defer requestScope.CloseWithContext(context.Background())

    registerErr := requestScope.RegisterScoped(
        "app.greeter",
        func(resolver containercontract.Resolver) (scopeOverrideGreeter, error) {
            return &containerScopeGreeter{}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    greeterType := reflect.TypeOf((*scopeOverrideGreeter)(nil)).Elem()

    built := requestScope.MustGetByType(t.Context(), greeterType)
    if nil == built {
        t.Fatalf("expected the live-scope registration to build")
    }

    override := &overrideScopeGreeter{}
    if overrideErr := requestScope.OverrideInstance("app.greeter", override, OverrideOptions{}); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if scopeOverrideGreeter(override) != requestScope.MustGetByType(t.Context(), greeterType) {
        t.Fatalf("expected the override to outrank the built own-registration instance on the type key")
    }
}

func TestScope_TypesImplementingReturnsEmptyWhenClosed(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*scopeTestService, error) {
        return &scopeTestService{}, nil
    }, RegisterOptions{})

    scopeInstance := serviceContainer.NewScope()

    closeErr := scopeInstance.CloseWithContext(context.Background())
    if nil != closeErr {
        t.Fatalf("expected the scope to close, got %v", closeErr)
    }

    typeLister, isTypeLister := scopeInstance.(containercontract.TypeLister)
    if false == isTypeLister {
        t.Fatalf("expected the scope to enumerate types")
    }

    matches := typeLister.TypesImplementing(reflect.TypeOf((*any)(nil)).Elem())
    if 0 != len(matches) {
        t.Fatalf("expected a closed scope to enumerate nothing, got %d", len(matches))
    }
}

type aliasNameHeldService struct {
    recorder *scopedCloseRecorder
}

func (instance *aliasNameHeldService) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record("dependency")

    return nil
}

type aliasKeptResolverHolder struct {
    resolver containercontract.Resolver
    recorder *scopedCloseRecorder
}

func (instance *aliasKeptResolverHolder) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record("holder")

    return nil
}

func TestScopeClose_TypeAliasStampedAfterItsDependentClosesAfterIt(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    recorder := &scopedCloseRecorder{}

    if registerErr := serviceContainer.RegisterScoped(
        "app.alias.held",
        func(resolver containercontract.Resolver) (*aliasNameHeldService, error) {
            return &aliasNameHeldService{recorder: recorder}, nil
        },
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.RegisterScoped(
        "app.alias.holder",
        func(resolver containercontract.Resolver) (*aliasKeptResolverHolder, error) {
            return &aliasKeptResolverHolder{resolver: resolver, recorder: recorder}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    holderValue, getErr := scopeInstance.Get(t.Context(), "app.alias.holder")
    if nil != getErr {
        t.Fatalf("unexpected holder resolution error: %v", getErr)
    }

    holder, isHolder := holderValue.(*aliasKeptResolverHolder)
    if false == isHolder {
        t.Fatalf("expected the holder, got %#v", holderValue)
    }

    if _, heldErr := holder.resolver.GetByType(t.Context(), reflect.TypeOf((*aliasNameHeldService)(nil))); nil != heldErr {
        t.Fatalf("unexpected late resolution error: %v", heldErr)
    }

    if closeErr := scopeInstance.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    recorded := recorder.recorded()
    if 2 != len(recorded) {
        t.Fatalf("expected exactly two closes, got %v", recorded)
    }

    if "holder" != recorded[0] || "dependency" != recorded[1] {
        t.Fatalf("expected the holder to close before the service its type alias also files, got %v", recorded)
    }
}

func TestScopeClose_TypeAliasOfAnOverrideClosesAfterItsDependent(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    recorder := &scopedCloseRecorder{}

    if registerErr := serviceContainer.RegisterScoped(
        "app.alias.overridden",
        func(resolver containercontract.Resolver) (*aliasNameHeldService, error) {
            return &aliasNameHeldService{recorder: recorder}, nil
        },
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.RegisterScoped(
        "app.alias.overrideholder",
        func(resolver containercontract.Resolver) (*aliasKeptResolverHolder, error) {
            return &aliasKeptResolverHolder{resolver: resolver, recorder: recorder}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    holderValue, getErr := scopeInstance.Get(t.Context(), "app.alias.overrideholder")
    if nil != getErr {
        t.Fatalf("unexpected holder resolution error: %v", getErr)
    }

    holder, isHolder := holderValue.(*aliasKeptResolverHolder)
    if false == isHolder {
        t.Fatalf("expected the holder, got %#v", holderValue)
    }

    if overrideErr := scopeInstance.OverrideInstance(
        "app.alias.overridden",
        &aliasNameHeldService{recorder: recorder},
        OverrideOptions{ClosedWithScope: true},
    ); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if _, overriddenErr := holder.resolver.Get(t.Context(), "app.alias.overridden"); nil != overriddenErr {
        t.Fatalf("unexpected late resolution error: %v", overriddenErr)
    }

    if closeErr := scopeInstance.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    recorded := recorder.recorded()
    if 2 != len(recorded) {
        t.Fatalf("expected exactly two closes, got %v", recorded)
    }

    if "holder" != recorded[0] || "dependency" != recorded[1] {
        t.Fatalf("expected the holder to close before the override its type alias also files, got %v", recorded)
    }
}

type aliasGroupSpanningService struct {
    label    string
    recorder *scopedCloseRecorder
}

func (instance *aliasGroupSpanningService) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record(instance.label)

    return nil
}

type aliasGroupMiddleService struct {
    recorder *scopedCloseRecorder
}

func (instance *aliasGroupMiddleService) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record("middle")

    return nil
}

func TestScopeClose_AnAliasGroupIsAsOldAsItsOldestMember(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    recorder := &scopedCloseRecorder{}

    if registerErr := serviceContainer.RegisterScoped(
        "app.aaa.shared",
        func(resolver containercontract.Resolver) (*aliasGroupSpanningService, error) {
            return &aliasGroupSpanningService{label: "original", recorder: recorder}, nil
        },
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.RegisterScoped(
        "app.middle",
        func(resolver containercontract.Resolver) (*aliasGroupMiddleService, error) {
            return &aliasGroupMiddleService{recorder: recorder}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get(t.Context(), "app.aaa.shared"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := scopeInstance.Get(t.Context(), "app.middle"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if overrideErr := scopeInstance.OverrideInstance(
        "app.aaa.shared",
        &aliasGroupSpanningService{label: "installed", recorder: recorder},
        OverrideOptions{ClosedWithScope: true},
    ); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if closeErr := scopeInstance.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    recorded := recorder.recorded()
    if 3 != len(recorded) {
        t.Fatalf("expected three closes, got %v", recorded)
    }

    if "middle" != recorded[0] || "installed" != recorded[1] {
        t.Fatalf("expected the alias group to close as old as its oldest member, got %v", recorded)
    }

    if "original" != recorded[2] {
        t.Fatalf("expected the evicted value to close last, got %v", recorded)
    }
}

func waitUntilParkedOnALock(t *testing.T, frame string) {
    t.Helper()

    buffer := make([]byte, 1<<20)
    deadline := time.Now().Add(5 * time.Second)
    for time.Now().Before(deadline) {
        written := runtime.Stack(buffer, true)
        for _, goroutineStack := range strings.Split(string(buffer[:written]), "\n\n") {
            header, _, _ := strings.Cut(goroutineStack, "\n")
            parked := strings.Contains(header, "[sync.Mutex.Lock") || strings.Contains(header, "[sync.RWMutex.Lock") || strings.Contains(header, "[sync.RWMutex.RLock")
            if true == parked && true == strings.Contains(goroutineStack, frame) {
                return
            }
        }

        time.Sleep(time.Millisecond)
    }

    t.Fatalf("expected a goroutine in %s parked on a lock", frame)
}

func TestScope_OverrideClosedDuringTheLockHandOffIsStillRefused(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    containerInstance := serviceContainer.(*container)

    scopeInstance := serviceContainer.NewScope().(*scope)

    overrideEntered := make(chan struct{})
    overrideDone := make(chan error, 1)

    containerInstance.mutex.Lock()

    go func() {
        close(overrideEntered)

        overrideDone <- scopeInstance.OverrideProtectedInstance("service.after_hand_off", &scopeTestService{value: "late"}, OverrideOptions{})
    }()

    <-overrideEntered
    waitUntilParkedOnALock(t, "container.(*scope).OverrideProtectedInstance(")

    scopeInstance.container.Store(nil)
    containerInstance.mutex.Unlock()

    overrideErr := <-overrideDone
    if nil == overrideErr {
        t.Fatalf("expected the override to be refused by the scope that closed under it")
    }

    if false == errors.Is(overrideErr, ErrScopeClosed) {
        t.Fatalf("expected the refusal to classify as ErrScopeClosed")
    }

    if "lockHandOff" != refusalStageOf(t, overrideErr) {
        t.Fatalf("expected the guard after the lock hand-off to answer, got %q", refusalStageOf(t, overrideErr))
    }

    if melodyErr, isMelodyErr := overrideErr.(*exception.Error); false == isMelodyErr || "service.after_hand_off" != melodyErr.Context()["serviceName"] {
        t.Fatalf("expected the refusal behind the lock to name the service, got %v", overrideErr)
    }

    scopeInstance.mutex.RLock()
    _, nameWritten := scopeInstance.instances["service.after_hand_off"]
    scopeInstance.mutex.RUnlock()

    if true == nameWritten {
        t.Fatalf("expected nothing to be written into a scope that closed during the hand-off")
    }
}

func TestScope_GetClosedAfterTheEntryCheckIsRefusedByTheLookup(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    scopeInstance := serviceContainer.NewScope().(*scope)

    getEntered := make(chan struct{})
    getDone := make(chan error, 1)

    scopeInstance.mutex.Lock()

    go func() {
        close(getEntered)

        _, getErr := scopeInstance.Get(t.Context(), "service.absent")
        getDone <- getErr
    }()

    <-getEntered
    waitUntilParkedOnALock(t, "container.(*scope).Get(")

    scopeInstance.container.Store(nil)
    scopeInstance.mutex.Unlock()

    getErr := <-getDone
    if nil == getErr {
        t.Fatalf("expected the resolution to be refused by the scope that closed under it")
    }

    if false == errors.Is(getErr, ErrScopeClosed) {
        t.Fatalf("expected the refusal to classify as ErrScopeClosed, got %v", getErr)
    }
}

func TestScope_GetByTypeClosedAfterTheEntryCheckIsRefusedByTheLookup(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    scopeInstance := serviceContainer.NewScope().(*scope)

    getEntered := make(chan struct{})
    getDone := make(chan error, 1)

    scopeInstance.mutex.Lock()

    go func() {
        close(getEntered)

        _, getByTypeErr := scopeInstance.GetByType(t.Context(), reflect.TypeOf((*scopeTestService)(nil)))
        getDone <- getByTypeErr
    }()

    <-getEntered
    waitUntilParkedOnALock(t, "container.(*scope).GetByType(")

    scopeInstance.container.Store(nil)
    scopeInstance.mutex.Unlock()

    getByTypeErr := <-getDone
    if nil == getByTypeErr {
        t.Fatalf("expected the by-type resolution to be refused by the scope that closed under it")
    }

    if false == errors.Is(getByTypeErr, ErrScopeClosed) {
        t.Fatalf("expected the refusal to classify as ErrScopeClosed, got %v", getByTypeErr)
    }
}

type scopeContextDoorService struct {
    closeCalls            int
    closeWithContextCalls int
    contextErr            error
}

func (instance *scopeContextDoorService) Close() error {
    instance.closeCalls = instance.closeCalls + 1

    return nil
}

func (instance *scopeContextDoorService) CloseWithContext(closeContext context.Context) error {
    instance.closeWithContextCalls = instance.closeWithContextCalls + 1
    instance.contextErr = closeContext.Err()

    return nil
}

type scopeContextDoorPanickingService struct{}

func (instance *scopeContextDoorPanickingService) Close() error {
    return nil
}

func (instance *scopeContextDoorPanickingService) CloseWithContext(closeContext context.Context) error {
    panic("the context door panicked")
}

func newScopeWithContextDoorService(t *testing.T) (containercontract.Scope, *scopeContextDoorService) {
    t.Helper()

    serviceContainer := NewContainer(ContainerConfig{})
    service := &scopeContextDoorService{}

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.scoped.contextDoor",
        func(resolver containercontract.Resolver) (*scopeContextDoorService, error) {
            return service, nil
        },
        RegisterOptions{},
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get(t.Context(), "app.scoped.contextDoor"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    return scopeInstance, service
}

func TestScope_Close_PrefersTheContextDoorOfAScopedService(t *testing.T) {
    scopeInstance, service := newScopeWithContextDoorService(t)

    if closeErr := scopeInstance.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != service.closeWithContextCalls || 0 != service.closeCalls {
        t.Fatalf("expected the scope to close the service through CloseWithContext once and Close never, got CloseWithContext=%d Close=%d", service.closeWithContextCalls, service.closeCalls)
    }

    if nil != service.contextErr {
        t.Fatalf("expected a plain Close to hand the service a context with no term, got %v", service.contextErr)
    }
}

func TestScope_CloseWithContext_HandsTheCallersContextToTheService(t *testing.T) {
    scopeInstance, service := newScopeWithContextDoorService(t)

    contextCloser, isContextCloser := scopeInstance.(interface {
        CloseWithContext(closeContext context.Context) error
    })
    if false == isContextCloser {
        t.Fatalf("expected the scope to carry CloseWithContext")
    }

    spentContext, cancel := context.WithCancel(context.Background())
    cancel()

    if closeErr := contextCloser.CloseWithContext(spentContext); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != service.closeWithContextCalls {
        t.Fatalf("expected CloseWithContext to be taken once, got %d", service.closeWithContextCalls)
    }

    if false == errors.Is(service.contextErr, context.Canceled) {
        t.Fatalf("expected the service to be handed the caller's spent context, got %v", service.contextErr)
    }

    if false == scopeInstance.(*scope).Closed() {
        t.Fatalf("expected CloseWithContext to end the scope the way Close does")
    }
}

func TestScope_Close_ContainsAPanickingContextDoor(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.scoped.panickingContextDoor",
        func(resolver containercontract.Resolver) (*scopeContextDoorPanickingService, error) {
            return &scopeContextDoorPanickingService{}, nil
        },
        RegisterOptions{},
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get(t.Context(), "app.scoped.panickingContextDoor"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    closeErr := scopeInstance.CloseWithContext(context.Background())
    if nil == closeErr {
        t.Fatalf("expected the panicking context door to be reported as a failed close")
    }

    var typedError *exception.Error
    if false == errors.As(closeErr, &typedError) {
        t.Fatalf("expected a melody error, got %T", closeErr)
    }

    failures, hasFailures := typedError.Context()["failures"].(map[string]string)
    if false == hasFailures || 1 != len(failures) {
        t.Fatalf("expected exactly the one contained panic to be recorded, got %v", typedError.Context()["failures"])
    }

    for _, failureText := range failures {
        if false == strings.Contains(failureText, "service close panicked") {
            t.Fatalf("expected the record to name the contained panic, got %q", failureText)
        }
    }
}

func TestScope_Close_CarriesTheFailureDetailsOfAPanickingCloseBesideItsLine(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.scoped.panics",
        func(resolver containercontract.Resolver) (*panickingCloseWithCauseService, error) {
            return &panickingCloseWithCauseService{cause: errors.New("the drain buffer was nil")}, nil
        },
        RegisterOptions{},
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get(t.Context(), "app.scoped.panics"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    closeErr := scopeInstance.CloseWithContext(context.Background())
    if nil == closeErr {
        t.Fatalf("expected the panicking close to be reported")
    }

    var typedError *exception.Error
    if false == errors.As(closeErr, &typedError) {
        t.Fatalf("expected a melody error, got %T", closeErr)
    }

    assertCloseFailureDetails(t, typedError.Context(), "scope:service:app.scoped.panics")
}

type scopedContextDoorOnlyService struct {
    closes      int
    hadDeadline bool
}

func (instance *scopedContextDoorOnlyService) CloseWithContext(closeContext context.Context) error {
    instance.closes = instance.closes + 1
    _, instance.hadDeadline = closeContext.Deadline()

    return nil
}

func TestScope_Close_ClosesAServiceThatCarriesOnlyCloseWithContextAndHandsItTheDeadline(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    service := &scopedContextDoorOnlyService{}

    MustRegisterScoped[*scopedContextDoorOnlyService](
        serviceContainer,
        "scoped.context.door.only",
        func(resolver containercontract.Resolver) (*scopedContextDoorOnlyService, error) {
            return service, nil
        },
        RegisterOptions{},
    )

    scopeInstance := serviceContainer.NewScope()
    _ = MustFromResolver[*scopedContextDoorOnlyService](t.Context(), scopeInstance, "scoped.context.door.only")

    closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()

    if closeErr := scopeInstance.(containercontract.ContextCloser).CloseWithContext(closeContext); nil != closeErr {
        t.Fatalf("expected a clean scope close, got %v", closeErr)
    }

    if 1 != service.closes {
        t.Fatalf("expected the scoped service carrying only CloseWithContext to be closed once, got %d closes", service.closes)
    }

    if false == service.hadDeadline {
        t.Fatalf("expected the scope's deadline to reach the service through its only door")
    }
}

func TestScope_Close_ACloseErrorWhoseUnwrapPanicsDoesNotEndTheTeardown(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.scoped.unwrapPanics",
        func(resolver containercontract.Resolver) (*panickingUnwrapCloseService, error) {
            return &panickingUnwrapCloseService{}, nil
        },
        RegisterOptions{},
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get(t.Context(), "app.scoped.unwrapPanics"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    var typedError *exception.Error
    if false == errors.As(scopeInstance.CloseWithContext(context.Background()), &typedError) {
        t.Fatalf("expected the failed close to be reported rather than ending the teardown")
    }

    failureDetails, _ := typedError.Context()["failureDetails"].(map[string]exceptioncontract.Context)
    if cause := failureDetails["scope:service:app.scoped.unwrapPanics"]["cause"]; "the links below could not be read, their Unwrap panicked: unwrap of a half-built error" != cause {
        t.Fatalf("expected the details to say where the chain was cut and why, got %v", failureDetails)
    }
}

func TestScope_CloseWithContext_HandsAContextWithinTheCallersToTheEvictedAndTheReplacementInstance(t *testing.T) {
    for _, expired := range []bool{false, true} {
        t.Run(map[bool]string{false: "live", true: "expired"}[expired], func(t *testing.T) {
            closeContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Minute))
            defer cancel()
            if true == expired {
                cancel()
            }

            serviceContainer := NewContainer(ContainerConfig{})
            original := &contextClosingScopedProbe{}
            replacement := &contextClosingScopedProbe{}
            registerErr := serviceContainer.RegisterScoped("probe", func(containercontract.Resolver) (*contextClosingScopedProbe, error) {
                return original, nil
            }, RegisterOptions{})
            if nil != registerErr {
                t.Fatalf("register: %v", registerErr)
            }

            requestScope := serviceContainer.NewScope()
            requestScope.MustGetByType(closeContext, reflect.TypeOf(original))

            overrideErr := requestScope.OverrideInstance("probe", replacement, OverrideOptions{ClosedWithScope: true})
            if nil != overrideErr {
                t.Fatalf("override: %v", overrideErr)
            }

            closeErr := requestScope.(*scope).CloseWithContext(closeContext)
            if nil != closeErr {
                t.Fatalf("close: %v", closeErr)
            }
            secondCloseErr := requestScope.CloseWithContext(context.Background())
            if nil != secondCloseErr {
                t.Fatalf("second close: %v", secondCloseErr)
            }

            for label, probe := range map[string]*contextClosingScopedProbe{"evicted": original, "replacement": replacement} {
                if 0 != probe.plainCalls || 1 != len(probe.contexts) {
                    t.Fatalf("expected the %s instance closed once through CloseWithContext, got %d context and %d plain closes", label, len(probe.contexts), probe.plainCalls)
                }
                callerDeadline, _ := closeContext.Deadline()
                if probeDeadline, hasDeadline := probe.contexts[0].Deadline(); false == hasDeadline || true == probeDeadline.After(callerDeadline) {
                    t.Fatalf("expected the %s instance to receive a context within the caller's, got %v", label, probeDeadline)
                }
                if true == expired && false == errors.Is(context.Cause(probe.contexts[0]), context.Canceled) {
                    t.Fatalf("expected the %s instance to observe the caller's cancellation", label)
                }
            }
        })
    }
}

func TestScope_ARingClosedThroughALazyHandleIsRefusedAtTheResolutionThatClosesIt(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    if registerErr := serviceContainer.RegisterScoped(
        "app.scoped.lazy.holder",
        func(resolver containercontract.Resolver) (*lazyRingHolder, error) {
            return &lazyRingHolder{recorder: recorder, partner: Lazy[*lazyRingPartner](resolver, "app.scoped.lazy.partner")}, nil
        },
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.RegisterScoped(
        "app.scoped.lazy.partner",
        func(resolver containercontract.Resolver) (*lazyRingPartner, error) {
            holder, getErr := FromResolver[*lazyRingHolder](resolver.Runtime().Context(), resolver, "app.scoped.lazy.holder")
            if nil != getErr {
                return nil, getErr
            }

            return &lazyRingPartner{recorder: recorder, holder: holder}, nil
        },
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    holder, getErr := FromResolver[*lazyRingHolder](t.Context(), scopeInstance, "app.scoped.lazy.holder")
    if nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    if _, resolveErr := holder.partner.resolveOver(t.Context()); false == errors.Is(resolveErr, ErrCircularDependency) {
        t.Fatalf("expected the lazy resolution closing the scoped ring refused with ErrCircularDependency, got %v", resolveErr)
    }

    if closeErr := scopeInstance.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("expected a clean scope teardown, got %v", closeErr)
    }

    if 1 != len(closeSequence) || "holder" != closeSequence[0] {
        t.Fatalf("expected the holder alone closed, got %v", closeSequence)
    }
}

func TestScope_ARingClosedOntoAScopedCreationInFlightIsRefused(t *testing.T) {
    for _, resolution := range []struct {
        name    string
        resolve func(ctx context.Context, resolver containercontract.Resolver) error
    }{
        {name: "byName", resolve: func(ctx context.Context, resolver containercontract.Resolver) error {
            _, getErr := resolver.Get(ctx, "app.scoped.first")
            return getErr
        }},
        {name: "byType", resolve: func(ctx context.Context, resolver containercontract.Resolver) error {
            _, getErr := resolver.GetByType(ctx, reflect.TypeFor[*lazyRingHolder]())
            return getErr
        }},
    } {
        t.Run(resolution.name, func(t *testing.T) {
            serviceContainer := NewContainer(ContainerConfig{}).(*container)

            firstEntered := make(chan struct{})
            secondEntered := make(chan struct{})
            releaseFirst := make(chan struct{})
            releaseSecond := make(chan struct{})

            serviceContainer.MustRegisterScoped(
                "app.scoped.first",
                func(resolver containercontract.Resolver) (*lazyRingHolder, error) {
                    close(firstEntered)
                    <-releaseFirst

                    if _, getErr := resolver.Get(resolver.Runtime().Context(), "app.scoped.second"); nil != getErr {
                        return nil, getErr
                    }

                    return &lazyRingHolder{}, nil
                },
                RegisterOptions{},
            )

            serviceContainer.MustRegisterScoped(
                "app.scoped.second",
                func(resolver containercontract.Resolver) (*lazyRingPartner, error) {
                    close(secondEntered)
                    <-releaseSecond

                    if resolveErr := resolution.resolve(resolver.Runtime().Context(), resolver); nil != resolveErr {
                        return nil, resolveErr
                    }

                    return &lazyRingPartner{}, nil
                },
                RegisterOptions{},
            )

            scopeInstance := serviceContainer.NewScope().(*scope)

            firstDone := make(chan error, 1)
            go func() {
                _, firstErr := scopeInstance.Get(t.Context(), "app.scoped.first")
                firstDone <- firstErr
            }()

            secondDone := make(chan error, 1)
            go func() {
                _, secondErr := scopeInstance.Get(t.Context(), "app.scoped.second")
                secondDone <- secondErr
            }()

            <-firstEntered
            <-secondEntered

            close(releaseFirst)
            awaitScopedCreationWaiter(t, serviceContainer, scopeInstance, "app.scoped.second", 1)

            close(releaseSecond)

            if secondErr := <-secondDone; false == errors.Is(secondErr, ErrCircularDependency) {
                t.Fatalf("expected the resolution closing the ring refused with ErrCircularDependency, got %v", secondErr)
            }

            if firstErr := <-firstDone; nil == firstErr {
                t.Fatalf("expected the waiting resolution to fail with the refused creation")
            }
        })
    }
}

func TestScope_AFailedScopedLookupLeavesNoDependency(t *testing.T) {
    for _, lookup := range []struct {
        name    string
        resolve func(ctx context.Context, resolver containercontract.Resolver)
    }{
        {name: "byName", resolve: func(ctx context.Context, resolver containercontract.Resolver) {
            _, _ = resolver.Get(ctx, "app.scoped.optional.dependency")
        }},
        {name: "byType", resolve: func(ctx context.Context, resolver containercontract.Resolver) {
            _, _ = resolver.GetByType(ctx, reflect.TypeFor[*lazyRingPartner]())
        }},
    } {
        t.Run(lookup.name, func(t *testing.T) {
            serviceContainer := NewContainer(ContainerConfig{})

            var dependencyAttempts atomic.Int64

            serviceContainer.MustRegisterScoped(
                "app.scoped.optional.holder",
                func(resolver containercontract.Resolver) (*lazyRingHolder, error) {
                    lookup.resolve(resolver.Runtime().Context(), resolver)

                    return &lazyRingHolder{}, nil
                },
                RegisterOptions{},
            )

            serviceContainer.MustRegisterScoped(
                "app.scoped.optional.dependency",
                func(resolver containercontract.Resolver) (*lazyRingPartner, error) {
                    if 1 == dependencyAttempts.Add(1) {
                        return nil, errors.New("first attempt fails")
                    }

                    if _, getErr := resolver.Get(resolver.Runtime().Context(), "app.scoped.optional.holder"); nil != getErr {
                        return nil, getErr
                    }

                    return &lazyRingPartner{}, nil
                },
                RegisterOptions{},
            )

            scopeInstance := serviceContainer.NewScope()

            if _, getErr := scopeInstance.Get(t.Context(), "app.scoped.optional.holder"); nil != getErr {
                t.Fatalf("unexpected resolution error: %v", getErr)
            }

            if _, getErr := scopeInstance.Get(t.Context(), "app.scoped.optional.dependency"); nil != getErr {
                t.Fatalf("expected the dependency to resolve the holder, the failed lookup having left no edge, got %v", getErr)
            }
        })
    }
}

func TestScope_ARingClosedThroughALazyByTypeHandleIsRefused(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegisterScoped(
        "app.scoped.lazy.holder",
        func(resolver containercontract.Resolver) (*lazyRingHolder, error) {
            return &lazyRingHolder{partnerByType: LazyByType[*lazyRingPartner](resolver)}, nil
        },
        RegisterOptions{},
    )

    serviceContainer.MustRegisterScoped(
        "app.scoped.lazy.partner",
        func(resolver containercontract.Resolver) (*lazyRingPartner, error) {
            if _, getErr := resolver.Get(resolver.Runtime().Context(), "app.scoped.lazy.holder"); nil != getErr {
                return nil, getErr
            }

            return &lazyRingPartner{}, nil
        },
        RegisterOptions{},
    )

    scopeInstance := serviceContainer.NewScope()

    holder, getErr := FromResolver[*lazyRingHolder](t.Context(), scopeInstance, "app.scoped.lazy.holder")
    if nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    if _, resolveErr := holder.partnerByType.resolveOver(t.Context()); false == errors.Is(resolveErr, ErrCircularDependency) {
        t.Fatalf("expected the by-type lazy resolution closing the ring refused with ErrCircularDependency, got %v", resolveErr)
    }

    if closeErr := scopeInstance.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("expected a clean scope teardown, got %v", closeErr)
    }
}

func TestScope_ARingClosedByTypeOntoABuiltInstanceIsRefused(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegisterScoped(
        "app.scoped.lazy.holder",
        func(resolver containercontract.Resolver) (*lazyRingHolder, error) {
            return &lazyRingHolder{partner: Lazy[*lazyRingPartner](resolver, "app.scoped.lazy.partner")}, nil
        },
        RegisterOptions{},
    )

    serviceContainer.MustRegisterScoped(
        "app.scoped.lazy.partner",
        func(resolver containercontract.Resolver) (*lazyRingPartner, error) {
            if _, getErr := resolver.GetByType(resolver.Runtime().Context(), reflect.TypeFor[*lazyRingHolder]()); nil != getErr {
                return nil, getErr
            }

            return &lazyRingPartner{}, nil
        },
        RegisterOptions{},
    )

    scopeInstance := serviceContainer.NewScope()

    holder, getErr := FromResolverByType[*lazyRingHolder](t.Context(), scopeInstance)
    if nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    if _, resolveErr := holder.partner.resolveOver(t.Context()); false == errors.Is(resolveErr, ErrCircularDependency) {
        t.Fatalf("expected the lazy resolution closing the ring by type refused with ErrCircularDependency, got %v", resolveErr)
    }
}

func TestScope_OverrideOnAClosedScopeNamesTheServiceAtEntry(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    scopeInstance := serviceContainer.NewScope().(*scope)
    scopeInstance.container.Store(nil)

    overrideErr := scopeInstance.OverrideProtectedInstance("service.closed", &scopeTestService{value: "late"}, OverrideOptions{})
    if false == errors.Is(overrideErr, ErrScopeClosed) {
        t.Fatalf("expected the override to be refused as ErrScopeClosed, got %v", overrideErr)
    }

    melodyErr, isMelodyErr := overrideErr.(*exception.Error)
    if false == isMelodyErr || "entry" != melodyErr.Context()["refusedAt"] || "service.closed" != melodyErr.Context()["serviceName"] {
        t.Fatalf("expected the entry refusal to name the service, got %v", overrideErr)
    }
}

func TestScope_MustOverrideDoors_CarryTheOptionsToTheTeardown(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        override func(requestScope containercontract.Scope, value any, options OverrideOptions)
    }{
        {name: "MustOverrideInstance", override: func(requestScope containercontract.Scope, value any, options OverrideOptions) {
            requestScope.MustOverrideInstance("app.probe", value, options)
        }},
        {name: "MustOverrideProtectedInstance", override: func(requestScope containercontract.Scope, value any, options OverrideOptions) {
            requestScope.MustOverrideProtectedInstance("app.probe", value, options)
        }},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            serviceContainer := NewContainer(ContainerConfig{})
            serviceContainer.MustRegister(
                "app.probe",
                func(resolver containercontract.Resolver) (*scopeLifetimeProbe, error) {
                    return &scopeLifetimeProbe{}, nil
                },
                RegisterOptions{},
            )

            for _, options := range []OverrideOptions{{ClosedWithScope: true}, {}} {
                requestScope := serviceContainer.NewScope()

                var closeCount atomic.Int64
                testCase.override(requestScope, &scopeLifetimeProbe{closed: &closeCount}, options)

                if closeErr := requestScope.CloseWithContext(context.Background()); nil != closeErr {
                    t.Fatalf("unexpected close error: %v", closeErr)
                }

                expectedCloses := int64(0)
                if true == options.ClosedWithScope {
                    expectedCloses = 1
                }

                if expectedCloses != closeCount.Load() {
                    t.Fatalf("expected %d closes for %+v, got %d", expectedCloses, options, closeCount.Load())
                }
            }
        })
    }
}

type deadlineRecordingScopedService struct {
    deadline    time.Time
    hasDeadline bool
    closed      chan struct{}
}

func (instance *deadlineRecordingScopedService) CloseWithContext(closeContext context.Context) error {
    instance.deadline, instance.hasDeadline = closeContext.Deadline()
    close(instance.closed)

    return nil
}

func newScopeWithDeadlineRecordingService(t *testing.T, config ContainerConfig) (containercontract.Scope, *deadlineRecordingScopedService) {
    t.Helper()

    service := &deadlineRecordingScopedService{closed: make(chan struct{})}

    serviceContainer := NewContainer(config)
    MustRegisterScoped[*deadlineRecordingScopedService](
        serviceContainer,
        "scoped.recording",
        func(resolver containercontract.Resolver) (*deadlineRecordingScopedService, error) {
            return service, nil
        },
        containercontract.RegisterOptions{},
    )

    scopeInstance := serviceContainer.NewScope()
    if _, resolveErr := FromResolver[*deadlineRecordingScopedService](context.Background(), scopeInstance, "scoped.recording"); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    return scopeInstance, service
}

func TestScope_CloseHandsItsServicesTheConfiguredTeardownTimeout(t *testing.T) {
    scopeInstance, service := newScopeWithDeadlineRecordingService(t, ContainerConfig{ScopeTeardownTimeout: bound.Of(40 * time.Second)})

    before := time.Now()
    if closeErr := scopeInstance.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if false == service.hasDeadline {
        t.Fatalf("expected the service to be handed a deadline")
    }

    if remaining := service.deadline.Sub(before); remaining < 39*time.Second || remaining > 41*time.Second {
        t.Fatalf("expected a deadline about 40s away, got %s", remaining)
    }
}

func TestScope_CloseHandsItsServicesTheDefaultTeardownTimeoutWhenLeftOut(t *testing.T) {
    scopeInstance, service := newScopeWithDeadlineRecordingService(t, ContainerConfig{})

    before := time.Now()
    if closeErr := scopeInstance.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if remaining := service.deadline.Sub(before); false == service.hasDeadline || remaining > DefaultScopeTeardownTimeout+time.Second || remaining < DefaultScopeTeardownTimeout-time.Second {
        t.Fatalf("expected the default deadline, got %v away (has deadline: %v)", remaining, service.hasDeadline)
    }
}

func TestScope_CloseHandsNoDeadlineUnderAnUnlimitedTeardownTimeout(t *testing.T) {
    scopeInstance, service := newScopeWithDeadlineRecordingService(t, ContainerConfig{ScopeTeardownTimeout: bound.UnlimitedDuration()})

    if closeErr := scopeInstance.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if true == service.hasDeadline {
        t.Fatalf("expected no deadline under an unlimited timeout, got %v", service.deadline)
    }
}

func TestScope_CloseWithContextAppliesTheConfiguredBoundWithinTheCallers(t *testing.T) {
    for _, testCase := range []struct {
        name             string
        callerTerm       time.Duration
        expectedDeadline time.Duration
    }{
        {name: "the caller's shorter deadline ends it", callerTerm: time.Second, expectedDeadline: time.Second},
        {name: "the configured bound ends it under a longer caller deadline", callerTerm: 5 * time.Minute, expectedDeadline: 40 * time.Second},
        {name: "the configured bound ends it under no caller deadline", callerTerm: 0, expectedDeadline: 40 * time.Second},
    } {
        scopeInstance, service := newScopeWithDeadlineRecordingService(t, ContainerConfig{ScopeTeardownTimeout: bound.Of(40 * time.Second)})

        closeContext := context.Background()
        if 0 < testCase.callerTerm {
            var cancel context.CancelFunc
            closeContext, cancel = context.WithTimeout(context.Background(), testCase.callerTerm)
            defer cancel()
        }

        if closeErr := scopeInstance.CloseWithContext(closeContext); nil != closeErr {
            t.Fatalf("%s: unexpected close error: %v", testCase.name, closeErr)
        }

        if false == service.hasDeadline {
            t.Fatalf("%s: expected a deadline", testCase.name)
        }

        if remaining := time.Until(service.deadline); remaining > testCase.expectedDeadline || remaining < testCase.expectedDeadline-5*time.Second {
            t.Fatalf("%s: expected a deadline about %v away, got %v", testCase.name, testCase.expectedDeadline, remaining)
        }
    }
}

func TestScope_AFailurePastTheConfiguredBoundNamesIt(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{ScopeTeardownTimeout: bound.Of(time.Millisecond)})
    MustRegisterScoped[*contextWaitingScopedService](serviceContainer, "waiting", func(containercontract.Resolver) (*contextWaitingScopedService, error) {
        return &contextWaitingScopedService{}, nil
    }, RegisterOptions{})

    requestScope := serviceContainer.NewScope()
    requestScope.MustGet(context.Background(), "waiting")

    closeErr := requestScope.CloseWithContext(context.Background())
    if nil == closeErr || "ContainerConfig.ScopeTeardownTimeout" != exception.FromError(closeErr).Context()["bound"] {
        t.Fatalf("expected the failure to name the configured bound, got %v", closeErr)
    }

    callerScope := serviceContainer.NewScope()
    callerScope.MustGet(context.Background(), "waiting")

    callerContext, cancel := context.WithCancel(context.Background())
    cancel()

    callerErr := callerScope.CloseWithContext(callerContext)
    if nil == callerErr || nil != exception.FromError(callerErr).Context()["bound"] {
        t.Fatalf("expected a close the caller ended not to name the configured bound, got %v", callerErr)
    }
}

var _ interface {
    CloseWithContext(closeContext context.Context) error
} = containercontract.Scope(nil)

func TestScope_HasNoPlainClose(t *testing.T) {
    var scopeInstance any = NewContainer(ContainerConfig{}).NewScope()

    if _, hasPlainClose := scopeInstance.(interface{ Close() error }); true == hasPlainClose {
        t.Fatalf("expected the scope to have one close door, CloseWithContext")
    }
}

type contextWaitingScopedService struct{}

func (instance *contextWaitingScopedService) CloseWithContext(closeContext context.Context) error {
    <-closeContext.Done()

    return closeContext.Err()
}

func TestScope_CloseReturnsAtTheTimeoutAndReportsTheServiceThatDidNotFinish(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{ScopeTeardownTimeout: bound.Of(50 * time.Millisecond)})
    MustRegisterScoped[*contextWaitingScopedService](
        serviceContainer,
        "scoped.waiting",
        func(resolver containercontract.Resolver) (*contextWaitingScopedService, error) {
            return &contextWaitingScopedService{}, nil
        },
        containercontract.RegisterOptions{},
    )

    scopeInstance := serviceContainer.NewScope()
    if _, resolveErr := FromResolver[*contextWaitingScopedService](context.Background(), scopeInstance, "scoped.waiting"); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    closed := make(chan error, 1)
    go func() {
        closed <- scopeInstance.CloseWithContext(context.Background())
    }()

    /* under the 3s default, so a close running on the default instead of the configured term is caught */
    select {
    case closeErr := <-closed:
        if nil == closeErr {
            t.Fatalf("expected the service that ran out of budget reported")
        }

        if false == strings.Contains(fmt.Sprint(exception.LogContext(closeErr, exception.LogContextOptions{})), "scoped.waiting") {
            t.Fatalf("expected the failure to name the service, got %v", exception.LogContext(closeErr, exception.LogContextOptions{}))
        }
    case <-time.After(DefaultScopeTeardownTimeout / 2):
        t.Fatalf("expected the close to return at its timeout")
    }
}

func TestScope_CloseWithContextRefusesANilContextAndLeavesTheCloseToALaterCall(t *testing.T) {
    scopeInstance, service := newScopeWithDeadlineRecordingService(t, ContainerConfig{})

    //nolint:staticcheck // the nil context is the input under test
    if closeErr := scopeInstance.CloseWithContext(nil); nil == closeErr || "the close context is nil" != closeErr.Error() {
        t.Fatalf("expected the nil context refused, got %v", closeErr)
    }

    if closeErr := scopeInstance.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    select {
    case <-service.closed:
    default:
        t.Fatalf("expected the later close to close the service")
    }
}
