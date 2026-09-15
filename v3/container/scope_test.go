package container

import (
    "context"
    "errors"
    "reflect"
    "runtime"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
)

func TestScope_GetDelegatesToContainerAndCachesPerScope(t *testing.T) {
    serviceContainer := NewContainer()

    calls := 0

    err := serviceContainer.Register(
        "service.test",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            calls++
            return &scopeTestService{value: "ok"}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    scope := serviceContainer.NewScope()

    _, err = scope.Get("service.test")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    _, err = scope.Get("service.test")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    if 1 != calls {
        t.Fatalf("expected provider to be called once per container singleton")
    }
}

func TestScope_OverrideInstance_IsolatedFromContainer(t *testing.T) {
    serviceContainer := NewContainer()

    err := serviceContainer.Register(
        "service.test",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "container"}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    scope := serviceContainer.NewScope()

    err = scope.OverrideProtectedInstance(
        "service.test",
        &scopeTestService{value: "scope"},
    )
    if nil != err {
        t.Fatalf("unexpected override error: %v", err)
    }

    valueAny, err := scope.Get("service.test")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    scopeValue := valueAny.(*scopeTestService)
    if "scope" != scopeValue.value {
        t.Fatalf("expected scope override value")
    }

    containerValueAny, err := serviceContainer.Get("service.test")
    if nil != err {
        t.Fatalf("unexpected container get error: %v", err)
    }

    containerValue := containerValueAny.(*scopeTestService)
    if "container" != containerValue.value {
        t.Fatalf("expected container value to remain unchanged")
    }
}

func TestScopeOverride_OfOneNameDoesNotAnswerAnotherServicesGetByType(t *testing.T) {
    serviceContainer := NewContainer()

    interfaceRegisterErr := serviceContainer.Register(
        "scope.poison.contract",
        func(resolver containercontract.Resolver) (testInterface, error) {
            return &testImplementation{name: "contract owner"}, nil
        },
    )
    if nil != interfaceRegisterErr {
        t.Fatalf("unexpected register error: %v", interfaceRegisterErr)
    }

    concreteRegisterErr := serviceContainer.Register(
        "scope.poison.concrete",
        func(resolver containercontract.Resolver) (*testImplementation, error) {
            return &testImplementation{name: "concrete owner"}, nil
        },
    )
    if nil != concreteRegisterErr {
        t.Fatalf("unexpected register error: %v", concreteRegisterErr)
    }

    scopeInstance := serviceContainer.NewScope()

    overrideErr := scopeInstance.OverrideProtectedInstance("scope.poison.contract", &testImplementation{name: "override"})
    if nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    concreteValue, concreteErr := scopeInstance.GetByType(reflect.TypeOf((*testImplementation)(nil)))
    if nil != concreteErr {
        t.Fatalf("unexpected get by type error: %v", concreteErr)
    }
    if implementation, isTyped := concreteValue.(*testImplementation); false == isTyped || "concrete owner" != implementation.name {
        t.Fatalf("expected the concrete service to keep answering its own type through the scope, got %#v", concreteValue)
    }
}

func TestScope_AnOverrideWinsOverAnInstanceTheContainerHasAlreadyBuilt(t *testing.T) {
    serviceContainer := NewContainer()

    if err := serviceContainer.Register(
        "service.test",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "container"}, nil
        },
    ); nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    if _, err := serviceContainer.Get("service.test"); nil != err {
        t.Fatalf("unexpected container get error: %v", err)
    }

    scope := serviceContainer.NewScope()

    if err := scope.OverrideProtectedInstance(
        "service.test",
        &scopeTestService{value: "scope"},
    ); nil != err {
        t.Fatalf("unexpected override error: %v", err)
    }

    valueAny, err := scope.Get("service.test")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    if "scope" != valueAny.(*scopeTestService).value {
        t.Fatalf("expected the scope override to answer, got the container's own instance")
    }
}

func TestScope_CloseReturnsErrorOnGet(t *testing.T) {
    serviceContainer := NewContainer()

    scope := serviceContainer.NewScope()
    _ = scope.Close()

    _, getErr := scope.Get("service.test")
    if nil == getErr {
        t.Fatalf("expected closed-scope error")
    }

    if false == errors.Is(getErr, ErrScopeClosed) {
        t.Fatalf("expected the refusal to classify as ErrScopeClosed")
    }
}

func TestScope_CloseKeepsMustGetPanicking(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "service.test",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "live"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()
    _ = scopeInstance.Close()

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

    _ = scopeInstance.MustGet("service.test")
}

