package container

import (
    "reflect"
    "strconv"
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

/* @info Get's create closure reads the providers map after serviceWithCreationGuardLocked releases the container mutex, racing Register's map writes (fatal concurrent map read/write). The provider must be snapshotted under the lock so this holds under -race. */
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

/* @info GetByType's type-branch create closure reads the typeProviders map after the container mutex is released, racing RegisterType's map writes. The provider must be snapshotted under the lock so this holds under -race. */
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

/* @info Has answers under the suspension Get enforces. A container-owned provider asking about a scope-only name used to hear "yes" from the very entries its Get refuses — the Has-then-MustGet idiom panicked, and a process-lifetime service could shape its wiring on one request's substitutes. */
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
