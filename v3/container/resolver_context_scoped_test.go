package container

import (
    "reflect"
    "sync"
    "testing"
    "time"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

type scopedProbe struct {
    value string
}

/* scopedCloseRecorder records the order its services were closed in, which is the only way to observe that a teardown honoured the dependency graph rather than the node names. */
type scopedCloseRecorder struct {
    mutex sync.Mutex
    order []string
}

func (instance *scopedCloseRecorder) record(name string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.order = append(instance.order, name)
}

func (instance *scopedCloseRecorder) recorded() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    copied := make([]string, len(instance.order))
    copy(copied, instance.order)

    return copied
}

type recordingScopedService struct {
    name     string
    recorder *scopedCloseRecorder
}

func (instance *recordingScopedService) Close() error {
    instance.recorder.record(instance.name)

    return nil
}

func TestScope_AScopedServiceIsBuiltOncePerScopeAndNotShared(t *testing.T) {
    serviceContainer := NewContainer()

    builds := 0

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.request.trail",
        func(resolver containercontract.Resolver) (*scopedProbe, error) {
            builds = builds + 1

            return &scopedProbe{value: "trail"}, nil
        },
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    firstScope := serviceContainer.NewScope()

    firstValue, getErr := firstScope.Get("app.request.trail")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    firstValueAgain, getErr := firstScope.Get("app.request.trail")
    if nil != getErr {
        t.Fatalf("unexpected second get error: %v", getErr)
    }

    if firstValue != firstValueAgain {
        t.Fatalf("expected one instance for the whole scope")
    }

    secondScope := serviceContainer.NewScope()

    secondValue, getErr := secondScope.Get("app.request.trail")
    if nil != getErr {
        t.Fatalf("unexpected get error on the second scope: %v", getErr)
    }

    if firstValue == secondValue {
        t.Fatalf("expected two scopes to hold two instances")
    }

    if 2 != builds {
        t.Fatalf("expected the provider to run once per scope, got %d builds", builds)
    }
}

/* the two resolutions are forced to overlap rather than merely started together: a shared guard is invisible to resolutions that run one after the other, since the second finds no creation in flight and builds its own. Holding both providers inside the creation at once is what makes the sharing observable. */
func TestScope_TwoConcurrentScopesEachBuildTheirOwnInstance(t *testing.T) {
    serviceContainer := NewContainer()

    entered := make(chan struct{}, 2)
    release := make(chan struct{})

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.request.trail",
        func(resolver containercontract.Resolver) (*scopedProbe, error) {
            entered <- struct{}{}
            <-release

            return &scopedProbe{value: "trail"}, nil
        },
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    firstScope := serviceContainer.NewScope()
    secondScope := serviceContainer.NewScope()

    values := make([]any, 2)
    errs := make([]error, 2)

    waitGroup := sync.WaitGroup{}
    waitGroup.Add(2)

    go func() {
        defer waitGroup.Done()

        values[0], errs[0] = firstScope.Get("app.request.trail")
    }()

    go func() {
        defer waitGroup.Done()

        values[1], errs[1] = secondScope.Get("app.request.trail")
    }()

    bothEntered := true
    for index := 0; index < 2; index = index + 1 {
        select {
        case <-entered:
        case <-time.After(5 * time.Second):
            bothEntered = false
        }

        if false == bothEntered {
            break
        }
    }

    close(release)
    waitGroup.Wait()

    if false == bothEntered {
        t.Fatalf("expected both scopes to be building at the same time; only one provider ran, so the two scopes shared a creation guard")
    }

    for index, err := range errs {
        if nil != err {
            t.Fatalf("unexpected get error on scope %d: %v", index, err)
        }
    }

    if values[0] == values[1] {
        t.Fatalf("expected each scope to end up with its own instance")
    }
}

