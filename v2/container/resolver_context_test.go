package container

import (
    "errors"
    "reflect"
    "strconv"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/exception"
)

type resolverRaceProbeFirst struct{}
type resolverRaceProbeSecond struct{}

type resolverRaceRegisteredZero struct{}
type resolverRaceRegisteredOne struct{}
type resolverRaceRegisteredTwo struct{}
type resolverRaceRegisteredThree struct{}
type resolverRaceRegisteredFour struct{}
type resolverRaceRegisteredFive struct{}

func TestResolverContext_GetSnapshotsProviderUnderTheLock(t *testing.T) {
    serviceContainer := NewContainer()

    var waitGroup sync.WaitGroup
    var stop int32

    readerCount := 4
    readersStarted := make(chan struct{}, readerCount)

    for readerIndex := 0; readerIndex < readerCount; readerIndex++ {
        missingServiceName := "resolver.race.missing." + strconv.Itoa(readerIndex)

        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()

            readersStarted <- struct{}{}

            for 0 == atomic.LoadInt32(&stop) {
                /* resolving a never-registered service reaches the create closure, which reads providers[serviceName] with the mutex released. */
                _, _ = serviceContainer.Get(missingServiceName)
            }
        }()
    }

    for readerIndex := 0; readerIndex < readerCount; readerIndex++ {
        <-readersStarted
    }

    for registrationIndex := 0; registrationIndex < 2000; registrationIndex++ {
        registerErr := serviceContainer.Register(
            "resolver.race.service."+strconv.Itoa(registrationIndex),
            func(resolver containercontract.Resolver) (*testService, error) {
                return &testService{Value: "ok"}, nil
            },
            WithoutTypeRegistration(),
        )
        if nil != registerErr {
            atomic.StoreInt32(&stop, 1)
            waitGroup.Wait()
            t.Fatalf("register: %v", registerErr)
        }
    }

    atomic.StoreInt32(&stop, 1)
    waitGroup.Wait()
}

func TestResolverContext_GetByTypeSnapshotsTypeProviderUnderTheLock(t *testing.T) {
    serviceContainer := NewContainer()

    var waitGroup sync.WaitGroup
    var stop int32

    probeTypes := []reflect.Type{
        reflect.TypeOf((*resolverRaceProbeFirst)(nil)),
        reflect.TypeOf((*resolverRaceProbeSecond)(nil)),
    }

    readersStarted := make(chan struct{}, len(probeTypes))

    for _, probeType := range probeTypes {
        probeType := probeType

        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()

            readersStarted <- struct{}{}

            for 0 == atomic.LoadInt32(&stop) {
                /* an unregistered target type reaches the type-branch create closure, which reads typeProviders[type] with the mutex released. */
                _, _ = serviceContainer.GetByType(probeType)
            }
        }()
    }

    for range probeTypes {
        <-readersStarted
    }

    registerTypeFuncs := []func() error{
        func() error {
            return RegisterType[*resolverRaceRegisteredZero](serviceContainer, func(resolver containercontract.Resolver) (*resolverRaceRegisteredZero, error) {
                return &resolverRaceRegisteredZero{}, nil
            })
        },
        func() error {
            return RegisterType[*resolverRaceRegisteredOne](serviceContainer, func(resolver containercontract.Resolver) (*resolverRaceRegisteredOne, error) {
                return &resolverRaceRegisteredOne{}, nil
            })
        },
        func() error {
            return RegisterType[*resolverRaceRegisteredTwo](serviceContainer, func(resolver containercontract.Resolver) (*resolverRaceRegisteredTwo, error) {
                return &resolverRaceRegisteredTwo{}, nil
            })
        },
        func() error {
            return RegisterType[*resolverRaceRegisteredThree](serviceContainer, func(resolver containercontract.Resolver) (*resolverRaceRegisteredThree, error) {
                return &resolverRaceRegisteredThree{}, nil
            })
        },
        func() error {
            return RegisterType[*resolverRaceRegisteredFour](serviceContainer, func(resolver containercontract.Resolver) (*resolverRaceRegisteredFour, error) {
                return &resolverRaceRegisteredFour{}, nil
            })
        },
        func() error {
            return RegisterType[*resolverRaceRegisteredFive](serviceContainer, func(resolver containercontract.Resolver) (*resolverRaceRegisteredFive, error) {
                return &resolverRaceRegisteredFive{}, nil
            })
        },
    }

    for _, registerTypeFunc := range registerTypeFuncs {
        registerErr := registerTypeFunc()
        if nil != registerErr {
            atomic.StoreInt32(&stop, 1)
            waitGroup.Wait()
            t.Fatalf("register type: %v", registerErr)
        }
    }

    atomic.StoreInt32(&stop, 1)
    waitGroup.Wait()
}

