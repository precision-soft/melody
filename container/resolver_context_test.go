package container

import (
    "errors"
    "reflect"
    "strconv"
    "strings"
    "sync"
    "sync/atomic"
    "testing"

    containercontract "github.com/precision-soft/melody/container/contract"
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

    if false == strings.Contains(renderedCauseChain(brokenErr), "failed to get service instance") {
        t.Fatalf("expected the resolver's own panic message in the cause chain, got %q", renderedCauseChain(brokenErr))
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

    if false == strings.Contains(renderedCauseChain(brokenErr), "failed to get service instance by type") {
        t.Fatalf("expected the by-type panic message in the cause chain, got %q", renderedCauseChain(brokenErr))
    }
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