func TestScope_HasReturnsFalseWhenClosed(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "a",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "live"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if false == scopeInstance.Has("a") {
        t.Fatalf("expected the live scope to answer for the registered name")
    }

    _ = scopeInstance.Close()

    if true == scopeInstance.Has("a") {
        t.Fatalf("expected false")
    }
}

func TestScope_CloseIsIdempotent(t *testing.T) {
    serviceContainer := NewContainer()

    closeCalls := int32(0)

    registerErr := serviceContainer.RegisterScoped(
        "app.idempotent",
        func(resolver containercontract.Resolver) (*closeCountingScopeService, error) {
            return &closeCountingScopeService{value: "scoped", closeCalls: &closeCalls}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get("app.idempotent"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if err := scopeInstance.Close(); nil != err {
        t.Fatalf("unexpected first close error: %v", err)
    }

    if err := scopeInstance.Close(); nil != err {
        t.Fatalf("unexpected second close error: %v", err)
    }

    if 1 != atomic.LoadInt32(&closeCalls) {
        t.Fatalf("expected the built service to be closed exactly once across both passes, got %d", atomic.LoadInt32(&closeCalls))
    }
}

func TestScope_OverrideAfterCloseReturnsError(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()
    _ = scopeInstance.Close()

    overrideErr := scopeInstance.OverrideProtectedInstance("service.after_close", &scopeTestService{value: "late"})
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
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()
    _ = scopeInstance.Close()

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected panic from MustOverrideProtectedInstance on a closed scope")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError || false == errors.Is(recoveredErr, ErrScopeClosed) {
            t.Fatalf("expected the panic to carry the closed-scope refusal, got %#v", recovered)
        }
    }()

    scopeInstance.MustOverrideProtectedInstance("service.after_close", &scopeTestService{value: "late"})
}

func TestScope_GetByTypeAfterCloseReturnsError(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()
    _ = scopeInstance.Close()

    _, getByTypeErr := scopeInstance.GetByType(reflect.TypeOf((*scopeTestService)(nil)))
    if nil == getByTypeErr {
        t.Fatalf("expected closed-scope error on get-by-type after close")
    }

    if false == errors.Is(getByTypeErr, ErrScopeClosed) {
        t.Fatalf("expected the refusal to classify as ErrScopeClosed")
    }
}

func TestScope_GetByTypeAfterCloseKeepsMustPanicking(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "service.getbytype",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "live"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()
    _ = scopeInstance.Close()

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

    _ = scopeInstance.MustGetByType(reflect.TypeOf((*scopeTestService)(nil)))
}

func TestScope_HasTypeReturnsFalseWhenClosed(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "service.hastype",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "live"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if false == scopeInstance.HasType(reflect.TypeOf((*scopeTestService)(nil))) {
        t.Fatalf("expected the live scope to answer for the registered type")
    }

    _ = scopeInstance.Close()

    if true == scopeInstance.HasType(reflect.TypeOf((*scopeTestService)(nil))) {
        t.Fatalf("expected false after close")
    }
}

func TestScope_ConcurrentGetAndClose(t *testing.T) {
    serviceContainer := NewContainer()

    err := serviceContainer.Register(
        "service.concurrent",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "ok"}, nil
        },
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
                _, getErr := scopeInstance.Get("service.concurrent")
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
        _ = scopeInstance.Close()
    }()

    waitGroup.Wait()

    if 0 != panics.Load() {
        t.Fatalf("expected no reader to panic while the scope closed under them, got %d", panics.Load())
    }
}

func TestScope_ConcurrentOverrideAndGet(t *testing.T) {
    serviceContainer := NewContainer()

    err := serviceContainer.Register(
        "service.mutable",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "base"}, nil
        },
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
                )
            }
        }()
    }

    for readerIndex := 0; readerIndex < 8; readerIndex++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            for iteration := 0; iteration < 200; iteration++ {
                _, _ = scopeInstance.Get("service.mutable")
                _ = scopeInstance.Has("service.mutable")
            }
        }()
    }

    waitGroup.Wait()
}

func TestScope_ConcurrentHasAndClose(t *testing.T) {
    serviceContainer := NewContainer()

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
        _ = scopeInstance.Close()
    }()

    waitGroup.Wait()
}

func TestScope_MustGetByTypeNilTypePanicsDescriptively(t *testing.T) {
    serviceContainer := NewContainer()
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

    _ = scope.MustGetByType(nil)
}