type suspensionHasProbe struct {
    value string
}

func TestResolverContext_HasHonorsScopeSuspension(t *testing.T) {
    serviceContainer := NewContainer()

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.scoped.only",
        func(resolver containercontract.Resolver) (*suspensionHasProbe, error) {
            return &suspensionHasProbe{value: "scoped"}, nil
        },
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    hasDuringSuspension := true
    hasTypeDuringSuspension := true

    registerErr := serviceContainer.Register(
        "app.container.owned",
        func(resolver containercontract.Resolver) (*resolverRaceProbeFirst, error) {
            hasDuringSuspension = resolver.Has("app.scoped.only")
            hasTypeDuringSuspension = resolver.HasType(reflect.TypeOf((*suspensionHasProbe)(nil)))

            return &resolverRaceProbeFirst{}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get("app.container.owned"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    if true == hasDuringSuspension {
        t.Fatalf("expected Has to refuse the scope-only name while the scope is suspended")
    }

    if true == hasTypeDuringSuspension {
        t.Fatalf("expected HasType to refuse the scope-only type while the scope is suspended")
    }

    if false == scopeInstance.Has("app.scoped.only") {
        t.Fatalf("expected the unsuspended scope to keep answering for its own name")
    }
}

type resolverContextMustProbe struct {
    value string
}

type resolverContextMustDependent struct {
    dependency *resolverContextMustProbe
}

func TestResolverContext_MustGet_AnswersInsideAProviderAndNamesItsOwnFailure(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.dependency",
        func(resolver containercontract.Resolver) (*resolverContextMustProbe, error) {
            return &resolverContextMustProbe{value: "dependency"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    registerErr = serviceContainer.Register(
        "app.dependent",
        func(resolver containercontract.Resolver) (*resolverContextMustDependent, error) {
            dependency := resolver.MustGet("app.dependency").(*resolverContextMustProbe)

            return &resolverContextMustDependent{dependency: dependency}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    value, getErr := serviceContainer.Get("app.dependent")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    dependent, isDependent := value.(*resolverContextMustDependent)
    if false == isDependent || "dependency" != dependent.dependency.value {
        t.Fatalf("expected the provider to have resolved its dependency, got %#v", value)
    }

    registerErr = serviceContainer.Register(
        "app.broken",
        func(resolver containercontract.Resolver) (*resolverContextMustDependent, error) {
            _ = resolver.MustGet("app.never.declared")

            return &resolverContextMustDependent{}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    _, brokenErr := serviceContainer.Get("app.broken")
    if nil == brokenErr {
        t.Fatalf("expected the provider's missing dependency to fail the resolution")
    }

    if false == strings.Contains(renderedCauseChain(brokenErr), "service is not registered") {
        t.Fatalf("expected the original failure in the cause chain, got %q", renderedCauseChain(brokenErr))
    }

    if true == strings.Contains(renderedCauseChain(brokenErr), "failed to get service instance") {
        t.Fatalf("expected no rebuilt wrapper in the cause chain, got %q", renderedCauseChain(brokenErr))
    }

    if "app.never.declared" != contextValueInChain(brokenErr, "serviceName") {
        t.Fatalf("expected the service name written into the original failure's context, got chain %q", renderedCauseChain(brokenErr))
    }
}

func TestResolverContext_MustGetByType_AnswersInsideAProviderAndNamesItsOwnFailure(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.dependency",
        func(resolver containercontract.Resolver) (*resolverContextMustProbe, error) {
            return &resolverContextMustProbe{value: "dependency"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    registerErr = serviceContainer.Register(
        "app.dependent",
        func(resolver containercontract.Resolver) (*resolverContextMustDependent, error) {
            dependency := resolver.MustGetByType(reflect.TypeOf((*resolverContextMustProbe)(nil))).(*resolverContextMustProbe)

            return &resolverContextMustDependent{dependency: dependency}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    value, getErr := serviceContainer.Get("app.dependent")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    dependent, isDependent := value.(*resolverContextMustDependent)
    if false == isDependent || "dependency" != dependent.dependency.value {
        t.Fatalf("expected the provider to have resolved its dependency by type, got %#v", value)
    }

    registerErr = serviceContainer.Register(
        "app.broken",
        func(resolver containercontract.Resolver) (*resolverContextMustDependent, error) {
            _ = resolver.MustGetByType(reflect.TypeOf((*resolverRaceProbeFirst)(nil)))

            return &resolverContextMustDependent{}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    _, brokenErr := serviceContainer.Get("app.broken")
    if nil == brokenErr {
        t.Fatalf("expected the provider's missing dependency to fail the resolution")
    }

    if false == strings.Contains(renderedCauseChain(brokenErr), "service type is not registered") {
        t.Fatalf("expected the original by-type failure in the cause chain, got %q", renderedCauseChain(brokenErr))
    }

    if "" == contextValueInChain(brokenErr, "type") {
        t.Fatalf("expected the type written into the original failure's context, got chain %q", renderedCauseChain(brokenErr))
    }
}

func TestResolverContext_MustGet_KeepsTheAlreadyLoggedMarkOfTheFailure(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.logged.failure",
        func(resolver containercontract.Resolver) (*resolverContextMustDependent, error) {
            return nil, exception.MarkLogged(exception.NewError("the provider refused after logging", nil, nil))
        },
        WithoutTypeRegistration(),
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the failing resolution to panic")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic value, got %#v", recoveredValue)
        }

        if false == exception.IsAlreadyLogged(recoveredErr) {
            t.Fatalf("expected the panic value to keep the already-logged mark of the provider's failure")
        }

        if "app.logged.failure" != contextValueInChain(recoveredErr, "serviceName") {
            t.Fatalf("expected the service name written into the original failure's context, got %v", recoveredErr)
        }
    }()

    _ = serviceContainer.MustGet("app.logged.failure")
}

/* contextValueInChain walks the wrap chain for the first melody error whose context carries key, because the original failure travels out whole and its coordinates live in its context, not in a wrapper's message. */
func contextValueInChain(err error, key string) string {
    for current := err; nil != current; current = errors.Unwrap(current) {
        melodyErr, isMelodyErr := current.(*exception.Error)
        if false == isMelodyErr || nil == melodyErr {
            continue
        }

        value, exists := melodyErr.Context()[key]
        if false == exists {
            continue
        }

        stringValue, isString := value.(string)
        if true == isString {
            return stringValue
        }
    }

    return ""
}

/* renderedCauseChain walks the whole chain because a provider panic is wrapped by the creation guard before it reaches the caller, and only the chain says what the provider itself refused. */
func renderedCauseChain(err error) string {
    rendered := ""
    for current := err; nil != current; current = errors.Unwrap(current) {
        rendered = rendered + current.Error() + "\n"
    }

    return rendered
}

func TestResolverContext_Get_EmptyNameRefused(t *testing.T) {
    serviceContainer := NewContainer()

    _, getErr := serviceContainer.Get("")
    if nil == getErr {
        t.Fatalf("expected an empty service name to be refused")
    }

    if "service name is required in get" != getErr.Error() {
        t.Fatalf("unexpected refusal message: %q", getErr.Error())
    }
}

func TestResolverContext_GetByType_NilTypeRefused(t *testing.T) {
    serviceContainer := NewContainer()

    _, getByTypeErr := serviceContainer.GetByType(nil)
    if nil == getByTypeErr {
        t.Fatalf("expected a nil type to be refused")
    }

    if "service type is required in get by type" != getByTypeErr.Error() {
        t.Fatalf("unexpected refusal message: %q", getByTypeErr.Error())
    }
}

func TestResolverContext_CarriesTheContainerThroughTheCarrierDoor(t *testing.T) {
    serviceContainer := NewContainer()

    var carried containercontract.Container
    serviceContainer.MustRegister(
        "probe.carrier",
        func(resolver containercontract.Resolver) (string, error) {
            carrier, isCarrier := resolver.(containercontract.ContainerCarrier)
            if false == isCarrier {
                return "", nil
            }

            carried = carrier.Container()

            return "built", nil
        },
    )

    if _, resolveErr := serviceContainer.Get("probe.carrier"); nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    if serviceContainer != carried {
        t.Fatalf("expected the provider's resolver to carry the container, got %v", carried)
    }
}

type resolverContextGraphDependency struct{}

type resolverContextGraphParent struct {
    dependency *resolverContextGraphDependency
}

type resolverContextGraphControlParent struct {
    dependency *resolverContextGraphDependency
}

/* the container's dependency graph is never pruned and its teardown walks only container-created representatives, so an edge recorded under a scoped parent would sit there unread for the life of the process — one permanent entry per distinct scoped name. The container-parent edge asserted beside it is the control that the filter removed only the scoped writes. */
func TestResolverContext_AScopedParentWritesNoEdgeIntoTheContainerGraph(t *testing.T) {
    serviceContainer := NewContainer().(*container)

    serviceContainer.MustRegister(
        "app.container.dependency",
        func(resolver containercontract.Resolver) (*resolverContextGraphDependency, error) {
            return &resolverContextGraphDependency{}, nil
        },
    )

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.scoped.parent",
        func(resolver containercontract.Resolver) (*resolverContextGraphParent, error) {
            dependency, getErr := resolver.Get("app.container.dependency")
            if nil != getErr {
                return nil, getErr
            }

            /* the by-type door runs the same filter at its own site, so both writes are exercised by the one scoped parent */
            if _, getByTypeErr := resolver.GetByType(reflect.TypeOf((*resolverContextGraphDependency)(nil))); nil != getByTypeErr {
                return nil, getByTypeErr
            }

            return &resolverContextGraphParent{dependency: dependency.(*resolverContextGraphDependency)}, nil
        },
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    scopeInstance := serviceContainer.NewScope()
    defer scopeInstance.Close()

    if _, getErr := scopeInstance.Get("app.scoped.parent"); nil != getErr {
        t.Fatalf("unexpected scoped resolution error: %v", getErr)
    }

    serviceContainer.mutex.Lock()
    scopedKeys := make([]string, 0)
    for dependentKey := range serviceContainer.dependencyGraph {
        if true == isScopedNodeKey(dependentKey) {
            scopedKeys = append(scopedKeys, dependentKey)
        }
    }
    serviceContainer.mutex.Unlock()

    if 0 != len(scopedKeys) {
        t.Fatalf("expected the container graph to hold no scope-keyed dependent, got %v", scopedKeys)
    }

    serviceContainer.MustRegister(
        "app.container.parent",
        func(resolver containercontract.Resolver) (*resolverContextGraphControlParent, error) {
            dependency, getErr := resolver.Get("app.container.dependency")
            if nil != getErr {
                return nil, getErr
            }

            return &resolverContextGraphControlParent{dependency: dependency.(*resolverContextGraphDependency)}, nil
        },
    )

    if _, getErr := serviceContainer.Get("app.container.parent"); nil != getErr {
        t.Fatalf("unexpected container resolution error: %v", getErr)
    }

    serviceContainer.mutex.Lock()
    containerParentDependencies, containerParentRecorded := serviceContainer.dependencyGraph["service:app.container.parent"]
    _, containerEdgeRecorded := containerParentDependencies["service:app.container.dependency"]
    serviceContainer.mutex.Unlock()

    if false == containerParentRecorded || false == containerEdgeRecorded {
        t.Fatalf("expected the container parent's edge to its dependency to be recorded")
    }
}

type lateViewOwner struct {
    recorder *closeOrderRecorder
    resolver containercontract.Resolver
}

func (instance *lateViewOwner) Close() error {
    instance.recorder.record("owner")

    return nil
}

/* a view kept by a provider that returned resolves on a chain of its own: resolved while the resolution that built its owner is still building another node, the dependency lands on the view's owner and not on the node the live chain is building, so the owner closes before what it resolved */
func TestResolverContext_AViewWhoseProviderReturnedRecordsItsOwnersDependency(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 3)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    MustRegister[*closeOrderServiceA](serviceContainer, "service.a", func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
        return &closeOrderServiceA{recorder: recorder}, nil
    })

    MustRegister[*lateViewOwner](serviceContainer, "app.owner", func(resolver containercontract.Resolver) (*lateViewOwner, error) {
        return &lateViewOwner{recorder: recorder, resolver: resolver}, nil
    })

    MustRegister[*closeOrderServiceB](serviceContainer, "service.b", func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
        owner, ownerErr := FromResolver[*lateViewOwner](resolver, "app.owner")
        if nil != ownerErr {
            return nil, ownerErr
        }

        /* the owner's provider has returned and this provider is still running on the same chain */
        if _, lateErr := FromResolver[*closeOrderServiceA](owner.resolver, "service.a"); nil != lateErr {
            return nil, lateErr
        }

        return &closeOrderServiceB{recorder: recorder}, nil
    })

    MustFromResolver[*closeOrderServiceB](serviceContainer, "service.b")

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if "b,owner,a" != strings.Join(closeSequence, ",") {
        t.Fatalf("expected b, then the owner, then what the owner resolved, got %v", closeSequence)
    }
}

/* the same through the by-type door: resolved while the resolution that built its owner is still building another node, the dependency lands on the view's owner and not on the node the live chain is building, so the owner closes before what it resolved */
func TestResolverContext_AViewWhoseProviderReturnedRecordsItsOwnersDependencyByType(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 3)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    MustRegister[*closeOrderServiceA](serviceContainer, "service.a", func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
        return &closeOrderServiceA{recorder: recorder}, nil
    })

    MustRegister[*lateViewOwner](serviceContainer, "app.owner", func(resolver containercontract.Resolver) (*lateViewOwner, error) {
        return &lateViewOwner{recorder: recorder, resolver: resolver}, nil
    })

    MustRegister[*closeOrderServiceB](serviceContainer, "service.b", func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
        owner, ownerErr := FromResolver[*lateViewOwner](resolver, "app.owner")
        if nil != ownerErr {
            return nil, ownerErr
        }

        /* the owner's provider has returned and this provider is still running on the same chain */
        if _, lateErr := FromResolverByType[*closeOrderServiceA](owner.resolver); nil != lateErr {
            return nil, lateErr
        }

        return &closeOrderServiceB{recorder: recorder}, nil
    })

    MustFromResolver[*closeOrderServiceB](serviceContainer, "service.b")

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if "b,owner,a" != strings.Join(closeSequence, ",") {
        t.Fatalf("expected b, then the owner, then what the owner resolved, got %v", closeSequence)
    }
}

type lateLeafA struct{}

type lateLeafB struct {
    leafA *lateLeafA
}

type lateLeafOwner struct{}

/* a Lazy built over a provider's resolver is a handle any goroutine may use first: concurrent first uses resolve on chains of their own, so none reads another's pushed node as its own ancestor and answers a circular dependency */
func TestResolverContext_ConcurrentFirstUsesOfALazyOverAProvidersResolverAllResolve(t *testing.T) {
    const workers = 8

    for round := 0; round < 50; round++ {
        serviceContainer := NewContainer()

        MustRegister[*lateLeafA](serviceContainer, "leaf.a", func(resolver containercontract.Resolver) (*lateLeafA, error) {
            return &lateLeafA{}, nil
        })

        MustRegister[*lateLeafB](serviceContainer, "leaf.b", func(resolver containercontract.Resolver) (*lateLeafB, error) {
            leafA, leafErr := FromResolver[*lateLeafA](resolver, "leaf.a")
            if nil != leafErr {
                return nil, leafErr
            }

            return &lateLeafB{leafA: leafA}, nil
        })

        var handle *LazyService[*lateLeafB]
        MustRegister[*lateLeafOwner](serviceContainer, "app.owner", func(resolver containercontract.Resolver) (*lateLeafOwner, error) {
            handle = Lazy[*lateLeafB](resolver, "leaf.b")

            return &lateLeafOwner{}, nil
        })
        MustFromResolver[*lateLeafOwner](serviceContainer, "app.owner")

        start := make(chan struct{})
        failures := make(chan error, workers)
        var waitGroup sync.WaitGroup
        for worker := 0; worker < workers; worker++ {
            waitGroup.Add(1)
            go func() {
                defer waitGroup.Done()
                <-start

                if _, resolveErr := handle.Resolve(); nil != resolveErr {
                    failures <- resolveErr
                }
            }()
        }

        close(start)
        waitGroup.Wait()
        close(failures)

        for failure := range failures {
            t.Fatalf("round %d: a first use through the provider's resolver failed: %v", round, failure)
        }
    }
}

type lateCycleParent struct{}

type lateCycleHolder struct {
    handle   *LazyService[*lateCycleLeaf]
    other    *LazyService[*lateCycleOther]
    resolver containercontract.Resolver
}

type lateCycleLeaf struct{}

type lateCycleOther struct{}

/* the parent resolves the holder, whose provider keeps a Lazy over its resolver and returns; parentBody then uses what the holder kept while the parent is still being built, and the leaf resolves the parent back when leafNeedsParent */
func newLateCycleContainer(parentBody func(holder *lateCycleHolder) error, leafNeedsParent bool) *container {
    serviceContainer := NewContainer()

    MustRegister[*lateCycleHolder](serviceContainer, "late.holder", func(resolver containercontract.Resolver) (*lateCycleHolder, error) {
        return &lateCycleHolder{
            handle:   Lazy[*lateCycleLeaf](resolver, "late.leaf"),
            other:    Lazy[*lateCycleOther](resolver, "late.other"),
            resolver: resolver,
        }, nil
    })

    MustRegister[*lateCycleLeaf](serviceContainer, "late.leaf", func(resolver containercontract.Resolver) (*lateCycleLeaf, error) {
        if true == leafNeedsParent {
            if _, parentErr := FromResolver[*lateCycleParent](resolver, "late.parent"); nil != parentErr {
                return nil, parentErr
            }
        }

        return &lateCycleLeaf{}, nil
    })

    MustRegister[*lateCycleOther](serviceContainer, "late.other", func(resolver containercontract.Resolver) (*lateCycleOther, error) {
        return &lateCycleOther{}, nil
    })

    MustRegister[*lateCycleParent](serviceContainer, "late.parent", func(resolver containercontract.Resolver) (*lateCycleParent, error) {
        holder, holderErr := FromResolver[*lateCycleHolder](resolver, "late.holder")
        if nil != holderErr {
            return nil, holderErr
        }

        if bodyErr := parentBody(holder); nil != bodyErr {
            return nil, bodyErr
        }

        return &lateCycleParent{}, nil
    })

    return serviceContainer.(*container)
}

func resolveLateCycleParentWithin(t *testing.T, serviceContainer *container) error {
    t.Helper()

    answer := make(chan error, 1)
    go func() {
        _, resolveErr := FromResolver[*lateCycleParent](serviceContainer, "late.parent")
        answer <- resolveErr
    }()

    select {
    case resolveErr := <-answer:
        return resolveErr
    case <-time.After(5 * time.Second):
        t.Fatalf("the resolution did not answer within 5s")

        return nil
    }
}

func assertResolverWaitGraphIsEmpty(t *testing.T, serviceContainer *container) {
    t.Helper()

    serviceContainer.mutex.RLock()
    defer serviceContainer.mutex.RUnlock()

    if 0 != len(serviceContainer.resolverWaitGraph) {
        t.Fatalf("expected no wait edge left once the resolutions returned, got %v", serviceContainer.resolverWaitGraph)
    }
}

func TestResolverContext_ACycleClosedThroughALazyOverAProvidersResolverIsRefusedAsCircular(t *testing.T) {
    serviceContainer := newLateCycleContainer(func(holder *lateCycleHolder) error {
        _, resolveErr := holder.handle.Resolve()

        return resolveErr
    }, true)

    resolveErr := resolveLateCycleParentWithin(t, serviceContainer)
    if nil == resolveErr || false == strings.Contains(resolveErr.Error(), "circular service dependency") {
        t.Fatalf("expected the cycle through the retained resolver to be refused as circular, got %v", resolveErr)
    }

    assertResolverWaitGraphIsEmpty(t, serviceContainer)
}

func TestResolverContext_ACycleClosedThroughAProvidersResolverByTypeIsRefusedAsCircular(t *testing.T) {
    serviceContainer := newLateCycleContainer(func(holder *lateCycleHolder) error {
        _, resolveErr := FromResolverByType[*lateCycleLeaf](holder.resolver)

        return resolveErr
    }, true)

    resolveErr := resolveLateCycleParentWithin(t, serviceContainer)
    if nil == resolveErr || false == strings.Contains(resolveErr.Error(), "circular service dependency") {
        t.Fatalf("expected the cycle through the retained resolver to be refused as circular, got %v", resolveErr)
    }

    assertResolverWaitGraphIsEmpty(t, serviceContainer)
}

func TestResolverContext_LateUsesInsideABuildingAncestorWithoutACycleResolveAndLeaveNoWaitEdge(t *testing.T) {
    serviceContainer := newLateCycleContainer(func(holder *lateCycleHolder) error {
        if _, otherErr := holder.other.Resolve(); nil != otherErr {
            return otherErr
        }

        _, leafErr := holder.handle.Resolve()

        return leafErr
    }, false)

    if resolveErr := resolveLateCycleParentWithin(t, serviceContainer); nil != resolveErr {
        t.Fatalf("expected the late uses without a cycle to resolve, got %v", resolveErr)
    }

    assertResolverWaitGraphIsEmpty(t, serviceContainer)
}

/* another goroutine uses the kept handle while the parent is still being built, on a path that never reaches the parent */
func TestResolverContext_ALateUseFromAnotherGoroutineOffTheAncestorsPathResolves(t *testing.T) {
    otherAnswer := make(chan error, 1)
    serviceContainer := newLateCycleContainer(func(holder *lateCycleHolder) error {
        go func() {
            _, otherErr := holder.other.Resolve()
            otherAnswer <- otherErr
        }()

        select {
        case otherErr := <-otherAnswer:
            otherAnswer <- otherErr
        case <-time.After(5 * time.Second):
        }

        return nil
    }, false)

    if resolveErr := resolveLateCycleParentWithin(t, serviceContainer); nil != resolveErr {
        t.Fatalf("expected the parent to resolve, got %v", resolveErr)
    }

    if otherErr := <-otherAnswer; nil != otherErr {
        t.Fatalf("expected the late use off the ancestor's path to resolve, got %v", otherErr)
    }

    assertResolverWaitGraphIsEmpty(t, serviceContainer)
}

type lateScopedCycleParent struct{}

type lateScopedCycleHolder struct {
    handle *LazyService[*lateScopedCycleLeaf]
}

type lateScopedCycleLeaf struct{}

/* the same cycle among scoped services: the parent's creation is in flight in the scope's own maps, which the late resolution reads too */
func TestResolverContext_ACycleClosedThroughALazyAmongScopedServicesIsRefusedAsCircular(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegisterScoped[*lateScopedCycleHolder](serviceContainer, "late.scoped.holder", func(resolver containercontract.Resolver) (*lateScopedCycleHolder, error) {
        return &lateScopedCycleHolder{handle: Lazy[*lateScopedCycleLeaf](resolver, "late.scoped.leaf")}, nil
    })

    MustRegisterScoped[*lateScopedCycleLeaf](serviceContainer, "late.scoped.leaf", func(resolver containercontract.Resolver) (*lateScopedCycleLeaf, error) {
        if _, parentErr := FromResolver[*lateScopedCycleParent](resolver, "late.scoped.parent"); nil != parentErr {
            return nil, parentErr
        }

        return &lateScopedCycleLeaf{}, nil
    })

    MustRegisterScoped[*lateScopedCycleParent](serviceContainer, "late.scoped.parent", func(resolver containercontract.Resolver) (*lateScopedCycleParent, error) {
        holder, holderErr := FromResolver[*lateScopedCycleHolder](resolver, "late.scoped.holder")
        if nil != holderErr {
            return nil, holderErr
        }

        if _, leafErr := holder.handle.Resolve(); nil != leafErr {
            return nil, leafErr
        }

        return &lateScopedCycleParent{}, nil
    })

    scopeInstance := serviceContainer.NewScope()

    answer := make(chan error, 1)
    go func() {
        _, resolveErr := FromResolver[*lateScopedCycleParent](scopeInstance, "late.scoped.parent")
        answer <- resolveErr
    }()

    select {
    case resolveErr := <-answer:
        if nil == resolveErr || false == strings.Contains(resolveErr.Error(), "circular service dependency") {
            t.Fatalf("expected the scoped cycle through the retained resolver to be refused as circular, got %v", resolveErr)
        }
    case <-time.After(5 * time.Second):
        t.Fatalf("the resolution did not answer within 5s")
    }
}

/* MustGet keeps the service name a failure already carries, as FromResolver does */
func TestResolverContext_MustGet_KeepsTheServiceNameANestedFailureCarries(t *testing.T) {
    nestedErr := exception.NewError(
        "service is not registered",
        map[string]any{"serviceName": "app.nested"},
        nil,
    )

    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.failing",
        func(resolver containercontract.Resolver) (*resolverContextMustProbe, error) {
            return nil, nestedErr
        },
        WithoutTypeRegistration(),
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    registerErr = serviceContainer.Register(
        "app.outer",
        func(resolver containercontract.Resolver) (*resolverContextMustDependent, error) {
            _ = resolver.MustGet("app.failing")

            return &resolverContextMustDependent{}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("app.outer"); nil == getErr {
        t.Fatalf("expected the nested failure to fail the resolution")
    }

    if "app.nested" != nestedErr.Context()["serviceName"] {
        t.Fatalf("expected the nested failure to keep the name of the service that failed, got %v", nestedErr.Context()["serviceName"])
    }
}
