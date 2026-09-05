package container

import (
    "errors"
    "strings"
    "sync"
    "testing"
    "time"

    containercontract "github.com/precision-soft/melody/v2/container/contract"
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

    /* the handle is built before the provider exists — exactly the boot-phase ordering the helper is for: a cli command captures the lazy handle while services are still being registered. */
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

/* the resolver runs outside the handle's lock, so a resolver that reaches back into the same handle — a provider chain that cycles through a lazy handle — recurses into the resolution machinery, where a cycle guard can answer with an error; under a lock held across the resolution it would deadlock instead, unreachable by any cycle detection. */
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

type lazyProbeItem struct {
    name string
}

/* a resolution yielding nil without an error must not be memoized as success: Get keeps its documented retry-on-failure promise only if the next call re-runs the resolver instead of returning the poisoned nil forever. */
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

/* what this guards is race-freedom, and nothing else: the value assertion holds whether or not Resolve synchronizes its memo — a shared container service is memoized by the container too, so every caller observes the one instance either way — which means a lost mutex here is invisible without the detector. The ci race job and .dev/validate/all.sh therefore run this package under -race, and this test is only meaningful there. */
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

/* Get is the panicking door of the handle, and its nil-yield refusal had never been entered: a resolver that answers (nil, nil) — a provider whose value the container never filed, a typed nil boxed by a foreign resolver — would otherwise hand the caller a nil the compiler says is a live service, dereferenced somewhere far from the handle. The refusal has to carry its own message, because the failure path of the same call answers with whatever the resolver refused and a reader has to be able to tell the two apart. */
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

/* the failure door of Get answers with the resolver's own error rather than the nil-yield message, which is what keeps the two refusals distinguishable in a boot log: a service that is missing and a service that resolved to nothing are different mistakes. */
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

/* the deferred by-type handle is the counterpart of Lazy for a component assembled before the service is safe to resolve; nothing executed it, so the closure it builds — the one that reaches FromResolverByType rather than FromResolver — was never proven to resolve anything at all. */
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

/* a by-type handle over a type nothing registered has to fail through the by-type resolution, not through a name lookup: the two doors report differently, and a handle wired to the wrong one would name a service the caller never spelled. */
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

/* a handle follows the scope of the resolver it was built over: once the scope closes, serving the memoized value would hand a dead request's state to every later caller forever — the answer is the scope-is-closed error, on this call and each one after */
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

    _, secondErr := lazyService.Resolve()
    if nil == secondErr || false == strings.Contains(secondErr.Error(), "lazy service scope is closed") {
        t.Fatalf("expected the terminal state to answer the same on every later call, got %v", secondErr)
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

/* the transition drops the memoized value, the closure and the resolver together — the handle keeps no path to the dead request's state, which is what lets the garbage collector take the scope */
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

/* a resolver that cannot answer the liveness question is read as open, exactly as the exit handler reads a logger that cannot: the container never reports closed through this door, so a container-backed handle keeps its memoization for the life of the process */
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

/* the closing service reaches through the handle from inside its own Close, which is the only place the
teardown window can be observed from: outside it the container is either open or finished. */
type lazyTeardownReader struct {
    lazyHandle    *LazyService[*lazyProbeItem]
    directResolve func() (*lazyProbeItem, error)

    lazyErr   error
    directErr error
    ran       bool
}

func (instance *lazyTeardownReader) Close() error {
    instance.ran = true
    _, instance.lazyErr = instance.lazyHandle.Resolve()
    _, instance.directErr = instance.directResolve()

    return nil
}

/* A service closing through a handle must be served what the resolver beside it still answers directly.
The container raises the flag IsClosed reports at the START of its teardown and stops answering resolutions
only at the end, deliberately, so that a service's own Close is entitled to what it depends on. A handle
that read the earlier flag turned terminal for the whole window and dropped its memoized value on the way,
so a worker releasing a lease from Close was refused by the handle and served by the door beside it. */
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

    /* memoized before anything closes, so a drop is observable as a loss rather than a miss */
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

/* The same window, read through the resolver a provider was handed — the shape the LazyService godoc
recommends when the teardown ordering matters, and therefore the shape that must not be the broken one.
That view carries no scope, so its liveness answer comes from the container underneath it. */
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

/* The scope keeps the terminal reading, and this is what says so: a scope stops answering resolutions the
moment it is marked closed, so both its doors refuse together and a handle over it must go on refusing. The
window the container has does not exist there, and widening the container's repair onto the scope would
serve a dead request's state. */
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

    if nil == reader.lazyErr || false == strings.Contains(reader.lazyErr.Error(), "lazy service scope is closed") {
        t.Fatalf("expected the scope-backed handle to stay terminal with the scope-is-closed refusal, got %v", reader.lazyErr)
    }
}

/* A handle built over the resolver a SCOPED provider was handed holds that one request's state, so it has
to turn terminal with the request — the container it layers over is still live and would answer forever.
This is the arm the container's own teardown window must not be widened onto. */
func TestLazyService_AScopedProviderResolverBackedHandleTurnsTerminalWithTheScope(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegisterScoped[*lazyProbeItem](serviceContainer, "scoped.unit", func(resolver containercontract.Resolver) (*lazyProbeItem, error) {
        return &lazyProbeItem{name: "per-request"}, nil
    })

    /* the holder carries its own type: two scoped registrations of one type collide on the canonical key */
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

    if false == strings.Contains(resolveErr.Error(), "lazy service scope is closed") {
        t.Fatalf("expected the scope-is-closed refusal, got %v", resolveErr)
    }
}

/* A service registered on the CONTAINER is one instance for the whole process, but its provider first
runs inside whichever request happened to reach it, and the view it is handed still carries that request's
scope — suspended, so the provider's own wiring cannot read it. The liveness question has to follow the
same predicate: a suspended view reads the container, so a handle the singleton captured must outlive the
request that built it. Asking only whether a scope pointer was present killed the handle with that one
request, while the container went on answering the same name directly for the rest of the process. */
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

    /* the singleton is first reached through a request, which is the ordinary case */
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
