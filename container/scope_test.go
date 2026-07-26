package container

import (
    "reflect"
    "runtime"
    "strings"
    "sync"
    "sync/atomic"
    "testing"

    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
)

type scopeTestService struct {
    value string
}

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

func TestScope_CloseReturnsErrorOnGet(t *testing.T) {
    serviceContainer := NewContainer()

    scope := serviceContainer.NewScope()
    _ = scope.Close()

    _, getErr := scope.Get("service.test")
    if nil == getErr {
        t.Fatalf("expected closed-scope error")
    }
}

func TestScope_CloseKeepsMustGetPanicking(t *testing.T) {
    serviceContainer := NewContainer()

    scope := serviceContainer.NewScope()
    _ = scope.Close()

    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic from MustGet on a closed scope")
        }
    }()

    _ = scope.MustGet("service.test")
}

func TestScope_HasReturnsFalseWhenClosed(t *testing.T) {
    serviceContainer := NewContainer()

    scope := serviceContainer.NewScope()
    _ = scope.Close()

    if true == scope.Has("a") {
        t.Fatalf("expected false")
    }
}

func TestScope_CloseIsIdempotent(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    if err := scopeInstance.Close(); nil != err {
        t.Fatalf("unexpected first close error: %v", err)
    }

    if err := scopeInstance.Close(); nil != err {
        t.Fatalf("unexpected second close error: %v", err)
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
}

func TestScope_OverrideAfterCloseKeepsMustPanicking(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()
    _ = scopeInstance.Close()

    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic from MustOverrideProtectedInstance on a closed scope")
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
}

func TestScope_GetByTypeAfterCloseKeepsMustPanicking(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()
    _ = scopeInstance.Close()

    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic from MustGetByType on a closed scope")
        }
    }()

    _ = scopeInstance.MustGetByType(reflect.TypeOf((*scopeTestService)(nil)))
}

func TestScope_HasTypeReturnsFalseWhenClosed(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()
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
        /* @important the panic must carry the descriptive wrapped GetByType error, not an obscure nil-pointer dereference from calling String() on a nil reflect.Type */
        if _, isRuntimeError := recoveredValue.(runtime.Error); true == isRuntimeError {
            t.Fatalf("expected a descriptive panic carrying the GetByType cause, got a runtime error: %v", recoveredValue)
        }
    }()

    _ = scope.MustGetByType(nil)
}

/* @info A provider that reached for a per-request override through the resolver it was handed had its result written into the ROOT container's instances, so the second request was served the first request's object: the kernel puts the request logger (carrying the request id) and the request context in the scope, so following the documented "collect with the resolver a provider receives" rule froze request 1's request id into a process-lifetime singleton. */
func TestScope_ServiceBuiltFromAnOverrideDoesNotOutliveTheScope(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.consumer",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            tag, getErr := resolver.Get("app.tag")
            if nil != getErr {
                return nil, getErr
            }

            return &scopeTestService{value: tag.(*scopeTestService).value}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    resolveThroughScope := func(tag string) *scopeTestService {
        t.Helper()

        scopeInstance := serviceContainer.NewScope()

        overrideErr := scopeInstance.OverrideInstance("app.tag", &scopeTestService{value: tag})
        if nil != overrideErr {
            t.Fatalf("unexpected override error: %v", overrideErr)
        }

        value, getErr := scopeInstance.Get("app.consumer")
        if nil != getErr {
            t.Fatalf("unexpected get error: %v", getErr)
        }

        closeErr := scopeInstance.Close()
        if nil != closeErr {
            t.Fatalf("unexpected scope close error: %v", closeErr)
        }

        return value.(*scopeTestService)
    }

    first := resolveThroughScope("request-1")
    second := resolveThroughScope("request-2")

    if "request-1" != first.value {
        t.Fatalf("expected the first scope to build from its own override, got %q", first.value)
    }

    if "request-2" != second.value {
        t.Fatalf("expected the second scope to build from its own override, got %q — the first request's instance was kept in the root container", second.value)
    }

    if first == second {
        t.Fatalf("expected each scope to hold its own instance, got one shared object")
    }
}

