package container

import (
    "errors"
    "strings"
    "sync"
    "testing"
    "time"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

func TestLazyService_DefersResolutionUntilFirstGet(t *testing.T) {
    serviceContainer := NewContainer()

    resolveCount := 0
    MustRegister[string](serviceContainer, "service.lazy.value", func(resolver containercontract.Resolver) (string, error) {
        resolveCount++

        return "resolved", nil
    })

    lazy := Lazy[string](serviceContainer, "service.lazy.value")
    if 0 != resolveCount {
        t.Fatalf("expected no resolution before the first Get, got %d", resolveCount)
    }

    if "resolved" != lazy.Get() {
        t.Fatalf("expected the resolved value on first Get")
    }

    if "resolved" != lazy.Get() {
        t.Fatalf("expected the memoized value on the second Get")
    }

    if 1 != resolveCount {
        t.Fatalf("expected exactly one resolution, got %d", resolveCount)
    }
}

func TestLazyService_ResolvesProviderRegisteredAfterTheHandle(t *testing.T) {
    serviceContainer := NewContainer()

    lazy := Lazy[string](serviceContainer, "service.lazy.late")

    MustRegister[string](serviceContainer, "service.lazy.late", func(resolver containercontract.Resolver) (string, error) {
        return "late", nil
    })

    if "late" != lazy.Get() {
        t.Fatalf("expected the lazily-registered value")
    }
}

func TestLazyService_ResolveReportsMissingService(t *testing.T) {
    serviceContainer := NewContainer()

    lazy := Lazy[string](serviceContainer, "service.lazy.absent")

    _, resolveErr := lazy.Resolve()
    if nil == resolveErr {
        t.Fatalf("expected an error resolving an unregistered service")
    }
}

func TestLazyService_RetriesResolutionAfterFailure(t *testing.T) {
    serviceContainer := NewContainer()

    resolveCount := 0
    MustRegister[string](serviceContainer, "service.lazy.flaky", func(resolver containercontract.Resolver) (string, error) {
        resolveCount++

        if 1 == resolveCount {
            return "", errors.New("store not reachable yet")
        }

        return "recovered", nil
    })

    lazy := Lazy[string](serviceContainer, "service.lazy.flaky")

    _, firstErr := lazy.Resolve()
    if nil == firstErr {
        t.Fatalf("expected the first resolution failure to surface")
    }

    value, secondErr := lazy.Resolve()
    if nil != secondErr {
        t.Fatalf("expected the second resolution to retry the resolver and succeed, got %v", secondErr)
    }

    if "recovered" != value {
        t.Fatalf("expected the recovered value, got %q", value)
    }

    if "recovered" != lazy.Get() {
        t.Fatalf("expected the memoized value after the successful resolution")
    }

    if 2 != resolveCount {
        t.Fatalf("expected the success to be memoized after two attempts, got %d", resolveCount)
    }
}

func TestLazyService_ResolverReachingBackIntoTheHandleIsNotDeadlocked(t *testing.T) {
    depth := 0
    var handle *LazyService[string]
    handle = &LazyService[string]{
        resolve: func() (string, error) {
            depth++
            if 1 < depth {
                return "", errors.New("cycle detected by the resolution stack")
            }

            _, innerErr := handle.Resolve()
            if nil != innerErr {
                return "", innerErr
            }

            return "unreachable", nil
        },
    }

    resolved := make(chan error, 1)
    go func() {
        _, resolveErr := handle.Resolve()
        resolved <- resolveErr
    }()

    select {
    case resolveErr := <-resolved:
        if nil == resolveErr {
            t.Fatal("expected the reentrant resolution to surface the cycle error")
        }
    case <-time.After(2 * time.Second):
        t.Fatal("expected the reentrant resolution to return instead of deadlocking on the handle's lock")
    }
}

func TestLazyService_NilYieldIsNotMemoized(t *testing.T) {
    resolveCount := 0
    handle := &LazyService[*lazyProbeItem]{
        resolve: func() (*lazyProbeItem, error) {
            resolveCount++
            if 1 == resolveCount {
                return nil, nil
            }

            return &lazyProbeItem{name: "real"}, nil
        },
    }

    firstValue, firstErr := handle.Resolve()
    if nil != firstErr {
        t.Fatalf("expected the nil yield to pass through without an error, got %v", firstErr)
    }

    if nil != firstValue {
        t.Fatalf("expected the first resolution to yield nil, got %+v", firstValue)
    }

    secondValue, secondErr := handle.Resolve()
    if nil != secondErr {
        t.Fatalf("expected the second resolution to retry the resolver, got %v", secondErr)
    }

    if nil == secondValue || "real" != secondValue.name {
        t.Fatalf("expected the retried resolution to yield the real value, got %+v", secondValue)
    }

    if "real" != handle.Get().name {
        t.Fatalf("expected the memoized value after the successful resolution")
    }

    if 2 != resolveCount {
        t.Fatalf("expected the nil yield not to be memoized and the success to be, got %d resolutions", resolveCount)
    }
}

func TestLazyService_GetIsSafeForConcurrentUse(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegister[string](serviceContainer, "service.lazy.concurrent", func(resolver containercontract.Resolver) (string, error) {
        return "shared", nil
    })

    lazy := Lazy[string](serviceContainer, "service.lazy.concurrent")

    var waitGroup sync.WaitGroup
    for index := 0; index < 32; index++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()

            if "shared" != lazy.Get() {
                t.Errorf("expected the shared value under concurrent Get")
            }
        }()
    }

    waitGroup.Wait()
}

