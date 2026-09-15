package container

import (
    "runtime"
    "context"
    "errors"
    "fmt"
    "reflect"
    "sort"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

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

func TestContainer_Close_ValueTypeServiceWithUnhashableContentDoesNotPanicAndClosesOnce(t *testing.T) {
    serviceContainer := NewContainer()

    var lock sync.Mutex
    count := 0

    MustRegister[unhashableValueCloser](
        serviceContainer,
        "unhashable.value.closer",
        func(resolver containercontract.Resolver) (unhashableValueCloser, error) {
            return unhashableValueCloser{counter: &count, lock: &lock, payload: []int{1, 2, 3}}, nil
        },
    )

    _ = MustFromResolver[unhashableValueCloser](serviceContainer, "unhashable.value.closer")
    _ = MustFromResolverByType[unhashableValueCloser](serviceContainer)

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

    MustRegister[nonComparableValueCloser](
        serviceContainer,
        "non.comparable.value.closer",
        func(resolver containercontract.Resolver) (nonComparableValueCloser, error) {
            return nonComparableValueCloser{counter: &count, lock: &lock, tags: []string{"a", "b"}}, nil
        },
    )

    _ = MustFromResolver[nonComparableValueCloser](serviceContainer, "non.comparable.value.closer")
    _ = MustFromResolverByType[nonComparableValueCloser](serviceContainer)

    if err := serviceContainer.Close(); nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    lock.Lock()
    defer lock.Unlock()

    if 1 != count {
        t.Fatalf("expected non-comparable value-type service Close to be called once, got %d", count)
    }
}

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

func TestContainer_Close_OverrideProtectedInstanceWithoutTypeRegistrationClosesOnce(t *testing.T) {
    serviceContainer := NewContainer()

    var lock sync.Mutex
    count := 0

    MustRegister[overrideValueCloser](
        serviceContainer,
        "override.no.type.value.closer",
        func(resolver containercontract.Resolver) (overrideValueCloser, error) {
            return overrideValueCloser{counter: &count, lock: &lock, tags: []string{"a", "b"}}, nil
        },
        WithoutTypeRegistration(),
    )

    if overrideErr := serviceContainer.OverrideProtectedInstance(
        "override.no.type.value.closer",
        overrideValueCloser{counter: &count, lock: &lock, tags: []string{"x", "y"}},
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

func TestClose_ConcurrentCallersShareTheResult(t *testing.T) {
    serviceContainer := NewContainer()

    releaseClose := make(chan struct{})
    closeObserved := make(chan struct{}, 1)

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*blockingCloser, error) {
        return &blockingCloser{release: releaseClose, observed: closeObserved}, nil
    })

    _, getErr := FromResolverByType[*blockingCloser](serviceContainer)
    if nil != getErr {
        t.Fatalf("expected the service to resolve, got %v", getErr)
    }

    firstDone := make(chan error, 1)
    go func() {
        firstDone <- serviceContainer.Close()
    }()

    <-closeObserved

    secondDone := make(chan error, 1)
    go func() {
        secondDone <- serviceContainer.Close()
    }()

    select {
    case <-secondDone:
        t.Fatalf("the second Close returned before the first finished tearing down")
    case <-time.After(50 * time.Millisecond):
    }

    close(releaseClose)

    firstErr := <-firstDone
    secondErr := <-secondDone

    if nil == firstErr {
        t.Fatalf("expected the teardown failure to be reported to the first caller, got nil")
    }

    if firstErr != secondErr {
        t.Fatalf("expected both callers to share the teardown result, got %v and %v", firstErr, secondErr)
    }
}

func TestContainer_Close_PanickingServiceCloseIsRecordedAndSiblingsStillClose(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 1)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    err := serviceContainer.Register(
        "service.z.panics",
        func(resolver containercontract.Resolver) (*panickingCloseService, error) {
            return &panickingCloseService{}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    err = serviceContainer.Register(
        "service.a.sibling",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{recorder: recorder}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    if _, getErr := serviceContainer.Get("service.z.panics"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := serviceContainer.Get("service.a.sibling"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    closeErr := serviceContainer.Close()
    if nil == closeErr {
        t.Fatalf("expected the panicking Close to surface as a close failure")
    }

    typedError, isTyped := closeErr.(*exception.Error)
    if false == isTyped {
        t.Fatalf("expected an exception error, got %T", closeErr)
    }

    failures, hasFailures := typedError.Context()["failures"].(map[string]string)
    if false == hasFailures {
        t.Fatalf("expected a failures map in the close error context, got %+v", typedError.Context())
    }

    if "service close panicked" != failures["service:service.z.panics"] {
        t.Fatalf("expected the panic recorded against the panicking service, got %+v", failures)
    }

    if 1 != len(closeSequence) || "a" != closeSequence[0] {
        t.Fatalf("expected the sibling service to still close after the panic, got %v", closeSequence)
    }

    repeatedErr := serviceContainer.Close()
    if closeErr != repeatedErr {
        t.Fatalf("expected a repeated Close to report the same teardown error, got %v and %v", closeErr, repeatedErr)
    }
}

func TestContainer_Close_PanickingErrorMessageIsContainedAndRecorded(t *testing.T) {
    serviceContainer := NewContainer()

    err := serviceContainer.Register(
        "service.panicking.message",
        func(resolver containercontract.Resolver) (*panickingErrorMessageService, error) {
            return &panickingErrorMessageService{}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    if _, getErr := serviceContainer.Get("service.panicking.message"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    closeErr := serviceContainer.Close()
    if nil == closeErr {
        t.Fatalf("expected the panicking error message to surface as a close failure")
    }

    typedError, isTyped := closeErr.(*exception.Error)
    if false == isTyped {
        t.Fatalf("expected an exception error, got %T", closeErr)
    }

    failures, hasFailures := typedError.Context()["failures"].(map[string]string)
    if false == hasFailures {
        t.Fatalf("expected a failures map in the close error context, got %+v", typedError.Context())
    }

    if "close error message panicked: boom from Error()" != failures["service:service.panicking.message"] {
        t.Fatalf("expected the contained panic message, got %+v", failures)
    }

    repeatedErr := serviceContainer.Close()
    if closeErr != repeatedErr {
        t.Fatalf("expected a repeated Close to report the same teardown error, got %v and %v", closeErr, repeatedErr)
    }
}

func TestContainer_Close_DistinctZeroSizeServicesEachClose(t *testing.T) {
    zeroSizeCloserOneClosed = false
    zeroSizeCloserTwoClosed = false

    serviceContainer := NewContainer()

    MustRegister[*zeroSizeCloserOne](
        serviceContainer,
        "zero.size.one",
        func(resolver containercontract.Resolver) (*zeroSizeCloserOne, error) {
            return &zeroSizeCloserOne{}, nil
        },
    )

    MustRegister[*zeroSizeCloserTwo](
        serviceContainer,
        "zero.size.two",
        func(resolver containercontract.Resolver) (*zeroSizeCloserTwo, error) {
            return &zeroSizeCloserTwo{}, nil
        },
    )

    _ = MustFromResolver[*zeroSizeCloserOne](serviceContainer, "zero.size.one")
    _ = MustFromResolver[*zeroSizeCloserTwo](serviceContainer, "zero.size.two")

    if err := serviceContainer.Close(); nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    if false == zeroSizeCloserOneClosed {
        t.Fatalf("expected the first zero-size service to be closed")
    }

    if false == zeroSizeCloserTwoClosed {
        t.Fatalf("expected the second zero-size service to be closed")
    }
}

func TestContainer_Close_FirstFieldPointerAliasEachClose(t *testing.T) {
    serviceContainer := NewContainer()

    var lock sync.Mutex
    count := 0

    outer := &firstFieldOuterCloser{counter: &count, lock: &lock}
    outer.inner = firstFieldInnerCloser{counter: &count, lock: &lock}

    MustRegister[*firstFieldOuterCloser](
        serviceContainer,
        "first.field.outer",
        func(resolver containercontract.Resolver) (*firstFieldOuterCloser, error) {
            return outer, nil
        },
    )

    MustRegister[*firstFieldInnerCloser](
        serviceContainer,
        "first.field.inner",
        func(resolver containercontract.Resolver) (*firstFieldInnerCloser, error) {
            return &outer.inner, nil
        },
    )

    _ = MustFromResolver[*firstFieldOuterCloser](serviceContainer, "first.field.outer")
    _ = MustFromResolver[*firstFieldInnerCloser](serviceContainer, "first.field.inner")

    if err := serviceContainer.Close(); nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    lock.Lock()
    defer lock.Unlock()

    if 11 != count {
        t.Fatalf("expected both the outer and the first-field-aliased inner service to close, got counter %d", count)
    }
}

func TestContainer_Close_DistinctServicesOfOneZeroSizeTypeEachClose(t *testing.T) {
    sameTypeZeroSizeCloseCount = 0

    serviceContainer := NewContainer()

    MustRegister[*sameTypeZeroSizeCloser](
        serviceContainer,
        "same.type.zero.size.first",
        func(resolver containercontract.Resolver) (*sameTypeZeroSizeCloser, error) {
            return &sameTypeZeroSizeCloser{}, nil
        },
    )

    MustRegister[*sameTypeZeroSizeCloser](
        serviceContainer,
        "same.type.zero.size.second",
        func(resolver containercontract.Resolver) (*sameTypeZeroSizeCloser, error) {
            return &sameTypeZeroSizeCloser{}, nil
        },
        WithoutTypeRegistration(),
    )

    _ = MustFromResolver[*sameTypeZeroSizeCloser](serviceContainer, "same.type.zero.size.first")
    _ = MustFromResolver[*sameTypeZeroSizeCloser](serviceContainer, "same.type.zero.size.second")

    if err := serviceContainer.Close(); nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    if 2 != sameTypeZeroSizeCloseCount {
        t.Fatalf("expected both distinct zero-size services to be closed, got %d", sameTypeZeroSizeCloseCount)
    }
}

func TestContainer_Close_ResolverMediatedZeroSizeAliasClosesOnce(t *testing.T) {
    sameTypeZeroSizeCloseCount = 0

    serviceContainer := NewContainer()

    MustRegister[*sameTypeZeroSizeCloser](
        serviceContainer,
        "zero.size.origin",
        func(resolver containercontract.Resolver) (*sameTypeZeroSizeCloser, error) {
            return &sameTypeZeroSizeCloser{}, nil
        },
    )

    MustRegister[*sameTypeZeroSizeCloser](
        serviceContainer,
        "zero.size.alias",
        func(resolver containercontract.Resolver) (*sameTypeZeroSizeCloser, error) {
            return FromResolver[*sameTypeZeroSizeCloser](resolver, "zero.size.origin")
        },
        WithoutTypeRegistration(),
    )

    _ = MustFromResolver[*sameTypeZeroSizeCloser](serviceContainer, "zero.size.alias")

    if err := serviceContainer.Close(); nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    if 1 != sameTypeZeroSizeCloseCount {
        t.Fatalf("expected the shared instance to be closed exactly once, got %d", sameTypeZeroSizeCloseCount)
    }
}

func TestContainer_Close_ReplacedBuiltInstanceIsClosed(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    registerErr := serviceContainer.Register(
        "app.replaced.built",
        func(resolver containercontract.Resolver) (*replacedBuiltProbe, error) {
            return &replacedBuiltProbe{label: "built", recorder: recorder}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("app.replaced.built"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    overrideErr := serviceContainer.OverrideProtectedInstance(
        "app.replaced.built",
        &replacedBuiltProbe{label: "override", recorder: recorder},
    )
    if nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    builtCloses := 0
    overrideCloses := 0
    for _, label := range closeSequence {
        if "built" == label {
            builtCloses = builtCloses + 1
        }
        if "override" == label {
            overrideCloses = overrideCloses + 1
        }
    }

    if 1 != builtCloses {
        t.Fatalf("expected the replaced built instance to be closed exactly once, got %v", closeSequence)
    }

    if 1 != overrideCloses {
        t.Fatalf("expected the final override to be closed exactly once, got %v", closeSequence)
    }
}

func TestContainer_Close_TwoFailingReplacedInstancesAreBothRecorded(t *testing.T) {
    serviceContainer := NewContainer()

    registerFirstErr := serviceContainer.Register(
        "app.replaced.failing.first",
        func(resolver containercontract.Resolver) (*replacedFailingProbe, error) {
            return &replacedFailingProbe{failure: errors.New("refusing to close app.replaced.failing.first")}, nil
        },
    )
    if nil != registerFirstErr {
        t.Fatalf("unexpected register error: %v", registerFirstErr)
    }

    registerSecondErr := serviceContainer.Register(
        "app.replaced.failing.second",
        func(resolver containercontract.Resolver) (*secondReplacedFailingProbe, error) {
            return &secondReplacedFailingProbe{failure: errors.New("refusing to close app.replaced.failing.second")}, nil
        },
    )
    if nil != registerSecondErr {
        t.Fatalf("unexpected register error: %v", registerSecondErr)
    }

    if _, getErr := serviceContainer.Get("app.replaced.failing.first"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    if _, getErr := serviceContainer.Get("app.replaced.failing.second"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    if overrideErr := serviceContainer.OverrideProtectedInstance("app.replaced.failing.first", &replacedFailingProbe{}); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if overrideErr := serviceContainer.OverrideProtectedInstance("app.replaced.failing.second", &secondReplacedFailingProbe{}); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    closeErr := serviceContainer.Close()
    if nil == closeErr {
        t.Fatalf("expected the two failing replaced closes to be reported")
    }

    typedError, isTyped := closeErr.(*exception.Error)
    if false == isTyped {
        t.Fatalf("expected an exception error, got %T", closeErr)
    }

    failures, hasFailures := typedError.Context()["failures"].(map[string]string)
    if false == hasFailures {
        t.Fatalf("expected a failures map in the close error context, got %+v", typedError.Context())
    }

    if "refusing to close app.replaced.failing.first" != failures["container.replacedInstance[0]"] {
        t.Fatalf("expected the first replaced failure under its own key, got %+v", failures)
    }

    if "refusing to close app.replaced.failing.second" != failures["container.replacedInstance[1]"] {
        t.Fatalf("expected the second replaced failure under its own key, got %+v", failures)
    }
}

func TestContainer_Close_ReplacedOverrideIsNotClosed(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    registerErr := serviceContainer.Register(
        "app.replaced.override",
        func(resolver containercontract.Resolver) (*replacedBuiltProbe, error) {
            return &replacedBuiltProbe{label: "built", recorder: recorder}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    firstOverrideErr := serviceContainer.OverrideProtectedInstance(
        "app.replaced.override",
        &replacedBuiltProbe{label: "first-override", recorder: recorder},
    )
    if nil != firstOverrideErr {
        t.Fatalf("unexpected override error: %v", firstOverrideErr)
    }

    secondOverrideErr := serviceContainer.OverrideProtectedInstance(
        "app.replaced.override",
        &replacedBuiltProbe{label: "second-override", recorder: recorder},
    )
    if nil != secondOverrideErr {
        t.Fatalf("unexpected override error: %v", secondOverrideErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != len(closeSequence) || "second-override" != closeSequence[0] {
        t.Fatalf("expected only the surviving override to be closed, got %v", closeSequence)
    }
}

func TestContainer_Close_CycleFailureNamesTheNodes(t *testing.T) {
    serviceContainer := NewContainer().(*container)

    registerFirstErr := serviceContainer.Register(
        "app.cycle.first",
        func(resolver containercontract.Resolver) (*failingCloseService, error) {
            return &failingCloseService{}, nil
        },
    )
    if nil != registerFirstErr {
        t.Fatalf("unexpected register error: %v", registerFirstErr)
    }

    registerSecondErr := serviceContainer.Register(
        "app.cycle.second",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{recorder: &closeOrderRecorder{mutex: &sync.Mutex{}, closeSequence: &[]string{}}}, nil
        },
    )
    if nil != registerSecondErr {
        t.Fatalf("unexpected register error: %v", registerSecondErr)
    }

    if _, getErr := serviceContainer.Get("app.cycle.first"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }
    if _, getErr := serviceContainer.Get("app.cycle.second"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    serviceContainer.mutex.Lock()
    serviceContainer.registerDependencyLocked("service:app.cycle.first", "service:app.cycle.second")
    serviceContainer.registerDependencyLocked("service:app.cycle.second", "service:app.cycle.first")
    serviceContainer.mutex.Unlock()

    closeErr := serviceContainer.Close()
    if nil == closeErr {
        t.Fatalf("expected the failing close to be reported")
    }

    var typedError *exception.Error
    if false == errors.As(closeErr, &typedError) {
        t.Fatalf("expected a melody error, got %T", closeErr)
    }

    failures, hasFailures := typedError.Context()["failures"].(map[string]string)
    if false == hasFailures {
        t.Fatalf("expected the failures map in the close error context")
    }

    cycleText, hasCycle := failures["container.dependencyCycle"]
    if false == hasCycle {
        t.Fatalf("expected the dependency cycle to be recorded among the failures")
    }

    if false == strings.Contains(cycleText, "app.cycle.first") || false == strings.Contains(cycleText, "app.cycle.second") {
        t.Fatalf("expected the cycle failure to name its nodes, got %q", cycleText)
    }
}

func TestContainerIsClosed_FlipsExactlyAtClose(t *testing.T) {
    containerInstance := NewContainer()

    closedChecker, isChecker := interface{}(containerInstance).(interface{ IsClosed() bool })
    if false == isChecker {
        t.Fatalf("expected the container to report closedness")
    }

    if true == closedChecker.IsClosed() {
        t.Fatalf("expected a fresh container to report not closed")
    }

    if closeErr := containerInstance.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if false == closedChecker.IsClosed() {
        t.Fatalf("expected a closed container to report closed")
    }
}

func TestContainer_Close_ClosesAHolderBeforeTheServiceItResolvedThroughAKeptResolver(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    if err := serviceContainer.Register(
        "service.a",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{recorder: recorder}, nil
        },
    ); nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    if err := serviceContainer.Register(
        "app.holder",
        func(resolver containercontract.Resolver) (*lazyHoldingService, error) {
            return &lazyHoldingService{
                recorder: recorder,
                handle:   Lazy[*closeOrderServiceA](resolver, "service.a"),
            }, nil
        },
    ); nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    holder, err := FromResolver[*lazyHoldingService](serviceContainer, "app.holder")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    if _, err := serviceContainer.Get("service.a"); nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    if nil == holder.handle.Get() {
        t.Fatalf("expected the lazy handle to resolve the service")
    }

    if err := serviceContainer.Close(); nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    if 2 != len(closeSequence) {
        t.Fatalf("expected 2 close calls, got %d: %v", len(closeSequence), closeSequence)
    }

    if "holder" != closeSequence[0] {
        t.Fatalf("expected the holder to close before the service it resolved, got %v", closeSequence)
    }

    if "a" != closeSequence[1] {
        t.Fatalf("expected the resolved service to close last, got %v", closeSequence)
    }
}

func TestContainer_Close_APanickingCloseCarriesItsCauseAndItsStack(t *testing.T) {
    panicCause := exception.NewError(
        "the socket was already gone",
        map[string]any{"peer": "10.0.0.7"},
        errors.New("connection reset by peer"),
    )

    closeErr := closeServiceValue(&panickingCloseWithCauseService{cause: panicCause})

    if nil == closeErr {
        t.Fatalf("expected the panic to be contained as a failure")
    }

    if false == errors.Is(closeErr, panicCause) {
        t.Fatalf("expected the panic value to travel as the cause, got %v", closeErr)
    }

    typedError, isTyped := closeErr.(*exception.Error)
    if false == isTyped {
        t.Fatalf("expected an exception error, got %T", closeErr)
    }

    panicStack, hasStack := typedError.Context()["panicStack"].(string)
    if false == hasStack || false == strings.Contains(panicStack, "closeServiceValue") {
        t.Fatalf("expected the frames that ran to be captured, got %v", typedError.Context()["panicStack"])
    }

    causeChain := exception.BuildCauseChain(closeErr, 8)
    if 3 != len(causeChain) {
        t.Fatalf("expected the whole chain beneath the panic, got %v", causeChain)
    }
}

func TestContainer_Close_APanickingCloseWithoutAnErrorValueStillRecordsTheStack(t *testing.T) {
    closeErr := closeServiceValue(&panickingCloseWithTextService{})

    typedError, isTyped := closeErr.(*exception.Error)
    if false == isTyped {
        t.Fatalf("expected an exception error, got %T", closeErr)
    }

    if nil != typedError.Unwrap() {
        t.Fatalf("expected no cause for a panic value that is not an error, got %v", typedError.Unwrap())
    }

    if _, hasStack := typedError.Context()["panicStack"].(string); false == hasStack {
        t.Fatalf("expected the stack to be captured anyway, got %v", typedError.Context())
    }
}

func TestContainer_Close_ClosesTheEarliestCreatedServiceLast(t *testing.T) {
    for _, dependentName := range []string{"service.aaa.worker", "service.zzz.worker"} {
        serviceContainer := NewContainer()

        var mutex sync.Mutex
        closeSequence := make([]string, 0, 2)
        recorder := &closeOrderRecorder{
            mutex:         &mutex,
            closeSequence: &closeSequence,
        }

        if registerErr := serviceContainer.Register(
            "service.mmm.shared",
            func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
                return &closeOrderServiceA{recorder: recorder}, nil
            },
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }

        if registerErr := serviceContainer.Register(
            dependentName,
            func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
                return &closeOrderServiceB{recorder: recorder}, nil
            },
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }

        if _, getErr := serviceContainer.Get("service.mmm.shared"); nil != getErr {
            t.Fatalf("unexpected get error: %v", getErr)
        }

        if _, getErr := serviceContainer.Get(dependentName); nil != getErr {
            t.Fatalf("unexpected get error: %v", getErr)
        }

        if closeErr := serviceContainer.Close(); nil != closeErr {
            t.Fatalf("unexpected close error: %v", closeErr)
        }

        if 2 != len(closeSequence) {
            t.Fatalf("expected both services closed, got %v", closeSequence)
        }

        if "b" != closeSequence[0] || "a" != closeSequence[1] {
            t.Fatalf("expected the later-created service closed first for %s, got %v", dependentName, closeSequence)
        }
    }
}

func TestContainer_Close_ADeclaredEdgeIsHonoured(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    if registerErr := serviceContainer.Register(
        "service.dependency",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{recorder: recorder}, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "service.dependent",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            if _, resolveErr := resolver.Get("service.dependency"); nil != resolveErr {
                return nil, resolveErr
            }

            return &closeOrderServiceB{recorder: recorder}, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("service.dependent"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 2 != len(closeSequence) || "b" != closeSequence[0] || "a" != closeSequence[1] {
        t.Fatalf("expected the declared edge to decide, got %v", closeSequence)
    }
}

func TestContainer_Close_AServiceStillResolvesDuringTheTeardown(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 1)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    if registerErr := serviceContainer.Register(
        "service.mmm.shared",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{recorder: recorder}, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    resolvingService := &closeTimeResolvingService{serviceContainer: serviceContainer}

    if registerErr := serviceContainer.Register(
        "service.zzz.worker",
        func(resolver containercontract.Resolver) (*closeTimeResolvingService, error) {
            return resolvingService, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("service.mmm.shared"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := serviceContainer.Get("service.zzz.worker"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if nil != resolvingService.resolveErr {
        t.Fatalf("expected a service closing to still resolve what it reports through, got %v", resolvingService.resolveErr)
    }

    if nil == resolvingService.resolvedValue {
        t.Fatalf("expected the resolution during the teardown to answer the instance")
    }
}

func TestContainer_Get_RefusesAfterTheTeardownFinished(t *testing.T) {
    serviceContainer := NewContainer()

    closed := false

    if registerErr := serviceContainer.Register(
        "service.probe",
        func(resolver containercontract.Resolver) (*probeClosableService, error) {
            return &probeClosableService{closed: &closed}, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("service.probe"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    lateValue, lateErr := serviceContainer.Get("service.probe")

    if nil == lateErr {
        t.Fatalf("expected a resolution after the teardown to be refused, got %v", lateValue)
    }

    if false == strings.Contains(lateErr.Error(), "container is closed") {
        t.Fatalf("expected the closed-container refusal, got %v", lateErr)
    }

    if nil != lateValue {
        t.Fatalf("expected no value beside the refusal, got %v", lateValue)
    }
}

func TestScope_Get_RefusesAfterTheContainerTeardownFinished(t *testing.T) {
    serviceContainer := NewContainer()

    closed := false

    if registerErr := serviceContainer.Register(
        "service.probe",
        func(resolver containercontract.Resolver) (*probeClosableService, error) {
            return &probeClosableService{closed: &closed}, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get("service.probe"); nil != getErr {
        t.Fatalf("unexpected warm get error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    lateValue, lateErr := scopeInstance.Get("service.probe")

    if nil == lateErr {
        t.Fatalf("expected the resolution through the scope to be refused, got %v", lateValue)
    }

    if false == strings.Contains(lateErr.Error(), "container is closed") {
        t.Fatalf("expected the closed-container refusal, got %v", lateErr)
    }
}

func TestContainer_Close_AnEvictedOverrideAfterABuiltInstanceIsNotClosed(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 3)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    registerErr := serviceContainer.Register(
        "app.built.then.replaced",
        func(resolver containercontract.Resolver) (*replacedBuiltProbe, error) {
            return &replacedBuiltProbe{label: "built", recorder: recorder}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("app.built.then.replaced"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    firstOverrideErr := serviceContainer.OverrideProtectedInstance(
        "app.built.then.replaced",
        &replacedBuiltProbe{label: "first-override", recorder: recorder},
    )
    if nil != firstOverrideErr {
        t.Fatalf("unexpected override error: %v", firstOverrideErr)
    }

    secondOverrideErr := serviceContainer.OverrideProtectedInstance(
        "app.built.then.replaced",
        &replacedBuiltProbe{label: "second-override", recorder: recorder},
    )
    if nil != secondOverrideErr {
        t.Fatalf("unexpected override error: %v", secondOverrideErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    builtCloses := 0
    for _, label := range closeSequence {
        if "first-override" == label {
            t.Fatalf("expected the evicted override to stay its installer's, got %v", closeSequence)
        }
        if "built" == label {
            builtCloses = builtCloses + 1
        }
    }

    if 1 != builtCloses {
        t.Fatalf("expected the replaced built instance to be closed exactly once, got %v", closeSequence)
    }
}

func TestContainer_Close_ADeclaredTeardownDependencyBeatsTheCreationOrder(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    if registerErr := serviceContainer.Register(
        "service.dependency",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{recorder: recorder}, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "service.dependent",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{recorder: recorder}, nil
        },
        WithTeardownDependency("service.dependency"),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("service.dependent"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := serviceContainer.Get("service.dependency"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 2 != len(closeSequence) || "b" != closeSequence[0] || "a" != closeSequence[1] {
        t.Fatalf("expected the declared teardown dependency to decide, got %v", closeSequence)
    }
}

func TestContainer_Close_TheCreationOrderAloneClosesTheDependencyFirst(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    if registerErr := serviceContainer.Register(
        "service.dependency",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{recorder: recorder}, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "service.dependent",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{recorder: recorder}, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("service.dependent"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := serviceContainer.Get("service.dependency"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 2 != len(closeSequence) || "a" != closeSequence[0] || "b" != closeSequence[1] {
        t.Fatalf("expected the creation-order tie-break to close the dependency first, got %v", closeSequence)
    }
}

func TestContainer_Close_ADeclaredTeardownDependencyOnAnUnbuiltServiceIsDropped(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 1)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    if registerErr := serviceContainer.Register(
        "service.dependency",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{recorder: recorder}, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "service.dependent",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{recorder: recorder}, nil
        },
        WithTeardownDependency("service.dependency"),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("service.dependent"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != len(closeSequence) || "b" != closeSequence[0] {
        t.Fatalf("expected the dependent alone to close, got %v", closeSequence)
    }
}

func TestContainer_Close_ASharedDependencyWaitsForEveryDependent(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 3)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    if registerErr := serviceContainer.Register(
        "service.shared",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{recorder: recorder}, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.holder.first",
        func(resolver containercontract.Resolver) (*labelledLazyHolder, error) {
            return &labelledLazyHolder{
                label:    "first",
                recorder: recorder,
                handle:   Lazy[*closeOrderServiceA](resolver, "service.shared"),
            }, nil
        },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.holder.second",
        func(resolver containercontract.Resolver) (*labelledLazyHolder, error) {
            return &labelledLazyHolder{
                label:    "second",
                recorder: recorder,
                handle:   Lazy[*closeOrderServiceA](resolver, "service.shared"),
            }, nil
        },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    firstHolder, firstErr := FromResolver[*labelledLazyHolder](serviceContainer, "app.holder.first")
    if nil != firstErr {
        t.Fatalf("unexpected get error: %v", firstErr)
    }

    secondHolder, secondErr := FromResolver[*labelledLazyHolder](serviceContainer, "app.holder.second")
    if nil != secondErr {
        t.Fatalf("unexpected get error: %v", secondErr)
    }

    if nil == firstHolder.handle.Get() {
        t.Fatalf("expected the first handle to resolve the shared service")
    }

    if nil == secondHolder.handle.Get() {
        t.Fatalf("expected the second handle to resolve the shared service")
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 3 != len(closeSequence) {
        t.Fatalf("expected 3 close calls, got %d: %v", len(closeSequence), closeSequence)
    }

    if "second" != closeSequence[0] || "first" != closeSequence[1] || "a" != closeSequence[2] {
        t.Fatalf("expected the shared service to wait for both holders, got %v", closeSequence)
    }
}

func TestContainer_Close_AnAliasGroupIsAsOldAsItsOldestMember(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    shared := &closeOrderServiceA{recorder: recorder}

    if registerErr := serviceContainer.Register(
        "app.aaa.first",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return shared, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.middle",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{recorder: recorder}, nil
        },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.zzz.second",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{recorder: recorder}, nil
        },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("app.aaa.first"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := serviceContainer.Get("app.middle"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if overrideErr := serviceContainer.OverrideInstance("app.zzz.second", shared); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 2 != len(closeSequence) {
        t.Fatalf("expected 2 close calls, got %d: %v", len(closeSequence), closeSequence)
    }

    if "b" != closeSequence[0] || "a" != closeSequence[1] {
        t.Fatalf("expected the alias group to close as old as its oldest member, got %v", closeSequence)
    }
}

func TestContainer_Close_TheCycleRemainderClosesLatestFirst(t *testing.T) {
    serviceContainer := NewContainer().(*container)

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    if registerErr := serviceContainer.Register(
        "app.cycle.early",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{recorder: recorder}, nil
        },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.cycle.late",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{recorder: recorder}, nil
        },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("app.cycle.early"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := serviceContainer.Get("app.cycle.late"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    serviceContainer.mutex.Lock()
    serviceContainer.registerDependencyLocked("service:app.cycle.early", "service:app.cycle.late")
    serviceContainer.registerDependencyLocked("service:app.cycle.late", "service:app.cycle.early")
    serviceContainer.mutex.Unlock()

    if closeErr := serviceContainer.Close(); nil == closeErr {
        t.Fatalf("expected the dependency cycle to be reported")
    }

    if 2 != len(closeSequence) {
        t.Fatalf("expected 2 close calls, got %d: %v", len(closeSequence), closeSequence)
    }

    if "b" != closeSequence[0] || "a" != closeSequence[1] {
        t.Fatalf("expected the cycle remainder to close latest first, got %v", closeSequence)
    }
}

func TestContainer_CloseWithContext_PrefersTheContextTakingDoorAndHandsItTheDeadline(t *testing.T) {
    serviceContainer := NewContainer()

    recorder := &contextCloseRecorder{}

    registerErr := serviceContainer.Register(
        "service.context-closeable",
        func(resolver containercontract.Resolver) (*contextCloseRecorder, error) {
            return recorder, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("service.context-closeable"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    closeContext, cancel := context.WithTimeout(context.Background(), time.Minute)
    defer cancel()

    concreteContainer, isConcrete := serviceContainer.(interface {
        CloseWithContext(closeContext context.Context) error
    })
    if false == isConcrete {
        t.Fatal("the container does not carry the context-taking teardown")
    }

    if closeErr := concreteContainer.CloseWithContext(closeContext); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != recorder.contextCalls {
        t.Fatalf("the context-taking door was called %d times, wanted once", recorder.contextCalls)
    }

    if 0 != recorder.plainCalls {
        t.Fatalf("the plain door was called %d times, wanted none", recorder.plainCalls)
    }

    if false == recorder.hadDeadline {
        t.Fatal("the service was handed a context with no deadline")
    }

    if 0 >= recorder.grantedTerm || time.Minute < recorder.grantedTerm {
        t.Fatalf("the service was handed %s of a one minute teardown deadline", recorder.grantedTerm)
    }
}

func TestContainer_Close_ReachesTheContextTakingDoorWithoutADeadline(t *testing.T) {
    serviceContainer := NewContainer()

    recorder := &contextCloseRecorder{}

    registerErr := serviceContainer.Register(
        "service.context-closeable",
        func(resolver containercontract.Resolver) (*contextCloseRecorder, error) {
            return recorder, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("service.context-closeable"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != recorder.contextCalls {
        t.Fatalf("the context-taking door was called %d times, wanted once", recorder.contextCalls)
    }

    if true == recorder.hadDeadline {
        t.Fatal("a close with no budget handed the service a deadline")
    }
}

func TestContainer_Close_ArmedTheServicesOfOneWaveCloseAtOnce(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    registerWaveMatePair(t, serviceContainer, time.Second)

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("expected both services of the wave to be closing at the same moment, got: %v", closeErr)
    }
}

func TestContainer_Close_NotArmedTheServicesOfOneWaveCloseOneAfterTheOther(t *testing.T) {
    serviceContainer := NewContainer()

    registerWaveMatePair(t, serviceContainer, 50*time.Millisecond)

    closeErr := serviceContainer.Close()
    if nil == closeErr {
        t.Fatalf("expected the serial teardown to report a service that waited for a mate that had not started")
    }

    if false == strings.Contains(closeErr.Error(), "failed to close container services") {
        t.Fatalf("expected the failure map to carry the report, got: %v", closeErr)
    }
}

func TestTeardownCloseOrder_ADependencyIsOneWavePastItsLastDependent(t *testing.T) {
    nodeKeys := []string{"service:dependent.first", "service:dependent.second", "service:shared", "service:unrelated"}

    edges := map[string]map[string]struct{}{
        "service:dependent.first":  {"service:shared": struct{}{}},
        "service:dependent.second": {"service:shared": struct{}{}},
    }

    creationOrderOf := map[string]int{
        "service:dependent.first":  1,
        "service:dependent.second": 2,
        "service:shared":           3,
        "service:unrelated":        4,
    }

    closeOrder, closeWaveIndexOf, remaining := teardownCloseOrder(nodeKeys, edges, creationOrderOf)

    if 0 != len(remaining) {
        t.Fatalf("expected no cycle remainder, got %v", remaining)
    }

    if 4 != len(closeOrder) {
        t.Fatalf("expected every node in the close order, got %v", closeOrder)
    }

    for _, nodeKey := range []string{"service:dependent.first", "service:dependent.second", "service:unrelated"} {
        if 0 != closeWaveIndexOf[nodeKey] {
            t.Fatalf("expected %s to open the teardown in wave 0, got wave %d", nodeKey, closeWaveIndexOf[nodeKey])
        }
    }

    if 1 != closeWaveIndexOf["service:shared"] {
        t.Fatalf("expected the shared dependency one wave past both dependents, got wave %d", closeWaveIndexOf["service:shared"])
    }
}

func TestTeardownCloseOrder_TheSerialOrderIsUnchangedByTheWaves(t *testing.T) {
    nodeKeys := []string{"service:dependent.first", "service:dependent.second", "service:shared", "service:unrelated"}

    edges := map[string]map[string]struct{}{
        "service:dependent.first":  {"service:shared": struct{}{}},
        "service:dependent.second": {"service:shared": struct{}{}},
    }

    creationOrderOf := map[string]int{
        "service:dependent.first":  1,
        "service:dependent.second": 2,
        "service:shared":           3,
        "service:unrelated":        4,
    }

    closeOrder, _, _ := teardownCloseOrder(nodeKeys, edges, creationOrderOf)

    expectedOrder := []string{"service:unrelated", "service:dependent.second", "service:dependent.first", "service:shared"}

    if false == reflect.DeepEqual(expectedOrder, closeOrder) {
        t.Fatalf("expected the serial order untouched by the waves, wanted %v got %v", expectedOrder, closeOrder)
    }
}

func TestTeardownCloseOrder_APureDependencyOfARingMemberClosesAfterTheRingAndIsNotARingMember(t *testing.T) {
    nodeKeys := []string{"service:ring.a", "service:ring.b", "service:ring.dependency"}

    edges := map[string]map[string]struct{}{
        "service:ring.a": {"service:ring.b": struct{}{}},
        "service:ring.b": {"service:ring.a": struct{}{}, "service:ring.dependency": struct{}{}},
    }

    creationOrderOf := map[string]int{
        "service:ring.a":          1,
        "service:ring.b":          2,
        "service:ring.dependency": 3,
    }

    closeOrder, closeWaveIndexOf, ringMembers := teardownCloseOrder(nodeKeys, edges, creationOrderOf)

    expectedOrder := []string{"service:ring.b", "service:ring.a", "service:ring.dependency"}
    if false == reflect.DeepEqual(expectedOrder, closeOrder) {
        t.Fatalf("expected the ring closed first and its pure dependency after it, wanted %v got %v", expectedOrder, closeOrder)
    }

    sort.Strings(ringMembers)
    if false == reflect.DeepEqual([]string{"service:ring.a", "service:ring.b"}, ringMembers) {
        t.Fatalf("expected the ring members alone to be reported, got %v", ringMembers)
    }

    if closeWaveIndexOf["service:ring.dependency"] <= closeWaveIndexOf["service:ring.a"] {
        t.Fatalf("expected the pure dependency one wave past the ring, got ring %d dependency %d", closeWaveIndexOf["service:ring.a"], closeWaveIndexOf["service:ring.dependency"])
    }
}

func TestTeardownCloseOrder_TwoRingsJoinedByABridgeCloseInOrderAndTheBridgeIsNoRingMember(t *testing.T) {
    nodeKeys := []string{"service:ring.a", "service:ring.b", "service:bridge", "service:ring.e", "service:ring.f"}

    edges := map[string]map[string]struct{}{
        "service:ring.a": {"service:ring.b": struct{}{}, "service:bridge": struct{}{}},
        "service:ring.b": {"service:ring.a": struct{}{}},
        "service:bridge": {"service:ring.e": struct{}{}},
        "service:ring.e": {"service:ring.f": struct{}{}},
        "service:ring.f": {"service:ring.e": struct{}{}},
    }

    creationOrderOf := map[string]int{
        "service:ring.a": 1,
        "service:ring.b": 2,
        "service:bridge": 3,
        "service:ring.e": 4,
        "service:ring.f": 5,
    }

    closeOrder, closeWaveIndexOf, ringMembers := teardownCloseOrder(nodeKeys, edges, creationOrderOf)

    expectedOrder := []string{"service:ring.b", "service:ring.a", "service:bridge", "service:ring.f", "service:ring.e"}
    if false == reflect.DeepEqual(expectedOrder, closeOrder) {
        t.Fatalf("expected the first ring, the bridge, then the second ring, wanted %v got %v", expectedOrder, closeOrder)
    }

    sort.Strings(ringMembers)
    if false == reflect.DeepEqual([]string{"service:ring.a", "service:ring.b", "service:ring.e", "service:ring.f"}, ringMembers) {
        t.Fatalf("expected the four ring members alone to be reported, got %v", ringMembers)
    }

    if closeWaveIndexOf["service:ring.a"] != closeWaveIndexOf["service:ring.b"] || closeWaveIndexOf["service:ring.e"] != closeWaveIndexOf["service:ring.f"] {
        t.Fatalf("expected each ring in one wave of its own, got %v", closeWaveIndexOf)
    }

    if closeWaveIndexOf["service:ring.a"] >= closeWaveIndexOf["service:bridge"] || closeWaveIndexOf["service:bridge"] >= closeWaveIndexOf["service:ring.e"] {
        t.Fatalf("expected the waves ring, bridge, ring in that order, got %v", closeWaveIndexOf)
    }
}

func TestTeardownCloseOrder_ARingDependingDirectlyOnAnotherRingClosesBeforeIt(t *testing.T) {
    nodeKeys := []string{"service:ring.a", "service:ring.b", "service:ring.c", "service:ring.d"}

    edges := map[string]map[string]struct{}{
        "service:ring.a": {"service:ring.b": struct{}{}, "service:ring.c": struct{}{}},
        "service:ring.b": {"service:ring.a": struct{}{}},
        "service:ring.c": {"service:ring.d": struct{}{}},
        "service:ring.d": {"service:ring.c": struct{}{}},
    }

    creationOrderOf := map[string]int{
        "service:ring.a": 1,
        "service:ring.b": 2,
        "service:ring.c": 3,
        "service:ring.d": 4,
    }

    closeOrder, closeWaveIndexOf, _ := teardownCloseOrder(nodeKeys, edges, creationOrderOf)

    expectedOrder := []string{"service:ring.b", "service:ring.a", "service:ring.d", "service:ring.c"}
    if false == reflect.DeepEqual(expectedOrder, closeOrder) {
        t.Fatalf("expected the dependent ring before the ring it depends on, wanted %v got %v", expectedOrder, closeOrder)
    }

    if closeWaveIndexOf["service:ring.a"] >= closeWaveIndexOf["service:ring.c"] {
        t.Fatalf("expected the dependent ring's wave before the dependency ring's, got %v", closeWaveIndexOf)
    }
}

func TestTeardownCloseOrder_ARingTakesAWaveOfItsOwn(t *testing.T) {
    nodeKeys := []string{"service:lone.dependent", "service:lone.dependency", "service:ring.a", "service:ring.b"}

    edges := map[string]map[string]struct{}{
        "service:lone.dependent": {"service:lone.dependency": struct{}{}},
        "service:ring.a":         {"service:ring.b": struct{}{}},
        "service:ring.b":         {"service:ring.a": struct{}{}},
    }

    creationOrderOf := map[string]int{
        "service:lone.dependent":  1,
        "service:lone.dependency": 2,
        "service:ring.a":          3,
        "service:ring.b":          4,
    }

    _, closeWaveIndexOf, _ := teardownCloseOrder(nodeKeys, edges, creationOrderOf)

    for _, loneKey := range []string{"service:lone.dependent", "service:lone.dependency"} {
        if closeWaveIndexOf[loneKey] == closeWaveIndexOf["service:ring.a"] {
            t.Fatalf("expected the ring in a wave no unrelated node shares, got %v", closeWaveIndexOf)
        }
    }
}

func TestTeardownCloseOrder_TheWaveIndexesHaveNoHole(t *testing.T) {
    nodeKeys := []string{"service:chain.0", "service:chain.1", "service:chain.2", "service:dep", "service:ring.a", "service:ring.b"}

    edges := map[string]map[string]struct{}{
        "service:chain.0": {"service:chain.1": struct{}{}},
        "service:chain.1": {"service:chain.2": struct{}{}},
        "service:chain.2": {"service:dep": struct{}{}},
        "service:ring.a":  {"service:ring.b": struct{}{}, "service:dep": struct{}{}},
        "service:ring.b":  {"service:ring.a": struct{}{}},
    }

    creationOrderOf := map[string]int{
        "service:chain.0": 1,
        "service:chain.1": 2,
        "service:chain.2": 3,
        "service:dep":     4,
        "service:ring.a":  5,
        "service:ring.b":  6,
    }

    _, closeWaveIndexOf, _ := teardownCloseOrder(nodeKeys, edges, creationOrderOf)

    highest := 0
    populated := make(map[int]struct{})
    for _, waveIndex := range closeWaveIndexOf {
        populated[waveIndex] = struct{}{}
        if waveIndex > highest {
            highest = waveIndex
        }
    }

    for waveIndex := 0; waveIndex <= highest; waveIndex++ {
        if _, closesSomething := populated[waveIndex]; false == closesSomething {
            t.Fatalf("expected every wave up to %d to close something, wave %d closes nothing: %v", highest, waveIndex, closeWaveIndexOf)
        }
    }

    if closeWaveIndexOf["service:dep"] <= closeWaveIndexOf["service:ring.a"] {
        t.Fatalf("expected the dependency the ring releases to close after the ring, got %v", closeWaveIndexOf)
    }
}

func TestTeardownCloseOrder_HundredsOfRingsCloseInMilliseconds(t *testing.T) {
    const ringCount = 400

    nodeKeys := make([]string, 0, 2*ringCount)
    edges := make(map[string]map[string]struct{}, 2*ringCount)
    creationOrderOf := make(map[string]int, 2*ringCount)

    for ringIndex := 0; ringIndex < ringCount; ringIndex++ {
        first := fmt.Sprintf("service:ring.%d.a", ringIndex)
        second := fmt.Sprintf("service:ring.%d.b", ringIndex)
        nodeKeys = append(nodeKeys, first, second)
        edges[first] = map[string]struct{}{second: {}}
        edges[second] = map[string]struct{}{first: {}}
        creationOrderOf[first] = 2 * ringIndex
        creationOrderOf[second] = 2*ringIndex + 1
    }

    startedAt := time.Now()
    closeOrder, _, cycleNodeKeys := teardownCloseOrder(nodeKeys, edges, creationOrderOf)
    elapsed := time.Since(startedAt)

    if 2*ringCount != len(closeOrder) || 2*ringCount != len(cycleNodeKeys) {
        t.Fatalf("expected every ring member closed and reported, got %d closed and %d reported", len(closeOrder), len(cycleNodeKeys))
    }

    if 500*time.Millisecond < elapsed {
        t.Fatalf("expected %d rings to drain within 500ms, took %s", ringCount, elapsed)
    }
}

func TestContainer_ArmParallelTeardown_RefusesADeclaredDependencyOnAServiceThatWasNeverRegistered(t *testing.T) {
    serviceContainer := NewContainer()

    if registerErr := serviceContainer.Register(
        "app.reporter",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{}, nil
        },
        WithTeardownDependency("app.stroage"),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    armable, isArmable := serviceContainer.(interface{ ArmParallelTeardown() error })
    if false == isArmable {
        t.Fatalf("expected the container to carry the parallel teardown door")
    }

    armErr := armable.ArmParallelTeardown()
    if nil == armErr {
        t.Fatalf("expected the misspelled teardown dependency to refuse the arming")
    }

    if false == errors.Is(armErr, ErrTeardownDependencyWasNeverRegistered) {
        t.Fatalf("expected the never-registered sentinel, got: %v", armErr)
    }

    if false == strings.Contains(armErr.Error(), "never registered") {
        t.Fatalf("expected the refusal to name what is missing, got: %v", armErr)
    }
}

func TestContainer_ArmParallelTeardown_AdmitsADeclaredDependencyOnARegisteredServiceNobodyBuilt(t *testing.T) {
    serviceContainer := NewContainer()

    if registerErr := serviceContainer.Register(
        "app.storage",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{}, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.reporter",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{}, nil
        },
        WithTeardownDependency("app.storage"),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    armParallelTeardown(t, serviceContainer)
}

func TestContainer_Close_ADeclaredTeardownDependencyKeyedByTypeOrdersTheTeardown(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    if registerErr := serviceContainer.Register(
        "app.storage",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{recorder: recorder}, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.reporter",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{recorder: recorder}, nil
        },
        WithTeardownDependencyOfType[closeOrderServiceB](),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := FromResolver[*closeOrderServiceA](serviceContainer, "app.reporter"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := FromResolver[*closeOrderServiceB](serviceContainer, "app.storage"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 2 != len(closeSequence) {
        t.Fatalf("expected 2 close calls, got %d: %v", len(closeSequence), closeSequence)
    }

    if "a" != closeSequence[0] || "b" != closeSequence[1] {
        t.Fatalf("expected the type-keyed declaration to close the dependent first, got %v", closeSequence)
    }
}

func TestContainer_Close_ADeclarationOnAnAmbiguousTypeOrdersNothingAndReportsNoCycle(t *testing.T) {
    serviceContainer := NewContainer()

    recorder, closeSequence := newAmbiguousTypeRecorder()

    registerAmbiguousTypeWiring(t, serviceContainer, recorder, false)
    buildEveryRegisteredService(t, serviceContainer, "app.fallback", "app.primary", "app.router")

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("a declaration on an ambiguous type reported a failure over a teardown that closed everything: %v", closeErr)
    }

    if 3 != len(*closeSequence) {
        t.Fatalf("expected all three services to close, got %v", *closeSequence)
    }
}

func TestContainer_ArmParallelTeardown_RefusesADeclarationOnAnAmbiguousType(t *testing.T) {
    serviceContainer := NewContainer()

    recorder, _ := newAmbiguousTypeRecorder()

    registerAmbiguousTypeWiring(t, serviceContainer, recorder, false)

    armable, isArmable := serviceContainer.(interface{ ArmParallelTeardown() error })
    if false == isArmable {
        t.Fatalf("expected the container to carry the parallel teardown door")
    }

    armErr := armable.ArmParallelTeardown()
    if nil == armErr {
        t.Fatalf("arming was allowed over a declaration that names a type more than one service is registered under")
    }

    if false == errors.Is(armErr, ErrTeardownDependencyTypeIsAmbiguous) {
        t.Fatalf("the refusal does not carry the ambiguity cause: %v", armErr)
    }

    if false == strings.Contains(armErr.Error(), "more than one service is registered under") {
        t.Fatalf("the refusal does not name what is ambiguous: %v", armErr)
    }

    var typedError *exception.Error
    if false == errors.As(armErr, &typedError) {
        t.Fatalf("the refusal is not a melody error carrying a context: %v", armErr)
    }

    if "app.router" != typedError.Context()["serviceName"] {
        t.Fatalf("the refusal does not name the declaring service: %v", typedError.Context())
    }

    registeredNames, areNames := typedError.Context()["registered"].([]string)
    if false == areNames || 2 != len(registeredNames) {
        t.Fatalf("the refusal does not name the services the type stands for: %v", typedError.Context())
    }
}

func TestContainer_Close_ADeclarationOnAnUnambiguousTypeStillOrdersAndStillReportsARealCycle(t *testing.T) {
    serviceContainer := NewContainer()

    recorder, closeSequence := newAmbiguousTypeRecorder()

    registerAmbiguousTypeWiring(t, serviceContainer, recorder, true)
    buildEveryRegisteredService(t, serviceContainer, "app.primary", "app.router")

    closeErr := serviceContainer.Close()
    if nil == closeErr {
        t.Fatalf("a cycle the wiring actually declares was not reported")
    }

    if false == strings.Contains(closeErr.Error(), "cycle") {
        t.Fatalf("the failure does not name the cycle: %v", closeErr)
    }

    if 2 != len(*closeSequence) {
        t.Fatalf("expected both services to close even under a cycle, got %v", *closeSequence)
    }
}

func TestContainer_Close_ArmedTheCycleRemainderClosesOneAfterTheOther(t *testing.T) {
    serviceContainer := NewContainer()

    var running, peak atomic.Int64

    for _, pair := range [][2]string{{"cycle.a", "cycle.b"}, {"cycle.b", "cycle.c"}, {"cycle.c", "cycle.a"}} {
        if registerErr := serviceContainer.Register(
            pair[0],
            func(_ containercontract.Resolver) (*concurrentCloser, error) {
                return &concurrentCloser{running: &running, peak: &peak}, nil
            },
            WithoutTypeRegistration(),
            WithTeardownDependency(pair[1]),
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }
    }

    armParallelTeardown(t, serviceContainer)

    buildEveryRegisteredService(t, serviceContainer, "cycle.a", "cycle.b", "cycle.c")

    closeErr := serviceContainer.Close()
    if nil == closeErr || false == strings.Contains(closeErr.Error(), "dependency cycle detected") {
        t.Fatalf("expected the declared cycle to still be reported, got %v", closeErr)
    }

    if 1 != peak.Load() {
        t.Fatalf("expected the cycle remainder to close one service at a time, got %d at once", peak.Load())
    }
}

func TestContainer_Close_ArmedEachRingClosesOneAfterTheOther(t *testing.T) {
    serviceContainer := NewContainer()

    var running, peak atomic.Int64

    for _, pair := range [][2]string{{"ring.a", "ring.b"}, {"ring.b", "ring.a"}, {"ring.e", "ring.f"}, {"ring.f", "ring.e"}} {
        if registerErr := serviceContainer.Register(
            pair[0],
            func(_ containercontract.Resolver) (*concurrentCloser, error) {
                return &concurrentCloser{running: &running, peak: &peak}, nil
            },
            WithoutTypeRegistration(),
            WithTeardownDependency(pair[1]),
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }
    }

    armParallelTeardown(t, serviceContainer)

    buildEveryRegisteredService(t, serviceContainer, "ring.a", "ring.b", "ring.e", "ring.f")

    closeErr := serviceContainer.Close()
    if nil == closeErr || false == strings.Contains(closeErr.Error(), "dependency cycle detected") {
        t.Fatalf("expected the two declared rings to be reported, got %v", closeErr)
    }

    if 1 != peak.Load() {
        t.Fatalf("expected each ring to close one service at a time, got %d at once", peak.Load())
    }
}

func TestContainer_Close_TheCycleReportNamesTheRingMembersAlone(t *testing.T) {
    serviceContainer := NewContainer()

    serviceContainer.MustRegister(
        "cycle.c",
        func(_ containercontract.Resolver) (*poolDeclarerService, error) { return &poolDeclarerService{label: "c"}, nil },
        WithoutTypeRegistration(),
    )

    serviceContainer.MustRegister(
        "cycle.b",
        func(resolver containercontract.Resolver) (*poolDeclarerService, error) {
            if _, resolveErr := resolver.Get("cycle.c"); nil != resolveErr {
                return nil, resolveErr
            }

            return &poolDeclarerService{label: "b"}, nil
        },
        WithoutTypeRegistration(),
        WithTeardownDependency("cycle.a"),
    )

    serviceContainer.MustRegister(
        "cycle.a",
        func(resolver containercontract.Resolver) (*poolDeclarerService, error) {
            if _, resolveErr := resolver.Get("cycle.b"); nil != resolveErr {
                return nil, resolveErr
            }

            return &poolDeclarerService{label: "a"}, nil
        },
        WithoutTypeRegistration(),
    )

    MustFromResolver[*poolDeclarerService](serviceContainer, "cycle.a")

    closeErr := serviceContainer.Close()
    if nil == closeErr {
        t.Fatalf("expected the ring to be reported")
    }

    var typedError *exception.Error
    if false == errors.As(closeErr, &typedError) {
        t.Fatalf("expected a melody error, got %T", closeErr)
    }

    nodes, areNodes := typedError.Context()["nodes"].([]string)
    if false == areNodes {
        t.Fatalf("expected the ring members in the close error context, got %v", typedError.Context())
    }

    sort.Strings(nodes)
    if false == reflect.DeepEqual([]string{"service:cycle.a", "service:cycle.b"}, nodes) {
        t.Fatalf("expected the ring members alone to be named, got %v", nodes)
    }
}

func TestContainer_ArmParallelTeardown_RefusesADeclaredDependencyOnAScopedService(t *testing.T) {
    serviceContainer := NewContainer()

    if registerErr := serviceContainer.RegisterScoped(
        "scoped.store",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{}, nil
        },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected scoped register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.reporter",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{}, nil
        },
        WithoutTypeRegistration(),
        WithTeardownDependency("scoped.store"),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    armErr := serviceContainer.(interface{ ArmParallelTeardown() error }).ArmParallelTeardown()
    if false == errors.Is(armErr, ErrTeardownDependencyIsScoped) {
        t.Fatalf("expected the dependency on a scoped service to refuse the arming with ErrTeardownDependencyIsScoped, got %v", armErr)
    }
}

func TestContainer_Close_ArmedTheReplacedInstancesCloseOneAfterTheOther(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    var running, peak, closed atomic.Int64

    for _, serviceName := range []string{"app.replaced.first", "app.replaced.second"} {
        if registerErr := serviceContainer.Register(
            serviceName,
            func(resolver containercontract.Resolver) (*countingConcurrentCloser, error) {
                return &countingConcurrentCloser{running: &running, peak: &peak, closed: &closed}, nil
            },
            WithoutTypeRegistration(),
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }

        if _, getErr := serviceContainer.Get(serviceName); nil != getErr {
            t.Fatalf("unexpected get error: %v", getErr)
        }

        if overrideErr := serviceContainer.OverrideProtectedInstance(serviceName, &closeOrderServiceA{recorder: &closeOrderRecorder{mutex: &sync.Mutex{}, closeSequence: &[]string{}}}); nil != overrideErr {
            t.Fatalf("unexpected override error: %v", overrideErr)
        }
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 2 != closed.Load() {
        t.Fatalf("expected both replaced instances to be closed, got %d", closed.Load())
    }

    if 1 != peak.Load() {
        t.Fatalf("expected the replaced instances to close one at a time, got %d at once", peak.Load())
    }
}

func TestContainer_Close_ArmedAMutuallyHeldPairClosesOneAfterTheOther(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    var running, peak, closed atomic.Int64

    holder := &mutualConcurrentCloser{running: &running, peak: &peak, closed: &closed}
    collaborator := &mutualConcurrentCloser{running: &running, peak: &peak, closed: &closed, partner: holder}
    holder.partner = collaborator

    if registerErr := serviceContainer.Register(
        "app.mutual.holder",
        func(resolver containercontract.Resolver) (*mutualConcurrentCloser, error) { return holder, nil },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.mutual.collaborator",
        func(resolver containercontract.Resolver) (*mutualConcurrentCloser, error) { return collaborator, nil },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    buildEveryRegisteredService(t, serviceContainer, "app.mutual.holder", "app.mutual.collaborator")

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 2 != closed.Load() {
        t.Fatalf("expected both services of the pair to be closed, got %d", closed.Load())
    }

    if 1 != peak.Load() {
        t.Fatalf("expected the mutually held pair to close one at a time, got %d at once", peak.Load())
    }
}

func TestContainer_Close_ArmedTwoUnrelatedMutuallyHeldPairsCloseAtOnce(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    firstStarted := make(chan struct{})
    secondStarted := make(chan struct{})
    mates := []*waveMateCloser{
        {started: firstStarted, mateStarted: secondStarted, mateDeadline: time.Second},
        {started: secondStarted, mateStarted: firstStarted, mateDeadline: time.Second},
    }

    for pairIndex := 0; pairIndex < 2; pairIndex = pairIndex + 1 {
        plainMateStarted := make(chan struct{})
        close(plainMateStarted)
        plain := &waveMateCloser{started: make(chan struct{}), mateStarted: plainMateStarted, mateDeadline: time.Second}
        first := &mutualWaveMate{mate: mates[pairIndex]}
        second := &mutualWaveMate{mate: plain, partner: first}
        first.partner = second

        for memberIndex, member := range []*mutualWaveMate{first, second} {
            captured := member
            serviceName := fmt.Sprintf("app.pair%d.member%d", pairIndex, memberIndex)

            if registerErr := serviceContainer.Register(
                serviceName,
                func(resolver containercontract.Resolver) (*mutualWaveMate, error) { return captured, nil },
                WithoutTypeRegistration(),
            ); nil != registerErr {
                t.Fatalf("unexpected register error: %v", registerErr)
            }

            if _, getErr := serviceContainer.Get(serviceName); nil != getErr {
                t.Fatalf("unexpected get error: %v", getErr)
            }
        }
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("expected the two groups to be closing at the same moment, got: %v", closeErr)
    }
}

func TestContainer_CloseWithContext_AnOverrunNamesTheServiceThatSpentTheBudget(t *testing.T) {
    serviceContainer := NewContainer()

    serviceContainer.MustRegister("app.eater", func(_ containercontract.Resolver) (*budgetSleeper, error) { return &budgetSleeper{sleep: 80 * time.Millisecond, label: "eater"}, nil })
    MustFromResolver[*budgetSleeper](serviceContainer, "app.eater")

    if closeErr := closeContainerWithin(t, serviceContainer, 40*time.Millisecond); nil != closeErr {
        t.Fatalf("expected the overrun alone not to be a failure, got %v", closeErr)
    }

    record := teardownDeadlineOverrunOf(t, serviceContainer)
    if nil == record {
        t.Fatalf("expected a teardown that ran past its deadline to keep a record")
    }

    if _, named := namedDurations(t, record, "spentBy")["service:app.eater"]; false == named {
        t.Fatalf("expected the closer running when the deadline passed to be named, got %v", record)
    }

    if starved, _ := record["starved"].([]string); 0 != len(starved) {
        t.Fatalf("expected no closer reached after the deadline, got %v", starved)
    }

    if spent := durationOfRecord(t, record, "spent"); spent < 80*time.Millisecond {
        t.Fatalf("expected the whole teardown spent at least the eater's eighty milliseconds, got %v", spent)
    }

    if budget := durationOfRecord(t, record, "budget"); budget > 40*time.Millisecond || 0 >= budget {
        t.Fatalf("expected the budget recorded as what was left of forty milliseconds, got %v", budget)
    }
}

func TestContainer_CloseWithContext_AServiceReachedWithTheBudgetSpentIsNamedStarved(t *testing.T) {
    serviceContainer := NewContainer()

    serviceContainer.MustRegister("app.fast", func(_ containercontract.Resolver) (*budgetSleeper, error) { return &budgetSleeper{sleep: 0, label: "fast"}, nil }, WithoutTypeRegistration())
    serviceContainer.MustRegister("app.eater", func(_ containercontract.Resolver) (*budgetSleeper, error) { return &budgetSleeper{sleep: 80 * time.Millisecond, label: "eater"}, nil }, WithoutTypeRegistration())
    MustFromResolver[*budgetSleeper](serviceContainer, "app.fast")
    MustFromResolver[*budgetSleeper](serviceContainer, "app.eater")

    if closeErr := closeContainerWithin(t, serviceContainer, 40*time.Millisecond); nil != closeErr {
        t.Fatalf("expected the overrun alone not to be a failure, got %v", closeErr)
    }

    record := teardownDeadlineOverrunOf(t, serviceContainer)
    if nil == record {
        t.Fatalf("expected a record of the overrun")
    }

    starved, _ := record["starved"].([]string)
    if 1 != len(starved) || "service:app.fast" != starved[0] {
        t.Fatalf("expected the service reached after the deadline named starved, got %v", record)
    }

    if _, named := namedDurations(t, record, "spentBy")["service:app.eater"]; false == named {
        t.Fatalf("expected the eater named as the closer that spent the budget, got %v", record)
    }

    if _, timed := namedDurations(t, record, "durations")["service:app.fast"]; false == timed {
        t.Fatalf("expected every close timed, the starved one included, got %v", record)
    }
}

func TestContainer_CloseWithContext_ArmedTwoClosersOfOneWaveThatOverlapAreBothNamed(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    serviceContainer.MustRegister("app.first", func(_ containercontract.Resolver) (*budgetSleeper, error) { return &budgetSleeper{sleep: 80 * time.Millisecond, label: "first"}, nil }, WithoutTypeRegistration())
    serviceContainer.MustRegister("app.second", func(_ containercontract.Resolver) (*budgetSleeper, error) { return &budgetSleeper{sleep: 80 * time.Millisecond, label: "second"}, nil }, WithoutTypeRegistration())
    MustFromResolver[*budgetSleeper](serviceContainer, "app.first")
    MustFromResolver[*budgetSleeper](serviceContainer, "app.second")

    if closeErr := closeContainerWithin(t, serviceContainer, 40*time.Millisecond); nil != closeErr {
        t.Fatalf("expected the overrun alone not to be a failure, got %v", closeErr)
    }

    record := teardownDeadlineOverrunOf(t, serviceContainer)
    if nil == record {
        t.Fatalf("expected a record of the overrun")
    }

    spentBy := namedDurations(t, record, "spentBy")
    if _, named := spentBy["service:app.first"]; false == named {
        t.Fatalf("expected the first closer named, got %v", record)
    }

    if _, named := spentBy["service:app.second"]; false == named {
        t.Fatalf("expected the second closer named beside it, got %v", record)
    }

    if spent := durationOfRecord(t, record, "spent"); spent >= 160*time.Millisecond {
        t.Fatalf("expected the two closers to overlap in one wave, got a teardown of %v", spent)
    }
}

func TestContainer_CloseWithContext_ATeardownThatClosedNothingKeepsNoOverrunRecord(t *testing.T) {
    serviceContainer := NewContainer()

    spentContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    if closeErr := serviceContainer.(interface {
        CloseWithContext(context.Context) error
    }).CloseWithContext(spentContext); nil != closeErr {
        t.Fatalf("expected a clean close of an empty container, got %v", closeErr)
    }

    if record := serviceContainer.(interface {
        TeardownDeadlineOverrun() exceptioncontract.Context
    }).TeardownDeadlineOverrun(); nil != record {
        t.Fatalf("expected no overrun record for a teardown that closed nothing, got %v", record)
    }
}

func TestContainer_CloseWithContext_AFailureUnderAnOverrunCarriesTheDeadlineRecordBesideTheFailures(t *testing.T) {
    serviceContainer := NewContainer()

    serviceContainer.MustRegister("app.eater", func(_ containercontract.Resolver) (*budgetSleeper, error) { return &budgetSleeper{sleep: 80 * time.Millisecond, label: "eater"}, nil })
    serviceContainer.MustRegister("app.failing", func(_ containercontract.Resolver) (*failingCloser, error) { return &failingCloser{}, nil })
    MustFromResolver[*budgetSleeper](serviceContainer, "app.eater")
    MustFromResolver[*failingCloser](serviceContainer, "app.failing")

    closeErr := closeContainerWithin(t, serviceContainer, 40*time.Millisecond)
    if nil == closeErr {
        t.Fatalf("expected the failing close reported")
    }

    errorContext := exception.LogContext(closeErr)

    failures, hasFailures := errorContext["failures"].(map[string]string)
    if false == hasFailures || "" == failures["service:app.failing"] {
        t.Fatalf("expected the failure map beside the record, got %v", errorContext)
    }

    record, hasRecord := errorContext["deadline"].(exceptioncontract.Context)
    if false == hasRecord {
        t.Fatalf("expected the deadline record beside the failures, got %v", errorContext)
    }

    if _, named := namedDurations(t, record, "spentBy")["service:app.eater"]; false == named {
        t.Fatalf("expected the eater named in the record carried by the error, got %v", record)
    }
}

func TestContainer_CloseWithContext_ATeardownWithinItsBudgetLeavesNoDeadlineRecord(t *testing.T) {
    serviceContainer := NewContainer()

    serviceContainer.MustRegister("app.fast", func(_ containercontract.Resolver) (*budgetSleeper, error) { return &budgetSleeper{sleep: 0, label: "fast"}, nil })
    MustFromResolver[*budgetSleeper](serviceContainer, "app.fast")

    if closeErr := closeContainerWithin(t, serviceContainer, time.Second); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if record := teardownDeadlineOverrunOf(t, serviceContainer); nil != record {
        t.Fatalf("expected no record for a teardown inside its budget, got %v", record)
    }
}

func TestContainer_Close_WithoutADeadlineLeavesNoDeadlineRecord(t *testing.T) {
    serviceContainer := NewContainer()

    serviceContainer.MustRegister("app.eater", func(_ containercontract.Resolver) (*budgetSleeper, error) { return &budgetSleeper{sleep: 20 * time.Millisecond, label: "eater"}, nil })
    MustFromResolver[*budgetSleeper](serviceContainer, "app.eater")

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if record := teardownDeadlineOverrunOf(t, serviceContainer); nil != record {
        t.Fatalf("expected no record for a close with no deadline, got %v", record)
    }
}

func TestContainer_CloseWithContext_ANonCloserAndADuplicateValueAreNotRecorded(t *testing.T) {
    serviceContainer := NewContainer()

    eater := &budgetSleeper{sleep: 80 * time.Millisecond, label: "eater"}

    serviceContainer.MustRegister("app.eater", func(_ containercontract.Resolver) (*budgetSleeper, error) { return eater, nil }, WithoutTypeRegistration())
    serviceContainer.MustRegister("app.eater.again", func(_ containercontract.Resolver) (*budgetSleeper, error) { return eater, nil }, WithoutTypeRegistration())
    serviceContainer.MustRegister("app.plain", func(_ containercontract.Resolver) (*plainValueService, error) { return &plainValueService{label: "plain"}, nil })
    MustFromResolver[*budgetSleeper](serviceContainer, "app.eater")
    MustFromResolver[*budgetSleeper](serviceContainer, "app.eater.again")
    MustFromResolver[*plainValueService](serviceContainer, "app.plain")

    if closeErr := closeContainerWithin(t, serviceContainer, 40*time.Millisecond); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    record := teardownDeadlineOverrunOf(t, serviceContainer)
    if nil == record {
        t.Fatalf("expected a record of the overrun")
    }

    if durations := namedDurations(t, record, "durations"); 1 != len(durations) {
        t.Fatalf("expected exactly the one close that ran timed, got %v", durations)
    }
}

func TestContainer_ArmParallelTeardown_RefusesADeclaredDependencyOnAScopedTypeAsScoped(t *testing.T) {
    serviceContainer := NewContainer()

    serviceContainer.MustRegisterScoped(
        "app.scoped",
        func(_ containercontract.Resolver) (*scopedOnlyService, error) { return &scopedOnlyService{label: "scoped"}, nil },
    )

    if registerErr := serviceContainer.Register(
        "app.declarer",
        func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{}, nil },
        WithTeardownDependencyOfType[*scopedOnlyService](),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    armErr := serviceContainer.(interface{ ArmParallelTeardown() error }).ArmParallelTeardown()
    if false == errors.Is(armErr, ErrTeardownDependencyIsScoped) {
        t.Fatalf("expected the dependency on a scoped type to refuse the arming as scoped, got %v", armErr)
    }
}

func TestContainer_Close_ADeclarationOnATypeResolvedThroughItselfLeavesNoRawEdgeBehind(t *testing.T) {
    serviceContainer := NewContainer()

    serviceContainer.MustRegister(
        "app.declarer",
        func(_ containercontract.Resolver) (*poolDeclarerService, error) { return &poolDeclarerService{label: "declarer"}, nil },
        WithTeardownDependencyOfType[*sharedPoolService](),
    )

    serviceContainer.MustRegister(
        "app.pool.first",
        func(resolver containercontract.Resolver) (*sharedPoolService, error) {
            if _, resolveErr := resolver.Get("app.declarer"); nil != resolveErr {
                return nil, resolveErr
            }

            return &sharedPoolService{label: "first"}, nil
        },
        WithTypeRegistration(false),
    )

    serviceContainer.MustGetByType(reflect.TypeOf((*sharedPoolService)(nil)))

    serviceContainer.MustRegister(
        "app.pool.second",
        func(_ containercontract.Resolver) (*sharedPoolService, error) { return &sharedPoolService{label: "second"}, nil },
        WithTypeRegistration(false),
    )

    MustFromResolver[*sharedPoolService](serviceContainer, "app.pool.second")

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("expected a clean teardown once the declaration turned ambiguous, got %v", closeErr)
    }
}

func TestContainer_Close_ArmedACapturedPointerBackAgainstATypeDeclarationIsNoRing(t *testing.T) {
    for _, byType := range []bool{true, false} {
        serviceContainer := NewContainer()
        declarerInstance := &poolDeclarerService{label: "declarer"}

        option := WithTeardownDependency("app.pool")
        if true == byType {
            option = WithTeardownDependencyOfType[*capturedDeclarerPool]()
        }

        serviceContainer.MustRegister(
            "app.declarer",
            func(_ containercontract.Resolver) (*poolDeclarerService, error) { return declarerInstance, nil },
            option,
        )

        serviceContainer.MustRegister(
            "app.pool",
            func(_ containercontract.Resolver) (*capturedDeclarerPool, error) {
                return &capturedDeclarerPool{declarer: declarerInstance}, nil
            },
        )

        if armErr := serviceContainer.(parallelTeardownArmer).ArmParallelTeardown(); nil != armErr {
            t.Fatalf("byType=%v arm: %v", byType, armErr)
        }

        MustFromResolver[*poolDeclarerService](serviceContainer, "app.declarer")
        MustFromResolver[*capturedDeclarerPool](serviceContainer, "app.pool")

        plan := serviceContainer.(teardownPlanner).TeardownPlan()

        for _, entry := range plan {
            if "service:app.pool" == entry.NodeKey && 0 != len(entry.Dependencies) {
                t.Fatalf("byType=%v: the held pointer back was written as an inference over the declaration it contradicts: %v", byType, entry.Dependencies)
            }
        }

        if closeErr := serviceContainer.Close(); nil != closeErr {
            t.Fatalf("byType=%v: expected a clean armed teardown, got %v", byType, closeErr)
        }
    }
}

func TestContainer_TeardownPlan_AResolutionBetweenTwoNamesOfOneInstanceIsNoEdge(t *testing.T) {
    serviceContainer := NewContainer()

    serviceContainer.MustRegister(
        "app.a",
        func(_ containercontract.Resolver) (*poolDeclarerService, error) { return &poolDeclarerService{label: "a"}, nil },
        WithoutTypeRegistration(),
    )

    serviceContainer.MustRegister(
        "app.b",
        func(resolver containercontract.Resolver) (*poolDeclarerService, error) {
            value, resolveErr := resolver.Get("app.a")
            if nil != resolveErr {
                return nil, resolveErr
            }

            return value.(*poolDeclarerService), nil
        },
        WithoutTypeRegistration(),
    )

    MustFromResolver[*poolDeclarerService](serviceContainer, "app.b")

    for _, entry := range serviceContainer.(teardownPlanner).TeardownPlan() {
        if 0 != len(entry.Dependencies) {
            t.Fatalf("%s is listed as closed before %v — one instance under two names, ordered against itself", entry.NodeKey, entry.Dependencies)
        }
    }
}

func TestContainer_Close_TheViewLeavesNoExpandedTypeEdgeBehindOnTheDefaultPath(t *testing.T) {
    serviceContainer := NewContainer()

    serviceContainer.MustRegister(
        "app.declarer",
        func(_ containercontract.Resolver) (*poolDeclarerService, error) { return &poolDeclarerService{label: "declarer"}, nil },
        WithTeardownDependencyOfType[*sharedPoolService](),
    )

    serviceContainer.MustRegister(
        "app.pool.first",
        func(resolver containercontract.Resolver) (*sharedPoolService, error) {
            if _, resolveErr := resolver.Get("app.declarer"); nil != resolveErr {
                return nil, resolveErr
            }

            return &sharedPoolService{label: "first"}, nil
        },
        WithTypeRegistration(false),
    )

    MustFromResolver[*sharedPoolService](serviceContainer, "app.pool.first")

    serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    }).TeardownPlan()

    serviceContainer.MustRegister(
        "app.pool.second",
        func(_ containercontract.Resolver) (*sharedPoolService, error) { return &sharedPoolService{label: "second"}, nil },
        WithTypeRegistration(false),
    )

    MustFromResolver[*sharedPoolService](serviceContainer, "app.pool.second")

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("expected a clean teardown once the declaration turned ambiguous, got %v", closeErr)
    }
}

func TestContainerClosesContextOnlyServices(t *testing.T) {
    serviceContainer := NewContainer()
    failure := errors.New("context-only close failed")
    value := &contextOnlyCleanup{failure: failure}
    MustRegister(serviceContainer, "context-only", func(resolver containercontract.Resolver) (*contextOnlyCleanup, error) { return value, nil })
    _ = serviceContainer.MustGet("context-only")
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    err := serviceContainer.(interface { CloseWithContext(context.Context) error }).CloseWithContext(ctx)
    if 1 != value.calls || ctx != value.seen || nil == err { t.Fatalf("context-only service skipped or failure lost: calls=%d err=%v", value.calls, err) }
}

func TestContainer_Close_ReleasesTheHeldIdentityRecords(t *testing.T) {
    finalized := make(chan struct{}, 1)

    serviceContainer := NewContainer()

    defer runtime.KeepAlive(serviceContainer)

    armParallelTeardown(t, serviceContainer)

    holder := &capturingHolder{recorder: &closeOrderRecorder{mutex: &sync.Mutex{}, closeSequence: &[]string{}}, held: &closeOrderServiceB{}}
    runtime.SetFinalizer(holder.held, func(*closeOrderServiceB) { finalized <- struct{}{} })

    if registerErr := serviceContainer.Register(
        "app.holder",
        func(_ containercontract.Resolver) (*capturingHolder, error) { return holder, nil },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("app.holder"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    holder.held = nil

    for cycle := 0; cycle < 3; cycle = cycle + 1 {
        runtime.GC()
    }

    select {
    case <-finalized:
        t.Fatalf("expected the record to keep the dropped collaborator alive until the teardown")
    case <-time.After(50 * time.Millisecond):
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    for cycle := 0; cycle < 5; cycle = cycle + 1 {
        runtime.GC()

        select {
        case <-finalized:
            return
        case <-time.After(20 * time.Millisecond):
        }
    }

    t.Fatalf("expected the collaborator to be collectable once the container had closed")
}