/* @info Repeating the resolution inside one scope must still build the service once: it belongs to the scope, not to every call. */
func TestScope_ServiceBuiltFromAnOverrideIsBuiltOncePerScope(t *testing.T) {
    serviceContainer := NewContainer()

    calls := 0

    registerErr := serviceContainer.Register(
        "app.consumer",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            calls = calls + 1

            tag, getErr := resolver.Get("app.tag")
            if nil != getErr {
                return nil, getErr
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

    first, getErr := scopeInstance.Get("app.consumer")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    second, getErr := scopeInstance.Get("app.consumer")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if 1 != calls {
        t.Fatalf("expected the provider to run once per scope, ran %d times", calls)
    }

    if first != second {
        t.Fatalf("expected the scope to hand back the instance it already built")
    }
}

/* @info A service that reads nothing out of the scope has to stay a process singleton. Melody instantiates lazily, so the first resolution of nearly every service happens inside a request; scoping all of them would rebuild the whole graph — connection pools included — once per request. */
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

/* @info The container's graph must stay consistent with itself: after a scope-bound service was built, resolving the same name from the container may not answer with the instance that belongs to a closed request. */
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

type closeCountingScopeService struct {
    value      string
    closeCalls *int32
    closeErr   error
}

func (instance *closeCountingScopeService) Close() error {
    atomic.AddInt32(instance.closeCalls, 1)

    return instance.closeErr
}

/* @info A service the scope built is scope-bound: it holds the request's substitutes and no later request will ever be handed it, so if the scope does not close it nothing else ever will. */
func TestScope_CloseClosesTheServicesItBuiltItself(t *testing.T) {
    serviceContainer := NewContainer()

    var closeCalls int32

    registerErr := serviceContainer.Register(
        "app.consumer",
        func(resolver containercontract.Resolver) (*closeCountingScopeService, error) {
            tag, getErr := resolver.Get("app.tag")
            if nil != getErr {
                return nil, getErr
            }

            return &closeCountingScopeService{
                value:      tag.(*scopeTestService).value,
                closeCalls: &closeCalls,
            }, nil
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

    closeErr := scopeInstance.Close()
    if nil != closeErr {
        t.Fatalf("unexpected scope close error: %v", closeErr)
    }

    if 1 != atomic.LoadInt32(&closeCalls) {
        t.Fatalf("expected the scope to close the service it built exactly once, got %d", atomic.LoadInt32(&closeCalls))
    }
}

/* @info An override was installed from outside and outlives the scope, and a singleton belongs to the root container which closes it at the end of the process; closing either here would tear down, once per request, something the next request still needs. */
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

/* @info The same instance is filed under its name and under its type when both were known, and the aliases must collapse onto one Close, exactly as the container's own teardown collapses them. */
func TestScope_CloseClosesAServiceFiledUnderBothNameAndTypeOnce(t *testing.T) {
    serviceContainer := NewContainer()

    var closeCalls int32

    registerErr := serviceContainer.Register(
        "app.consumer",
        func(resolver containercontract.Resolver) (*closeCountingScopeService, error) {
            tag, getErr := resolver.Get("app.tag")
            if nil != getErr {
                return nil, getErr
            }

            return &closeCountingScopeService{
                value:      tag.(*scopeTestService).value,
                closeCalls: &closeCalls,
            }, nil
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

    _, getByTypeErr := scopeInstance.GetByType(reflect.TypeOf(&closeCountingScopeService{}))
    if nil != getByTypeErr {
        t.Fatalf("unexpected get by type error: %v", getByTypeErr)
    }

    closeErr := scopeInstance.Close()
    if nil != closeErr {
        t.Fatalf("unexpected scope close error: %v", closeErr)
    }

    if 1 != atomic.LoadInt32(&closeCalls) {
        t.Fatalf("expected one close for the instance behind both entries, got %d", atomic.LoadInt32(&closeCalls))
    }
}

/* @info A scope closes on the way out of a handler, so one service that fails or panics on Close must not keep the rest of that request's services alive; the failures are reported the way the container reports its own. */
func TestScope_CloseReportsFailuresAndStillClosesTheRest(t *testing.T) {
    serviceContainer := NewContainer()

    var failingCloseCalls int32
    var panickingCloseCalls int32
    var healthyCloseCalls int32

    registerErr := serviceContainer.Register(
        "app.failing",
        func(resolver containercontract.Resolver) (*closeCountingScopeService, error) {
            _, getErr := resolver.Get("app.tag")
            if nil != getErr {
                return nil, getErr
            }

            return &closeCountingScopeService{
                value:      "failing",
                closeCalls: &failingCloseCalls,
                closeErr:   exception.NewError("close refused", nil, nil),
            }, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    registerErr = serviceContainer.Register(
        "app.panicking",
        func(resolver containercontract.Resolver) (*panickingScopeService, error) {
            _, getErr := resolver.Get("app.tag")
            if nil != getErr {
                return nil, getErr
            }

            return &panickingScopeService{closeCalls: &panickingCloseCalls}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    registerErr = serviceContainer.Register(
        "app.healthy",
        func(resolver containercontract.Resolver) (*healthyScopeService, error) {
            _, getErr := resolver.Get("app.tag")
            if nil != getErr {
                return nil, getErr
            }

            return &healthyScopeService{closeCalls: &healthyCloseCalls}, nil
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

    for _, serviceName := range []string{"app.failing", "app.panicking", "app.healthy"} {
        _, getErr := scopeInstance.Get(serviceName)
        if nil != getErr {
            t.Fatalf("unexpected get error for %q: %v", serviceName, getErr)
        }
    }

    closeErr := scopeInstance.Close()
    if nil == closeErr {
        t.Fatalf("expected the scope close to report the failing services")
    }

    if false == strings.Contains(closeErr.Error(), "failed to close scope services") {
        t.Fatalf("unexpected scope close error: %v", closeErr)
    }

    if 1 != atomic.LoadInt32(&failingCloseCalls) {
        t.Fatalf("expected the failing service to be closed once, got %d", atomic.LoadInt32(&failingCloseCalls))
    }

    if 1 != atomic.LoadInt32(&panickingCloseCalls) {
        t.Fatalf("expected the panicking service to be closed once, got %d", atomic.LoadInt32(&panickingCloseCalls))
    }

    if 1 != atomic.LoadInt32(&healthyCloseCalls) {
        t.Fatalf("expected the healthy service to be closed even though another one failed, got %d", atomic.LoadInt32(&healthyCloseCalls))
    }
}

type healthyScopeService struct {
    closeCalls *int32
}

func (instance *healthyScopeService) Close() error {
    atomic.AddInt32(instance.closeCalls, 1)

    return nil
}

type panickingScopeService struct {
    closeCalls *int32
}

func (instance *panickingScopeService) Close() error {
    atomic.AddInt32(instance.closeCalls, 1)

    exception.Panic(exception.NewError("close panicked", nil, nil))

    return nil
}

/* @info Closing twice must not close the same service twice: the cli path closes the runtime scope and the deferred close in runCli reaches it again. */
func TestScope_CloseTwiceClosesTheCreatedServicesOnlyOnce(t *testing.T) {
    serviceContainer := NewContainer()

    var closeCalls int32

    registerErr := serviceContainer.Register(
        "app.consumer",
        func(resolver containercontract.Resolver) (*closeCountingScopeService, error) {
            tag, getErr := resolver.Get("app.tag")
            if nil != getErr {
                return nil, getErr
            }

            return &closeCountingScopeService{
                value:      tag.(*scopeTestService).value,
                closeCalls: &closeCalls,
            }, nil
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

    if nil != scopeInstance.Close() {
        t.Fatalf("unexpected scope close error")
    }

    if nil != scopeInstance.Close() {
        t.Fatalf("unexpected second scope close error")
    }

    if 1 != atomic.LoadInt32(&closeCalls) {
        t.Fatalf("expected exactly one close across two scope closes, got %d", atomic.LoadInt32(&closeCalls))
    }
}