func TestScope_ServiceThatReadsNothingFromTheScopeStaysASingleton(t *testing.T) {
    serviceContainer := NewContainer()

    calls := 0

    registerErr := serviceContainer.Register(
        "app.shared",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            calls = calls + 1

            return &scopeTestService{value: "shared"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    firstScope := serviceContainer.NewScope()
    first, getErr := firstScope.Get("app.shared")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }
    _ = firstScope.Close()

    secondScope := serviceContainer.NewScope()
    second, getErr := secondScope.Get("app.shared")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }
    _ = secondScope.Close()

    if 1 != calls {
        t.Fatalf("expected the provider to run once for the whole process, ran %d times", calls)
    }

    if first != second {
        t.Fatalf("expected both scopes to be handed the same singleton")
    }

    fromContainer, getErr := serviceContainer.Get("app.shared")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if first != fromContainer {
        t.Fatalf("expected the container itself to hold the singleton the scopes built")
    }
}

func TestScope_RootContainerDoesNotSeeTheScopeBoundInstance(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.consumer",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            tag, getErr := resolver.Get("app.tag")
            if nil != getErr {
                return &scopeTestService{value: "no-tag"}, nil
            }

            return &scopeTestService{value: tag.(*scopeTestService).value}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    overrideErr := scopeInstance.OverrideInstance("app.tag", &scopeTestService{value: "request-1"})
    if nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    _, getErr := scopeInstance.Get("app.consumer")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    _ = scopeInstance.Close()

    fromContainer, getErr := serviceContainer.Get("app.consumer")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if "no-tag" != fromContainer.(*scopeTestService).value {
        t.Fatalf("expected the container to build its own instance, got the request-bound %q", fromContainer.(*scopeTestService).value)
    }
}

func TestScope_CloseLeavesOverridesAndRootSingletonsAlone(t *testing.T) {
    serviceContainer := NewContainer()

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
    )
    if nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    _, getErr := scopeInstance.Get("app.singleton")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    _, getErr = scopeInstance.Get("app.override")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    closeErr := scopeInstance.Close()
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

