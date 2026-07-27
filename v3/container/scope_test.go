package container

import (
    "reflect"
    "runtime"
    "sync"
    "sync/atomic"
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
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

/* @info a closed scope enumerates nothing, mirroring Has: the request is over and its collaborators are gone, so a late collection gets an empty set instead of reaching into a container the scope no longer holds */
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

/* scopeLifetimeProbe counts how many times it was built and closed, which is the whole question a request scope poses about a container service. */
type scopeLifetimeProbe struct {
    closed *atomic.Int64
}

func (instance *scopeLifetimeProbe) Close() error {
    instance.closed.Add(1)

    return nil
}

/* @info The container is request-agnostic: a service it owns is one instance for the whole process. Resolving that service THROUGH a request scope must not change what it is — the scope layers over the container for the code running inside a request, it does not reach underneath into the container's own wiring.

This is the shape that broke: a provider that asks for the logger. The kernel installs a request logger into every scope under the same name the container registers, so a provider doing nothing request-specific at all was assembled from a scope entry, kept per request, and closed when the request ended. Live in the repository: the bunorm providers read the logger while opening, so the *bun.DB pool was closed at the end of the request that first resolved it. The provider must see the container's own logger, be built once, and never be closed by a request ending. */
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

/* @info the other half of the same rule, and the reason it is safe: a container provider that genuinely needs something only a request carries is told the service does not exist, at the point the mistake is made, instead of quietly becoming a per-request object that a request ending destroys. */
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

    /* the scope carries it, so asking the scope directly answers */
    if value, getErr := requestScope.Get("request.context"); nil != getErr || "the-request" != value {
        t.Fatalf("expected the scope to answer for its own entry, got %v / %v", value, getErr)
    }

    /* the container provider may not, and must be told so */
    _, getErr := requestScope.Get("app.reporter")
    if nil == getErr {
        t.Fatal("a container provider reached a scope-only entry: it would hold one request for the life of the process")
    }
}
