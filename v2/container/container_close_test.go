package container

import (
    "reflect"
    "sync"
    "testing"

    containercontract "github.com/precision-soft/melody/v2/container/contract"
)

type closeOrderRecorder struct {
    mutex         *sync.Mutex
    closeSequence *[]string
}

func (instance *closeOrderRecorder) record(value string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    *instance.closeSequence = append(*instance.closeSequence, value)
}

type closeOrderServiceA struct {
    recorder *closeOrderRecorder
}

func (instance *closeOrderServiceA) Close() error {
    instance.recorder.record("a")
    return nil
}

type closeOrderServiceB struct {
    recorder *closeOrderRecorder
}

func (instance *closeOrderServiceB) Close() error {
    instance.recorder.record("b")
    return nil
}

type closeOrderServiceC struct {
    recorder *closeOrderRecorder
}

func (instance *closeOrderServiceC) Close() error {
    instance.recorder.record("c")
    return nil
}

type closeOrderServiceD struct {
    recorder *closeOrderRecorder
}

func (instance *closeOrderServiceD) Close() error {
    instance.recorder.record("d")
    return nil
}

func TestContainer_Close_ClosesDependentsBeforeDependencies_ByServiceName(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    err := serviceContainer.Register(
        "service.a",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{recorder: recorder}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    err = serviceContainer.Register(
        "service.b",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            _, err := resolver.Get("service.a")
            if nil != err {
                return nil, err
            }

            return &closeOrderServiceB{recorder: recorder}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    _, err = serviceContainer.Get("service.b")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    err = serviceContainer.Close()
    if nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    if 2 != len(closeSequence) {
        t.Fatalf("expected 2 close calls, got %d", len(closeSequence))
    }

    if "b" != closeSequence[0] {
        t.Fatalf("expected b to close first, got %s", closeSequence[0])
    }

    if "a" != closeSequence[1] {
        t.Fatalf("expected a to close second, got %s", closeSequence[1])
    }
}

type closeOrderTypeDependency struct {
    recorder *closeOrderRecorder
}

func (instance *closeOrderTypeDependency) Close() error {
    instance.recorder.record("dep")
    return nil
}

type closeOrderTypeDependent struct {
    recorder *closeOrderRecorder
}

func (instance *closeOrderTypeDependent) Close() error {
    instance.recorder.record("dependent")
    return nil
}

func TestContainer_Close_ClosesDependentsBeforeDependencies_ByTypeResolution(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    err := RegisterType[*closeOrderTypeDependency](
        serviceContainer,
        func(resolver containercontract.Resolver) (*closeOrderTypeDependency, error) {
            return &closeOrderTypeDependency{recorder: recorder}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register type error: %v", err)
    }

    err = RegisterType[*closeOrderTypeDependent](
        serviceContainer,
        func(resolver containercontract.Resolver) (*closeOrderTypeDependent, error) {
            _, err := resolver.GetByType(reflect.TypeOf((*closeOrderTypeDependency)(nil)))
            if nil != err {
                return nil, err
            }

            return &closeOrderTypeDependent{recorder: recorder}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register type error: %v", err)
    }

    _, err = serviceContainer.GetByType(reflect.TypeOf((*closeOrderTypeDependent)(nil)))
    if nil != err {
        t.Fatalf("unexpected get by type error: %v", err)
    }

    err = serviceContainer.Close()
    if nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    if 2 != len(closeSequence) {
        t.Fatalf("expected 2 close calls, got %d", len(closeSequence))
    }

    if "dependent" != closeSequence[0] {
        t.Fatalf("expected dependent to close first, got %s", closeSequence[0])
    }

    if "dep" != closeSequence[1] {
        t.Fatalf("expected dep to close second, got %s", closeSequence[1])
    }
}

type circularServiceA struct{}
type circularServiceB struct{}

func TestContainer_Get_DetectsCircularDependency_SameResolverContext(t *testing.T) {
    serviceContainer := NewContainer()

    err := serviceContainer.Register(
        "service.a",
        func(resolver containercontract.Resolver) (*circularServiceA, error) {
            _, err := resolver.Get("service.b")
            if nil != err {
                return nil, err
            }

            return &circularServiceA{}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    err = serviceContainer.Register(
        "service.b",
        func(resolver containercontract.Resolver) (*circularServiceB, error) {
            _, err := resolver.Get("service.a")
            if nil != err {
                return nil, err
            }

            return &circularServiceB{}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    _, err = serviceContainer.Get("service.a")
    if nil == err {
        t.Fatalf("expected circular dependency error")
    }
}

func TestContainer_Close_ClosesDiamondDependencyInDeterministicOrder(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 4)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    err := serviceContainer.Register(
        "service.d",
        func(resolver containercontract.Resolver) (*closeOrderServiceD, error) {
            return &closeOrderServiceD{recorder: recorder}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    err = serviceContainer.Register(
        "service.b",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            _, err := resolver.Get("service.d")
            if nil != err {
                return nil, err
            }

            return &closeOrderServiceB{recorder: recorder}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    err = serviceContainer.Register(
        "service.c",
        func(resolver containercontract.Resolver) (*closeOrderServiceC, error) {
            _, err := resolver.Get("service.d")
            if nil != err {
                return nil, err
            }

            return &closeOrderServiceC{recorder: recorder}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    err = serviceContainer.Register(
        "service.a",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            _, err := resolver.Get("service.b")
            if nil != err {
                return nil, err
            }

            _, err = resolver.Get("service.c")
            if nil != err {
                return nil, err
            }

            return &closeOrderServiceA{recorder: recorder}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    _, err = serviceContainer.Get("service.a")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    err = serviceContainer.Close()
    if nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    if 4 != len(closeSequence) {
        t.Fatalf("expected 4 close calls, got %d", len(closeSequence))
    }

    expected := []string{"a", "c", "b", "d"}

    for index := range expected {
        if expected[index] != closeSequence[index] {
            t.Fatalf("expected closeSequence[%d] == %s, got %s", index, expected[index], closeSequence[index])
        }
    }
}

/* @info named-depends-by-type on type-registered service */

func TestContainer_Close_ClosesDependentsBeforeDependencies_NamedServiceDependsByTypeOnTypeRegisteredService(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    err := Register(
        serviceContainer,
        "service.b",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{recorder: recorder}, nil
        },
        WithTypeRegistration(true),
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    err = Register(
        serviceContainer,
        "service.a",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            _, dependencyErr := FromResolverByType[*closeOrderServiceB](resolver)
            if nil != dependencyErr {
                return nil, dependencyErr
            }

            return &closeOrderServiceA{recorder: recorder}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    _, err = serviceContainer.Get("service.a")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    err = serviceContainer.Close()
    if nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    if 2 != len(closeSequence) {
        t.Fatalf("expected 2 close calls, got %d", len(closeSequence))
    }

    if "a" != closeSequence[0] {
        t.Fatalf("expected dependent a to close first, got %s", closeSequence[0])
    }

    if "b" != closeSequence[1] {
        t.Fatalf("expected dependency b to close second, got %s", closeSequence[1])
    }
}

/* @info value-type service single close */

type valueCloser struct {
    counter *int
    lock    *sync.Mutex
}

func (instance valueCloser) Close() error {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    *instance.counter++

    return nil
}

func TestContainer_Close_ValueTypeServiceClosedOnce(t *testing.T) {
    serviceContainer := NewContainer()

    var lock sync.Mutex
    count := 0

    MustRegister[valueCloser](
        serviceContainer,
        "value.closer",
        func(resolver containercontract.Resolver) (valueCloser, error) {
            return valueCloser{counter: &count, lock: &lock}, nil
        },
    )

    _ = MustFromResolver[valueCloser](serviceContainer, "value.closer")
    _ = MustFromResolverByType[valueCloser](serviceContainer)

    if err := serviceContainer.Close(); nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    lock.Lock()
    defer lock.Unlock()

    if 1 != count {
        t.Fatalf("expected value-type service Close to be called once, got %d", count)
    }
}

/* @info value-type service with unhashable/non-comparable content */

type cr40UnhashableValueCloser struct {
    counter *int
    lock    *sync.Mutex
    payload any
}

func (instance cr40UnhashableValueCloser) Close() error {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    *instance.counter++

    return nil
}

type cr40NonComparableValueCloser struct {
    counter *int
    lock    *sync.Mutex
    tags    []string
}

func (instance cr40NonComparableValueCloser) Close() error {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    *instance.counter++

    return nil
}

func TestContainer_Close_ValueTypeServiceWithUnhashableContentDoesNotPanicAndClosesOnce(t *testing.T) {
    serviceContainer := NewContainer()

    var lock sync.Mutex
    count := 0

    MustRegister[cr40UnhashableValueCloser](
        serviceContainer,
        "cr40.unhashable.value.closer",
        func(resolver containercontract.Resolver) (cr40UnhashableValueCloser, error) {
            return cr40UnhashableValueCloser{counter: &count, lock: &lock, payload: []int{1, 2, 3}}, nil
        },
    )

    _ = MustFromResolver[cr40UnhashableValueCloser](serviceContainer, "cr40.unhashable.value.closer")
    _ = MustFromResolverByType[cr40UnhashableValueCloser](serviceContainer)

    if err := serviceContainer.Close(); nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    lock.Lock()
    defer lock.Unlock()

    if 1 != count {
        t.Fatalf("expected value-type service Close to be called once, got %d", count)
    }
}

func TestContainer_Close_NonComparableValueTypeServiceClosedOnce(t *testing.T) {
    serviceContainer := NewContainer()

    var lock sync.Mutex
    count := 0

    MustRegister[cr40NonComparableValueCloser](
        serviceContainer,
        "cr40.non.comparable.value.closer",
        func(resolver containercontract.Resolver) (cr40NonComparableValueCloser, error) {
            return cr40NonComparableValueCloser{counter: &count, lock: &lock, tags: []string{"a", "b"}}, nil
        },
    )

    _ = MustFromResolver[cr40NonComparableValueCloser](serviceContainer, "cr40.non.comparable.value.closer")
    _ = MustFromResolverByType[cr40NonComparableValueCloser](serviceContainer)

    if err := serviceContainer.Close(); nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    lock.Lock()
    defer lock.Unlock()

    if 1 != count {
        t.Fatalf("expected non-comparable value-type service Close to be called once, got %d", count)
    }
}
/* @info OverrideProtectedInstance on a WithoutTypeRegistration value service must close once (CR #64) */

type overrideValueCloserCR64 struct {
    counter *int
    lock    *sync.Mutex
    tags    []string
}

func (instance overrideValueCloserCR64) Close() error {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    *instance.counter++

    return nil
}

func TestContainer_Close_OverrideProtectedInstanceWithoutTypeRegistrationClosesOnce(t *testing.T) {
    serviceContainer := NewContainer()

    var lock sync.Mutex
    count := 0

    MustRegister[overrideValueCloserCR64](
        serviceContainer,
        "override.no.type.value.closer",
        func(resolver containercontract.Resolver) (overrideValueCloserCR64, error) {
            return overrideValueCloserCR64{counter: &count, lock: &lock, tags: []string{"a", "b"}}, nil
        },
        WithoutTypeRegistration(),
    )

    if overrideErr := serviceContainer.OverrideProtectedInstance(
        "override.no.type.value.closer",
        overrideValueCloserCR64{counter: &count, lock: &lock, tags: []string{"x", "y"}},
    ); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if err := serviceContainer.Close(); nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    lock.Lock()
    defer lock.Unlock()

    if 1 != count {
        t.Fatalf("expected overridden non-type-registered value-type service Close to be called once, got %d", count)
    }
}