func TestLazyService_GetRefusesANilYieldByName(t *testing.T) {
    handle := &LazyService[*lazyProbeItem]{
        resolve: func() (*lazyProbeItem, error) {
            return nil, nil
        },
    }

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the nil yield to panic on the Get door")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic value, got %#v", recoveredValue)
        }

        if "lazy service resolved to nil" != recoveredErr.Error() {
            t.Fatalf("unexpected panic message: %q", recoveredErr.Error())
        }
    }()

    _ = handle.Get()
}

func TestLazyService_GetCarriesTheResolversOwnFailure(t *testing.T) {
    handle := &LazyService[*lazyProbeItem]{
        resolve: func() (*lazyProbeItem, error) {
            return nil, errors.New("the resolver refused")
        },
    }

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the failed resolution to panic on the Get door")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic value, got %#v", recoveredValue)
        }

        if "the resolver refused" != recoveredErr.Error() {
            t.Fatalf("unexpected panic message: %q", recoveredErr.Error())
        }
    }()

    _ = handle.Get()
}

func TestLazyByType_ResolvesTheServiceOnFirstUse(t *testing.T) {
    serviceContainer := NewContainer()

    resolutionCount := 0

    registerErr := RegisterType[*registerScopedProbe](
        serviceContainer,
        func(resolver containercontract.Resolver) (*registerScopedProbe, error) {
            resolutionCount++

            return &registerScopedProbe{value: "lazy by type"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    lazyService := LazyByType[*registerScopedProbe](serviceContainer)

    if 0 != resolutionCount {
        t.Fatalf("expected the handle to resolve nothing before its first use, got %d resolutions", resolutionCount)
    }

    value := lazyService.Get()
    if "lazy by type" != value.value {
        t.Fatalf("unexpected resolved value: %#v", value)
    }

    if value != lazyService.Get() {
        t.Fatalf("expected the second use to answer with the memoized value")
    }
}

func TestLazyByType_MissingRegistrationFailsThroughTheByTypeResolution(t *testing.T) {
    serviceContainer := NewContainer()

    lazyService := LazyByType[*registerScopedOtherProbe](serviceContainer)

    _, resolveErr := lazyService.Resolve()
    if nil == resolveErr {
        t.Fatalf("expected the by-type resolution of an unregistered type to fail")
    }

    if false == strings.Contains(resolveErr.Error(), "service type is not registered") {
        t.Fatalf("expected the by-type refusal, got %q", resolveErr.Error())
    }
}

func TestLazyService_AClosedScopeTurnsTheHandleTerminal(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.RegisterScoped(
        "app.unit",
        func(resolver containercontract.Resolver) (*lazyProbeItem, error) {
            return &lazyProbeItem{}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    requestScope := serviceContainer.NewScope()

    lazyService := Lazy[*lazyProbeItem](requestScope, "app.unit")

    if nil == lazyService.Get() {
        t.Fatalf("expected the live scope to serve the value")
    }

    if closeErr := requestScope.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    _, resolveErr := lazyService.Resolve()
    if nil == resolveErr {
        t.Fatalf("expected a closed scope to end the handle")
    }

    if false == strings.Contains(resolveErr.Error(), "lazy service scope is closed") {
        t.Fatalf("expected the scope-is-closed refusal, got %q", resolveErr.Error())
    }

    if false == errors.Is(resolveErr, ErrScopeClosed) {
        t.Fatalf("expected the refusal to classify as ErrScopeClosed")
    }

    _, secondErr := lazyService.Resolve()
    if nil == secondErr || false == strings.Contains(secondErr.Error(), "lazy service scope is closed") {
        t.Fatalf("expected the terminal state to answer the same on every later call, got %v", secondErr)
    }

    if false == errors.Is(secondErr, ErrScopeClosed) {
        t.Fatalf("expected the terminal refusal to classify as ErrScopeClosed too")
    }
}

func TestLazyService_AScopeClosedBeforeFirstUseNeverRunsTheResolver(t *testing.T) {
    serviceContainer := NewContainer()

    resolverRan := false
    registerErr := serviceContainer.RegisterScoped(
        "app.unit",
        func(resolver containercontract.Resolver) (*lazyProbeItem, error) {
            resolverRan = true
            return &lazyProbeItem{}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    requestScope := serviceContainer.NewScope()

    lazyService := Lazy[*lazyProbeItem](requestScope, "app.unit")

    if closeErr := requestScope.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    _, resolveErr := lazyService.Resolve()
    if nil == resolveErr || false == strings.Contains(resolveErr.Error(), "lazy service scope is closed") {
        t.Fatalf("expected the scope-is-closed refusal before any resolution, got %v", resolveErr)
    }

    if true == resolverRan {
        t.Fatalf("expected the resolver never to run over a closed scope")
    }
}

func TestLazyService_TheTerminalHandleDropsItsReferences(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.RegisterScoped(
        "app.unit",
        func(resolver containercontract.Resolver) (*lazyProbeItem, error) {
            return &lazyProbeItem{}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    requestScope := serviceContainer.NewScope()

    lazyService := Lazy[*lazyProbeItem](requestScope, "app.unit")
    _ = lazyService.Get()

    if closeErr := requestScope.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    _, _ = lazyService.Resolve()

    lazyService.mutex.Lock()
    defer lazyService.mutex.Unlock()

    if nil != lazyService.resolve || nil != lazyService.source {
        t.Fatalf("expected the closure and the resolver to be dropped")
    }

    if true == lazyService.resolved || nil != lazyService.value {
        t.Fatalf("expected the memoized value to be dropped")
    }
}

func TestLazyService_AContainerBackedHandleIsUntouchedByScopeLifecycles(t *testing.T) {
    serviceContainer := NewContainer()

    buildCount := 0
    registerErr := serviceContainer.Register(
        "app.shared",
        func(resolver containercontract.Resolver) (*lazyProbeItem, error) {
            buildCount = buildCount + 1
            return &lazyProbeItem{}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    lazyService := Lazy[*lazyProbeItem](serviceContainer, "app.shared")

    first := lazyService.Get()

    requestScope := serviceContainer.NewScope()
    if closeErr := requestScope.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if first != lazyService.Get() {
        t.Fatalf("expected the container-backed handle to keep serving its memoized value")
    }

    if 1 != buildCount {
        t.Fatalf("expected one build, got %d", buildCount)
    }
}

func TestLazyService_AContainerBackedHandleTurnsTerminalAfterContainerClose(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.shared",
        func(resolver containercontract.Resolver) (*lazyProbeItem, error) {
            return &lazyProbeItem{}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    lazyService := Lazy[*lazyProbeItem](serviceContainer, "app.shared")

    _ = lazyService.Get()

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    _, resolveErr := lazyService.Resolve()
    if nil == resolveErr {
        t.Fatalf("expected the container-backed handle to refuse after the container closed, not serve the dead value")
    }
}

func TestLazyService_AContainerBackedHandleServesTheClosingServiceDuringTheTeardown(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegister[*lazyProbeItem](serviceContainer, "app.dependency", func(resolver containercontract.Resolver) (*lazyProbeItem, error) {
        return &lazyProbeItem{name: "dependency"}, nil
    })

    reader := &lazyTeardownReader{
        lazyHandle: Lazy[*lazyProbeItem](serviceContainer, "app.dependency"),
        directResolve: func() (*lazyProbeItem, error) {
            return FromResolver[*lazyProbeItem](serviceContainer, "app.dependency")
        },
    }

    if _, resolveErr := reader.lazyHandle.Resolve(); nil != resolveErr {
        t.Fatalf("unexpected resolve error before the teardown: %v", resolveErr)
    }

    MustRegister[*lazyTeardownReader](serviceContainer, "app.reader", func(resolver containercontract.Resolver) (*lazyTeardownReader, error) {
        return reader, nil
    })
    MustFromResolver[*lazyTeardownReader](serviceContainer, "app.reader")

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if false == reader.ran {
        t.Fatalf("the closing service never ran, so the teardown window was never observed")
    }

    if nil != reader.directErr {
        t.Fatalf("the direct door is expected to answer a closing service, got %v", reader.directErr)
    }

    if nil != reader.lazyErr {
        t.Fatalf("expected the handle to answer the closing service exactly as the direct door did, got %v", reader.lazyErr)
    }
}

func TestLazyService_AProviderResolverBackedHandleServesTheClosingServiceDuringTheTeardown(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegister[*lazyProbeItem](serviceContainer, "app.dependency", func(resolver containercontract.Resolver) (*lazyProbeItem, error) {
        return &lazyProbeItem{name: "dependency"}, nil
    })

    reader := &lazyTeardownReader{}

    MustRegister[*lazyTeardownReader](serviceContainer, "app.reader", func(resolver containercontract.Resolver) (*lazyTeardownReader, error) {
        reader.lazyHandle = Lazy[*lazyProbeItem](resolver, "app.dependency")
        reader.directResolve = func() (*lazyProbeItem, error) {
            return FromResolver[*lazyProbeItem](resolver, "app.dependency")
        }

        return reader, nil
    })
    MustFromResolver[*lazyTeardownReader](serviceContainer, "app.reader")

    if _, resolveErr := reader.lazyHandle.Resolve(); nil != resolveErr {
        t.Fatalf("unexpected resolve error before the teardown: %v", resolveErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if false == reader.ran {
        t.Fatalf("the closing service never ran, so the teardown window was never observed")
    }

    if nil != reader.directErr {
        t.Fatalf("the direct door is expected to answer a closing service, got %v", reader.directErr)
    }

    if nil != reader.lazyErr {
        t.Fatalf("expected the handle to answer the closing service exactly as the direct door did, got %v", reader.lazyErr)
    }
}

func TestLazyService_AScopeBackedHandleAndTheScopeDoorRefuseTogetherDuringTheScopeTeardown(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegisterScoped[*lazyProbeItem](serviceContainer, "scoped.dependency", func(resolver containercontract.Resolver) (*lazyProbeItem, error) {
        return &lazyProbeItem{name: "dependency"}, nil
    })

    reader := &lazyTeardownReader{}

    MustRegisterScoped[*lazyTeardownReader](serviceContainer, "scoped.reader", func(resolver containercontract.Resolver) (*lazyTeardownReader, error) {
        return reader, nil
    })

    scopeInstance := serviceContainer.NewScope()

    reader.lazyHandle = Lazy[*lazyProbeItem](scopeInstance, "scoped.dependency")
    reader.directResolve = func() (*lazyProbeItem, error) {
        return FromResolver[*lazyProbeItem](scopeInstance, "scoped.dependency")
    }

    if _, resolveErr := reader.lazyHandle.Resolve(); nil != resolveErr {
        t.Fatalf("unexpected resolve error before the teardown: %v", resolveErr)
    }

    MustFromResolver[*lazyTeardownReader](scopeInstance, "scoped.reader")

    if closeErr := scopeInstance.Close(); nil != closeErr {
        t.Fatalf("unexpected scope close error: %v", closeErr)
    }

    if false == reader.ran {
        t.Fatalf("the closing service never ran, so the scope teardown was never observed")
    }

    if nil == reader.directErr {
        t.Fatalf("expected the scope door to refuse a resolution during its own teardown")
    }

    if nil == reader.lazyErr || false == errors.Is(reader.lazyErr, ErrScopeClosed) {
        t.Fatalf("expected the scope-backed handle to stay terminal with the scope-is-closed refusal, got %v", reader.lazyErr)
    }
}

func TestLazyService_AScopedProviderResolverBackedHandleTurnsTerminalWithTheScope(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegisterScoped[*lazyProbeItem](serviceContainer, "scoped.unit", func(resolver containercontract.Resolver) (*lazyProbeItem, error) {
        return &lazyProbeItem{name: "per-request"}, nil
    })

    capturedHandle := (*LazyService[*lazyProbeItem])(nil)
    MustRegisterScoped[string](serviceContainer, "scoped.holder", func(resolver containercontract.Resolver) (string, error) {
        capturedHandle = Lazy[*lazyProbeItem](resolver, "scoped.unit")

        return "holder", nil
    })

    requestScope := serviceContainer.NewScope()
    MustFromResolver[string](requestScope, "scoped.holder")

    if nil == capturedHandle {
        t.Fatalf("the scoped provider never ran, so no handle was captured")
    }

    if _, resolveErr := capturedHandle.Resolve(); nil != resolveErr {
        t.Fatalf("expected the live scope to serve the value, got %v", resolveErr)
    }

    if closeErr := requestScope.Close(); nil != closeErr {
        t.Fatalf("unexpected scope close error: %v", closeErr)
    }

    _, resolveErr := capturedHandle.Resolve()
    if nil == resolveErr {
        t.Fatalf("expected the handle to turn terminal with the request scope, not serve the dead request's value")
    }

    if false == errors.Is(resolveErr, ErrScopeClosed) {
        t.Fatalf("expected the scope-is-closed refusal, got %v", resolveErr)
    }
}

func TestLazyService_ASingletonsCapturedHandleOutlivesTheRequestThatBuiltIt(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegister[string](serviceContainer, "app.dependency", func(resolver containercontract.Resolver) (string, error) {
        return "process-lifetime-dependency", nil
    })

    capturedHandle := (*LazyService[string])(nil)
    MustRegister[*lazyProbeItem](serviceContainer, "app.singleton", func(resolver containercontract.Resolver) (*lazyProbeItem, error) {
        capturedHandle = Lazy[string](resolver, "app.dependency")

        return &lazyProbeItem{name: "singleton"}, nil
    })

    firstRequest := serviceContainer.NewScope()
    MustFromResolver[*lazyProbeItem](firstRequest, "app.singleton")

    if nil == capturedHandle {
        t.Fatalf("the singleton's provider never ran, so no handle was captured")
    }

    if _, resolveErr := capturedHandle.Resolve(); nil != resolveErr {
        t.Fatalf("unexpected resolve error inside the first request: %v", resolveErr)
    }

    if closeErr := firstRequest.Close(); nil != closeErr {
        t.Fatalf("unexpected scope close error: %v", closeErr)
    }

    value, resolveErr := capturedHandle.Resolve()
    if nil != resolveErr {
        t.Fatalf("expected the singleton's handle to outlive the request that built it, got %v", resolveErr)
    }

    if "process-lifetime-dependency" != value {
        t.Fatalf("expected the process-lifetime value, got %q", value)
    }
}