func TestScope_TheRootContainerNeverSeesAScopedService(t *testing.T) {
    serviceContainer := NewContainer()

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.request.trail",
        func(resolver containercontract.Resolver) (*scopedProbe, error) {
            return &scopedProbe{value: "trail"}, nil
        },
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    scopeInstance := serviceContainer.NewScope()

    _, getErr := scopeInstance.Get("app.request.trail")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if true == serviceContainer.Has("app.request.trail") {
        t.Fatalf("expected the container not to report a scoped service")
    }

    _, containerGetErr := serviceContainer.Get("app.request.trail")
    if nil == containerGetErr {
        t.Fatalf("expected the container to refuse a scoped service")
    }

    if "service is not registered" != containerGetErr.Error() {
        t.Fatalf("unexpected refusal message: %q", containerGetErr.Error())
    }
}

func TestScope_AScopedServiceSeesTheScopesOverridesAndTheContainersSingletons(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.writer",
        func(resolver containercontract.Resolver) (*scopedProbe, error) {
            return &scopedProbe{value: "writer"}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.request.trail",
        func(resolver containercontract.Resolver) (*scopedProbe, error) {
            requestValue, getErr := resolver.Get("app.request.id")
            if nil != getErr {
                return nil, getErr
            }

            writerValue, getErr := resolver.Get("app.writer")
            if nil != getErr {
                return nil, getErr
            }

            return &scopedProbe{
                value: requestValue.(*scopedProbe).value + "+" + writerValue.(*scopedProbe).value,
            }, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    scopeInstance := serviceContainer.NewScope()

    overrideErr := scopeInstance.OverrideInstance("app.request.id", &scopedProbe{value: "request-1"})
    if nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    value, getErr := scopeInstance.Get("app.request.trail")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if "request-1+writer" != value.(*scopedProbe).value {
        t.Fatalf("expected the scoped service to see both levels, got %q", value.(*scopedProbe).value)
    }
}

func TestScope_AContainerProviderStillCannotSeeAScopedService(t *testing.T) {
    serviceContainer := NewContainer()

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.request.id",
        func(resolver containercontract.Resolver) (*scopedProbe, error) {
            return &scopedProbe{value: "request-1"}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    registerErr := serviceContainer.Register(
        "app.singleton",
        func(resolver containercontract.Resolver) (*scopedProbe, error) {
            _, getErr := resolver.Get("app.request.id")
            if nil != getErr {
                return nil, getErr
            }

            return &scopedProbe{value: "singleton"}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    _, getErr := scopeInstance.Get("app.singleton")
    if nil == getErr {
        t.Fatalf("expected a container provider reaching for a scoped service to fail")
    }

    if "service is not registered" != getErr.Error() {
        t.Fatalf("unexpected refusal message: %q", getErr.Error())
    }
}

func TestScope_AContainerSingletonReachedFromAScopedServiceStaysASingleton(t *testing.T) {
    serviceContainer := NewContainer()

    builds := 0

    registerErr := serviceContainer.Register(
        "app.writer",
        func(resolver containercontract.Resolver) (*scopedProbe, error) {
            builds = builds + 1

            return &scopedProbe{value: "writer"}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.request.trail",
        func(resolver containercontract.Resolver) (*scopedProbe, error) {
            writerValue, getErr := resolver.Get("app.writer")
            if nil != getErr {
                return nil, getErr
            }

            return &scopedProbe{value: writerValue.(*scopedProbe).value}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    firstScope := serviceContainer.NewScope()
    secondScope := serviceContainer.NewScope()

    _, getErr := firstScope.Get("app.request.trail")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    _, getErr = secondScope.Get("app.request.trail")
    if nil != getErr {
        t.Fatalf("unexpected second get error: %v", getErr)
    }

    if 1 != builds {
        t.Fatalf("expected the container singleton to be built once for the process, got %d builds", builds)
    }

    firstWriter, getErr := firstScope.Get("app.writer")
    if nil != getErr {
        t.Fatalf("unexpected writer get error: %v", getErr)
    }

    secondWriter, getErr := secondScope.Get("app.writer")
    if nil != getErr {
        t.Fatalf("unexpected second writer get error: %v", getErr)
    }

    if firstWriter != secondWriter {
        t.Fatalf("expected both scopes to reach the same container singleton")
    }
}

func TestScope_AScopedProviderResolvingItsOwnNameIsReportedAsACycle(t *testing.T) {
    serviceContainer := NewContainer()

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.request.trail",
        func(resolver containercontract.Resolver) (*scopedProbe, error) {
            selfValue, getErr := resolver.Get("app.request.trail")
            if nil != getErr {
                return nil, getErr
            }

            return selfValue.(*scopedProbe), nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    scopeInstance := serviceContainer.NewScope()

    _, getErr := scopeInstance.Get("app.request.trail")
    if nil == getErr {
        t.Fatalf("expected the self-resolving scoped provider to be reported")
    }

    if "circular service dependency detected" != getErr.Error() {
        t.Fatalf("unexpected report: %q", getErr.Error())
    }
}

func TestScope_HasAnswersTrueForAScopedRegistrationBeforeItIsBuilt(t *testing.T) {
    serviceContainer := NewContainer()

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.request.trail",
        func(resolver containercontract.Resolver) (*scopedProbe, error) {
            return &scopedProbe{value: "trail"}, nil
        },
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if false == scopeInstance.Has("app.request.trail") {
        t.Fatalf("expected the scope to report a scoped registration it has not built yet")
    }

    if false == scopeInstance.HasType(reflect.TypeOf(&scopedProbe{})) {
        t.Fatalf("expected the scope to report the scoped registration by type as well")
    }
}

func TestScope_GetByTypeResolvesAScopedRegistrationThroughItsName(t *testing.T) {
    serviceContainer := NewContainer()

    builds := 0

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.request.trail",
        func(resolver containercontract.Resolver) (*scopedProbe, error) {
            builds = builds + 1

            return &scopedProbe{value: "trail"}, nil
        },
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    scopeInstance := serviceContainer.NewScope()

    byName, getErr := scopeInstance.Get("app.request.trail")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    byType, getByTypeErr := scopeInstance.GetByType(reflect.TypeOf(&scopedProbe{}))
    if nil != getByTypeErr {
        t.Fatalf("unexpected get by type error: %v", getByTypeErr)
    }

    if byName != byType {
        t.Fatalf("expected the name and the type to yield one instance")
    }

    if 1 != builds {
        t.Fatalf("expected one build for both lookups, got %d", builds)
    }
}

func TestScope_AScopedServiceIsClosedWhenTheScopeCloses(t *testing.T) {
    serviceContainer := NewContainer()

    recorder := &scopedCloseRecorder{}

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.request.trail",
        func(resolver containercontract.Resolver) (*recordingScopedService, error) {
            return &recordingScopedService{name: "trail", recorder: recorder}, nil
        },
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    scopeInstance := serviceContainer.NewScope()

    _, getErr := scopeInstance.Get("app.request.trail")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    closeErr := scopeInstance.Close()
    if nil != closeErr {
        t.Fatalf("unexpected scope close error: %v", closeErr)
    }

    recorded := recorder.recorded()
    if 1 != len(recorded) || "trail" != recorded[0] {
        t.Fatalf("expected the scoped service to be closed exactly once, got %v", recorded)
    }
}

/* the names are chosen so the node-key fallback would produce the opposite order. That fallback no longer decides: it survives only as the last tie-break between two nodes carrying the same creation stamp, and what holds this order is the creation-order tie-break — a dependency built during its dependent is the older node, so latest-first reaches the dependent first with or without the edge. This fixture therefore pins that scoped dependents close before their dependencies, not that the graph is what decides it: cutting the graph leaves it green. */
func TestScope_ScopedServicesAreClosedDependentsBeforeDependencies(t *testing.T) {
    serviceContainer := NewContainer()

    recorder := &scopedCloseRecorder{}

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.aaa.dependent",
        func(resolver containercontract.Resolver) (*recordingScopedService, error) {
            _, getErr := resolver.Get("app.zzz.dependency")
            if nil != getErr {
                return nil, getErr
            }

            return &recordingScopedService{name: "dependent", recorder: recorder}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    registerScopedErr = serviceContainer.RegisterScoped(
        "app.zzz.dependency",
        func(resolver containercontract.Resolver) (*recordingScopedService, error) {
            return &recordingScopedService{name: "dependency", recorder: recorder}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected second scoped register error: %v", registerScopedErr)
    }

    scopeInstance := serviceContainer.NewScope()

    _, getErr := scopeInstance.Get("app.aaa.dependent")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    closeErr := scopeInstance.Close()
    if nil != closeErr {
        t.Fatalf("unexpected scope close error: %v", closeErr)
    }

    recorded := recorder.recorded()
    if 2 != len(recorded) {
        t.Fatalf("expected both scoped services to be closed, got %v", recorded)
    }

    if "dependent" != recorded[0] || "dependency" != recorded[1] {
        t.Fatalf("expected the dependent to be closed before its dependency, got %v", recorded)
    }
}

/* the names are chosen so the node-key fallback would close the dependency first. That fallback no longer decides: it survives only as the last tie-break between two nodes carrying the same creation stamp, and the order here is held by the creation-order tie-break, which closes the later-created dependent first. So this pins that an early answer still records its edge, not that the edge outranks the tie-break: cutting the graph leaves it green. */
func TestScopedResolution_ExistingInstanceRecordsDependencyEdge(t *testing.T) {
    serviceContainer := NewContainer()

    recorder := &scopedCloseRecorder{}

    registerDependencyErr := serviceContainer.RegisterScoped(
        "app.zzz.dependency",
        func(resolver containercontract.Resolver) (*recordingScopedService, error) {
            return &recordingScopedService{name: "dependency", recorder: recorder}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerDependencyErr {
        t.Fatalf("unexpected register error: %v", registerDependencyErr)
    }

    registerDependentErr := serviceContainer.RegisterScoped(
        "app.aaa.dependent",
        func(resolver containercontract.Resolver) (*recordingScopedService, error) {
            _, getErr := resolver.Get("app.zzz.dependency")
            if nil != getErr {
                return nil, getErr
            }

            return &recordingScopedService{name: "dependent", recorder: recorder}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerDependentErr {
        t.Fatalf("unexpected register error: %v", registerDependentErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get("app.zzz.dependency"); nil != getErr {
        t.Fatalf("unexpected direct dependency resolution error: %v", getErr)
    }

    if _, getErr := scopeInstance.Get("app.aaa.dependent"); nil != getErr {
        t.Fatalf("unexpected dependent resolution error: %v", getErr)
    }

    if closeErr := scopeInstance.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    recorded := recorder.recorded()
    if 2 != len(recorded) {
        t.Fatalf("expected exactly two closes, got %v", recorded)
    }

    if "dependent" != recorded[0] || "dependency" != recorded[1] {
        t.Fatalf("expected the dependent to be closed before its already-created dependency, got %v", recorded)
    }
}

type scopedTypeOnlyProbe struct {
    value  string
    closed *int
}

func (instance *scopedTypeOnlyProbe) Close() error {
    *instance.closed = *instance.closed + 1

    return nil
}

/* no public door can produce the state this exercises: every registrar writes the type provider and the type-to-name index in the same two lines, so the name index always answers first and this path is never taken. The state is built by hand because the point is what the path does if it ever IS taken. */
func TestScopedServiceByType_BuildsATypeOnlyScopedServiceAndClosesItWithTheScope(t *testing.T) {
    serviceContainer := NewContainer().(*container)

    scopeInstance := serviceContainer.NewScope().(*scope)

    canonicalType := canonicalServiceType(reflect.TypeOf((*scopedTypeOnlyProbe)(nil)))

    buildCount := 0
    closeCount := 0

    scopeInstance.mutex.Lock()
    scopeInstance.ownTypeProviders[canonicalType] = func(resolver containercontract.Resolver) (any, error) {
        buildCount = buildCount + 1

        return &scopedTypeOnlyProbe{value: "type only", closed: &closeCount}, nil
    }
    scopeInstance.mutex.Unlock()

    firstValue, firstErr := scopeInstance.GetByType(canonicalType)
    if nil != firstErr {
        t.Fatalf("unexpected get by type error: %v", firstErr)
    }

    firstProbe, isProbe := firstValue.(*scopedTypeOnlyProbe)
    if false == isProbe || "type only" != firstProbe.value {
        t.Fatalf("unexpected resolved value: %#v", firstValue)
    }

    secondValue, secondErr := scopeInstance.GetByType(canonicalType)
    if nil != secondErr {
        t.Fatalf("unexpected second get by type error: %v", secondErr)
    }

    if firstValue != secondValue {
        t.Fatalf("expected the type-only scoped service to be memoized within its scope")
    }

    if 1 != buildCount {
        t.Fatalf("expected the provider to run exactly once, got %d", buildCount)
    }

    if closeErr := scopeInstance.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != closeCount {
        t.Fatalf("expected the type-only scoped service to be closed exactly once with its scope, got %d", closeCount)
    }
}

type keptResolverHolder struct {
    resolver containercontract.Resolver
    recorder *scopedCloseRecorder
}

func (instance *keptResolverHolder) Close() error {
    instance.recorder.record("holder")

    return nil
}

/* the sibling above cannot tell the edge from the creation-order tie-break: a dependency that is ALREADY held was necessarily created before its dependent, so latest-first closes the dependent first whether the edge exists or not. Here the two disagree. The holder is created first and keeps the resolver it was handed; the scoped entry it later reaches for is installed AFTER it, so the tie-break alone would close that entry first — out from under the holder that is still open. The edge recorded on the already-held path is the only thing that puts them back in order. */
func TestScopedResolution_ExistingInstanceEdgeOutranksTheCreationOrder(t *testing.T) {
    serviceContainer := NewContainer()

    recorder := &scopedCloseRecorder{}

    if registerErr := serviceContainer.RegisterScoped(
        "app.aaa.keptholder",
        func(resolver containercontract.Resolver) (*keptResolverHolder, error) {
            return &keptResolverHolder{resolver: resolver, recorder: recorder}, nil
        },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    /* the name carries a scoped registration of its own, which is what makes it a SCOPED node: the provider never runs, because the override installed below answers before anything is built */
    if registerErr := serviceContainer.RegisterScoped(
        "app.zzz.late",
        func(resolver containercontract.Resolver) (*recordingScopedService, error) {
            return &recordingScopedService{name: "unbuilt", recorder: recorder}, nil
        },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    holderValue, getErr := scopeInstance.Get("app.aaa.keptholder")
    if nil != getErr {
        t.Fatalf("unexpected holder resolution error: %v", getErr)
    }

    holder, isHolder := holderValue.(*keptResolverHolder)
    if false == isHolder {
        t.Fatalf("expected the holder, got %#v", holderValue)
    }

    /* installed AFTER the holder was created, so its creation stamp is the later of the two */
    if overrideErr := scopeInstance.(containercontract.OverrideServiceWithOptions).OverrideInstanceWithOptions(
        "app.zzz.late",
        &recordingScopedService{name: "late", recorder: recorder},
        ClosedWithScope(),
    ); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if _, lateErr := holder.resolver.Get("app.zzz.late"); nil != lateErr {
        t.Fatalf("unexpected late resolution error: %v", lateErr)
    }

    if closeErr := scopeInstance.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    recorded := recorder.recorded()
    if 2 != len(recorded) {
        t.Fatalf("expected exactly two closes, got %v", recorded)
    }

    if "holder" != recorded[0] || "late" != recorded[1] {
        t.Fatalf("expected the holder to close before the entry it reached through its kept resolver, got %v", recorded)
    }
}
