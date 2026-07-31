package container

import (
    "reflect"
    "sync"
    "testing"
    "time"

    containercontract "github.com/precision-soft/melody/container/contract"
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

/* @info One registration yields one instance per scope: shared by everything inside one request, shared by nothing between two. Anything less makes a scoped service either a singleton in disguise or a fresh object per lookup, and neither is what a request-lifetime service means. */
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

/* @info Two scopes resolving one scoped name must contend on nothing: each holds its own creation guard, so neither ever waits on the other's build and neither inherits the other's failure.

@info The two resolutions are forced to overlap rather than merely started together. A shared guard is invisible to resolutions that happen to run one after the other — the second finds no creation in flight and simply builds its own — so a test that only launches two goroutines stays green with the guard pointed at the container. Holding both providers inside the creation at once is what makes the sharing observable: under a shared guard the second provider never runs at all. */
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

/* @info The container is blind to what its scopes own. A scoped service reaching the root container would outlive the request it was built for and be handed, holding that request's substitutes, to every request that follows. */
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

/* @info A scoped service is the request, so it reads both levels: the scope's own substitutes and the container's process-wide singletons. Suspending the scope for it — the rule that keeps container providers request-agnostic — would hide from it the very entries it exists to consume. */
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

/* @info A container provider stays request-agnostic even while a scoped service is being built around it: it is a process singleton and a value assembled out of one request would be handed to every other one. The refusal is the same "not registered" a wiring mistake gets anywhere else, reported where the mistake is. */
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

/* @info A container singleton resolved from inside a scoped provider is still one instance for the whole process. Suspension is per creation and restored afterwards, so the scoped service around it keeps seeing its scope while the singleton inside it never did. */
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

/* @info A scoped service that resolves its own name re-enters itself, and the repeat has to be reported rather than waited on: the creation guard for that name is already held by this very resolution, so waiting would hang the request instead of naming the wiring mistake. */
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

/* @info A scoped registration answers Has before anything is built. Has reports what the scope can produce, not what it happens to hold, or every caller would have to resolve a service just to find out whether it exists. */
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

/* @info A scoped service registered by type and reached by type must be the same instance the name yields, so the type resolves through the name it was registered under rather than building a second one beside it. */
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

/* @info What the scope built, the scope closes: it holds that request's substitutes and has nobody else to close it. */
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

/* @info A scoped repository holding a scoped transaction is the ordinary shape now that a scope owns registrations, and closing the two by name would be a coin flip: the dependent has to go first, or it is torn down against a collaborator that is already gone. The names here are chosen so the descending-name fallback would produce the opposite order, which is what makes the graph the thing being tested. */
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

/* @info a scoped dependency the handler resolved FIRST is already in the scope when the dependent's provider asks for it, and the early answer used to skip the dependency edge: the graph guarantee held only for whichever resolution came first, and the close order fell back to the name lottery. The names are chosen so the fallback would close the dependency first. */
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
