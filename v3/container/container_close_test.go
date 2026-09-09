package container

import (
    "context"
    "errors"
    "reflect"
    "strings"
    "sync"
    "testing"
    "time"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
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

type unhashableValueCloser struct {
    counter *int
    lock    *sync.Mutex
    payload any
}

func (instance unhashableValueCloser) Close() error {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    *instance.counter++

    return nil
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

type nonComparableValueCloser struct {
    counter *int
    lock    *sync.Mutex
    tags    []string
}

func (instance nonComparableValueCloser) Close() error {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    *instance.counter++

    return nil
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

/* OverrideProtectedInstance on a WithoutTypeRegistration value service must close once */

type overrideValueCloser struct {
    counter *int
    lock    *sync.Mutex
    tags    []string
}

func (instance overrideValueCloser) Close() error {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    *instance.counter++

    return nil
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

/* a concurrent second Close must not report success while the first is still tearing services down: both callers get the first teardown's result once it finishes */
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

    /* the equality below says nothing while both results are nil — two callers who each got a plain success share it by accident. The blocking service fails its Close so the shared result is a value there is only one of */
    if nil == firstErr {
        t.Fatalf("expected the teardown failure to be reported to the first caller, got nil")
    }

    if firstErr != secondErr {
        t.Fatalf("expected both callers to share the teardown result, got %v and %v", firstErr, secondErr)
    }
}

type blockingCloser struct {
    release  chan struct{}
    observed chan struct{}
}

var errBlockingCloserFailed = errors.New("the blocking closer failed")

func (instance *blockingCloser) Close() error {
    instance.observed <- struct{}{}
    <-instance.release

    return errBlockingCloserFailed
}

type panickingCloseService struct{}

func (instance *panickingCloseService) Close() error {
    panic("teardown exploded")
}

/* a service whose Close panics must not abort the teardown: the remaining services still close, the panic is recorded as a close failure, and a repeated Close reports the same error instead of a silent success */
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

type panickingErrorMessage struct{}

func (instance panickingErrorMessage) Error() string {
    panic("boom from Error()")
}

type panickingErrorMessageService struct{}

func (instance *panickingErrorMessageService) Close() error {
    return panickingErrorMessage{}
}

/* a user error whose Error() panics must not escape the teardown: the failure is recorded with a deterministic message and a repeated Close reports the same error instead of a silent success */
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

type zeroSizeCloserOne struct{}

func (instance *zeroSizeCloserOne) Close() error {
    zeroSizeCloserOneClosed = true
    return nil
}

type zeroSizeCloserTwo struct{}

func (instance *zeroSizeCloserTwo) Close() error {
    zeroSizeCloserTwoClosed = true
    return nil
}

var (
    zeroSizeCloserOneClosed = false
    zeroSizeCloserTwoClosed = false
)

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

type firstFieldOuterCloser struct {
    inner   firstFieldInnerCloser
    counter *int
    lock    *sync.Mutex
}

func (instance *firstFieldOuterCloser) Close() error {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    *instance.counter++

    return nil
}

type firstFieldInnerCloser struct {
    counter *int
    lock    *sync.Mutex
}

func (instance *firstFieldInnerCloser) Close() error {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    *instance.counter += 10

    return nil
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

type sameTypeZeroSizeCloser struct{}

var sameTypeZeroSizeCloseCount = 0

func (instance *sameTypeZeroSizeCloser) Close() error {
    sameTypeZeroSizeCloseCount = sameTypeZeroSizeCloseCount + 1

    return nil
}

/* two distinct services of one zero-size type share an address, so pairing the address with the type is not enough to tell them apart; without the dependency-graph qualifier the second one was collapsed onto the first and never closed */
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

/* the mirror case: a service whose provider hands back what it resolved from another service holds the same instance, and the dependency edge proves it, so the shared instance is still closed exactly once */
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

type failingCloseService struct{}

func (instance *failingCloseService) Close() error {
    return errors.New("refusing to close")
}

type replacedBuiltProbe struct {
    label    string
    recorder *closeOrderRecorder
}

func (instance *replacedBuiltProbe) Close() error {
    instance.recorder.record(instance.label)

    return nil
}

/* an override replacing an instance the container built evicts it from the only maps the close sweep reads: it used to leak forever, with both the resolution and the override reporting success. The evicted value waits in the graveyard and the teardown closes it — once — alongside the override that took its place. */
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

type replacedFailingProbe struct {
    failure error
}

func (instance *replacedFailingProbe) Close() error {
    return instance.failure
}

type secondReplacedFailingProbe struct {
    failure error
}

func (instance *secondReplacedFailingProbe) Close() error {
    return instance.failure
}

/* two replaced built instances whose closes both fail are both recorded: the graveyard entries carry no node key of their own, so a shared constant key let the second failure overwrite the first's record, naming one failure where two happened */
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

/* an override evicting an EARLIER override closes nothing: an installed override belongs to whoever installed it, and only what the container itself built enters the graveyard. */
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

    /* MEASURED, not assumed: the surviving override IS closed with the container, and only the EVICTED one is left to its installer. A loop that merely refuses the evicted label passes over an empty slice too, so it cannot tell this apart from a teardown that closed nothing at all */
    if 1 != len(closeSequence) || "second-override" != closeSequence[0] {
        t.Fatalf("expected only the surviving override to be closed, got %v", closeSequence)
    }
}

/* a close that both fails and cycles used to keep the failures and drop the cycle's node list — the operator saw WHICH services failed but not which ones cycled. The nodes ride inside the failure text now. */
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

/* IsClosed is asked before a defensive Close: the memoized close error makes a repeated Close indistinguishable from the discovering one, and the asker must not re-report a failure somebody else already carried away. */
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

type lazyHoldingService struct {
    recorder *closeOrderRecorder
    handle   *LazyService[*closeOrderServiceA]
}

func (instance *lazyHoldingService) Close() error {
    instance.recorder.record("holder")
    return nil
}

/* a service that keeps its resolver and reaches through it after its provider returned depends on what it then resolves exactly as hard as one that resolved it during construction. The edge used to be read from the live resolution stack, which is empty by then, so no edge was recorded at all — here that closes the dependency FIRST, and the holder's own Close then runs over a service that has already ended. Without the edge the creation-order tie-break decides, and it disagrees with the graph: the dependency is built AFTER the holder that reaches for it, so latest-first closes it before its holder. */
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
            /* nothing is resolved here: the handle defers it to first use, which is the whole point */
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

    /* the dependency is built before the handle ever asks for it, which is the ordinary case and the one that tells the read path from the write path: a resolution that finds the instance already there still has an edge to record */
    if _, err := serviceContainer.Get("service.a"); nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    /* the first use, on a path that runs long after the provider returned */
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


type panickingCloseWithCauseService struct {
    cause error
}

func (instance *panickingCloseWithCauseService) Close() error {
    panic(instance.cause)
}

type panickingCloseWithTextService struct{}

func (instance *panickingCloseWithTextService) Close() error {
    panic("the drain buffer was nil")
}

/* TestContainer_Close_APanickingCloseCarriesItsCauseAndItsStack pins what an operator learns from the one boundary that contains a teardown panic: nothing above it sees the panic and nothing below it survives, so whatever the recorded failure drops is gone. An error-shaped panic value kept only as its stringified message collapsed to one line at the render boundary, taking the context map and the cause chain of the very error the Close raised with it, and the frames that ran existed only inside the recover. */
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

/* a panic value that is not an error has no cause to give, and the record still carries what it can */
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

/* TestContainer_Close_ClosesTheEarliestCreatedServiceLast pins the tie-break the dependency graph leaves open. It used to be the node key descending — a string comparison nobody wrote — so a service resolved first at boot and used silently by everything afterwards, the logger being the case that matters, was closed in the middle of the teardown by nothing but its name. Whether a worker still had somewhere to report its drain came down to whether it sorted above or below its dependency: renaming app.worker to zz.worker was the whole difference. */
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

        /* the shared service is resolved FIRST and no edge is ever declared: this is the whole shape of a logger resolved at boot and used by everything through its own door afterwards */
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

/* a declared edge is honoured: the provider resolves its dependency during construction, so the edge is recorded and the dependency closes last. The creation-order tie-break answers the same here — a dependency built DURING its dependent finishes its own filing FIRST and is therefore the older node, which latest-first closes last anyway — so this pins that the edge is read, not that it outranks the tie-break. The fixture where the two disagree is the kept-handle one below, whose dependency is built after the holder that reaches for it. */
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

    /* the dependent is resolved first, but its dependency is built INSIDE that provider and finishes its own filing before it returns, so the dependency is the OLDER node: the edge and the creation-order tie-break agree here */
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

type closeTimeResolvingService struct {
    serviceContainer containercontract.Container
    resolveErr       error
    resolvedValue    any
}

func (instance *closeTimeResolvingService) Close() error {
    instance.resolvedValue, instance.resolveErr = instance.serviceContainer.Get("service.mmm.shared")

    return nil
}

/* TestContainer_Close_AServiceStillResolvesDuringTheTeardown pins the first of the two closing states against the second. Refusing every resolution from the moment Close begins would take away the very thing closing the logger last exists to give: a worker reporting its drain resolves what it reports through, from inside its own Close. What is refused is a resolution made after the LAST close returned, which used to answer the instance found in the map — already closed — with a nil error. */
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

/* TestContainer_Get_RefusesAfterTheTeardownFinished pins the second closing state. A resolution performed once the teardown is over was answered out of the maps — which the teardown has just emptied of meaning — so a caller holding a resolver received a handle to a service every Close in the process had already run on, with a nil error saying it was fine. The fast path is asked separately because a memoized instance never reaches the creation guard that refuses a closed container. */
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

type probeClosableService struct {
    closed *bool
}

func (instance *probeClosableService) Close() error {
    *instance.closed = true

    return nil
}

/* the refusal covers the resolution paths the fast path never sees: a resolution made through a SCOPE skips the memoized read entirely and reaches the creation guard, which is the second door the same closing state has to be asked at */
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

/* the built mark leaves the maps together with the instance it marks: the first override moves the BUILT instance to the graveyard and takes the mark down, so the second override evicts a value that is its installer's, not the container's. With the mark left standing, the first override was graveyarded as if the container had built it, and closed under the feet of whoever installed it. */
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

/* TestContainer_Close_ADeclaredTeardownDependencyBeatsTheCreationOrder is the declarative twin of the test above, for the registration that resolves nothing. Neither provider touches the resolver, so the container derives no edge at all, and the two are resolved in the order that makes the creation-order tie-break answer WRONG on its own: the dependency is created LAST, so the latest-first tie-break would close it FIRST, while its dependent is still alive. Only the declaration can put it last, which is what makes this probe separate the guard from the ordering the container would have reached anyway. */
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

    /* the dependent is built FIRST and the dependency SECOND, so the creation-order tie-break alone would close the dependency first — the wrong way round. Nothing resolves anything, so the declaration is the only edge there is. */
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

/* TestContainer_Close_TheCreationOrderAloneClosesTheDependencyFirst is the positive control for the test above: the same two services, the same resolution order, the declaration removed. It asserts the order the container reaches WITHOUT the declaration, so a reader can see that the probe above is not green for a reason that has nothing to do with the option — and so a change to the tie-break itself fails here rather than quietly making the guard vacuous. */
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

/* TestContainer_Close_ADeclaredTeardownDependencyOnAnUnbuiltServiceIsDropped pins the promise the option's documentation makes about an optional collaborator: the edge names a service that was registered but never resolved, so the teardown walk drops it rather than refusing the walk or wedging the dependent behind a node that will never close. */
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

    /* only the dependent is ever built */
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

type labelledLazyHolder struct {
    label    string
    recorder *closeOrderRecorder
    handle   *LazyService[*closeOrderServiceA]
}

func (instance *labelledLazyHolder) Close() error {
    instance.recorder.record(instance.label)

    return nil
}

/* the in-degree is what makes a shared dependency wait for the LAST of its dependents, and only a fixture where releasing it on the first one changes the answer can tell that apart from the creation-order tie-break. Both holders keep a handle and reach through it after their providers returned, so the shared service is stamped after both of them: released on the first decrement it is the newest node with nothing left pointing at it, and it closes BETWEEN the two holders — the teardown running over a service the second holder is still about to use. The sibling diamond above cannot see this, because there the shared dependency is built during its dependents and is the oldest node either way. */
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

    /* both holders carry the same Go type, so neither files a type node: the stamps below are the name nodes alone */
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

    /* the shared service is built here, after BOTH holders, and each reach records an edge onto it */
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

/* an alias group is as old as its OLDEST member, and only a group that SPANS a third node can tell that from taking the newest. Two registered names answer the same pointer — the ordinary shape of an override installed over a service somebody already holds — and an unrelated service is built between the two filings, so the group runs from the first name to the second with the middle node inside it. Read as old as its first filing the pair closes after the middle; read as new as its second it closes before, tearing a service down ahead of one built later than it. No edge exists anywhere here, which is what keeps this pinned to the collapse and not to the graph. */
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

    /* the same pointer under a second name, filed after the middle service: this is the far end of the group */
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

/* the nodes a cycle leaves behind are still closed, and the order they are closed in is the same latest-first the rest of the teardown uses — the sibling above asserts only that the cycle is NAMED, so the comparator that sorts the remainder can be reversed with the suite green. The two are stamped apart and the edges are installed by hand, which is the only way to build a cycle the container refuses to create on its own. */
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

/* a service that carries the context-taking door is closed through it, with the teardown's own deadline, and its plain Close is not called at all: the two would otherwise both run, or the budget would stop at the container that has it while the component that spends the time never hears of it. */
type contextCloseRecorder struct {
    plainCalls   int
    contextCalls int
    grantedTerm  time.Duration
    hadDeadline  bool
}

func (instance *contextCloseRecorder) Close() error {
    instance.plainCalls = instance.plainCalls + 1

    return nil
}

func (instance *contextCloseRecorder) CloseWithContext(closeContext context.Context) error {
    instance.contextCalls = instance.contextCalls + 1

    deadline, hasDeadline := closeContext.Deadline()
    instance.hadDeadline = hasDeadline
    if true == hasDeadline {
        instance.grantedTerm = time.Until(deadline)
    }

    return nil
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

/* Close with no context reaches the same door with no deadline, so a caller that declared no budget does not have one invented for it. */
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

/* armParallelTeardown reaches the opt-in the way an application does: through a type assertion on the concrete container, because the Container contract declares neither this door nor IsClosed nor CloseWithContext, for the reason written at each of them. */
func armParallelTeardown(t *testing.T, serviceContainer containercontract.Container) {
    t.Helper()

    armable, isArmable := serviceContainer.(interface{ ArmParallelTeardown() error })
    if false == isArmable {
        t.Fatalf("expected the container to carry the parallel teardown door")
    }

    if armErr := armable.ArmParallelTeardown(); nil != armErr {
        t.Fatalf("unexpected arm error: %v", armErr)
    }
}

/* waveMateCloser closes by announcing that it started and then waiting, for a bounded moment, to be told that its wave-mate started too. Serially the first one to close waits the whole moment out and reports it, because the second has not begun; in one wave both announce before either waits. The bound is what keeps the failing arm a failure rather than a hung suite. */
type waveMateCloser struct {
    started      chan struct{}
    mateStarted  <-chan struct{}
    mateDeadline time.Duration
}

func (instance *waveMateCloser) Close() error {
    close(instance.started)

    select {
    case <-instance.mateStarted:
        return nil
    case <-time.After(instance.mateDeadline):
        return errors.New("the wave mate had not started")
    }
}

func registerWaveMatePair(t *testing.T, serviceContainer containercontract.Container, mateDeadline time.Duration) {
    t.Helper()

    firstStarted := make(chan struct{})
    secondStarted := make(chan struct{})

    first := &waveMateCloser{started: firstStarted, mateStarted: secondStarted, mateDeadline: mateDeadline}
    second := &waveMateCloser{started: secondStarted, mateStarted: firstStarted, mateDeadline: mateDeadline}

    if registerErr := serviceContainer.Register(
        "app.wave.first",
        func(resolver containercontract.Resolver) (*waveMateCloser, error) {
            return first, nil
        },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.wave.second",
        func(resolver containercontract.Resolver) (*waveMateCloser, error) {
            return second, nil
        },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := FromResolver[*waveMateCloser](serviceContainer, "app.wave.first"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := FromResolver[*waveMateCloser](serviceContainer, "app.wave.second"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }
}

/* the two services below have no edge between them, so the graph puts them in one wave: armed, both are inside their Close at the same moment and neither has to wait. */
func TestContainer_Close_ArmedTheServicesOfOneWaveCloseAtOnce(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    registerWaveMatePair(t, serviceContainer, time.Second)

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("expected both services of the wave to be closing at the same moment, got: %v", closeErr)
    }
}

/* the sibling that makes the test above mean something: the same pair, the same bound, the door NOT armed. One of the two waits its whole moment out and says so, which is the observation the armed run has to remove. Without this arm, an armed run that closed serially would pass on a bound nobody exercised. */
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

/* a wave is the set of nodes with no relation to one another, so a dependency is one wave past the LAST dependent that frees it — which is what keeps the ordering the whole point of the walk. */
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

/* the waves are read off the same drain that produces the serial order, so a caller that ignores them closes exactly what it closed before, in exactly that order. The order asserted here is the one the walk answered before the waves existed: newest first among the free nodes, the shared dependency last. */
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

/* arming is the moment the application says its teardown graph is complete, so it is the moment a declared edge naming a service nobody registered stops being a tolerated no-op and becomes the ordering that is not there. */
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

/* the sibling that keeps the refusal above from being a refusal of the door's own purpose: a dependency that WAS registered and simply never built is the optional collaborator and the lazy singleton, and it must arm. */
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

/* a declaration keyed by TYPE writes into the same graph the name form writes into, so it orders the teardown the same way — and T and *T name one node, because the container files them under one canonical type. */
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

    /* the DEPENDENT is built first and the dependency second, so the creation stamp alone would close the dependency FIRST — under the service still holding it. Only the declared edge can turn that around, which is what makes this an observation rather than a coincidence. */
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

/* capturingHolder is the shape the reflection walk exists for: a service that HOLDS a collaborator it never resolved, because its provider closed over a value built before it. Nothing about it writes an edge. */
type capturingHolder struct {
    recorder *closeOrderRecorder
    held     *closeOrderServiceB
}

func (instance *capturingHolder) Close() error {
    instance.recorder.record("holder")

    return nil
}

func registerCapturingPair(t *testing.T, serviceContainer containercontract.Container, recorder *closeOrderRecorder) {
    t.Helper()

    /* built before the container, the way a composition root builds what it then publishes: the provider below has nothing to resolve and hands it back */
    captured := &closeOrderServiceB{recorder: recorder}

    if registerErr := serviceContainer.Register(
        "app.storage",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return captured, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.holder",
        func(resolver containercontract.Resolver) (*capturingHolder, error) {
            return &capturingHolder{recorder: recorder, held: captured}, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    /* the holder is built FIRST, so the creation stamp alone closes the storage before it — which is the wrong order, and the accident the walk has to correct */
    if _, getErr := FromResolver[*capturingHolder](serviceContainer, "app.holder"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := FromResolver[*closeOrderServiceB](serviceContainer, "app.storage"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }
}

/* the holder captured its collaborator instead of resolving it, so no edge was ever written and the creation order alone closes the collaborator FIRST — under the holder that is still using it. Armed, the walk sees what the holder holds and the graph gains the edge the provider did not write. */
func TestContainer_Close_ArmedAHeldCollaboratorIsClosedAfterTheServiceHoldingIt(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    registerCapturingPair(t, serviceContainer, recorder)

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 2 != len(closeSequence) {
        t.Fatalf("expected 2 close calls, got %d: %v", len(closeSequence), closeSequence)
    }

    if "holder" != closeSequence[0] || "b" != closeSequence[1] {
        t.Fatalf("expected the held collaborator to be closed after its holder, got %v", closeSequence)
    }
}

/* the sequence above is the property, but on its own it cannot pin the edge: with no edge the two land in ONE wave and are closed at the same moment, so the order recorded is whatever the scheduler chose and the assertion passes about half the time. The wave is what nothing can decide by luck — a dependency is one past its last dependent, or it is beside it. */
func TestContainer_ArmParallelTeardown_AHeldCollaboratorIsOneWavePastTheServiceHoldingIt(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    registerCapturingPair(t, serviceContainer, recorder)

    planned, carriesPlan := serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    })
    if false == carriesPlan {
        t.Fatalf("expected the container to carry the teardown plan door")
    }

    waveByNode := make(map[string]int)
    for _, entry := range planned.TeardownPlan() {
        waveByNode[entry.NodeKey] = entry.WaveIndex
    }

    holderWave, holderPlanned := waveByNode["service:app.holder"]
    storageWave, storagePlanned := waveByNode["service:app.storage"]

    if false == holderPlanned || false == storagePlanned {
        t.Fatalf("expected both services in the teardown plan, got %v", waveByNode)
    }

    if holderWave >= storageWave {
        t.Fatalf("expected the held collaborator at least one wave past its holder, got holder=%d storage=%d", holderWave, storageWave)
    }
}

/* the sibling that makes the test above an observation rather than a coincidence: the same pair, not armed, closes in the order the creation stamp gives — the collaborator first, under the service still holding it. It is the state this repository has shipped, and it is what the walk changes. */
func TestContainer_Close_NotArmedAHeldCollaboratorIsClosedBeforeTheServiceHoldingIt(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    registerCapturingPair(t, serviceContainer, recorder)

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if "b" != closeSequence[0] || "holder" != closeSequence[1] {
        t.Fatalf("expected the unarmed teardown to keep the creation order, got %v", closeSequence)
    }
}

/* a map is counted and never entered, because iterating one another goroutine writes is a fatal error no recover catches. The cost is a real edge the walk cannot see, and it is written down here so the limit is a decision rather than an oversight. */
func TestHeldPointerIdentities_DoesNotEnterAMap(t *testing.T) {
    held := &closeOrderServiceB{}

    throughField := heldPointerIdentities(&capturingHolder{held: held})
    if 0 == len(throughField) {
        t.Fatalf("expected the walk to reach a collaborator held in a field")
    }

    heldIdentity, hasPointer := pointerKeyOf(held)
    if false == hasPointer {
        t.Fatalf("expected the collaborator to carry a pointer identity")
    }

    foundThroughField := false
    for _, identity := range throughField {
        if identity == heldIdentity {
            foundThroughField = true
        }
    }

    if false == foundThroughField {
        t.Fatalf("expected the field walk to find the collaborator")
    }

    throughMap := heldPointerIdentities(map[string]*closeOrderServiceB{"held": held})
    for _, identity := range throughMap {
        if identity == heldIdentity {
            t.Fatalf("expected the walk to leave a map unentered")
        }
    }
}
