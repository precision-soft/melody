package container

import (
    "reflect"
    "strconv"
    "sync"
    "sync/atomic"
    "testing"

    containercontract "github.com/precision-soft/melody/v2/container/contract"
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
