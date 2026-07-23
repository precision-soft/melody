package container

import (
    "reflect"
    "strconv"
    "strings"
    "sync"
    "sync/atomic"
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
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

/* @info the resolution key is unique per type identity, not the type's String() which two same-named types from different packages share, so the creation guard and cycle detection cannot alias two distinct types onto one key */
func TestTypeIdentityKey_DistinguishesSameStringTypesFromDifferentPackages(t *testing.T) {
    interfaceType := reflect.TypeOf((*collectableHandler)(nil)).Elem()
    pointerType := reflect.TypeOf(&invoiceHandler{})

    if typeIdentityKey(interfaceType) == typeIdentityKey(pointerType) {
        t.Fatalf("expected distinct types to yield distinct keys")
    }

    if typeIdentityKey(pointerType) != typeIdentityKey(reflect.TypeOf(&invoiceHandler{})) {
        t.Fatalf("expected the same type to yield a stable key")
    }

    /* the discriminator is the named type's import path, which differs even when String() would not: the key carries it ahead of the String() */
    if false == strings.HasPrefix(typeIdentityKey(pointerType), pointerType.Elem().PkgPath()) {
        t.Fatalf("expected the key to lead with the named type's package path, got %q", typeIdentityKey(pointerType))
    }
}