func TestScope_AContainerServiceStaysASingletonWhenResolvedThroughAScope(t *testing.T) {
    var buildCount atomic.Int64
    var closeCount atomic.Int64

    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "infrastructure.logger",
        func(resolver containercontract.Resolver) (string, error) {
            return "container-logger", nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    seenLogger := ""
    registerErr = serviceContainer.Register(
        "app.pool",
        func(resolver containercontract.Resolver) (*scopeLifetimeProbe, error) {
            buildCount.Add(1)

            logger, getErr := resolver.Get("infrastructure.logger")
            if nil != getErr {
                return nil, getErr
            }
            seenLogger, _ = logger.(string)

            return &scopeLifetimeProbe{closed: &closeCount}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    for requestIndex := 0; requestIndex < 3; requestIndex = requestIndex + 1 {
        requestScope := serviceContainer.NewScope()

        if overrideErr := requestScope.OverrideProtectedInstance("infrastructure.logger", "request-logger"); nil != overrideErr {
            t.Fatalf("request %d: unexpected override error: %v", requestIndex, overrideErr)
        }

        if _, getErr := requestScope.Get("app.pool"); nil != getErr {
            t.Fatalf("request %d: unexpected get error: %v", requestIndex, getErr)
        }

        if closeErr := requestScope.Close(); nil != closeErr {
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
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.reporter",
        func(resolver containercontract.Resolver) (string, error) {
            requestContext, getErr := resolver.Get("request.context")
            if nil != getErr {
                return "", getErr
            }

            return requestContext.(string), nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    requestScope := serviceContainer.NewScope()
    defer requestScope.Close()

    if overrideErr := requestScope.OverrideProtectedInstance("request.context", "the-request"); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if value, getErr := requestScope.Get("request.context"); nil != getErr || "the-request" != value {
        t.Fatalf("expected the scope to answer for its own entry, got %v / %v", value, getErr)
    }

    _, getErr := requestScope.Get("app.reporter")
    if nil == getErr {
        t.Fatal("a container provider reached a scope-only entry: it would hold one request for the life of the process")
    }
}

func TestScopeClose_TypeAliasClosesAfterDependent(t *testing.T) {
    serviceContainer := NewContainer()

    recorder := &scopedCloseRecorder{}

    registerTransactionErr := serviceContainer.RegisterScoped(
        "app.alias.transaction",
        func(resolver containercontract.Resolver) (*recordingScopedService, error) {
            return &recordingScopedService{name: "transaction", recorder: recorder}, nil
        },
    )
    if nil != registerTransactionErr {
        t.Fatalf("unexpected register error: %v", registerTransactionErr)
    }

    registerRepositoryErr := serviceContainer.RegisterScoped(
        "app.alias.repository",
        func(resolver containercontract.Resolver) (*aliasRepositoryService, error) {
            _, getErr := resolver.GetByType(reflect.TypeOf((*recordingScopedService)(nil)))
            if nil != getErr {
                return nil, getErr
            }

            return &aliasRepositoryService{recorder: recorder}, nil
        },
    )
    if nil != registerRepositoryErr {
        t.Fatalf("unexpected register error: %v", registerRepositoryErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get("app.alias.repository"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    if closeErr := scopeInstance.Close(); nil != closeErr {
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

func TestScopeClose_DualFiledUncomparableValue_ClosedOnce(t *testing.T) {
    serviceContainer := NewContainer()

    closeCount := int32(0)

    registerErr := serviceContainer.RegisterScoped(
        "app.alias.value",
        func(resolver containercontract.Resolver) (dualFiledValueService, error) {
            return dualFiledValueService{
                closeCount: &closeCount,
                padding:    []string{"uncomparable"},
            }, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.GetByType(reflect.TypeOf(dualFiledValueService{})); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    if closeErr := scopeInstance.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != atomic.LoadInt32(&closeCount) {
        t.Fatalf("expected exactly one close for the dual-filed value, got %d", atomic.LoadInt32(&closeCount))
    }
}

func TestScopeClose_EvictedCreatedInstanceClosedAtTeardown(t *testing.T) {
    serviceContainer := NewContainer()

    recorder := &scopedCloseRecorder{}

    registerErr := serviceContainer.RegisterScoped(
        "app.evicted.service",
        func(resolver containercontract.Resolver) (*recordingScopedService, error) {
            return &recordingScopedService{name: "created", recorder: recorder}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get("app.evicted.service"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    overridingScope, hasOptions := scopeInstance.(containercontract.OverrideServiceWithOptions)
    if false == hasOptions {
        t.Fatalf("expected the scope to implement OverrideServiceWithOptions")
    }

    overrideErr := overridingScope.OverrideProtectedInstanceWithOptions(
        "app.evicted.service",
        &recordingScopedService{name: "override", recorder: recorder},
        ClosedWithScope(),
    )
    if nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if closeErr := scopeInstance.Close(); nil != closeErr {
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

func TestScopeClose_TwoFailingEvictedInstancesAreBothRecorded(t *testing.T) {
    serviceContainer := NewContainer()

    registerFirstErr := serviceContainer.RegisterScoped(
        "app.evicted.failing.first",
        func(resolver containercontract.Resolver) (*scopeCloseFailingService, error) {
            return &scopeCloseFailingService{failure: errors.New("refusing to close app.evicted.failing.first")}, nil
        },
    )
    if nil != registerFirstErr {
        t.Fatalf("unexpected register error: %v", registerFirstErr)
    }

    registerSecondErr := serviceContainer.RegisterScoped(
        "app.evicted.failing.second",
        func(resolver containercontract.Resolver) (*secondEvictedFailingService, error) {
            return &secondEvictedFailingService{failure: errors.New("refusing to close app.evicted.failing.second")}, nil
        },
    )
    if nil != registerSecondErr {
        t.Fatalf("unexpected register error: %v", registerSecondErr)
    }

    scopeInstance := serviceContainer.NewScope()

    overridingScope, hasOptions := scopeInstance.(containercontract.OverrideServiceWithOptions)
    if false == hasOptions {
        t.Fatalf("expected the scope to implement OverrideServiceWithOptions")
    }

    if _, getErr := scopeInstance.Get("app.evicted.failing.first"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    if _, getErr := scopeInstance.Get("app.evicted.failing.second"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    firstOverrideErr := overridingScope.OverrideProtectedInstanceWithOptions(
        "app.evicted.failing.first",
        &scopeCloseFailingService{},
        ClosedWithScope(),
    )
    if nil != firstOverrideErr {
        t.Fatalf("unexpected override error: %v", firstOverrideErr)
    }

    secondOverrideErr := overridingScope.OverrideProtectedInstanceWithOptions(
        "app.evicted.failing.second",
        &secondEvictedFailingService{},
        ClosedWithScope(),
    )
    if nil != secondOverrideErr {
        t.Fatalf("unexpected override error: %v", secondOverrideErr)
    }

    closeErr := scopeInstance.Close()
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
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    scopeInstance.MustOverrideInstance("app.request.substitute", &scopeTestService{value: "installed"})

    value, getErr := scopeInstance.Get("app.request.substitute")
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

    scopeInstance.MustOverrideInstance("service.protected", &scopeTestService{value: "installed"})
}

func TestScope_MustOverrideInstanceWithOptions_InstallsAndNamesItsOwnFailure(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    withOptions, hasOptions := scopeInstance.(containercontract.OverrideServiceWithOptions)
    if false == hasOptions {
        t.Fatalf("expected a scope to carry the override options companion")
    }

    withOptions.MustOverrideInstanceWithOptions("app.request.substitute", &scopeTestService{value: "installed"})

    value, getErr := scopeInstance.Get("app.request.substitute")
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

    withOptions.MustOverrideInstanceWithOptions("service.protected", &scopeTestService{value: "installed"})
}

func TestScope_MustOverrideProtectedInstanceWithOptions_InstallsAndNamesItsOwnFailure(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    withOptions, hasOptions := scopeInstance.(containercontract.OverrideServiceWithOptions)
    if false == hasOptions {
        t.Fatalf("expected a scope to carry the override options companion")
    }

    withOptions.MustOverrideProtectedInstanceWithOptions("service.protected", &scopeTestService{value: "installed"})

    value, getErr := scopeInstance.Get("service.protected")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    service, isService := value.(*scopeTestService)
    if false == isService || "installed" != service.value {
        t.Fatalf("expected the protected door to install the override, got %#v", value)
    }

    if closeErr := scopeInstance.Close(); nil != closeErr {
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

    withOptions.MustOverrideProtectedInstanceWithOptions("service.protected", &scopeTestService{value: "late"})
}

func TestScope_OverrideInstance_ProtectedNameRefused(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    overrideErr := scopeInstance.OverrideInstance("service.protected", &scopeTestService{value: "substitute"})
    if nil == overrideErr {
        t.Fatalf("expected the protected name to be refused by the plain scope override door")
    }

    if "service is protected and cannot be overridden" != overrideErr.Error() {
        t.Fatalf("unexpected refusal message: %q", overrideErr.Error())
    }
}

func TestScope_OverrideProtectedInstance_EmptyNameRefused(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    overrideErr := scopeInstance.OverrideProtectedInstance("", &scopeTestService{value: "installed"})
    if nil == overrideErr {
        t.Fatalf("expected an empty name to be refused")
    }

    if "service name is empty in override instance" != overrideErr.Error() {
        t.Fatalf("unexpected refusal message: %q", overrideErr.Error())
    }
}

func TestScope_OverrideProtectedInstance_NilValueRefusedInBothSpellings(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    untypedErr := scopeInstance.OverrideProtectedInstance("app.request.substitute", nil)
    if nil == untypedErr {
        t.Fatalf("expected an untyped nil override to be refused")
    }

    if "value is nil in override instance" != untypedErr.Error() {
        t.Fatalf("unexpected refusal message: %q", untypedErr.Error())
    }

    var typedNil *scopeTestService

    typedErr := scopeInstance.OverrideProtectedInstance("app.request.substitute", typedNil)
    if nil == typedErr {
        t.Fatalf("expected a typed nil override to be refused")
    }

    if "value is nil in override instance" != typedErr.Error() {
        t.Fatalf("unexpected refusal message: %q", typedErr.Error())
    }
}

func TestScope_Get_EmptyNameRefused(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    _, getErr := scopeInstance.Get("")
    if nil == getErr {
        t.Fatalf("expected an empty service name to be refused")
    }

    if "service name is empty in get" != getErr.Error() {
        t.Fatalf("unexpected refusal message: %q", getErr.Error())
    }
}

func TestScope_HasAndHasType_AnswerFalseForTheEmptyNameAndTheNilType(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.thing",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "thing"}, nil
        },
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

func TestScope_Close_ReportsAFailingServiceClose(t *testing.T) {
    serviceContainer := NewContainer()

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.scoped.failing",
        func(resolver containercontract.Resolver) (*scopeCloseFailingService, error) {
            return &scopeCloseFailingService{failure: errors.New("the connection refused to close")}, nil
        },
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get("app.scoped.failing"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    closeErr := scopeInstance.Close()
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
    serviceContainer := NewContainer().(*container)

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

    if closeErr := scopeInstance.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }
}

func TestScope_OverridePropagatesToEveryRegisteredTypeOfTheName(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.greeter",
        func(resolver containercontract.Resolver) (scopeOverrideGreeter, error) {
            return &containerScopeGreeter{}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    _ = serviceContainer.MustGet("app.greeter")

    requestScope := serviceContainer.NewScope()
    defer requestScope.Close()

    override := &overrideScopeGreeter{}
    if overrideErr := requestScope.OverrideInstance("app.greeter", override); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    byName := requestScope.MustGet("app.greeter")
    if scopeOverrideGreeter(override) != byName {
        t.Fatalf("expected the name to answer the override, got %T", byName)
    }

    byType := requestScope.MustGetByType(reflect.TypeOf((*scopeOverrideGreeter)(nil)).Elem())
    if scopeOverrideGreeter(override) != byType {
        t.Fatalf("expected the type-keyed resolution to answer the override, got %T", byType)
    }
}

func TestScope_OverridePropagationDoesNotPolluteTheContainer(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.greeter",
        func(resolver containercontract.Resolver) (scopeOverrideGreeter, error) {
            return &containerScopeGreeter{}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    containerOwn := serviceContainer.MustGet("app.greeter")

    requestScope := serviceContainer.NewScope()

    override := &overrideScopeGreeter{}
    if overrideErr := requestScope.OverrideInstance("app.greeter", override); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    _ = requestScope.MustGet("app.greeter")
    _ = requestScope.MustGetByType(reflect.TypeOf((*scopeOverrideGreeter)(nil)).Elem())

    greeterType := reflect.TypeOf((*scopeOverrideGreeter)(nil)).Elem()

    if containerOwn != serviceContainer.MustGetByType(greeterType) {
        t.Fatalf("expected the container's type-keyed answer to stay its own instance")
    }

    otherScope := serviceContainer.NewScope()
    defer otherScope.Close()

    if containerOwn != otherScope.MustGetByType(greeterType) {
        t.Fatalf("expected a sibling scope to keep resolving the container's instance")
    }

    if closeErr := requestScope.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if containerOwn != serviceContainer.MustGetByType(greeterType) {
        t.Fatalf("expected the container untouched after the overriding scope closed")
    }
}

func TestScope_OverrideRefusesAValueARegisteredTypeCannotHold(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.greeter",
        func(resolver containercontract.Resolver) (scopeOverrideGreeter, error) {
            return &containerScopeGreeter{}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    containerOwn := serviceContainer.MustGet("app.greeter")

    requestScope := serviceContainer.NewScope()
    defer requestScope.Close()

    overrideErr := requestScope.OverrideInstance("app.greeter", &scopeOverrideOutsider{})
    if nil == overrideErr {
        t.Fatalf("expected the unassignable override to be refused")
    }

    if false == strings.Contains(overrideErr.Error(), "override value is not assignable to the registered service type") {
        t.Fatalf("expected the assignability refusal, got %q", overrideErr.Error())
    }

    if containerOwn != requestScope.MustGet("app.greeter") {
        t.Fatalf("expected the refused override to have written nothing")
    }
}

func TestScope_AClosedWithScopeOverrideUnderARegisteredTypeClosesOnce(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.probe",
        func(resolver containercontract.Resolver) (*scopeLifetimeProbe, error) {
            return &scopeLifetimeProbe{}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    requestScope := serviceContainer.NewScope()

    var closeCount atomic.Int64
    override := &scopeLifetimeProbe{closed: &closeCount}

    overridingScope, hasOptions := requestScope.(containercontract.OverrideServiceWithOptions)
    if false == hasOptions {
        t.Fatalf("expected the scope to implement OverrideServiceWithOptions")
    }

    if overrideErr := overridingScope.OverrideInstanceWithOptions("app.probe", override, ClosedWithScope()); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if closeErr := requestScope.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != closeCount.Load() {
        t.Fatalf("expected the override closed exactly once by the teardown, got %d", closeCount.Load())
    }
}

func TestScope_ClosedAnswersTheLifecycle(t *testing.T) {
    serviceContainer := NewContainer()

    requestScope := serviceContainer.NewScope()

    closedChecker, isChecker := requestScope.(interface{ Closed() bool })
    if false == isChecker {
        t.Fatalf("expected the concrete scope to answer the closed question")
    }

    if true == closedChecker.Closed() {
        t.Fatalf("expected a fresh scope to report open")
    }

    if closeErr := requestScope.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if false == closedChecker.Closed() {
        t.Fatalf("expected a closed scope to report closed")
    }
}

func TestScope_OverridePropagationCoversTheScopedPlanLayer(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.RegisterScoped(
        "app.greeter",
        func(resolver containercontract.Resolver) (scopeOverrideGreeter, error) {
            return &containerScopeGreeter{}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    requestScope := serviceContainer.NewScope()
    defer requestScope.Close()

    greeterType := reflect.TypeOf((*scopeOverrideGreeter)(nil)).Elem()

    built := requestScope.MustGetByType(greeterType)
    if nil == built {
        t.Fatalf("expected the scoped registration to build")
    }

    override := &overrideScopeGreeter{}
    if overrideErr := requestScope.OverrideInstance("app.greeter", override); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if scopeOverrideGreeter(override) != requestScope.MustGetByType(greeterType) {
        t.Fatalf("expected the override to outrank the built scoped instance on the type key")
    }
}

func TestScope_OverridePropagationCoversTheScopeOwnRegistrations(t *testing.T) {
    serviceContainer := NewContainer()

    requestScope := serviceContainer.NewScope()
    defer requestScope.Close()

    registerErr := requestScope.RegisterScoped(
        "app.greeter",
        func(resolver containercontract.Resolver) (scopeOverrideGreeter, error) {
            return &containerScopeGreeter{}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    greeterType := reflect.TypeOf((*scopeOverrideGreeter)(nil)).Elem()

    built := requestScope.MustGetByType(greeterType)
    if nil == built {
        t.Fatalf("expected the live-scope registration to build")
    }

    override := &overrideScopeGreeter{}
    if overrideErr := requestScope.OverrideInstance("app.greeter", override); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if scopeOverrideGreeter(override) != requestScope.MustGetByType(greeterType) {
        t.Fatalf("expected the override to outrank the built own-registration instance on the type key")
    }
}

func TestScope_TypesImplementingReturnsEmptyWhenClosed(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*scopeTestService, error) {
        return &scopeTestService{}, nil
    })

    scopeInstance := serviceContainer.NewScope()

    closeErr := scopeInstance.Close()
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

func TestScopeClose_TypeAliasStampedAfterItsDependentClosesAfterIt(t *testing.T) {
    serviceContainer := NewContainer()

    recorder := &scopedCloseRecorder{}

    if registerErr := serviceContainer.RegisterScoped(
        "app.alias.held",
        func(resolver containercontract.Resolver) (*aliasNameHeldService, error) {
            return &aliasNameHeldService{recorder: recorder}, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.RegisterScoped(
        "app.alias.holder",
        func(resolver containercontract.Resolver) (*aliasKeptResolverHolder, error) {
            return &aliasKeptResolverHolder{resolver: resolver, recorder: recorder}, nil
        },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    holderValue, getErr := scopeInstance.Get("app.alias.holder")
    if nil != getErr {
        t.Fatalf("unexpected holder resolution error: %v", getErr)
    }

    holder, isHolder := holderValue.(*aliasKeptResolverHolder)
    if false == isHolder {
        t.Fatalf("expected the holder, got %#v", holderValue)
    }

    if _, heldErr := holder.resolver.GetByType(reflect.TypeOf((*aliasNameHeldService)(nil))); nil != heldErr {
        t.Fatalf("unexpected late resolution error: %v", heldErr)
    }

    if closeErr := scopeInstance.Close(); nil != closeErr {
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
    serviceContainer := NewContainer()

    recorder := &scopedCloseRecorder{}

    if registerErr := serviceContainer.RegisterScoped(
        "app.alias.overridden",
        func(resolver containercontract.Resolver) (*aliasNameHeldService, error) {
            return &aliasNameHeldService{recorder: recorder}, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.RegisterScoped(
        "app.alias.overrideholder",
        func(resolver containercontract.Resolver) (*aliasKeptResolverHolder, error) {
            return &aliasKeptResolverHolder{resolver: resolver, recorder: recorder}, nil
        },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    holderValue, getErr := scopeInstance.Get("app.alias.overrideholder")
    if nil != getErr {
        t.Fatalf("unexpected holder resolution error: %v", getErr)
    }

    holder, isHolder := holderValue.(*aliasKeptResolverHolder)
    if false == isHolder {
        t.Fatalf("expected the holder, got %#v", holderValue)
    }

    if overrideErr := scopeInstance.(containercontract.OverrideServiceWithOptions).OverrideInstanceWithOptions(
        "app.alias.overridden",
        &aliasNameHeldService{recorder: recorder},
        ClosedWithScope(),
    ); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if _, overriddenErr := holder.resolver.Get("app.alias.overridden"); nil != overriddenErr {
        t.Fatalf("unexpected late resolution error: %v", overriddenErr)
    }

    if closeErr := scopeInstance.Close(); nil != closeErr {
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

func TestScopeClose_AnAliasGroupIsAsOldAsItsOldestMember(t *testing.T) {
    serviceContainer := NewContainer()

    recorder := &scopedCloseRecorder{}

    if registerErr := serviceContainer.RegisterScoped(
        "app.aaa.shared",
        func(resolver containercontract.Resolver) (*aliasGroupSpanningService, error) {
            return &aliasGroupSpanningService{label: "original", recorder: recorder}, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.RegisterScoped(
        "app.middle",
        func(resolver containercontract.Resolver) (*aliasGroupMiddleService, error) {
            return &aliasGroupMiddleService{recorder: recorder}, nil
        },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get("app.aaa.shared"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := scopeInstance.Get("app.middle"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if overrideErr := scopeInstance.(containercontract.OverrideServiceWithOptions).OverrideInstanceWithOptions(
        "app.aaa.shared",
        &aliasGroupSpanningService{label: "installed", recorder: recorder},
        ClosedWithScope(),
    ); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if closeErr := scopeInstance.Close(); nil != closeErr {
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

func TestScope_OverrideClosedDuringTheLockHandOffIsStillRefused(t *testing.T) {
    serviceContainer := NewContainer()
    containerInstance := serviceContainer.(*container)

    scopeInstance := serviceContainer.NewScope().(*scope)

    overrideEntered := make(chan struct{})
    overrideDone := make(chan error, 1)

    containerInstance.mutex.Lock()

    go func() {
        close(overrideEntered)

        overrideDone <- scopeInstance.OverrideProtectedInstance("service.after_hand_off", &scopeTestService{value: "late"})
    }()

    <-overrideEntered
    time.Sleep(50 * time.Millisecond)

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

    scopeInstance.mutex.RLock()
    _, nameWritten := scopeInstance.instances["service.after_hand_off"]
    scopeInstance.mutex.RUnlock()

    if true == nameWritten {
        t.Fatalf("expected nothing to be written into a scope that closed during the hand-off")
    }
}

func TestScope_GetClosedAfterTheEntryCheckIsRefusedByTheLookup(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope().(*scope)

    getEntered := make(chan struct{})
    getDone := make(chan error, 1)

    scopeInstance.mutex.Lock()

    go func() {
        close(getEntered)

        _, getErr := scopeInstance.Get("service.absent")
        getDone <- getErr
    }()

    <-getEntered
    time.Sleep(50 * time.Millisecond)

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
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope().(*scope)

    getEntered := make(chan struct{})
    getDone := make(chan error, 1)

    scopeInstance.mutex.Lock()

    go func() {
        close(getEntered)

        _, getByTypeErr := scopeInstance.GetByType(reflect.TypeOf((*scopeTestService)(nil)))
        getDone <- getByTypeErr
    }()

    <-getEntered
    time.Sleep(50 * time.Millisecond)

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

func TestScope_Close_PrefersTheContextDoorOfAScopedService(t *testing.T) {
    scopeInstance, service := newScopeWithContextDoorService(t)

    if closeErr := scopeInstance.Close(); nil != closeErr {
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
    serviceContainer := NewContainer()

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.scoped.panickingContextDoor",
        func(resolver containercontract.Resolver) (*scopeContextDoorPanickingService, error) {
            return &scopeContextDoorPanickingService{}, nil
        },
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get("app.scoped.panickingContextDoor"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    closeErr := scopeInstance.Close()
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

func TestScopeClosesContextOnlyServices(t *testing.T) {
    serviceContainer := NewContainer()
    value := &contextOnlyCleanup{failure: errors.New("scope context-only failure")}
    MustRegisterScoped(serviceContainer, "context-only", func(resolver containercontract.Resolver) (*contextOnlyCleanup, error) { return value, nil })
    scopeInstance := serviceContainer.NewScope()
    _ = scopeInstance.MustGet("context-only")
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    err := scopeInstance.(interface { CloseWithContext(context.Context) error }).CloseWithContext(ctx)
    if 1 != value.calls || ctx != value.seen || nil == err { t.Fatalf("context-only scoped service skipped: calls=%d err=%v", value.calls, err) }
}
