package container

import (
    "errors"
    "reflect"
    "runtime"
    "strings"
    "sync"
    "sync/atomic"
    "testing"

    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/exception"
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

/* an override answers before anything else, and that holds for a name the container has ALREADY built: the container's own resolution reads its instance map without taking the exclusive lock when there is nothing to write, and a resolution layered over a scope must never be answered from there — the scope is the whole reason the caller asked through it. The container instance is built first here on purpose, because a name the container has not built yet cannot tell the two paths apart. */
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
        /* the panic must carry the descriptive wrapped GetByType error, not an obscure nil-pointer dereference from calling String() on a nil reflect.Type */
        if _, isRuntimeError := recoveredValue.(runtime.Error); true == isRuntimeError {
            t.Fatalf("expected a descriptive panic carrying the GetByType cause, got a runtime error: %v", recoveredValue)
        }
    }()

    _ = scope.MustGetByType(nil)
}

/* A service that reads nothing out of the scope has to stay a process singleton. Melody instantiates lazily, so the first resolution of nearly every service happens inside a request; scoping all of them would rebuild the whole graph — connection pools included — once per request. */
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

/* The container's graph must stay consistent with itself: after a scope-bound service was built, resolving the same name from the container may not answer with the instance that belongs to a closed request. */
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

/* An override was installed from outside and outlives the scope, and a singleton belongs to the root container which closes it at the end of the process; closing either here would tear down, once per request, something the next request still needs. */
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

/* scopeLifetimeProbe counts how many times it was built and closed, which is the whole question a request scope poses about a container service. */
type scopeLifetimeProbe struct {
    closed *atomic.Int64
}

func (instance *scopeLifetimeProbe) Close() error {
    instance.closed.Add(1)

    return nil
}

/* The container is request-agnostic: a service it owns is one instance for the whole process. Resolving that service THROUGH a request scope must not change what it is — the scope layers over the container for the code running inside a request, it does not reach underneath into the container's own wiring.

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

/* the other half of the same rule, and the reason it is safe: a container provider that genuinely needs something only a request carries is told the service does not exist, at the point the mistake is made, instead of quietly becoming a per-request object that a request ending destroys. */
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

/* a scoped service resolved BY TYPE is filed under its name AND its type, and the dependency edge targets the name node while the resolution stack carried the type node. Without the alias collapse the type node carried no edges, sorted ahead of every "scope:service:" key, and closed the shared instance in the first heap wave — the transaction underneath a repository still holding it. */
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

type aliasRepositoryService struct {
    recorder *scopedCloseRecorder
}

func (instance *aliasRepositoryService) Close() error {
    instance.recorder.record("repository")

    return nil
}

type dualFiledValueService struct {
    closeCount *int32
    padding    []string
}

func (instance dualFiledValueService) Close() error {
    atomic.AddInt32(instance.closeCount, 1)

    return nil
}

/* a VALUE-typed scoped service with an uncomparable field, filed under name and type, defeats both identity marks — no pointer, no equality — and used to be closed once per node. The alias link recorded at filing time is what tells the teardown the two nodes are one filing. */
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

/* a ClosedWithScope override replacing a created instance evicts it from the maps the teardown reads, but the evicted value is still the scope's to close — it waits in the graveyard and closes with the scope, exactly once. Before the graveyard it simply leaked, with both calls reporting success. */
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

/* the three panicking override doors on a scope had never been executed. Two of them are not on the Scope interface at all — they are reached through the optional options companion, which is exactly the shape a caller gets wrong — and all three have to carry their own message: a request-scoped substitution that failed has to say whether the protected door or the plain one refused it. */
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

/* the options companion is a separate interface precisely so the four original signatures never move, which means a caller reaches it through a type assertion — and a scope that stopped satisfying it would fail silently at that assertion rather than at compile time. The assertion is part of what this test pins. */
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

/* the protected door admits the "service." namespace the plain one refuses, and its panicking form has to say so — a failure reported with the plain door's message would send a reader looking for a protection that was never in the way. Its refusal here is the closed scope, the only thing the protected door still turns away. */
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

/* a scope-level substitution of a protected name is the framework's own service replaced inside a live request, which is the one thing the namespace exists to prevent; the scope door refuses it exactly as the container door does. */
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

/* an empty name on a scope is refused before the options are even read, for the reason the container refuses it: nothing filed under it can ever be asked for again, and a request-scoped substitution silently lost is worse than one that failed loudly. */
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

/* a nil substituted into a scope is dereferenced by the first handler that reads it, inside the request, and a typed nil boxed into an interface is not equal to nil — so the plain comparison and the interface one are two guards, and each has to be entered by something. */
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

/* Get on a scope answers the empty-name refusal rather than reaching the resolver with it, so a caller whose configuration resolved a name away is told what it did rather than shown a missing-service report about a service nobody named. */
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

/* Has and HasType on a scope answer false for the empty name and the nil type. Both refusals are SHADOWED — nothing can be filed under the empty name, and canonicalServiceType answers nil for a nil type so the guard below returns the same false — so this pins the verdict a caller relies on, not the position of the guard. */
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

type scopeCloseFailingService struct {
    failure error
}

func (instance *scopeCloseFailingService) Close() error {
    return instance.failure
}

/* a scoped service whose Close fails is the request's own teardown reporting a leak, and the report has to reach the caller of Close rather than being swallowed into the silence a nil return means — a connection returned to nobody looks exactly like a clean scope from the outside. */
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

/* newScope falls back to an empty plan when it is handed none, which no public path can produce — NewScope always passes the container's published plan, and the rebuild never yields nil. The fallback is what keeps a scope built from a nil plan usable instead of dereferencing it on the first Has, so it is pinned white-box, with the state constructed by hand because the API cannot construct it. */
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

/* the scope override propagates to every type its name is registered under, exactly as the container-level sibling propagates: without it, a type-keyed resolution through the scope answered the container's memoized instance while the name answered the override — a divergence that appeared exactly once the container had built the name, the ordinary warm state of a running process */
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

/* the condition the propagation stands under: the maps written are the scope's own, so the container and every other scope keep answering the container's instance, and the override dies with its scope */
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

/* the propagation carries the container sibling's guard with it: a value a registered type cannot hold is refused before anything is written, so the name and type maps never learn two different answers */
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

/* a closed-with-scope override filed under the name and every registered type is still one value: the alias links written at filing time collapse the nodes, and the teardown closes it once */
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

/* the propagation reads the scoped plan too: a scoped service resolved by type files what it built under the registered type, and an override arriving after that build must outrank it — filed under the value's concrete type alone, the created instance kept answering the type while the name answered the override */
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

/* the same rule for a registration made on the live scope itself, the third place a name can be registered under a type */
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

func TestScope_ContainerAnswersTheContainerAndNilAfterClose(t *testing.T) {
    serviceContainer := NewContainer()
    scopeInstance := serviceContainer.NewScope().(*scope)

    carried := scopeInstance.Container()
    if serviceContainer != carried {
        t.Fatalf("expected the scope to answer the container it layers over, got %v", carried)
    }

    if closeErr := scopeInstance.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if nil != scopeInstance.Container() {
        t.Fatal("expected a closed scope to answer nil")
    }
}
