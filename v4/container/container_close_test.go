package container

import (
    "context"
    "errors"
    "fmt"
    "reflect"
    "runtime"
    "sort"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"
    "unicode/utf8"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

type closeOrderServiceD struct {
    recorder *closeOrderRecorder
}

func (instance *closeOrderServiceD) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record("d")
    return nil
}

func TestContainer_Close_ClosesDependentsBeforeDependencies_ByServiceName(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

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
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    err = serviceContainer.Register(
        "service.b",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            _, err := resolver.Get(ContextOf(resolver), "service.a")
            if nil != err {
                return nil, err
            }

            return &closeOrderServiceB{recorder: recorder}, nil
        },
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    _, err = serviceContainer.Get(t.Context(), "service.b")
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

/* a pin of the fact the plan's WaveIndex documents, not of a guard: the sequential teardown honours the one edge here and still interleaves the waves, closing a wave-one service before a wave-zero one it has no relation to, because it drains creation latest-first among what is free */
func TestContainer_TheSequentialCloseOrderInterleavesTheWaves(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{TeardownSequential: true})

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 3)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    serviceContainer.MustRegister(
        "service.b",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{recorder: recorder}, nil
        },
        RegisterOptions{},
    )
    serviceContainer.MustRegister(
        "service.a",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{recorder: recorder}, nil
        },
        RegisterOptions{},
    )
    serviceContainer.MustRegister(
        "service.c",
        func(resolver containercontract.Resolver) (*closeOrderServiceC, error) {
            if _, resolveErr := resolver.Get(ContextOf(resolver), "service.a"); nil != resolveErr {
                return nil, resolveErr
            }

            return &closeOrderServiceC{recorder: recorder}, nil
        },
        RegisterOptions{},
    )

    MustFromResolver[*closeOrderServiceB](t.Context(), serviceContainer, "service.b")
    MustFromResolver[*closeOrderServiceC](t.Context(), serviceContainer, "service.c")

    expectedWaves := map[string]int{"service:service.c": 0, "service:service.a": 1, "service:service.b": 0}
    for _, entry := range serviceContainer.(teardownPlanner).TeardownPlan() {
        if expectedWave, planned := expectedWaves[entry.NodeKey]; false == planned || expectedWave != entry.WaveIndex {
            t.Fatalf("expected %s in wave %d, got wave %d", entry.NodeKey, expectedWave, entry.WaveIndex)
        }
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if "[c a b]" != fmt.Sprint(closeSequence) {
        t.Fatalf("expected the sequential teardown to close c, a, b — the wave-one a before the wave-zero b — got %v", closeSequence)
    }
}

type closeOrderTypeDependency struct {
    recorder *closeOrderRecorder
}

func (instance *closeOrderTypeDependency) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record("dep")
    return nil
}

type closeOrderTypeDependent struct {
    recorder *closeOrderRecorder
}

func (instance *closeOrderTypeDependent) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record("dependent")
    return nil
}

func TestContainer_Close_ClosesDependentsBeforeDependencies_ByTypeResolution(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

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
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register type error: %v", err)
    }

    err = RegisterType[*closeOrderTypeDependent](
        serviceContainer,
        func(resolver containercontract.Resolver) (*closeOrderTypeDependent, error) {
            _, err := resolver.GetByType(ContextOf(resolver), reflect.TypeOf((*closeOrderTypeDependency)(nil)))
            if nil != err {
                return nil, err
            }

            return &closeOrderTypeDependent{recorder: recorder}, nil
        },
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register type error: %v", err)
    }

    _, err = serviceContainer.GetByType(t.Context(), reflect.TypeOf((*closeOrderTypeDependent)(nil)))
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

func (instance valueCloser) CloseWithContext(closeContext context.Context) error {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    *instance.counter++

    return nil
}

func TestContainer_Close_ValueTypeServiceClosedOnce(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    var lock sync.Mutex
    count := 0

    MustRegister[valueCloser](
        serviceContainer,
        "value.closer",
        func(resolver containercontract.Resolver) (valueCloser, error) {
            return valueCloser{counter: &count, lock: &lock}, nil
        },
        RegisterOptions{},
    )

    _ = MustFromResolver[valueCloser](t.Context(), serviceContainer, "value.closer")
    _ = MustFromResolverByType[valueCloser](t.Context(), serviceContainer)

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

func (instance unhashableValueCloser) CloseWithContext(closeContext context.Context) error {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    *instance.counter++

    return nil
}

func TestContainer_Close_ValueTypeServiceWithUnhashableContentDoesNotPanicAndClosesOnce(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    var lock sync.Mutex
    count := 0

    MustRegister[unhashableValueCloser](
        serviceContainer,
        "unhashable.value.closer",
        func(resolver containercontract.Resolver) (unhashableValueCloser, error) {
            return unhashableValueCloser{counter: &count, lock: &lock, payload: []int{1, 2, 3}}, nil
        },
        RegisterOptions{},
    )

    _ = MustFromResolver[unhashableValueCloser](t.Context(), serviceContainer, "unhashable.value.closer")
    _ = MustFromResolverByType[unhashableValueCloser](t.Context(), serviceContainer)

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

func (instance nonComparableValueCloser) CloseWithContext(closeContext context.Context) error {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    *instance.counter++

    return nil
}

func TestContainer_Close_NonComparableValueTypeServiceClosedOnce(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    var lock sync.Mutex
    count := 0

    MustRegister[nonComparableValueCloser](
        serviceContainer,
        "non.comparable.value.closer",
        func(resolver containercontract.Resolver) (nonComparableValueCloser, error) {
            return nonComparableValueCloser{counter: &count, lock: &lock, tags: []string{"a", "b"}}, nil
        },
        RegisterOptions{},
    )

    _ = MustFromResolver[nonComparableValueCloser](t.Context(), serviceContainer, "non.comparable.value.closer")
    _ = MustFromResolverByType[nonComparableValueCloser](t.Context(), serviceContainer)

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
    serviceContainer := NewContainer(ContainerConfig{})

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
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    err = Register(
        serviceContainer,
        "service.a",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            _, dependencyErr := FromResolverByType[*closeOrderServiceB](ContextOf(resolver), resolver)
            if nil != dependencyErr {
                return nil, dependencyErr
            }

            return &closeOrderServiceA{recorder: recorder}, nil
        },
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    _, err = serviceContainer.Get(t.Context(), "service.a")
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
    serviceContainer := NewContainer(ContainerConfig{})

    err := serviceContainer.Register(
        "service.a",
        func(resolver containercontract.Resolver) (*circularServiceA, error) {
            _, err := resolver.Get(ContextOf(resolver), "service.b")
            if nil != err {
                return nil, err
            }

            return &circularServiceA{}, nil
        },
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    err = serviceContainer.Register(
        "service.b",
        func(resolver containercontract.Resolver) (*circularServiceB, error) {
            _, err := resolver.Get(ContextOf(resolver), "service.a")
            if nil != err {
                return nil, err
            }

            return &circularServiceB{}, nil
        },
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    _, err = serviceContainer.Get(t.Context(), "service.a")
    if false == errors.Is(err, ErrCircularDependency) {
        t.Fatalf("expected the one-chain ring refused with ErrCircularDependency, got %v", err)
    }
}

func TestContainer_Close_ClosesDiamondDependencyInDeterministicOrder(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{TeardownSequential: true})

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
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    err = serviceContainer.Register(
        "service.b",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            _, err := resolver.Get(ContextOf(resolver), "service.d")
            if nil != err {
                return nil, err
            }

            return &closeOrderServiceB{recorder: recorder}, nil
        },
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    err = serviceContainer.Register(
        "service.c",
        func(resolver containercontract.Resolver) (*closeOrderServiceC, error) {
            _, err := resolver.Get(ContextOf(resolver), "service.d")
            if nil != err {
                return nil, err
            }

            return &closeOrderServiceC{recorder: recorder}, nil
        },
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    err = serviceContainer.Register(
        "service.a",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            _, err := resolver.Get(ContextOf(resolver), "service.b")
            if nil != err {
                return nil, err
            }

            _, err = resolver.Get(ContextOf(resolver), "service.c")
            if nil != err {
                return nil, err
            }

            return &closeOrderServiceA{recorder: recorder}, nil
        },
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    _, err = serviceContainer.Get(t.Context(), "service.a")
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

/* OverrideProtectedInstance on a value service registered with TypeRegistrationDisabled must close once */

type overrideValueCloser struct {
    counter *int
    lock    *sync.Mutex
    tags    []string
}

func (instance overrideValueCloser) CloseWithContext(closeContext context.Context) error {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    *instance.counter++

    return nil
}

func TestContainer_Close_OverrideProtectedInstanceWithTypeRegistrationDisabledClosesOnce(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    var lock sync.Mutex
    count := 0

    MustRegister[overrideValueCloser](
        serviceContainer,
        "override.no.type.value.closer",
        func(resolver containercontract.Resolver) (overrideValueCloser, error) {
            return overrideValueCloser{counter: &count, lock: &lock, tags: []string{"a", "b"}}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    )

    if overrideErr := serviceContainer.OverrideProtectedInstance(
        "override.no.type.value.closer",
        overrideValueCloser{counter: &count, lock: &lock, tags: []string{"x", "y"}},
        OverrideOptions{},
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
    serviceContainer := NewContainer(ContainerConfig{})

    releaseClose := make(chan struct{})
    closeObserved := make(chan struct{}, 1)

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*blockingCloser, error) {
        return &blockingCloser{release: releaseClose, observed: closeObserved}, nil
    }, RegisterOptions{})

    _, getErr := FromResolverByType[*blockingCloser](t.Context(), serviceContainer)
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

func (instance *blockingCloser) CloseWithContext(closeContext context.Context) error {
    instance.observed <- struct{}{}
    <-instance.release

    return errBlockingCloserFailed
}

type panickingCloseService struct{}

func (instance *panickingCloseService) CloseWithContext(closeContext context.Context) error {
    panic("teardown exploded")
}

/* a service whose Close panics must not abort the teardown: the remaining services still close, the panic is recorded as a close failure, and a repeated Close reports the same error instead of a silent success */
func TestContainer_Close_PanickingServiceCloseIsRecordedAndSiblingsStillClose(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

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
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    err = serviceContainer.Register(
        "service.a.sibling",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{recorder: recorder}, nil
        },
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "service.z.panics"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "service.a.sibling"); nil != getErr {
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

func (instance *panickingErrorMessageService) CloseWithContext(closeContext context.Context) error {
    return panickingErrorMessage{}
}

/* a user error whose Error() panics must not escape the teardown: the failure is recorded with a deterministic message and a repeated Close reports the same error instead of a silent success */
func TestContainer_Close_PanickingErrorMessageIsContainedAndRecorded(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    err := serviceContainer.Register(
        "service.panicking.message",
        func(resolver containercontract.Resolver) (*panickingErrorMessageService, error) {
            return &panickingErrorMessageService{}, nil
        },
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "service.panicking.message"); nil != getErr {
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

func (instance *zeroSizeCloserOne) CloseWithContext(closeContext context.Context) error {
    zeroSizeCloserOneClosed = true
    return nil
}

type zeroSizeCloserTwo struct{}

func (instance *zeroSizeCloserTwo) CloseWithContext(closeContext context.Context) error {
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

    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister[*zeroSizeCloserOne](
        serviceContainer,
        "zero.size.one",
        func(resolver containercontract.Resolver) (*zeroSizeCloserOne, error) {
            return &zeroSizeCloserOne{}, nil
        },
        RegisterOptions{},
    )

    MustRegister[*zeroSizeCloserTwo](
        serviceContainer,
        "zero.size.two",
        func(resolver containercontract.Resolver) (*zeroSizeCloserTwo, error) {
            return &zeroSizeCloserTwo{}, nil
        },
        RegisterOptions{},
    )

    _ = MustFromResolver[*zeroSizeCloserOne](t.Context(), serviceContainer, "zero.size.one")
    _ = MustFromResolver[*zeroSizeCloserTwo](t.Context(), serviceContainer, "zero.size.two")

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

func (instance *firstFieldOuterCloser) CloseWithContext(closeContext context.Context) error {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    *instance.counter++

    return nil
}

type firstFieldInnerCloser struct {
    counter *int
    lock    *sync.Mutex
}

func (instance *firstFieldInnerCloser) CloseWithContext(closeContext context.Context) error {
    instance.lock.Lock()
    defer instance.lock.Unlock()

    *instance.counter += 10

    return nil
}

func TestContainer_Close_FirstFieldPointerAliasEachClose(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

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
        RegisterOptions{},
    )

    MustRegister[*firstFieldInnerCloser](
        serviceContainer,
        "first.field.inner",
        func(resolver containercontract.Resolver) (*firstFieldInnerCloser, error) {
            return &outer.inner, nil
        },
        RegisterOptions{},
    )

    _ = MustFromResolver[*firstFieldOuterCloser](t.Context(), serviceContainer, "first.field.outer")
    _ = MustFromResolver[*firstFieldInnerCloser](t.Context(), serviceContainer, "first.field.inner")

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

/* atomic: the two services share no edge, so the default teardown closes them in one wave, concurrently */
var sameTypeZeroSizeCloseCount atomic.Int64

func (instance *sameTypeZeroSizeCloser) CloseWithContext(closeContext context.Context) error {
    sameTypeZeroSizeCloseCount.Add(1)

    return nil
}

/* two distinct services of one zero-size type share an address, so pairing the address with the type is not enough to tell them apart; without the dependency-graph qualifier the second one was collapsed onto the first and never closed */
func TestContainer_Close_DistinctServicesOfOneZeroSizeTypeEachClose(t *testing.T) {
    sameTypeZeroSizeCloseCount.Store(0)

    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister[*sameTypeZeroSizeCloser](
        serviceContainer,
        "same.type.zero.size.first",
        func(resolver containercontract.Resolver) (*sameTypeZeroSizeCloser, error) {
            return &sameTypeZeroSizeCloser{}, nil
        },
        RegisterOptions{},
    )

    MustRegister[*sameTypeZeroSizeCloser](
        serviceContainer,
        "same.type.zero.size.second",
        func(resolver containercontract.Resolver) (*sameTypeZeroSizeCloser, error) {
            return &sameTypeZeroSizeCloser{}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    )

    _ = MustFromResolver[*sameTypeZeroSizeCloser](t.Context(), serviceContainer, "same.type.zero.size.first")
    _ = MustFromResolver[*sameTypeZeroSizeCloser](t.Context(), serviceContainer, "same.type.zero.size.second")

    if err := serviceContainer.Close(); nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    if 2 != sameTypeZeroSizeCloseCount.Load() {
        t.Fatalf("expected both distinct zero-size services to be closed, got %d", sameTypeZeroSizeCloseCount.Load())
    }
}

/* the mirror case: a service whose provider hands back what it resolved from another service holds the same instance, and the dependency edge proves it, so the shared instance is still closed exactly once */
func TestContainer_Close_ResolverMediatedZeroSizeAliasClosesOnce(t *testing.T) {
    sameTypeZeroSizeCloseCount.Store(0)

    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister[*sameTypeZeroSizeCloser](
        serviceContainer,
        "zero.size.origin",
        func(resolver containercontract.Resolver) (*sameTypeZeroSizeCloser, error) {
            return &sameTypeZeroSizeCloser{}, nil
        },
        RegisterOptions{},
    )

    MustRegister[*sameTypeZeroSizeCloser](
        serviceContainer,
        "zero.size.alias",
        func(resolver containercontract.Resolver) (*sameTypeZeroSizeCloser, error) {
            return FromResolver[*sameTypeZeroSizeCloser](ContextOf(resolver), resolver, "zero.size.origin")
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    )

    _ = MustFromResolver[*sameTypeZeroSizeCloser](t.Context(), serviceContainer, "zero.size.alias")

    if err := serviceContainer.Close(); nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    if 1 != sameTypeZeroSizeCloseCount.Load() {
        t.Fatalf("expected the shared instance to be closed exactly once, got %d", sameTypeZeroSizeCloseCount.Load())
    }
}

type failingCloseService struct{}

func (instance *failingCloseService) CloseWithContext(closeContext context.Context) error {
    return errors.New("refusing to close")
}

type replacedBuiltProbe struct {
    label    string
    recorder *closeOrderRecorder
}

func (instance *replacedBuiltProbe) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record(instance.label)

    return nil
}

func TestContainer_Close_ReplacedBuiltInstanceIsClosed(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

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
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.replaced.built"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    overrideErr := serviceContainer.OverrideProtectedInstance(
        "app.replaced.built",
        &replacedBuiltProbe{label: "override", recorder: recorder},
        OverrideOptions{},
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

func (instance *replacedFailingProbe) CloseWithContext(closeContext context.Context) error {
    return instance.failure
}

type secondReplacedFailingProbe struct {
    failure error
}

func (instance *secondReplacedFailingProbe) CloseWithContext(closeContext context.Context) error {
    return instance.failure
}

/* two replaced built instances whose closes both fail are both recorded: the graveyard entries carry no node key of their own, so a shared constant key let the second failure overwrite the first's record, naming one failure where two happened */
func TestContainer_Close_TwoFailingReplacedInstancesAreBothRecorded(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerFirstErr := serviceContainer.Register(
        "app.replaced.failing.first",
        func(resolver containercontract.Resolver) (*replacedFailingProbe, error) {
            return &replacedFailingProbe{failure: errors.New("refusing to close app.replaced.failing.first")}, nil
        },
        RegisterOptions{},
    )
    if nil != registerFirstErr {
        t.Fatalf("unexpected register error: %v", registerFirstErr)
    }

    registerSecondErr := serviceContainer.Register(
        "app.replaced.failing.second",
        func(resolver containercontract.Resolver) (*secondReplacedFailingProbe, error) {
            return &secondReplacedFailingProbe{failure: errors.New("refusing to close app.replaced.failing.second")}, nil
        },
        RegisterOptions{},
    )
    if nil != registerSecondErr {
        t.Fatalf("unexpected register error: %v", registerSecondErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.replaced.failing.first"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.replaced.failing.second"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    if overrideErr := serviceContainer.OverrideProtectedInstance("app.replaced.failing.first", &replacedFailingProbe{}, OverrideOptions{}); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if overrideErr := serviceContainer.OverrideProtectedInstance("app.replaced.failing.second", &secondReplacedFailingProbe{}, OverrideOptions{}); nil != overrideErr {
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
    serviceContainer := NewContainer(ContainerConfig{})

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
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    firstOverrideErr := serviceContainer.OverrideProtectedInstance(
        "app.replaced.override",
        &replacedBuiltProbe{label: "first-override", recorder: recorder},
        OverrideOptions{},
    )
    if nil != firstOverrideErr {
        t.Fatalf("unexpected override error: %v", firstOverrideErr)
    }

    secondOverrideErr := serviceContainer.OverrideProtectedInstance(
        "app.replaced.override",
        &replacedBuiltProbe{label: "second-override", recorder: recorder},
        OverrideOptions{},
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
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    registerFirstErr := serviceContainer.Register(
        "app.cycle.first",
        func(resolver containercontract.Resolver) (*failingCloseService, error) {
            return &failingCloseService{}, nil
        },
        RegisterOptions{},
    )
    if nil != registerFirstErr {
        t.Fatalf("unexpected register error: %v", registerFirstErr)
    }

    registerSecondErr := serviceContainer.Register(
        "app.cycle.second",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{recorder: &closeOrderRecorder{mutex: &sync.Mutex{}, closeSequence: &[]string{}}}, nil
        },
        RegisterOptions{},
    )
    if nil != registerSecondErr {
        t.Fatalf("unexpected register error: %v", registerSecondErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.cycle.first"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }
    if _, getErr := serviceContainer.Get(t.Context(), "app.cycle.second"); nil != getErr {
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
    containerInstance := NewContainer(ContainerConfig{})

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

func (instance *lazyHoldingService) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record("holder")
    return nil
}

func TestContainer_Close_ClosesAHolderBeforeTheServiceItResolvedThroughAKeptResolver(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

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
        RegisterOptions{},
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
        RegisterOptions{},
    ); nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    holder, err := FromResolver[*lazyHoldingService](t.Context(), serviceContainer, "app.holder")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    /* the dependency is built before the handle ever asks for it, which is the ordinary case and the one that tells the read path from the write path: a resolution that finds the instance already there still has an edge to record */
    if _, err := serviceContainer.Get(t.Context(), "service.a"); nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    /* the first use, on a path that runs long after the provider returned */
    if nil == holder.handle.Get(t.Context()) {
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

type panickingCloseWithTextService struct{}

func (instance *panickingCloseWithTextService) CloseWithContext(closeContext context.Context) error {
    panic("the drain buffer was nil")
}

/* TestContainer_Close_APanickingCloseCarriesItsCauseAndItsStack pins what an operator learns from the one boundary that contains a teardown panic: nothing above it sees the panic and nothing below it survives, so whatever the recorded failure drops is gone. An error-shaped panic value kept only as its stringified message collapsed to one line at the render boundary, taking the context map and the cause chain of the very error the Close raised with it, and the frames that ran existed only inside the recover. */
func TestContainer_Close_APanickingCloseCarriesItsCauseAndItsStack(t *testing.T) {
    panicCause := exception.NewError(
        "the socket was already gone",
        map[string]any{"peer": "10.0.0.7"},
        errors.New("connection reset by peer"),
    )

    closeErr := closeServiceValueWithin(t.Context(), &panickingCloseWithCauseService{cause: panicCause})

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

/* the failure map holds one line per service, and a line is where the recovered value, the frames that ran and the cause of a contained panic collapsed; the details map beside it carries them whole, under the same key, only when a close left any */
func TestContainer_Close_CarriesTheFailureDetailsOfAPanickingCloseBesideItsLine(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "service.panics",
        func(resolver containercontract.Resolver) (*panickingCloseWithCauseService, error) {
            return &panickingCloseWithCauseService{cause: errors.New("the drain buffer was nil")}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "service.panics"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    closeErr := serviceContainer.Close()
    if nil == closeErr {
        t.Fatalf("expected the panicking close to be reported")
    }

    var typedError *exception.Error
    if false == errors.As(closeErr, &typedError) {
        t.Fatalf("expected a melody error, got %T", closeErr)
    }

    assertCloseFailureDetails(t, typedError.Context(), "service:service.panics")
}

func TestContainer_Close_AFailureThatSaysNothingBeyondItsLineAddsNoDetailsMap(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "service.refuses",
        func(resolver containercontract.Resolver) (*failingCloseService, error) {
            return &failingCloseService{}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "service.refuses"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    closeErr := serviceContainer.Close()

    var typedError *exception.Error
    if false == errors.As(closeErr, &typedError) {
        t.Fatalf("expected a melody error, got %T", closeErr)
    }

    failures, hasFailures := typedError.Context()["failures"].(map[string]string)
    if false == hasFailures || "refusing to close" != failures["service:service.refuses"] {
        t.Fatalf("expected the failure line, got %v", typedError.Context()["failures"])
    }

    if _, hasDetails := typedError.Context()["failureDetails"]; true == hasDetails {
        t.Fatalf("expected no details map beside a line that already says everything, got %v", typedError.Context()["failureDetails"])
    }
}

/* the drop of the duplicate entry is ONE key — the one LogContext seeds with the error's own message —
   and only when it holds the failure line verbatim. Swept by VALUE across the whole key space, it took a
   producer's own keys with it whenever their value happened to be that message, which is exactly the
   shape a refusal written for an operator has: the line, and the same line filed under the name of the
   thing to do about it. */
func TestContainer_Close_TheDuplicateDropTakesOnlyTheSeededMessage(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "service.refuses",
        func(resolver containercontract.Resolver) (*operatorFailingCloseService, error) {
            return &operatorFailingCloseService{}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "service.refuses"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    var typedError *exception.Error
    if false == errors.As(serviceContainer.Close(), &typedError) {
        t.Fatalf("expected a melody error")
    }

    failureDetails, hasDetails := typedError.Context()["failureDetails"].(map[string]exceptioncontract.Context)
    if false == hasDetails {
        t.Fatalf("expected the failure details beside the failure map, got %v", typedError.Context()["failureDetails"])
    }

    details := failureDetails["service:service.refuses"]

    if _, seeded := details["error"]; true == seeded {
        t.Fatalf("expected the seeded message to be dropped as the duplicate of the line, got %v", details)
    }

    if "the pool is still draining" != details["operator"] {
        t.Fatalf("expected the producer's own key to survive the drop, got %v", details)
    }

    if "db-1" != details["host"] {
        t.Fatalf("expected the producer's other key to survive, got %v", details)
    }
}

type operatorFailingCloseService struct{}

func (instance *operatorFailingCloseService) CloseWithContext(closeContext context.Context) error {
    return exception.NewError(
        "the pool is still draining",
        exceptioncontract.Context{"host": "db-1", "operator": "the pool is still draining"},
        nil,
    )
}

/* the recovered value is bounded for the reason the frames are: it is the panic value as the SERVICE
   wrote it, so twenty services panicking with five kilobytes apiece put a hundred into the one record
   the stack limit exists to keep out of it. */
func TestContainer_Close_TheRecoveredValueOfAPanickingCloseIsBounded(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "service.panics",
        func(resolver containercontract.Resolver) (*hugePanicCloseService, error) {
            return &hugePanicCloseService{}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "service.panics"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    var typedError *exception.Error
    if false == errors.As(serviceContainer.Close(), &typedError) {
        t.Fatalf("expected a melody error")
    }

    failureDetails, hasDetails := typedError.Context()["failureDetails"].(map[string]exceptioncontract.Context)
    if false == hasDetails {
        t.Fatalf("expected the failure details, got %v", typedError.Context()["failureDetails"])
    }

    recoveredValue, isText := failureDetails["service:service.panics"]["recoveredValue"].(string)
    if false == isText {
        t.Fatalf("expected the recovered value to be recorded as text, got %v", failureDetails["service:service.panics"])
    }

    if 5000 <= len(recoveredValue) {
        t.Fatalf("expected the recovered value to be cut, it is %d bytes", len(recoveredValue))
    }

    if false == strings.Contains(recoveredValue, "bytes kept") {
        t.Fatalf("expected the cut to name itself, got %q", recoveredValue[:64])
    }
}

type hugePanicCloseService struct{}

func (instance *hugePanicCloseService) CloseWithContext(closeContext context.Context) error {
    panic(strings.Repeat("H", 5000))
}

/* the frames of one contained panic are bounded: the teardown error is ONE record the application journals, and an unbounded stack per failed node made a shutdown that loses twenty services write sixty kilobytes of it in a single line. The cut names itself and keeps the top frames, which are the ones that name the close. */
func TestContainer_Close_TheFramesOfAPanickingCloseAreBounded(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "service.panics",
        func(resolver containercontract.Resolver) (*deeplyPanickingCloseService, error) {
            return &deeplyPanickingCloseService{depth: 60}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "service.panics"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    var typedError *exception.Error
    if false == errors.As(serviceContainer.Close(), &typedError) {
        t.Fatalf("expected a melody error")
    }

    failureDetails, _ := typedError.Context()["failureDetails"].(map[string]exceptioncontract.Context)
    panicStack, hasStack := failureDetails["service:service.panics"]["panicStack"].(string)
    if false == hasStack {
        t.Fatalf("expected the frames that ran, got %v", failureDetails)
    }

    if false == strings.Contains(panicStack, "bytes kept") {
        t.Fatalf("expected a stack this deep to be cut and to say so, got %d bytes", len(panicStack))
    }

    if closeFailureStackLimit+128 < len(panicStack) {
        t.Fatalf("expected the cut to bound the frames near the limit, got %d bytes", len(panicStack))
    }
}

func TestContainer_Close_ACloseErrorWhoseUnwrapPanicsDoesNotEndTheTeardown(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "service.unwrapPanics",
        func(resolver containercontract.Resolver) (*panickingUnwrapCloseService, error) {
            return &panickingUnwrapCloseService{}, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "service.unwrapPanics"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    var typedError *exception.Error
    if false == errors.As(serviceContainer.Close(), &typedError) {
        t.Fatalf("expected the failed close to be reported rather than ending the process")
    }

    failures, _ := typedError.Context()["failures"].(map[string]string)
    if "the drain refused" != failures["service:service.unwrapPanics"] {
        t.Fatalf("expected the failure line to survive, got %v", typedError.Context()["failures"])
    }

    failureDetails, _ := typedError.Context()["failureDetails"].(map[string]exceptioncontract.Context)
    if cause := failureDetails["service:service.unwrapPanics"]["cause"]; "the links below could not be read, their Unwrap panicked: unwrap of a half-built error" != cause {
        t.Fatalf("expected the details to say where the chain was cut and why, got %v", failureDetails)
    }
}

type deeplyPanickingCloseService struct {
    depth int
}

func (instance *deeplyPanickingCloseService) CloseWithContext(closeContext context.Context) error {
    instance.panicAtDepth(instance.depth)

    return nil
}

func (instance *deeplyPanickingCloseService) panicAtDepth(remaining int) {
    if 0 >= remaining {
        panic(errors.New("the drain buffer was nil"))
    }

    instance.panicAtDepth(remaining - 1)
}

type panickingUnwrapError struct{}

func (instance panickingUnwrapError) Error() string { return "the drain refused" }

func (instance panickingUnwrapError) Unwrap() error { panic("unwrap of a half-built error") }

/* a panic value that is not an error has no cause to give, and the record still carries what it can */
func TestContainer_Close_APanickingCloseWithoutAnErrorValueStillRecordsTheStack(t *testing.T) {
    closeErr := closeServiceValueWithin(t.Context(), &panickingCloseWithTextService{})

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
        serviceContainer := NewContainer(ContainerConfig{TeardownSequential: true})

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
            RegisterOptions{},
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }

        if registerErr := serviceContainer.Register(
            dependentName,
            func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
                return &closeOrderServiceB{recorder: recorder}, nil
            },
            RegisterOptions{},
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }

        /* the shared service is resolved FIRST and no edge is ever declared: this is the whole shape of a logger resolved at boot and used by everything through its own door afterwards */
        if _, getErr := serviceContainer.Get(t.Context(), "service.mmm.shared"); nil != getErr {
            t.Fatalf("unexpected get error: %v", getErr)
        }

        if _, getErr := serviceContainer.Get(t.Context(), dependentName); nil != getErr {
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
    serviceContainer := NewContainer(ContainerConfig{})

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
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "service.dependent",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            if _, resolveErr := resolver.Get(ContextOf(resolver), "service.dependency"); nil != resolveErr {
                return nil, resolveErr
            }

            return &closeOrderServiceB{recorder: recorder}, nil
        },
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    /* the dependent is resolved first, but its dependency is built INSIDE that provider and finishes its own filing before it returns, so the dependency is the OLDER node: the edge and the creation-order tie-break agree here */
    if _, getErr := serviceContainer.Get(t.Context(), "service.dependent"); nil != getErr {
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

func (instance *closeTimeResolvingService) CloseWithContext(closeContext context.Context) error {
    instance.resolvedValue, instance.resolveErr = instance.serviceContainer.Get(context.Background(), "service.mmm.shared")

    return nil
}

func TestContainer_Close_AServiceStillResolvesDuringTheTeardown(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

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
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    resolvingService := &closeTimeResolvingService{serviceContainer: serviceContainer}

    if registerErr := serviceContainer.Register(
        "service.zzz.worker",
        func(resolver containercontract.Resolver) (*closeTimeResolvingService, error) {
            return resolvingService, nil
        },
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "service.mmm.shared"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "service.zzz.worker"); nil != getErr {
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

/* TestContainer_Get_RefusesAfterTheTeardownFinished pins the second closing state. A resolution performed once the teardown is over is refused, since answering it out of the maps the teardown has just emptied of meaning would hand a caller holding a resolver a service every Close in the process has already run on, with a nil error. The fast path is asked separately because a memoized instance never reaches the creation guard that refuses a closed container. */
func TestContainer_Get_RefusesAfterTheTeardownFinished(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    closed := false

    if registerErr := serviceContainer.Register(
        "service.probe",
        func(resolver containercontract.Resolver) (*probeClosableService, error) {
            return &probeClosableService{closed: &closed}, nil
        },
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "service.probe"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    lateValue, lateErr := serviceContainer.Get(t.Context(), "service.probe")

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

func TestContainer_GetByType_RefusesAfterTheTeardownFinished(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    closed := false

    if registerErr := serviceContainer.Register(
        "service.probe",
        func(resolver containercontract.Resolver) (*probeClosableService, error) {
            return &probeClosableService{closed: &closed}, nil
        },
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    probeType := reflect.TypeOf((*probeClosableService)(nil))

    if _, getErr := serviceContainer.GetByType(t.Context(), probeType); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    lateValue, lateErr := serviceContainer.GetByType(t.Context(), probeType)

    if nil == lateErr || false == strings.Contains(lateErr.Error(), "container is closed") {
        t.Fatalf("expected the closed-container refusal, got %v and %v", lateValue, lateErr)
    }

    if nil != lateValue {
        t.Fatalf("expected no value beside the refusal, got %v", lateValue)
    }
}

type probeClosableService struct {
    closed *bool
}

func (instance *probeClosableService) CloseWithContext(closeContext context.Context) error {
    *instance.closed = true

    return nil
}

/* the refusal covers the resolution paths the fast path never sees: a resolution made through a SCOPE skips the memoized read entirely and reaches the creation guard, which is the second door the same closing state has to be asked at */
func TestScope_Get_RefusesAfterTheContainerTeardownFinished(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    closed := false

    if registerErr := serviceContainer.Register(
        "service.probe",
        func(resolver containercontract.Resolver) (*probeClosableService, error) {
            return &probeClosableService{closed: &closed}, nil
        },
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get(t.Context(), "service.probe"); nil != getErr {
        t.Fatalf("unexpected warm get error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    lateValue, lateErr := scopeInstance.Get(t.Context(), "service.probe")

    if nil == lateErr {
        t.Fatalf("expected the resolution through the scope to be refused, got %v", lateValue)
    }

    if false == strings.Contains(lateErr.Error(), "container is closed") {
        t.Fatalf("expected the closed-container refusal, got %v", lateErr)
    }
}

/* the built mark leaves the maps together with the instance it marks: the first override moves the BUILT instance to the graveyard and takes the mark down, so the second override evicts a value that is its installer's, not the container's. With the mark left standing, the first override was graveyarded as if the container had built it, and closed under the feet of whoever installed it. */
func TestContainer_Close_AnEvictedOverrideAfterABuiltInstanceIsNotClosed(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

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
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.built.then.replaced"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    firstOverrideErr := serviceContainer.OverrideProtectedInstance(
        "app.built.then.replaced",
        &replacedBuiltProbe{label: "first-override", recorder: recorder},
        OverrideOptions{},
    )
    if nil != firstOverrideErr {
        t.Fatalf("unexpected override error: %v", firstOverrideErr)
    }

    secondOverrideErr := serviceContainer.OverrideProtectedInstance(
        "app.built.then.replaced",
        &replacedBuiltProbe{label: "second-override", recorder: recorder},
        OverrideOptions{},
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
    serviceContainer := NewContainer(ContainerConfig{})

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
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "service.dependent",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{recorder: recorder}, nil
        },
        RegisterOptions{TeardownDependencyNames: []string{"service.dependency"}},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    /* the dependent is built FIRST and the dependency SECOND, so the creation-order tie-break alone would close the dependency first — the wrong way round. Nothing resolves anything, so the declaration is the only edge there is. */
    if _, getErr := serviceContainer.Get(t.Context(), "service.dependent"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "service.dependency"); nil != getErr {
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
    serviceContainer := NewContainer(ContainerConfig{TeardownSequential: true})

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
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "service.dependent",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{recorder: recorder}, nil
        },
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "service.dependent"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "service.dependency"); nil != getErr {
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
    serviceContainer := NewContainer(ContainerConfig{})

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
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "service.dependent",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{recorder: recorder}, nil
        },
        RegisterOptions{TeardownDependencyNames: []string{"service.dependency"}},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    /* only the dependent is ever built */
    if _, getErr := serviceContainer.Get(t.Context(), "service.dependent"); nil != getErr {
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

func (instance *labelledLazyHolder) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record(instance.label)

    return nil
}

/* the in-degree is what makes a shared dependency wait for the LAST of its dependents, and only a fixture where releasing it on the first one changes the answer can tell that apart from the creation-order tie-break. Both holders keep a handle and reach through it after their providers returned, so the shared service is stamped after both of them: released on the first decrement it is the newest node with nothing left pointing at it, and it closes BETWEEN the two holders — the teardown running over a service the second holder is still about to use. The sibling diamond above cannot see this, because there the shared dependency is built during its dependents and is the oldest node either way. */
func TestContainer_Close_ASharedDependencyWaitsForEveryDependent(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{TeardownSequential: true})

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
        RegisterOptions{},
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
        RegisterOptions{TypeRegistrationDisabled: true},
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
        RegisterOptions{TypeRegistrationDisabled: true},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    firstHolder, firstErr := FromResolver[*labelledLazyHolder](t.Context(), serviceContainer, "app.holder.first")
    if nil != firstErr {
        t.Fatalf("unexpected get error: %v", firstErr)
    }

    secondHolder, secondErr := FromResolver[*labelledLazyHolder](t.Context(), serviceContainer, "app.holder.second")
    if nil != secondErr {
        t.Fatalf("unexpected get error: %v", secondErr)
    }

    /* the shared service is built here, after BOTH holders, and each reach records an edge onto it */
    if nil == firstHolder.handle.Get(t.Context()) {
        t.Fatalf("expected the first handle to resolve the shared service")
    }

    if nil == secondHolder.handle.Get(t.Context()) {
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
    serviceContainer := NewContainer(ContainerConfig{TeardownSequential: true})

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
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.middle",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{recorder: recorder}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.zzz.second",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{recorder: recorder}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.aaa.first"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.middle"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    /* the same pointer under a second name, filed after the middle service: this is the far end of the group */
    if overrideErr := serviceContainer.OverrideInstance("app.zzz.second", shared, OverrideOptions{}); nil != overrideErr {
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
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

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
        RegisterOptions{TypeRegistrationDisabled: true},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.cycle.late",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{recorder: recorder}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.cycle.early"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.cycle.late"); nil != getErr {
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
    serviceContainer := NewContainer(ContainerConfig{})

    recorder := &contextCloseRecorder{}

    registerErr := serviceContainer.Register(
        "service.context-closeable",
        func(resolver containercontract.Resolver) (*contextCloseRecorder, error) {
            return recorder, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "service.context-closeable"); nil != getErr {
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
    serviceContainer := NewContainer(ContainerConfig{})

    recorder := &contextCloseRecorder{}

    registerErr := serviceContainer.Register(
        "service.context-closeable",
        func(resolver containercontract.Resolver) (*contextCloseRecorder, error) {
            return recorder, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "service.context-closeable"); nil != getErr {
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

/* waveMateCloser closes by announcing that it started and then waiting, for a bounded moment, to be told that its wave-mate started too. Serially the first one to close waits the whole moment out and reports it, because the second has not begun; in one wave both announce before either waits. The bound is what keeps the failing arm a failure rather than a hung suite. */
type waveMateCloser struct {
    started      chan struct{}
    mateStarted  <-chan struct{}
    mateDeadline time.Duration
}

func (instance *waveMateCloser) CloseWithContext(closeContext context.Context) error {
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
        RegisterOptions{TypeRegistrationDisabled: true},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.wave.second",
        func(resolver containercontract.Resolver) (*waveMateCloser, error) {
            return second, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := FromResolver[*waveMateCloser](t.Context(), serviceContainer, "app.wave.first"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := FromResolver[*waveMateCloser](t.Context(), serviceContainer, "app.wave.second"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }
}

/* the two services below have no edge between them, so the graph puts them in one wave: under the waves, both are inside their Close at the same moment and neither has to wait. */
func TestContainer_Close_WavesTheServicesOfOneWaveCloseAtOnce(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerWaveMatePair(t, serviceContainer, time.Second)

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("expected both services of the wave to be closing at the same moment, got: %v", closeErr)
    }
}

/* the sibling that makes the test above mean something: the same pair, the same bound, the container configured sequential. One of the two waits its whole moment out and says so, which is the observation the waves have to remove. Without it, a teardown in waves that closed serially would pass on a bound nobody exercised. */
func TestContainer_Close_SequentialTheServicesOfOneWaveCloseOneAfterTheOther(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{TeardownSequential: true})

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

/* a ring the drain cannot open is closed as one unit and the drain CONTINUES past it: a pure dependency of a ring member — created after its dependent, so the creation-order tie-break put it first — is released by the ring's close and closed after it, as the edge says. Closed whole in creation order, the remainder closed that dependency FIRST, against the one edge that ordered it, and the remainder named it as if it were on the ring. */
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

/* two rings joined by a bridge: the first ring closes, the bridge is released and closes, the second ring closes — the bridge is on no ring and is not reported as one, and the two rings take two waves of their own, each closed serially */
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

/* a ring that depends on another ring DIRECTLY — a member's edge into the other ring, no bridge node between them — is the one shape where the count of what depends on a ring is fed by a ring member: with that term dropped the dependency ring reads as depended on by nothing and closes first, under the very edge that orders it after. The bridge shape beside it does not see the term, because there the dependent of the second ring is the bridge, a plain node. */
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

/* a ring's wave is one of its own, past every wave the drain assigned so far: a ring sharing its index with a drained, unrelated node would have that node closed one at a time with the ring's members under the waves, for no reason the graph gives */
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

/* a ring's wave is one past the last wave that CLOSED, not one past every wave assigned: the probe's chain of three pushes the dependency it shares with the ring to a provisional wave three, and a ring counted past that assignment would leave a wave nothing closes in, which the operator's view prints */
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

/* a dependency shared by two dependents lands one past the LATEST of them, not one past whichever released it last: the drain meets Q, which R released, after P, and Y — depended on by both — is bumped to the wave past Q, not left at the wave past P, or the wave of a dependent and its dependency would coincide and a wave would close a pair the graph orders */
func TestTeardownCloseOrder_ADependencyLandsOnePastItsLatestDependentNotItsLastReleaser(t *testing.T) {
    nodeKeys := []string{"service:y", "service:p", "service:q", "service:r"}

    edges := map[string]map[string]struct{}{
        "service:p": {"service:y": struct{}{}},
        "service:q": {"service:y": struct{}{}},
        "service:r": {"service:q": struct{}{}},
    }

    creationOrderOf := map[string]int{
        "service:y": 1,
        "service:p": 2,
        "service:q": 3,
        "service:r": 4,
    }

    closeOrder, closeWaveIndexOf, ringMembers := teardownCloseOrder(nodeKeys, edges, creationOrderOf)

    if 0 != len(ringMembers) {
        t.Fatalf("expected no ring, got %v", ringMembers)
    }

    if "[service:r service:q service:p service:y]" != fmt.Sprint(closeOrder) {
        t.Fatalf("expected the drain to close r, q, p, y, got %v", closeOrder)
    }

    expectedWaves := map[string]int{"service:p": 0, "service:r": 0, "service:q": 1, "service:y": 2}
    for nodeKey, expectedWave := range expectedWaves {
        if expectedWave != closeWaveIndexOf[nodeKey] {
            t.Fatalf("expected %s in wave %d, got %v", nodeKey, expectedWave, closeWaveIndexOf)
        }
    }
}

/* two rings nothing bridges are closed latest-created first, like any two unrelated services, and each takes a wave of its own */
func TestTeardownCloseOrder_TwoIndependentRingsCloseLatestFirst(t *testing.T) {
    nodeKeys := []string{"service:a1", "service:a2", "service:b1", "service:b2"}

    edges := map[string]map[string]struct{}{
        "service:a1": {"service:a2": struct{}{}},
        "service:a2": {"service:a1": struct{}{}},
        "service:b1": {"service:b2": struct{}{}},
        "service:b2": {"service:b1": struct{}{}},
    }

    creationOrderOf := map[string]int{
        "service:a1": 1,
        "service:a2": 2,
        "service:b1": 3,
        "service:b2": 4,
    }

    closeOrder, closeWaveIndexOf, ringMembers := teardownCloseOrder(nodeKeys, edges, creationOrderOf)

    if 4 != len(ringMembers) {
        t.Fatalf("expected both rings reported, got %v", ringMembers)
    }

    if "[service:b2 service:b1 service:a2 service:a1]" != fmt.Sprint(closeOrder) {
        t.Fatalf("expected the ring created last closed first, got %v", closeOrder)
    }

    if 0 != closeWaveIndexOf["service:b1"] || 0 != closeWaveIndexOf["service:b2"] || 1 != closeWaveIndexOf["service:a1"] || 1 != closeWaveIndexOf["service:a2"] {
        t.Fatalf("expected the later ring in wave zero and the earlier in wave one, got %v", closeWaveIndexOf)
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

/* the verification runs once the wiring is done, so it is the moment a declared edge naming a service nobody registered stops being a name still to come and becomes the ordering that is not there. */
func TestContainer_VerifyDeclaredTeardownDependencies_RefusesADeclaredDependencyOnAServiceThatWasNeverRegistered(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    if registerErr := serviceContainer.Register(
        "app.reporter",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{}, nil
        },
        RegisterOptions{TeardownDependencyNames: []string{"app.stroage"}},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    verifier, verifies := serviceContainer.(declaredTeardownVerifier)
    if false == verifies {
        t.Fatalf("expected the container to carry the declared teardown dependency door")
    }

    verifyErr := verifier.VerifyDeclaredTeardownDependencies()
    if nil == verifyErr {
        t.Fatalf("expected the misspelled teardown dependency to refuse the verification")
    }

    if false == errors.Is(verifyErr, ErrTeardownDependencyWasNeverRegistered) {
        t.Fatalf("expected the never-registered sentinel, got: %v", verifyErr)
    }

    if false == strings.Contains(verifyErr.Error(), "never registered") {
        t.Fatalf("expected the refusal to name what is missing, got: %v", verifyErr)
    }

    refusalContext := exception.LogContext(verifyErr, exception.LogContextConfig{})
    if "app.reporter" != refusalContext["serviceName"] || "app.stroage" != refusalContext["dependency"] || "service:app.stroage" != refusalContext["dependencyNodeKey"] {
        t.Fatalf("expected the refusal to carry the declaring service, the spelling it declared and the node key it stands for, got %v", refusalContext)
    }
}

/* the sibling that keeps the refusal above from being a refusal of the door's own purpose: a dependency that WAS registered and simply never built is the lazy singleton, and it must pass. */
func TestContainer_VerifyDeclaredTeardownDependencies_AdmitsADeclaredDependencyOnARegisteredServiceNobodyBuilt(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    if registerErr := serviceContainer.Register(
        "app.storage",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{}, nil
        },
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.reporter",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{}, nil
        },
        RegisterOptions{TeardownDependencyNames: []string{"app.storage"}},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    verifyDeclaredTeardownDependencies(t, serviceContainer)
}

/* a declaration keyed by TYPE writes into the same graph the name form writes into, so it orders the teardown the same way — and T and *T name one node, because the container files them under one canonical type. */
func TestContainer_Close_ADeclaredTeardownDependencyKeyedByTypeOrdersTheTeardown(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

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
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.reporter",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{recorder: recorder}, nil
        },
        RegisterOptions{TeardownDependencyTypes: []reflect.Type{reflect.TypeFor[closeOrderServiceB]()}},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    /* the DEPENDENT is built first and the dependency second, so the creation stamp alone would close the dependency FIRST — under the service still holding it. Only the declared edge can turn that around, which is what makes this an observation rather than a coincidence. */
    if _, getErr := FromResolver[*closeOrderServiceA](t.Context(), serviceContainer, "app.reporter"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := FromResolver[*closeOrderServiceB](t.Context(), serviceContainer, "app.storage"); nil != getErr {
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

/* labelledCloser records its own name, which is what a test about WHICH of several services an ordering reached needs: the shared fixtures above record one letter per type, and the question below is about two services of the SAME type. */
type labelledCloser struct {
    label    string
    recorder *closeOrderRecorder
}

func (instance *labelledCloser) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record(instance.label)

    return nil
}

/* declaringCloser is a second type, so the declaration under test names a type its declarer is not itself registered under — the self-declaration is refused at the registration door and is a different case. */
type declaringCloser struct {
    label    string
    recorder *closeOrderRecorder
}

func (instance *declaringCloser) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record(instance.label)

    return nil
}

func newAmbiguousTypeRecorder() (*closeOrderRecorder, *[]string) {
    mutex := &sync.Mutex{}
    closeSequence := make([]string, 0, 4)

    return &closeOrderRecorder{mutex: mutex, closeSequence: &closeSequence}, &closeSequence
}

/* registerAmbiguousTypeWiring is the shape the fan-out turned into a cycle nobody declared. The author writes TWO orderings, neither circular: the router closes before the type it knows its collaborator by, and one service closes before the router. The second service happens to be registered under the same type, which only a non-strict type registration allows — and reading the declaration as "before EVERY service of that type" adds router -> primary, which nobody wrote and which closes the ring. */
func registerAmbiguousTypeWiring(t *testing.T, serviceContainer containercontract.Container, recorder *closeOrderRecorder) {
    t.Helper()

    if registerErr := serviceContainer.Register(
        "app.fallback",
        func(_ containercontract.Resolver) (*labelledCloser, error) {
            return &labelledCloser{label: "fallback", recorder: recorder}, nil
        },
        RegisterOptions{TypeRegistrationRelaxed: true},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.primary",
        func(_ containercontract.Resolver) (*labelledCloser, error) {
            return &labelledCloser{label: "primary", recorder: recorder}, nil
        },
        RegisterOptions{TypeRegistrationRelaxed: true, TeardownDependencyNames: []string{"app.router"}},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.router",
        func(_ containercontract.Resolver) (*declaringCloser, error) {
            return &declaringCloser{label: "router", recorder: recorder}, nil
        },
        RegisterOptions{TeardownDependencyTypes: []reflect.Type{reflect.TypeFor[*labelledCloser]()}},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }
}

/* a teardown dependency declared on a TYPE that more than one service is registered under orders NOTHING, and a close that reaches it does not report a cycle nobody declared: expanded onto every name of the type, it would write an edge the declaring code never asked for, and where one service of that type orders itself before the declarer that edge would close a ring over a teardown in which all three services close and every Close returns nil. The container is configured sequential here. */
func TestContainer_Close_ADeclarationOnAnAmbiguousTypeOrdersNothingAndReportsNoCycle(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{TeardownSequential: true})

    recorder, closeSequence := newAmbiguousTypeRecorder()

    registerAmbiguousTypeWiring(t, serviceContainer, recorder)
    buildEveryRegisteredService(t, serviceContainer, "app.fallback", "app.primary", "app.router")

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("a declaration on an ambiguous type reported a failure over a teardown that closed everything: %v", closeErr)
    }

    if 3 != len(*closeSequence) {
        t.Fatalf("expected all three services to close, got %v", *closeSequence)
    }
}

/* the same wiring is refused under the waves, at the registration of the declarer: a type several services are registered under names a set, and there is no reading of "close me before this type" that a set answers. The refusal lands at the door where the author can still choose. */
func TestContainer_Register_RefusesADeclarationOnAnAmbiguousType(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    recorder, _ := newAmbiguousTypeRecorder()

    for _, serviceName := range []string{"app.fallback", "app.primary"} {
        label := serviceName
        serviceContainer.MustRegister(
            serviceName,
            func(_ containercontract.Resolver) (*labelledCloser, error) {
                return &labelledCloser{label: label, recorder: recorder}, nil
            },
            RegisterOptions{TypeRegistrationRelaxed: true},
        )
    }

    verifyErr := serviceContainer.Register(
        "app.router",
        func(_ containercontract.Resolver) (*declaringCloser, error) {
            return &declaringCloser{label: "router", recorder: recorder}, nil
        },
        RegisterOptions{TeardownDependencyTypes: []reflect.Type{reflect.TypeFor[*labelledCloser]()}},
    )
    if nil == verifyErr {
        t.Fatalf("the registration was admitted with a declaration that names a type more than one service is registered under")
    }

    if false == errors.Is(verifyErr, ErrTeardownDependencyTypeIsAmbiguous) {
        t.Fatalf("the refusal does not carry the ambiguity cause: %v", verifyErr)
    }

    if false == strings.Contains(verifyErr.Error(), "more than one service is registered under") {
        t.Fatalf("the refusal does not name what is ambiguous: %v", verifyErr)
    }

    if true == serviceContainer.Has("app.router") {
        t.Fatalf("expected the refused registration to leave nothing behind")
    }

    /* the declarer and the set it could not choose between are what the author acts on, and they travel in the context rather than in the message */
    var typedError *exception.Error
    if false == errors.As(verifyErr, &typedError) {
        t.Fatalf("the refusal is not a melody error carrying a context: %v", verifyErr)
    }

    if "app.router" != typedError.Context()["serviceName"] {
        t.Fatalf("the refusal does not name the declaring service: %v", typedError.Context())
    }

    registeredNames, areNames := typedError.Context()["registered"].([]string)
    if false == areNames || 2 != len(registeredNames) {
        t.Fatalf("the refusal does not name the services the type stands for: %v", typedError.Context())
    }
}

/* a type exactly one service is registered under folds onto that service, so a declaration by type closing a ring with a declaration by name is refused at its registration: the single service of the type ordered itself before the router, and the router declaring the type would close the ring */
func TestContainer_ADeclarationOnAnUnambiguousTypeClosingARingIsRefused(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    recorder, _ := newAmbiguousTypeRecorder()

    serviceContainer.MustRegister(
        "app.primary",
        func(_ containercontract.Resolver) (*labelledCloser, error) {
            return &labelledCloser{label: "primary", recorder: recorder}, nil
        },
        RegisterOptions{TypeRegistrationRelaxed: true, TeardownDependencyNames: []string{"app.router"}},
    )

    registerErr := serviceContainer.Register(
        "app.router",
        func(_ containercontract.Resolver) (*declaringCloser, error) {
            return &declaringCloser{label: "router", recorder: recorder}, nil
        },
        RegisterOptions{TeardownDependencyTypes: []reflect.Type{reflect.TypeFor[*labelledCloser]()}},
    )
    if false == errors.Is(registerErr, ErrCircularDependency) {
        t.Fatalf("expected the type declaration closing the ring refused, got %v", registerErr)
    }
}

/* concurrentCloser records how many closes run at once — the peak is what tells a wave from a serial loop — and, where a test hands it a counter, how many closes it answered. */
type concurrentCloser struct {
    running *atomic.Int64
    peak    *atomic.Int64
    closed  *atomic.Int64
}

func (instance *concurrentCloser) CloseWithContext(closeContext context.Context) error {
    now := instance.running.Add(1)

    for {
        peak := instance.peak.Load()
        if now <= peak || true == instance.peak.CompareAndSwap(peak, now) {
            break
        }
    }

    time.Sleep(50 * time.Millisecond)
    instance.running.Add(-1)

    if nil != instance.closed {
        instance.closed.Add(1)
    }

    return nil
}

func TestContainer_Close_WavesTheCycleRemainderClosesOneAfterTheOther(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    var running, peak atomic.Int64

    for _, pair := range [][2]string{{"cycle.a", "cycle.b"}, {"cycle.b", "cycle.c"}, {"cycle.c", ""}} {
        options := RegisterOptions{TypeRegistrationDisabled: true}
        if "" != pair[1] {
            options.TeardownDependencyNames = []string{pair[1]}
        }

        if registerErr := serviceContainer.Register(
            pair[0],
            func(_ containercontract.Resolver) (*concurrentCloser, error) {
                return &concurrentCloser{running: &running, peak: &peak}, nil
            },
            options,
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }
    }

    writeDependencyRingEdge(t, serviceContainer, "cycle.c", "cycle.a")

    /* verified after the wiring, as the door asks: the first of these declarations names a service registered only later */
    verifyDeclaredTeardownDependencies(t, serviceContainer)

    buildEveryRegisteredService(t, serviceContainer, "cycle.a", "cycle.b", "cycle.c")

    closeErr := serviceContainer.Close()
    if nil == closeErr || false == strings.Contains(closeErr.Error(), "dependency cycle detected") {
        t.Fatalf("expected the cycle to still be reported, got %v", closeErr)
    }

    if 1 != peak.Load() {
        t.Fatalf("expected the cycle remainder to close one service at a time, got %d at once", peak.Load())
    }
}

/* two rings — each a pair ordered against one another — are two waves closed one service at a time */
func TestContainer_Close_WavesEachRingClosesOneAfterTheOther(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    var running, peak atomic.Int64

    for _, pair := range [][2]string{{"ring.a", "ring.b"}, {"ring.b", ""}, {"ring.e", "ring.f"}, {"ring.f", ""}} {
        options := RegisterOptions{TypeRegistrationDisabled: true}
        if "" != pair[1] {
            options.TeardownDependencyNames = []string{pair[1]}
        }

        if registerErr := serviceContainer.Register(
            pair[0],
            func(_ containercontract.Resolver) (*concurrentCloser, error) {
                return &concurrentCloser{running: &running, peak: &peak}, nil
            },
            options,
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }
    }

    writeDependencyRingEdge(t, serviceContainer, "ring.b", "ring.a")
    writeDependencyRingEdge(t, serviceContainer, "ring.f", "ring.e")

    verifyDeclaredTeardownDependencies(t, serviceContainer)

    buildEveryRegisteredService(t, serviceContainer, "ring.a", "ring.b", "ring.e", "ring.f")

    closeErr := serviceContainer.Close()
    if nil == closeErr || false == strings.Contains(closeErr.Error(), "dependency cycle detected") {
        t.Fatalf("expected the two rings to be reported, got %v", closeErr)
    }

    if 1 != peak.Load() {
        t.Fatalf("expected each ring to close one service at a time, got %d at once", peak.Load())
    }
}

/* the close report names the members of the ring alone: the pure dependency a ring member resolves is closed after the ring, in the order the graph proves, and naming it beside the ring would send the operator looking for a ring it is not on */
func TestContainer_Close_TheCycleReportNamesTheRingMembersAlone(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegister(
        "cycle.c",
        func(_ containercontract.Resolver) (*poolDeclarerService, error) { return &poolDeclarerService{label: "c"}, nil },
        RegisterOptions{TypeRegistrationDisabled: true},
    )

    serviceContainer.MustRegister(
        "cycle.b",
        func(resolver containercontract.Resolver) (*poolDeclarerService, error) {
            if _, resolveErr := resolver.Get(ContextOf(resolver), "cycle.c"); nil != resolveErr {
                return nil, resolveErr
            }

            return &poolDeclarerService{label: "b"}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    )

    serviceContainer.MustRegister(
        "cycle.a",
        func(resolver containercontract.Resolver) (*poolDeclarerService, error) {
            if _, resolveErr := resolver.Get(ContextOf(resolver), "cycle.b"); nil != resolveErr {
                return nil, resolveErr
            }

            return &poolDeclarerService{label: "a"}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    )

    MustFromResolver[*poolDeclarerService](t.Context(), serviceContainer, "cycle.a")

    writeDependencyRingEdge(t, serviceContainer, "cycle.b", "cycle.a")

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

func TestContainer_Register_RefusesADeclaredDependencyOnAScopedService(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    if registerErr := serviceContainer.RegisterScoped(
        "scoped.store",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    ); nil != registerErr {
        t.Fatalf("unexpected scoped register error: %v", registerErr)
    }

    registerErr := serviceContainer.Register(
        "app.reporter",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true, TeardownDependencyNames: []string{"scoped.store"}},
    )
    if false == errors.Is(registerErr, ErrTeardownDependencyIsScoped) {
        t.Fatalf("expected the dependency on a registered scoped service to be refused at the registration with ErrTeardownDependencyIsScoped, got %v", registerErr)
    }
}

func TestContainer_VerifyDeclaredTeardownDependencies_RefusesADeclarationThatNamedAScopedServiceRegisteredAfterIt(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegister(
        "app.reporter",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            return &closeOrderServiceA{}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true, TeardownDependencyNames: []string{"scoped.store"}},
    )

    serviceContainer.MustRegisterScoped(
        "scoped.store",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    )

    verifyErr := serviceContainer.(declaredTeardownVerifier).VerifyDeclaredTeardownDependencies()
    if false == errors.Is(verifyErr, ErrTeardownDependencyIsScoped) {
        t.Fatalf("expected the verification to refuse the dependency on a scoped service with ErrTeardownDependencyIsScoped, got %v", verifyErr)
    }
}

func TestContainer_Close_WavesTheReplacedInstancesCloseOneAfterTheOther(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    var running, peak, closed atomic.Int64

    for _, serviceName := range []string{"app.replaced.first", "app.replaced.second"} {
        if registerErr := serviceContainer.Register(
            serviceName,
            func(resolver containercontract.Resolver) (*concurrentCloser, error) {
                return &concurrentCloser{running: &running, peak: &peak, closed: &closed}, nil
            },
            RegisterOptions{TypeRegistrationDisabled: true},
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }

        if _, getErr := serviceContainer.Get(t.Context(), serviceName); nil != getErr {
            t.Fatalf("unexpected get error: %v", getErr)
        }

        if overrideErr := serviceContainer.OverrideProtectedInstance(serviceName, &closeOrderServiceA{recorder: &closeOrderRecorder{mutex: &sync.Mutex{}, closeSequence: &[]string{}}}, OverrideOptions{}); nil != overrideErr {
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

type mutualConcurrentCloser struct {
    running *atomic.Int64
    peak    *atomic.Int64
    closed  *atomic.Int64
    partner *mutualConcurrentCloser
}

func (instance *mutualConcurrentCloser) CloseWithContext(closeContext context.Context) error {
    return (&concurrentCloser{running: instance.running, peak: instance.peak, closed: instance.closed}).CloseWithContext(context.Background())
}

func TestContainer_Close_WavesAMutuallyHeldPairClosesOneAfterTheOther(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    var running, peak, closed atomic.Int64

    holder := &mutualConcurrentCloser{running: &running, peak: &peak, closed: &closed}
    collaborator := &mutualConcurrentCloser{running: &running, peak: &peak, closed: &closed, partner: holder}
    holder.partner = collaborator

    if registerErr := serviceContainer.Register(
        "app.mutual.holder",
        func(resolver containercontract.Resolver) (*mutualConcurrentCloser, error) { return holder, nil },
        RegisterOptions{TypeRegistrationDisabled: true},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.mutual.collaborator",
        func(resolver containercontract.Resolver) (*mutualConcurrentCloser, error) { return collaborator, nil },
        RegisterOptions{TypeRegistrationDisabled: true},
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

type mutualWaveMate struct {
    mate    *waveMateCloser
    partner *mutualWaveMate
}

func (instance *mutualWaveMate) CloseWithContext(closeContext context.Context) error {
    return instance.mate.CloseWithContext(context.Background())
}

/* the case that keeps the group from being a serialisation of the whole wave: two mutually held pairs with no relation between them are two groups, and the groups still start at once. Each pair's first member waits for the other pair's first member to have started; closed group after group, one of them waits its whole moment out and says so. */
func TestContainer_Close_WavesTwoUnrelatedMutuallyHeldPairsCloseAtOnce(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

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
                RegisterOptions{TypeRegistrationDisabled: true},
            ); nil != registerErr {
                t.Fatalf("unexpected register error: %v", registerErr)
            }

            if _, getErr := serviceContainer.Get(t.Context(), serviceName); nil != getErr {
                t.Fatalf("unexpected get error: %v", getErr)
            }
        }
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("expected the two groups to be closing at the same moment, got: %v", closeErr)
    }
}

/* budgetSleeper closes by sleeping through its deadline, ignoring the context it is handed, and answering nil: the plain closer CONTAINER.md declares bounded by nothing but itself, and the one that ate the budget without anything naming it. */
type budgetSleeper struct {
    sleep time.Duration
    label string
}

func (instance *budgetSleeper) CloseWithContext(_ context.Context) error {
    time.Sleep(instance.sleep)

    return nil
}

func (instance *budgetSleeper) Close() error {
    return instance.CloseWithContext(context.Background())
}

/* deadlineNotingCloser answers at once through the context-taking door and notes whether its context carries a deadline at all */
type deadlineNotingCloser struct {
    sawDeadline bool
}

func (instance *deadlineNotingCloser) CloseWithContext(closeContext context.Context) error {
    _, instance.sawDeadline = closeContext.Deadline()

    return nil
}

func (instance *deadlineNotingCloser) Close() error {
    return instance.CloseWithContext(context.Background())
}

/* refusingSleeper is budgetSleeper's failing twin: it sleeps through its deadline and then refuses, so the teardown that ran it both overran and failed and carries the deadline record in its error. */
type refusingSleeper struct {
    sleep time.Duration
}

func (instance *refusingSleeper) CloseWithContext(closeContext context.Context) error {
    time.Sleep(instance.sleep)

    return errors.New("the sleeper refused")
}

func teardownDeadlineOverrunOf(t *testing.T, serviceContainer containercontract.Container) exceptioncontract.Context {
    t.Helper()

    reporter, carriesDoor := serviceContainer.(interface {
        TeardownDeadlineOverrun() exceptioncontract.Context
    })
    if false == carriesDoor {
        t.Fatalf("expected the container to carry the deadline overrun door")
    }

    return reporter.TeardownDeadlineOverrun()
}

func closeContainerWithin(t *testing.T, serviceContainer containercontract.Container, budget time.Duration) error {
    t.Helper()

    closeContext, cancel := context.WithTimeout(context.Background(), budget)
    defer cancel()

    return serviceContainer.(interface {
        CloseWithContext(context.Context) error
    }).CloseWithContext(closeContext)
}

func durationOfRecord(t *testing.T, record exceptioncontract.Context, key string) time.Duration {
    t.Helper()

    text, isText := record[key].(string)
    if false == isText {
        t.Fatalf("expected %s rendered as a duration, got %v", key, record[key])
    }

    duration, parseErr := time.ParseDuration(text)
    if nil != parseErr {
        t.Fatalf("expected %s to parse as a duration, got %q: %v", key, text, parseErr)
    }

    return duration
}

func namedDurations(t *testing.T, record exceptioncontract.Context, key string) map[string]string {
    t.Helper()

    named, isNamed := record[key].(map[string]string)
    if false == isNamed {
        t.Fatalf("expected %s as durations by node, got %v", key, record[key])
    }

    return named
}

func TestContainer_CloseWithContext_AnOverrunNamesTheServiceThatSpentTheBudget(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegister("app.eater", func(_ containercontract.Resolver) (*budgetSleeper, error) { return &budgetSleeper{sleep: 80 * time.Millisecond, label: "eater"}, nil }, RegisterOptions{})
    MustFromResolver[*budgetSleeper](t.Context(), serviceContainer, "app.eater")

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

/* a teardown reached with its budget already gone records a budget of zero, not the negative remainder: the closer it reached is starved, nothing spent anything, and the record still says so */
func TestContainer_ATeardownReachedWithTheBudgetGoneRecordsAZeroBudget(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    closer := &deadlineNotingCloser{}
    serviceContainer.MustRegister("app.x", func(_ containercontract.Resolver) (*deadlineNotingCloser, error) { return closer, nil }, RegisterOptions{TypeRegistrationDisabled: true})
    MustFromResolver[*deadlineNotingCloser](t.Context(), serviceContainer, "app.x")

    closeContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    if closeErr := serviceContainer.(containercontract.ContextCloser).CloseWithContext(closeContext); nil != closeErr {
        t.Fatalf("expected a spent budget alone not to fail the teardown, got %v", closeErr)
    }

    if false == closer.sawDeadline {
        t.Fatalf("expected the closer handed the teardown's deadline")
    }

    record := teardownDeadlineOverrunOf(t, serviceContainer)
    if nil == record {
        t.Fatalf("expected a teardown reached with the budget gone to keep a record")
    }

    if "0s" != record["budget"] {
        t.Fatalf("expected the budget floored at zero, got %v", record["budget"])
    }

    if starved, _ := record["starved"].([]string); "[service:app.x]" != fmt.Sprint(starved) {
        t.Fatalf("expected the one closer reached after the deadline starved, got %v", record)
    }

    if 0 != len(namedDurations(t, record, "spentBy")) {
        t.Fatalf("expected nothing named as running when the deadline passed, got %v", record)
    }
}

/* the record travels twice, in the error of a failed teardown and through the door, and a reader may decorate what the door answered before writing it; it must not be decorating the error's record with it */
func TestContainer_TeardownDeadlineOverrunAnswersItsOwnCopyOfTheRecord(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegister("app.slow", func(_ containercontract.Resolver) (*refusingSleeper, error) { return &refusingSleeper{sleep: 30 * time.Millisecond}, nil }, RegisterOptions{TypeRegistrationDisabled: true})
    MustFromResolver[*refusingSleeper](t.Context(), serviceContainer, "app.slow")

    closeErr := closeContainerWithin(t, serviceContainer, 5*time.Millisecond)
    if nil == closeErr {
        t.Fatalf("expected the refusing closer to fail the teardown")
    }

    var melodyErr *exception.Error
    if false == errors.As(closeErr, &melodyErr) {
        t.Fatalf("expected the failure to carry the record, got %T", closeErr)
    }

    errorRecord, carriesRecord := melodyErr.Context()["deadline"].(exceptioncontract.Context)
    if false == carriesRecord {
        t.Fatalf("expected the error to carry the deadline record, got %v", melodyErr.Context())
    }

    doorRecord := teardownDeadlineOverrunOf(t, serviceContainer)
    if nil == doorRecord {
        t.Fatalf("expected the door to answer the record of the overrun")
    }

    if _, named := namedDurations(t, errorRecord, "spentBy")["service:app.slow"]; false == named {
        t.Fatalf("expected the error's record to name the closer running when the deadline passed, got %v", errorRecord)
    }

    doorRecord["spentBy"] = "edited by a reader"
    doorRecord["starved"] = []string{"edited by a reader"}

    if _, named := namedDurations(t, errorRecord, "spentBy")["service:app.slow"]; false == named {
        t.Fatalf("expected the error's record untouched by an edit of what the door answered, got %v", errorRecord)
    }

    if starved, _ := errorRecord["starved"].([]string); 0 != len(starved) {
        t.Fatalf("expected the error's starved list untouched by an edit of what the door answered, got %v", starved)
    }

    secondRecord := teardownDeadlineOverrunOf(t, serviceContainer)
    if _, named := namedDurations(t, secondRecord, "spentBy")["service:app.slow"]; false == named {
        t.Fatalf("expected a second reader of the door untouched by the first reader's edit, got %v", secondRecord)
    }
}

/* the fast service is registered FIRST, so the eater built after it closes before it — latest created, first closed — and the fast one is reached with the deadline already gone: starved, and named as such, while the eater is named as the one that spent it */
func TestContainer_CloseWithContext_AServiceReachedWithTheBudgetSpentIsNamedStarved(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{TeardownSequential: true})

    serviceContainer.MustRegister("app.fast", func(_ containercontract.Resolver) (*budgetSleeper, error) { return &budgetSleeper{sleep: 0, label: "fast"}, nil }, RegisterOptions{TypeRegistrationDisabled: true})
    serviceContainer.MustRegister("app.eater", func(_ containercontract.Resolver) (*budgetSleeper, error) { return &budgetSleeper{sleep: 80 * time.Millisecond, label: "eater"}, nil }, RegisterOptions{TypeRegistrationDisabled: true})
    MustFromResolver[*budgetSleeper](t.Context(), serviceContainer, "app.fast")
    MustFromResolver[*budgetSleeper](t.Context(), serviceContainer, "app.eater")

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

/* under the waves two unrelated closers start together; both are running when the deadline passes, both are named, and the whole teardown costs one of them rather than two */
func TestContainer_CloseWithContext_WavesTwoClosersOfOneWaveThatOverlapAreBothNamed(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegister("app.first", func(_ containercontract.Resolver) (*budgetSleeper, error) { return &budgetSleeper{sleep: 80 * time.Millisecond, label: "first"}, nil }, RegisterOptions{TypeRegistrationDisabled: true})
    serviceContainer.MustRegister("app.second", func(_ containercontract.Resolver) (*budgetSleeper, error) { return &budgetSleeper{sleep: 80 * time.Millisecond, label: "second"}, nil }, RegisterOptions{TypeRegistrationDisabled: true})
    MustFromResolver[*budgetSleeper](t.Context(), serviceContainer, "app.first")
    MustFromResolver[*budgetSleeper](t.Context(), serviceContainer, "app.second")

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

/* a teardown reached with its deadline already gone and nothing to close spent nothing on anything: it kept a record naming nobody — an empty budget, an empty map of durations — and the application then wrote "teardown overran its deadline" about a teardown that did nothing */
func TestContainer_CloseWithContext_ATeardownThatClosedNothingKeepsNoOverrunRecord(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

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

/* a close that failed under an overrun carries the deadline record beside its failures, so the operator reading a failed teardown asks the same question about the budget as one reading a clean one */
func TestContainer_CloseWithContext_AFailureUnderAnOverrunCarriesTheDeadlineRecordBesideTheFailures(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegister("app.eater", func(_ containercontract.Resolver) (*budgetSleeper, error) { return &budgetSleeper{sleep: 80 * time.Millisecond, label: "eater"}, nil }, RegisterOptions{})
    serviceContainer.MustRegister("app.failing", func(_ containercontract.Resolver) (*failingCloser, error) { return &failingCloser{}, nil }, RegisterOptions{})
    MustFromResolver[*budgetSleeper](t.Context(), serviceContainer, "app.eater")
    MustFromResolver[*failingCloser](t.Context(), serviceContainer, "app.failing")

    closeErr := closeContainerWithin(t, serviceContainer, 40*time.Millisecond)
    if nil == closeErr {
        t.Fatalf("expected the failing close reported")
    }

    errorContext := exception.LogContext(closeErr, exception.LogContextConfig{})

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
    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegister("app.fast", func(_ containercontract.Resolver) (*budgetSleeper, error) { return &budgetSleeper{sleep: 0, label: "fast"}, nil }, RegisterOptions{})
    MustFromResolver[*budgetSleeper](t.Context(), serviceContainer, "app.fast")

    if closeErr := closeContainerWithin(t, serviceContainer, time.Second); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if record := teardownDeadlineOverrunOf(t, serviceContainer); nil != record {
        t.Fatalf("expected no record for a teardown inside its budget, got %v", record)
    }
}

func TestContainer_Close_WithoutADeadlineLeavesNoDeadlineRecord(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegister("app.eater", func(_ containercontract.Resolver) (*budgetSleeper, error) { return &budgetSleeper{sleep: 20 * time.Millisecond, label: "eater"}, nil }, RegisterOptions{})
    MustFromResolver[*budgetSleeper](t.Context(), serviceContainer, "app.eater")

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if record := teardownDeadlineOverrunOf(t, serviceContainer); nil != record {
        t.Fatalf("expected no record for a close with no deadline, got %v", record)
    }
}

type plainValueService struct {
    label string
}

/* the durations name the closes that RAN: a service without a Close and a second filing of a value already claimed are neither closed nor timed */
func TestContainer_CloseWithContext_ANonCloserAndADuplicateValueAreNotRecorded(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    eater := &budgetSleeper{sleep: 80 * time.Millisecond, label: "eater"}

    serviceContainer.MustRegister("app.eater", func(_ containercontract.Resolver) (*budgetSleeper, error) { return eater, nil }, RegisterOptions{TypeRegistrationDisabled: true})
    serviceContainer.MustRegister("app.eater.again", func(_ containercontract.Resolver) (*budgetSleeper, error) { return eater, nil }, RegisterOptions{TypeRegistrationDisabled: true})
    serviceContainer.MustRegister("app.plain", func(_ containercontract.Resolver) (*plainValueService, error) { return &plainValueService{label: "plain"}, nil }, RegisterOptions{})
    MustFromResolver[*budgetSleeper](t.Context(), serviceContainer, "app.eater")
    MustFromResolver[*budgetSleeper](t.Context(), serviceContainer, "app.eater.again")
    MustFromResolver[*plainValueService](t.Context(), serviceContainer, "app.plain")

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

type failingCloser struct{}

func (instance *failingCloser) CloseWithContext(closeContext context.Context) error {
    return errors.New("the close refused")
}

type scopedOnlyService struct {
    label string
}

func (instance *scopedOnlyService) CloseWithContext(closeContext context.Context) error { return nil }

/* the two spellings of one declaration answer one cause: a dependency on a type only a scoped registration filed, registered after the declaration, is refused as scoped at the verification, the way the same dependency by name is, and not as never registered — the service IS registered, one lifetime away */
func TestContainer_VerifyDeclaredTeardownDependencies_RefusesADeclaredDependencyOnAScopedTypeAsScoped(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    if registerErr := serviceContainer.Register(
        "app.declarer",
        func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{}, nil },
        RegisterOptions{TeardownDependencyTypes: []reflect.Type{reflect.TypeFor[*scopedOnlyService]()}},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    serviceContainer.MustRegisterScoped(
        "app.scoped",
        func(_ containercontract.Resolver) (*scopedOnlyService, error) { return &scopedOnlyService{label: "scoped"}, nil },
        RegisterOptions{},
    )

    verifyErr := serviceContainer.(declaredTeardownVerifier).VerifyDeclaredTeardownDependencies()
    if false == errors.Is(verifyErr, ErrTeardownDependencyIsScoped) {
        t.Fatalf("expected the dependency on a scoped type to refuse the verification as scoped, got %v", verifyErr)
    }
}

/* a declaration by type counts as an edge to the one service the type stands for: the declarer closing before the pool while the pool, resolved through its type, depends on the declarer is a ring, refused at that resolution before anything is built */
func TestContainer_ADeclarationOnATypeRefusesTheTypeResolutionClosingARing(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegister(
        "app.declarer",
        func(_ containercontract.Resolver) (*poolDeclarerService, error) { return &poolDeclarerService{label: "declarer"}, nil },
        RegisterOptions{TeardownDependencyTypes: []reflect.Type{reflect.TypeFor[*sharedPoolService]()}},
    )

    serviceContainer.MustRegister(
        "app.pool.first",
        func(resolver containercontract.Resolver) (*sharedPoolService, error) {
            if _, resolveErr := resolver.Get(ContextOf(resolver), "app.declarer"); nil != resolveErr {
                return nil, resolveErr
            }

            return &sharedPoolService{label: "first"}, nil
        },
        RegisterOptions{TypeRegistrationRelaxed: true},
    )

    if _, getErr := serviceContainer.GetByType(t.Context(), reflect.TypeOf((*sharedPoolService)(nil))); false == errors.Is(getErr, ErrCircularDependency) {
        t.Fatalf("expected the type resolution closing the ring with the declaration refused, got %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("expected a clean teardown, got %v", closeErr)
    }
}

/* capturedDeclarerPool holds the declarer by a pointer it received, not one it resolved: the walk sees the pointer, the graph sees nothing */
type capturedDeclarerPool struct {
    declarer *poolDeclarerService
}

func (instance *capturedDeclarerPool) CloseWithContext(closeContext context.Context) error {
    return nil
}

func TestContainer_Close_WavesACapturedPointerBackAgainstATypeDeclarationIsNoRing(t *testing.T) {
    for _, byType := range []bool{true, false} {
        serviceContainer := NewContainer(ContainerConfig{})
        declarerInstance := &poolDeclarerService{label: "declarer"}

        option := RegisterOptions{TeardownDependencyNames: []string{"app.pool"}}
        if true == byType {
            option = RegisterOptions{TeardownDependencyTypes: []reflect.Type{reflect.TypeFor[*capturedDeclarerPool]()}}
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
            RegisterOptions{},
        )

        if verifyErr := serviceContainer.(declaredTeardownVerifier).VerifyDeclaredTeardownDependencies(); nil != verifyErr {
            t.Fatalf("byType=%v verify: %v", byType, verifyErr)
        }

        MustFromResolver[*poolDeclarerService](t.Context(), serviceContainer, "app.declarer")
        MustFromResolver[*capturedDeclarerPool](t.Context(), serviceContainer, "app.pool")

        plan := serviceContainer.(teardownPlanner).TeardownPlan()

        for _, entry := range plan {
            if "service:app.pool" == entry.NodeKey && 0 != len(entry.Dependencies) {
                t.Fatalf("byType=%v: the held pointer back was written as an inference over the declaration it contradicts: %v", byType, entry.Dependencies)
            }
        }

        if closeErr := serviceContainer.Close(); nil != closeErr {
            t.Fatalf("byType=%v: expected a clean teardown in waves, got %v", byType, closeErr)
        }
    }
}

/* a resolution between two names of ONE instance collapses onto a self-edge, and a self-edge is no edge: a plan that kept it would list a service as closed before itself */
func TestContainer_TeardownPlan_AResolutionBetweenTwoNamesOfOneInstanceIsNoEdge(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegister(
        "app.a",
        func(_ containercontract.Resolver) (*poolDeclarerService, error) { return &poolDeclarerService{label: "a"}, nil },
        RegisterOptions{TypeRegistrationDisabled: true},
    )

    serviceContainer.MustRegister(
        "app.b",
        func(resolver containercontract.Resolver) (*poolDeclarerService, error) {
            value, resolveErr := resolver.Get(ContextOf(resolver), "app.a")
            if nil != resolveErr {
                return nil, resolveErr
            }

            return value.(*poolDeclarerService), nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    )

    MustFromResolver[*poolDeclarerService](t.Context(), serviceContainer, "app.b")

    for _, entry := range serviceContainer.(teardownPlanner).TeardownPlan() {
        if 0 != len(entry.Dependencies) {
            t.Fatalf("%s is listed as closed before %v — one instance under two names, ordered against itself", entry.NodeKey, entry.Dependencies)
        }
    }
}

/* what a declared type stands for is the plan's to expand, for one plan: the operator's view asked for the plan while the type has one name writes no expanded edge into the graph, so a second, non-strict name registered under the type later leaves the declaration ordering nothing and the teardown clean */
func TestContainer_Close_TheViewLeavesNoExpandedTypeEdgeBehindOnTheSequentialPath(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{TeardownSequential: true})

    serviceContainer.MustRegister(
        "app.declarer",
        func(_ containercontract.Resolver) (*poolDeclarerService, error) { return &poolDeclarerService{label: "declarer"}, nil },
        RegisterOptions{TeardownDependencyTypes: []reflect.Type{reflect.TypeFor[*sharedPoolService]()}},
    )

    serviceContainer.MustRegister(
        "app.pool.first",
        func(_ containercontract.Resolver) (*sharedPoolService, error) {
            return &sharedPoolService{label: "first"}, nil
        },
        RegisterOptions{TypeRegistrationRelaxed: true},
    )

    MustFromResolver[*poolDeclarerService](t.Context(), serviceContainer, "app.declarer")
    MustFromResolver[*sharedPoolService](t.Context(), serviceContainer, "app.pool.first")

    serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    }).TeardownPlan()

    concrete := serviceContainer.(*container)
    concrete.mutex.Lock()
    declarerEdges := len(concrete.dependencyGraph["service:app.declarer"])
    concrete.mutex.Unlock()

    if 0 != declarerEdges {
        t.Fatalf("expected the view to write no expanded edge into the graph, got %d edges from the declarer", declarerEdges)
    }

    serviceContainer.MustRegister(
        "app.pool.second",
        func(_ containercontract.Resolver) (*sharedPoolService, error) { return &sharedPoolService{label: "second"}, nil },
        RegisterOptions{TypeRegistrationRelaxed: true},
    )

    MustFromResolver[*sharedPoolService](t.Context(), serviceContainer, "app.pool.second")

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("expected a clean teardown once the declaration turned ambiguous, got %v", closeErr)
    }
}

/* contextDoorOnlyService carries the context-taking close door and NOT Close: the shape a service grows when the deadline is the only reason it closes at all. */
type contextDoorOnlyService struct {
    closes      int
    hadDeadline bool
}

func (instance *contextDoorOnlyService) CloseWithContext(closeContext context.Context) error {
    instance.closes = instance.closes + 1
    _, instance.hadDeadline = closeContext.Deadline()

    return nil
}

func TestContainer_Close_ClosesAServiceThatCarriesOnlyCloseWithContextAndHandsItTheDeadline(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    service := &contextDoorOnlyService{}

    MustRegister[*contextDoorOnlyService](
        serviceContainer,
        "context.door.only",
        func(resolver containercontract.Resolver) (*contextDoorOnlyService, error) {
            return service, nil
        },
        RegisterOptions{},
    )
    _ = MustFromResolver[*contextDoorOnlyService](t.Context(), serviceContainer, "context.door.only")

    if closeErr := closeContainerWithin(t, serviceContainer, time.Second); nil != closeErr {
        t.Fatalf("expected a clean teardown, got %v", closeErr)
    }

    if 1 != service.closes {
        t.Fatalf("expected the service carrying only CloseWithContext to be closed once, got %d closes", service.closes)
    }

    if false == service.hadDeadline {
        t.Fatalf("expected the teardown's deadline to reach the service through its only door")
    }
}

/* a service whose Close answers a TYPED nil: the interface is not nil, the pointer
inside it is, which is what an application closer built on a *exception.Error it
never assigned hands back. */
type typedNilCloseErrorService struct {
    closeCount int
}

func (instance *typedNilCloseErrorService) CloseWithContext(closeContext context.Context) error {
    instance.closeCount++

    var refusal *exception.Error

    return refusal
}

/* a typed nil is the nil its producer meant: the close of a service that answers
one is a close that succeeded, and the teardown reports no failure for it. The
service counts its closes, so the answer is a close that RAN, not one skipped. */
func TestContainerClose_ATypedNilCloseErrorIsReadAsASuccessfulClose(t *testing.T) {
    containerInstance := NewContainer(ContainerConfig{})
    service := &typedNilCloseErrorService{}

    registerErr := Register[*typedNilCloseErrorService](
        containerInstance,
        "service.typednil",
        func(resolver containercontract.Resolver) (*typedNilCloseErrorService, error) {
            return service, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("register: %v", registerErr)
    }

    if _, resolveErr := FromResolver[*typedNilCloseErrorService](t.Context(), containerInstance, "service.typednil"); nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    if closeErr := containerInstance.Close(); nil != closeErr {
        t.Fatalf("expected the typed nil to read as a successful close, got %v", closeErr)
    }

    if 1 != service.closeCount {
        t.Fatalf("expected the service to be closed exactly once, closed %d times", service.closeCount)
    }
}

/* LogContext answers a NIL map for a typed nil, and the details of a failed close are
written into what it answers: the book is read without a write into it, and a context
with nothing in it books nothing. Latent since the containment reads a typed nil as
success; a write into the nil map would end this test with a panic. */
func TestRecordCloseFailureDetails_ATypedNilErrorWritesIntoNoNilMap(t *testing.T) {
    failureDetails := map[string]exceptioncontract.Context{}
    var refusal *exception.Error

    recordCloseFailureDetails(failureDetails, "service:typednil", refusal, "typed nil")

    if 0 != len(failureDetails) {
        t.Fatalf("expected a typed nil, which carries no context, to book no details, got %v", failureDetails)
    }
}

/* The limit is counted in bytes and one of the two details it bounds is the panic
value as the SERVICE wrote it, so arbitrary application text. A cut taken at the
byte alone splits whatever rune straddles it; the probe puts a three-byte rune
across the boundary deliberately. */
func TestBoundedCloseFailureDetail_CutsOnARuneBoundary(t *testing.T) {
    /* one filler byte short of the limit, then a three-byte rune straddling it */
    detail := strings.Repeat("a", closeFailureStackLimit-1) + strings.Repeat("\u4e16", 64)

    bounded, isText := boundedCloseFailureDetail(detail).(string)
    if false == isText {
        t.Fatalf("the detail came back as %T, wanted the cut text", boundedCloseFailureDetail(detail))
    }

    kept := strings.Split(bounded, "\n\t\u2026 cut,")[0]

    if false == utf8.ValidString(kept) {
        t.Fatalf("the cut left invalid utf8: %q", kept[len(kept)-8:])
    }

    if closeFailureStackLimit < len(kept) {
        t.Fatalf("the cut kept %d bytes, over the limit of %d", len(kept), closeFailureStackLimit)
    }
}

/* The back-off looks for the start of the rune the limit lands in, which is at
most three bytes back. A byte that is not valid UTF-8 ELSEWHERE in the text is the
service's own and is not the cut's to repair: asked of the whole prefix instead,
the back-off walked down to that byte and kept nothing of a value that led with
one. */
func TestBoundedCloseFailureDetail_KeepsTheLimitPastAnInvalidByte(t *testing.T) {
    for _, invalidAt := range []int{0, 100, closeFailureStackLimit - 8} {
        detail := strings.Repeat("a", invalidAt) + "\xff" + strings.Repeat("a", 2*closeFailureStackLimit)

        bounded, isText := boundedCloseFailureDetail(detail).(string)
        if false == isText {
            t.Fatalf("the detail came back as %T, wanted the cut text", boundedCloseFailureDetail(detail))
        }

        kept := strings.Split(bounded, "\n\t\u2026 cut,")[0]

        if closeFailureStackLimit != len(kept) {
            t.Fatalf("an invalid byte at %d cut the detail to %d bytes, wanted the limit of %d", invalidAt, len(kept), closeFailureStackLimit)
        }
    }

    /* a run of stray continuation bytes across the limit starts no rune anywhere in it: the walk stops at its
       bound of three bytes rather than following the run down */
    detail := strings.Repeat("a", closeFailureStackLimit-8) + strings.Repeat("\x80", 20) + strings.Repeat("a", closeFailureStackLimit)
    bounded, _ := boundedCloseFailureDetail(detail).(string)
    if kept := strings.Split(bounded, "\n\t\u2026 cut,")[0]; closeFailureStackLimit-(utf8.UTFMax-1) != len(kept) {
        t.Fatalf("a run of continuation bytes across the limit cut the detail to %d bytes, wanted the three-byte bound at %d", len(kept), closeFailureStackLimit-(utf8.UTFMax-1))
    }
}

/* doublyPanickingCloseValue panics, when rendered, with a value whose own rendering panics: fmt contains the first panic and re-raises the second */
type doublyPanickingCloseValue struct{}

func (instance doublyPanickingCloseValue) Error() string {
    panic(panickingErrorMessage{})
}

type doublyPanickingCloseService struct{}

func (instance *doublyPanickingCloseService) CloseWithContext(closeContext context.Context) error {
    panic(doublyPanickingCloseValue{})
}

func TestContainer_Close_APanicValueWhoseRenderingPanicsIsRecordedInsteadOfEscapingTheTeardown(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister[*doublyPanickingCloseService](serviceContainer, "service.doubly.panics", func(resolver containercontract.Resolver) (*doublyPanickingCloseService, error) {
        return &doublyPanickingCloseService{}, nil
    }, RegisterOptions{})

    MustFromResolver[*doublyPanickingCloseService](t.Context(), serviceContainer, "service.doubly.panics")

    closeErr := serviceContainer.Close()

    typedError, isTyped := closeErr.(*exception.Error)
    if false == isTyped {
        t.Fatalf("expected the close failure to be recorded, got %T %v", closeErr, closeErr)
    }

    failures, _ := typedError.Context()["failures"].(map[string]string)
    if "service close panicked" != failures["service:service.doubly.panics"] {
        t.Fatalf("expected the panic recorded against its service, got %+v", typedError.Context())
    }
}

/* a ring closed through a Lazy handle is refused at the resolution that closes it: the partner's provider resolving its holder would write the edge back, so the handle's resolution fails with ErrCircularDependency, the partner is not memoized, the plan holds no cycle and the holder alone closes */
func TestContainer_ARingClosedThroughALazyHandleIsRefusedAtTheResolutionThatClosesIt(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    registerLazyRing(t, serviceContainer, recorder, true, false, RegisterOptions{})

    holder, getErr := FromResolver[*lazyRingHolder](t.Context(), serviceContainer, "app.lazy.holder")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, resolveErr := holder.partner.Resolve(t.Context()); false == errors.Is(resolveErr, ErrCircularDependency) {
        t.Fatalf("expected the lazy resolution closing the ring refused with ErrCircularDependency, got %v", resolveErr)
    }

    serviceContainer.mutex.Lock()
    _, partnerBuilt := serviceContainer.instances["app.lazy.partner"]
    partnerToHolder := hasDependencyEdge(serviceContainer.dependencyGraph, "service:app.lazy.partner", "service:app.lazy.holder")
    cycleNodeKeys := serviceContainer.teardownPlanLocked().cycleNodeKeys
    serviceContainer.mutex.Unlock()

    if true == partnerBuilt || true == partnerToHolder {
        t.Fatalf("expected neither the partner nor the edge closing the ring recorded, built=%v edge=%v", partnerBuilt, partnerToHolder)
    }

    if 0 != len(cycleNodeKeys) {
        t.Fatalf("expected the plan to hold no cycle, got %v", cycleNodeKeys)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("expected a clean teardown, got %v", closeErr)
    }

    if 1 != len(closeSequence) || "holder" != closeSequence[0] {
        t.Fatalf("expected the holder alone closed, got %v", closeSequence)
    }
}

/* a declared teardown edge counts as an edge of the ring: with the holder declaring the partner, by type as by name, the partner's provider resolving the holder is refused the same way */
func TestContainer_ADeclaredEdgeRefusesTheLazyResolutionThatWouldCloseARing(t *testing.T) {
    for _, declaration := range []struct {
        name   string
        option containercontract.RegisterOptions
    }{
        {name: "byType", option: RegisterOptions{TeardownDependencyTypes: []reflect.Type{reflect.TypeFor[*lazyRingPartner]()}}},
        {name: "byName", option: RegisterOptions{TeardownDependencyNames: []string{"app.lazy.partner"}}},
    } {
        t.Run(declaration.name, func(t *testing.T) {
            serviceContainer := NewContainer(ContainerConfig{}).(*container)

            var mutex sync.Mutex
            closeSequence := make([]string, 0, 2)
            recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

            registerLazyRing(t, serviceContainer, recorder, true, true, declaration.option)

            holder, getErr := FromResolver[*lazyRingHolder](t.Context(), serviceContainer, "app.lazy.holder")
            if nil != getErr {
                t.Fatalf("unexpected get error: %v", getErr)
            }

            if _, resolveErr := holder.partner.Resolve(t.Context()); false == errors.Is(resolveErr, ErrCircularDependency) {
                t.Fatalf("expected the resolution closing the declared ring refused with ErrCircularDependency, got %v", resolveErr)
            }

            if closeErr := serviceContainer.Close(); nil != closeErr {
                t.Fatalf("expected a clean teardown, got %v", closeErr)
            }
        })
    }
}

/* the edge a Lazy handle writes still orders the teardown where it closes no ring: the holder closes before the partner it resolved later, against the latest-first order the creation stamps alone would give */
func TestContainer_Close_ALazyEdgeThatClosesNoRingStillOrders(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    registerLazyRing(t, serviceContainer, recorder, false, false, RegisterOptions{})

    holder, getErr := FromResolver[*lazyRingHolder](t.Context(), serviceContainer, "app.lazy.holder")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, resolveErr := holder.partner.Resolve(t.Context()); nil != resolveErr {
        t.Fatalf("unexpected lazy resolution error: %v", resolveErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("expected a clean teardown, got %v", closeErr)
    }

    if 2 != len(closeSequence) || "holder" != closeSequence[0] || "partner" != closeSequence[1] {
        t.Fatalf("expected the holder closed before the partner its handle resolved, got %v", closeSequence)
    }
}

/* a declared teardown edge closing a ring with an edge already recorded is refused at its declaration, through RegisterOptions by name and by type and through DeclareTeardownDependency, and nothing of the refused declaration is kept */
func TestContainer_ADeclaredRingIsRefusedAtTheDeclaration(t *testing.T) {
    newDeclaredPair := func(t *testing.T) containercontract.Container {
        t.Helper()

        serviceContainer := NewContainer(ContainerConfig{})
        serviceContainer.MustRegister("app.first", func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{}, nil }, RegisterOptions{TeardownDependencyNames: []string{"app.second"}})

        return serviceContainer
    }

    t.Run("RegisterByName", func(t *testing.T) {
        serviceContainer := newDeclaredPair(t)

        registerErr := serviceContainer.Register("app.second", func(_ containercontract.Resolver) (*closeOrderServiceB, error) { return &closeOrderServiceB{}, nil }, RegisterOptions{TeardownDependencyNames: []string{"app.first"}})
        if false == errors.Is(registerErr, ErrCircularDependency) {
            t.Fatalf("expected the declaration closing the ring refused, got %v", registerErr)
        }

        if true == serviceContainer.Has("app.second") {
            t.Fatalf("expected the refused registration not kept")
        }
    })

    t.Run("RegisterByType", func(t *testing.T) {
        serviceContainer := newDeclaredPair(t)

        registerErr := serviceContainer.Register("app.second", func(_ containercontract.Resolver) (*closeOrderServiceB, error) { return &closeOrderServiceB{}, nil }, RegisterOptions{TeardownDependencyTypes: []reflect.Type{reflect.TypeFor[*closeOrderServiceA]()}})
        if false == errors.Is(registerErr, ErrCircularDependency) {
            t.Fatalf("expected the type declaration closing the ring refused, got %v", registerErr)
        }
    })

    t.Run("DeclareTeardownDependency", func(t *testing.T) {
        serviceContainer := newDeclaredPair(t)
        serviceContainer.MustRegister("app.second", func(_ containercontract.Resolver) (*closeOrderServiceB, error) { return &closeOrderServiceB{}, nil }, RegisterOptions{})

        declareErr := serviceContainer.(teardownDependencyDeclarer).DeclareTeardownDependency("app.second", []string{"app.first"})
        if false == errors.Is(declareErr, ErrCircularDependency) {
            t.Fatalf("expected the declaration closing the ring refused, got %v", declareErr)
        }

        concrete := serviceContainer.(*container)
        concrete.mutex.Lock()
        defer concrete.mutex.Unlock()

        if true == hasDependencyEdge(concrete.dependencyGraph, "service:app.second", "service:app.first") || 1 != len(concrete.declaredTeardownEdges) {
            t.Fatalf("expected nothing of the refused declaration kept, got %d declarations", len(concrete.declaredTeardownEdges))
        }
    })

    t.Run("ARegistrationTurningOnAForwardTypeDeclaration", func(t *testing.T) {
        serviceContainer := NewContainer(ContainerConfig{})
        serviceContainer.MustRegister("app.first", func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{}, nil }, RegisterOptions{TeardownDependencyTypes: []reflect.Type{reflect.TypeFor[*closeOrderServiceB]()}})

        registerErr := serviceContainer.Register("app.second", func(_ containercontract.Resolver) (*closeOrderServiceB, error) { return &closeOrderServiceB{}, nil }, RegisterOptions{TeardownDependencyNames: []string{"app.first"}})
        if false == errors.Is(registerErr, ErrCircularDependency) {
            t.Fatalf("expected the registration turning on the type declaration into a ring refused, got %v", registerErr)
        }

        if true == serviceContainer.Has("app.second") || true == serviceContainer.HasType(reflect.TypeFor[*closeOrderServiceB]()) {
            t.Fatalf("expected the refused registration not kept")
        }
    })

    t.Run("AResolutionByTypeAgainstADeclaration", func(t *testing.T) {
        serviceContainer := NewContainer(ContainerConfig{})
        serviceContainer.MustRegister("app.first", func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            if _, getErr := resolver.GetByType(ContextOf(resolver), reflect.TypeFor[*closeOrderServiceB]()); nil != getErr {
                return nil, getErr
            }

            return &closeOrderServiceA{}, nil
        }, RegisterOptions{})
        serviceContainer.MustRegister("app.second", func(_ containercontract.Resolver) (*closeOrderServiceB, error) { return &closeOrderServiceB{}, nil }, RegisterOptions{TeardownDependencyNames: []string{"app.first"}})

        if _, getErr := serviceContainer.Get(t.Context(), "app.first"); false == errors.Is(getErr, ErrCircularDependency) {
            t.Fatalf("expected the resolution by type closing a ring with the declaration refused, got %v", getErr)
        }
    })

    t.Run("AResolutionAgainstADeclaration", func(t *testing.T) {
        serviceContainer := newDeclaredPair(t)
        serviceContainer.MustRegister("app.second", func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            if _, getErr := resolver.Get(ContextOf(resolver), "app.first"); nil != getErr {
                return nil, getErr
            }

            return &closeOrderServiceB{}, nil
        }, RegisterOptions{})

        if _, getErr := serviceContainer.Get(t.Context(), "app.second"); false == errors.Is(getErr, ErrCircularDependency) {
            t.Fatalf("expected the resolution closing a ring with the declaration refused, got %v", getErr)
        }
    })
}

/* a lookup that failed and was handled leaves no edge behind: a provider falling back when an optional collaborator is missing does not depend on it, so that collaborator, registered later, may depend on the provider's service */
func TestContainer_AFailedLookupLeavesNoDependency(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    serviceContainer.MustRegister("app.fallback", func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
        if _, getErr := resolver.Get(ContextOf(resolver), "app.optional"); nil == getErr {
            t.Fatalf("expected the optional collaborator to be missing")
        }

        if _, getErr := resolver.GetByType(ContextOf(resolver), reflect.TypeFor[*closeOrderServiceB]()); nil == getErr {
            t.Fatalf("expected the optional collaborator's type to be missing")
        }

        return &closeOrderServiceA{}, nil
    }, RegisterOptions{})

    MustFromResolver[*closeOrderServiceA](t.Context(), serviceContainer, "app.fallback")

    serviceContainer.mutex.Lock()
    leftEdges := len(serviceContainer.dependencyGraph["service:app.fallback"])
    leftProvisional := len(serviceContainer.provisionalDependencyEdges)
    serviceContainer.mutex.Unlock()

    if 0 != leftEdges || 0 != leftProvisional {
        t.Fatalf("expected the failed lookups to leave nothing, got %d edges and %d provisional entries", leftEdges, leftProvisional)
    }

    serviceContainer.MustRegister("app.optional", func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
        if _, getErr := resolver.Get(ContextOf(resolver), "app.fallback"); nil != getErr {
            return nil, getErr
        }

        return &closeOrderServiceB{}, nil
    }, RegisterOptions{})

    if _, getErr := serviceContainer.Get(t.Context(), "app.optional"); nil != getErr {
        t.Fatalf("expected the late collaborator to depend on the fallback, got %v", getErr)
    }
}

/* an edge two resolutions hold stays while one of them still holds it, and one that acquired it is kept whatever the other answers */
func TestHoldProvisionalEdge_TheLastFailingHolderTakesTheEdgeBackAndAnAcquiredOneStays(t *testing.T) {
    graph := make(map[string]map[string]struct{})
    provisional := make(map[string]map[string]int)

    firstRelease := holdProvisionalEdge(graph, provisional, "service:a", "service:b")
    secondRelease := holdProvisionalEdge(graph, provisional, "service:a", "service:b")

    firstRelease(false)
    if false == hasDependencyEdge(graph, "service:a", "service:b") {
        t.Fatalf("expected the edge kept while another resolution holds it")
    }

    secondRelease(false)
    if true == hasDependencyEdge(graph, "service:a", "service:b") || 0 != len(provisional) {
        t.Fatalf("expected the last failing holder to take the edge back")
    }

    acquiringRelease := holdProvisionalEdge(graph, provisional, "service:a", "service:b")
    failingRelease := holdProvisionalEdge(graph, provisional, "service:a", "service:b")

    acquiringRelease(true)
    failingRelease(false)
    if false == hasDependencyEdge(graph, "service:a", "service:b") || 0 != len(provisional) {
        t.Fatalf("expected an acquired edge kept whatever the other holder answers")
    }

    if release := holdProvisionalEdge(graph, provisional, "service:a", "service:b"); nil != release {
        release(false)
    }

    if false == hasDependencyEdge(graph, "service:a", "service:b") {
        t.Fatalf("expected an acquired edge not held again, so a failing resolution cannot take it back")
    }
}

/* a declaration acquires the edge a resolution in flight holds, so that resolution failing does not take the declared ordering back */
func TestContainer_ADeclarationAcquiresAnEdgeAFailingResolutionHolds(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    serviceContainer.mutex.Lock()
    defer serviceContainer.mutex.Unlock()

    release := holdProvisionalEdge(serviceContainer.dependencyGraph, serviceContainer.provisionalDependencyEdges, "service:app.first", "service:app.second")
    serviceContainer.recordDeclaredTeardownEdgeLocked(declaredNameEdge("app.first", "app.second"))
    release(false)

    if false == hasDependencyEdge(serviceContainer.dependencyGraph, "service:app.first", "service:app.second") {
        t.Fatalf("expected the declared edge kept after the resolution holding it failed")
    }
}

/* an edge resolved through a type standing for one name is that name's, so a second name registered under the type afterwards cannot unfold it and let a ring through */
func TestContainer_ASecondNameUnderATypeDoesNotUnfoldAResolvedEdge(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegister("app.holder", func(resolver containercontract.Resolver) (*lazyRingHolder, error) {
        return &lazyRingHolder{partnerByType: LazyByType[*lazyRingPartner](resolver)}, nil
    }, RegisterOptions{TypeRegistrationDisabled: true})

    serviceContainer.MustRegister("app.partner", func(resolver containercontract.Resolver) (*lazyRingPartner, error) {
        return &lazyRingPartner{holderHandle: Lazy[*lazyRingHolder](resolver, "app.holder")}, nil
    }, RegisterOptions{TypeRegistrationRelaxed: true})

    partner := MustFromResolver[*lazyRingPartner](t.Context(), serviceContainer, "app.partner")
    holder := MustFromResolver[*lazyRingHolder](t.Context(), serviceContainer, "app.holder")

    if _, resolveErr := holder.partnerByType.Resolve(t.Context()); nil != resolveErr {
        t.Fatalf("unexpected lazy resolution error: %v", resolveErr)
    }

    serviceContainer.MustRegister("app.partner.second", func(_ containercontract.Resolver) (*lazyRingPartner, error) { return &lazyRingPartner{}, nil }, RegisterOptions{TypeRegistrationRelaxed: true})

    if _, resolveErr := partner.holderHandle.Resolve(t.Context()); false == errors.Is(resolveErr, ErrCircularDependency) {
        t.Fatalf("expected the ring closed after the second name refused with ErrCircularDependency, got %v", resolveErr)
    }

    dependentContainer := NewContainer(ContainerConfig{})

    dependentContainer.MustRegister("app.holder", func(resolver containercontract.Resolver) (*lazyRingHolder, error) {
        return &lazyRingHolder{partner: Lazy[*lazyRingPartner](resolver, "app.partner")}, nil
    }, RegisterOptions{TypeRegistrationDisabled: true})

    dependentContainer.MustRegister("app.partner", func(resolver containercontract.Resolver) (*lazyRingPartner, error) {
        holder, getErr := FromResolver[*lazyRingHolder](ContextOf(resolver), resolver, "app.holder")
        if nil != getErr {
            return nil, getErr
        }

        return &lazyRingPartner{holder: holder}, nil
    }, RegisterOptions{TypeRegistrationRelaxed: true})

    builtPartner := MustFromResolverByType[*lazyRingPartner](t.Context(), dependentContainer)

    dependentContainer.MustRegister("app.partner.second", func(_ containercontract.Resolver) (*lazyRingPartner, error) { return &lazyRingPartner{}, nil }, RegisterOptions{TypeRegistrationRelaxed: true})

    if _, resolveErr := builtPartner.holder.partner.Resolve(t.Context()); false == errors.Is(resolveErr, ErrCircularDependency) {
        t.Fatalf("expected the ring closed onto a service built through its type refused with ErrCircularDependency, got %v", resolveErr)
    }
}

/* a type declaration counts as an edge only to the very type it declares: a same-named type of another scope sharing the key of a registered one orders nothing, so it closes no ring with that service */
func TestContainer_ADeclarationOnASameNamedTypeClosesNoRing(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegister("app.alpha", localCollisionProviderFirst(), RegisterOptions{TeardownDependencyNames: []string{"app.declarer"}})

    sameNamedType := reflect.TypeOf(localCollisionProviderSecond()).Out(0)

    registerErr := serviceContainer.Register("app.declarer", func(_ containercontract.Resolver) (*closeOrderServiceB, error) { return &closeOrderServiceB{}, nil }, RegisterOptions{TypeRegistrationDisabled: true, TeardownDependencyTypes: []reflect.Type{sameNamedType}})
    if nil != registerErr {
        t.Fatalf("expected a declaration on a type nothing is registered under admitted, got %v", registerErr)
    }

    reversedContainer := NewContainer(ContainerConfig{})
    reversedContainer.MustRegister("app.declarer", func(_ containercontract.Resolver) (*closeOrderServiceB, error) { return &closeOrderServiceB{}, nil }, RegisterOptions{TypeRegistrationDisabled: true, TeardownDependencyTypes: []reflect.Type{sameNamedType}})

    if registerErr := reversedContainer.Register("app.alpha", localCollisionProviderFirst(), RegisterOptions{TeardownDependencyNames: []string{"app.declarer"}}); nil != registerErr {
        t.Fatalf("expected the registration of a same-named type the declaration does not name admitted, got %v", registerErr)
    }
}

func TestContainer_DeclareTeardownDependency_OrdersAServiceAgainstOneItCaptured(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    serviceContainer.MustRegister("app.bus", func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{recorder: recorder}, nil }, RegisterOptions{})
    serviceContainer.MustRegister("app.transports", func(_ containercontract.Resolver) (*closeOrderServiceB, error) { return &closeOrderServiceB{recorder: recorder}, nil }, RegisterOptions{})

    /* the bus is built first, so creation order alone would close the transports before it */
    MustFromResolver[*closeOrderServiceA](t.Context(), serviceContainer, "app.bus")
    MustFromResolver[*closeOrderServiceB](t.Context(), serviceContainer, "app.transports")

    if declareErr := serviceContainer.(teardownDependencyDeclarer).DeclareTeardownDependency("app.bus", []string{"app.transports"}); nil != declareErr {
        t.Fatalf("unexpected declare error: %v", declareErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 2 != len(closeSequence) || "a" != closeSequence[0] || "b" != closeSequence[1] {
        t.Fatalf("expected the declaring service closed before its dependency, got %v", closeSequence)
    }
}

func TestContainer_DeclareTeardownDependency_RefusesWhatTheOptionRefuses(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    serviceContainer.MustRegister("app.bus", func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{}, nil }, RegisterOptions{})

    declarer := serviceContainer.(teardownDependencyDeclarer)

    cases := []struct {
        serviceName    string
        dependencyName string
        cause          error
    }{
        {serviceName: "app.unknown", dependencyName: "app.bus", cause: ErrTeardownDependencyWasNeverRegistered},
        {serviceName: "app.bus", dependencyName: "", cause: ErrTeardownDependencyNameIsRequired},
        {serviceName: "app.bus", dependencyName: "app.bus", cause: ErrTeardownDependencyIsSelf},
    }

    for _, testCase := range cases {
        if declareErr := declarer.DeclareTeardownDependency(testCase.serviceName, []string{testCase.dependencyName}); false == errors.Is(declareErr, testCase.cause) {
            t.Fatalf("expected %v for %q -> %q, got %v", testCase.cause, testCase.serviceName, testCase.dependencyName, declareErr)
        }
    }

    if verifyErr := serviceContainer.(declaredTeardownVerifier).VerifyDeclaredTeardownDependencies(); nil != verifyErr {
        t.Fatalf("unexpected verify error: %v", verifyErr)
    }

    if declareErr := declarer.DeclareTeardownDependency("app.bus", []string{"app.never"}); nil != declareErr {
        t.Fatalf("expected a dependency nothing registered yet to be admitted at the door, got %v", declareErr)
    }

    if verifyErr := serviceContainer.(declaredTeardownVerifier).VerifyDeclaredTeardownDependencies(); false == errors.Is(verifyErr, ErrTeardownDependencyWasNeverRegistered) {
        t.Fatalf("expected the verification to refuse a dependency nothing registered, got %v", verifyErr)
    }

    if closeErr := serviceContainer.Close(); false == errors.Is(closeErr, ErrTeardownDependencyWasNeverRegistered) {
        t.Fatalf("expected the teardown to report the dependency nothing registered, got %v", closeErr)
    }

    if declareErr := declarer.DeclareTeardownDependency("app.bus", []string{"app.other"}); false == errors.Is(declareErr, ErrContainerClosed) {
        t.Fatalf("expected a closed container to refuse the declaration, got %v", declareErr)
    }
}

func TestContainer_DeclareTeardownDependency_NilDeclaresNothingAndARefusedSliceRecordsNone(t *testing.T) {
    for _, testCase := range []struct {
        name            string
        dependencyNames []string
        expectedCause   error
    }{
        {name: "nil", dependencyNames: nil, expectedCause: nil},
        {name: "second name refused", dependencyNames: []string{"app.transports", ""}, expectedCause: ErrTeardownDependencyNameIsRequired},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            serviceContainer := NewContainer(ContainerConfig{TeardownSequential: true})

            var mutex sync.Mutex
            closeSequence := make([]string, 0, 2)
            recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

            serviceContainer.MustRegister("app.bus", func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{recorder: recorder}, nil }, RegisterOptions{})
            serviceContainer.MustRegister("app.transports", func(_ containercontract.Resolver) (*closeOrderServiceB, error) { return &closeOrderServiceB{recorder: recorder}, nil }, RegisterOptions{})

            MustFromResolver[*closeOrderServiceA](t.Context(), serviceContainer, "app.bus")
            MustFromResolver[*closeOrderServiceB](t.Context(), serviceContainer, "app.transports")

            declarer := serviceContainer.(teardownDependencyDeclarer)

            if declareErr := declarer.DeclareTeardownDependency("app.unknown", nil); false == errors.Is(declareErr, ErrTeardownDependencyWasNeverRegistered) {
                t.Fatalf("expected an unregistered declaring service refused with nil names, got %v", declareErr)
            }

            declareErr := declarer.DeclareTeardownDependency("app.bus", testCase.dependencyNames)
            if nil == testCase.expectedCause && nil != declareErr {
                t.Fatalf("unexpected declare error: %v", declareErr)
            }

            if nil != testCase.expectedCause && false == errors.Is(declareErr, testCase.expectedCause) {
                t.Fatalf("expected %v, got %v", testCase.expectedCause, declareErr)
            }

            if closeErr := serviceContainer.Close(); nil != closeErr {
                t.Fatalf("unexpected close error: %v", closeErr)
            }

            if 2 != len(closeSequence) || "b" != closeSequence[0] || "a" != closeSequence[1] {
                t.Fatalf("expected no edge recorded, the newest closed first, got %v", closeSequence)
            }
        })
    }
}

type plainCloseOnlyService struct {
    closeCalls atomic.Int32
    closeErr   error
}

func (instance *plainCloseOnlyService) Close() error {
    instance.closeCalls.Add(1)

    return instance.closeErr
}

type plainCloseOnlyHolder interface {
    Close() error
}

func TestRegister_AConcreteTypeCarryingCloseAloneIsRefusedAtEveryRegistrationDoor(t *testing.T) {
    provider := func(_ containercontract.Resolver) (*plainCloseOnlyService, error) {
        return &plainCloseOnlyService{}, nil
    }
    serviceType := reflect.TypeFor[*plainCloseOnlyService]()

    doors := map[string]func(serviceContainer containercontract.Container) (error, bool){
        "Register[T]": func(serviceContainer containercontract.Container) (error, bool) {
            registerErr := Register[*plainCloseOnlyService](serviceContainer, "plain.close", provider, RegisterOptions{})

            return registerErr, serviceContainer.Has("plain.close") || serviceContainer.HasType(serviceType)
        },
        "RegisterType[T]": func(serviceContainer containercontract.Container) (error, bool) {
            registerErr := RegisterType[*plainCloseOnlyService](serviceContainer, provider, RegisterOptions{})

            return registerErr, serviceContainer.HasType(serviceType)
        },
        "Registrar.Register": func(serviceContainer containercontract.Container) (error, bool) {
            registerErr := serviceContainer.Register("plain.close", provider, RegisterOptions{})

            return registerErr, serviceContainer.Has("plain.close") || serviceContainer.HasType(serviceType)
        },
        "RegisterScoped[T]": func(serviceContainer containercontract.Container) (error, bool) {
            registerErr := RegisterScoped[*plainCloseOnlyService](serviceContainer, "plain.close", provider, RegisterOptions{})
            scope := serviceContainer.NewScope()
            defer func() {
                _ = scope.Close()
            }()

            return registerErr, scope.Has("plain.close") || scope.HasType(serviceType)
        },
        "scope RegisterScoped": func(serviceContainer containercontract.Container) (error, bool) {
            scope := serviceContainer.NewScope()
            defer func() {
                _ = scope.Close()
            }()

            registerErr := scope.(containercontract.ScopedRegistrar).RegisterScoped("plain.close", provider, RegisterOptions{})

            return registerErr, scope.Has("plain.close") || scope.HasType(serviceType)
        },
    }

    for door, register := range doors {
        registerErr, leftRegistered := register(NewContainer(ContainerConfig{}))
        if false == errors.Is(registerErr, ErrCloseDoorRequired) {
            t.Fatalf("%s: expected ErrCloseDoorRequired, got %v", door, registerErr)
        }

        if true == leftRegistered {
            t.Fatalf("%s: expected nothing registered under the refused name or type", door)
        }

        renderedContext := fmt.Sprintf("%v", registerErr.(*exception.Error).Context())
        if false == strings.Contains(renderedContext, "plainCloseOnlyService") {
            t.Fatalf("%s: expected the refusal to name the type, got %s", door, renderedContext)
        }
    }
}

func TestRegister_ACloseOnlyTypeIsAdmittedWithACloserAndClosedThroughIt(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    service := &plainCloseOnlyService{}

    closerCalls := 0
    MustRegister[*plainCloseOnlyService](
        serviceContainer,
        "plain.close",
        func(_ containercontract.Resolver) (*plainCloseOnlyService, error) {
            return service, nil
        },
        RegisterOptions{Closer: NewCloser(func(_ context.Context, value *plainCloseOnlyService) error {
            closerCalls++

            return nil
        })},
    )

    if _, getErr := serviceContainer.Get(t.Context(), "plain.close"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != closerCalls || 0 != service.closeCalls.Load() {
        t.Fatalf("expected the closer once and the plain Close never, got closer=%d Close=%d", closerCalls, service.closeCalls.Load())
    }
}

func TestRegister_ATypeCarryingBothDoorsIsAdmittedAndClosedThroughTheContextDoor(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    service := &contextCloseRecorder{}

    MustRegister[*contextCloseRecorder](serviceContainer, "both.doors", func(_ containercontract.Resolver) (*contextCloseRecorder, error) {
        return service, nil
    }, RegisterOptions{})

    if _, getErr := serviceContainer.Get(t.Context(), "both.doors"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }
    if 1 != service.contextCalls || 0 != service.plainCalls {
        t.Fatalf("expected the context door once and Close never, got CloseWithContext=%d Close=%d", service.contextCalls, service.plainCalls)
    }
}

func TestOverrideInstance_AValueCarryingCloseAloneIsRefusedUnlessTheRegistrationsCloserTakesIt(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    MustRegister[*contextCloseRecorder](serviceContainer, "overridden", func(_ containercontract.Resolver) (*contextCloseRecorder, error) {
        return &contextCloseRecorder{}, nil
    }, RegisterOptions{TypeRegistrationDisabled: true})

    overrideErr := serviceContainer.OverrideInstance("overridden", &plainCloseOnlyService{}, containercontract.OverrideOptions{})
    if false == errors.Is(overrideErr, ErrCloseDoorRequired) {
        t.Fatalf("expected the override refused with ErrCloseDoorRequired, got %v", overrideErr)
    }

    closedContainer := NewContainer(ContainerConfig{})
    MustRegister[plainCloseOnlyHolder](closedContainer, "overridden", func(_ containercontract.Resolver) (plainCloseOnlyHolder, error) {
        return &plainCloseOnlyService{}, nil
    }, RegisterOptions{TypeRegistrationDisabled: true, Closer: NewCloser(func(_ context.Context, value plainCloseOnlyHolder) error {
        return value.Close()
    })})

    if overrideErr := closedContainer.OverrideInstance("overridden", &plainCloseOnlyService{}, containercontract.OverrideOptions{}); nil != overrideErr {
        t.Fatalf("expected the closer to admit the override, got %v", overrideErr)
    }
}

func TestScopeOverrideInstance_AValueCarryingCloseAloneIsRefusedOnlyWhenTheScopeClosesIt(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    MustRegister[*contextCloseRecorder](serviceContainer, "overridden", func(_ containercontract.Resolver) (*contextCloseRecorder, error) {
        return &contextCloseRecorder{}, nil
    }, RegisterOptions{TypeRegistrationDisabled: true})

    scope := serviceContainer.NewScope()
    defer func() {
        _ = scope.Close()
    }()

    overrideErr := scope.OverrideInstance("overridden", &plainCloseOnlyService{}, containercontract.OverrideOptions{ClosedWithScope: true})
    if false == errors.Is(overrideErr, ErrCloseDoorRequired) {
        t.Fatalf("expected the override closed with the scope refused with ErrCloseDoorRequired, got %v", overrideErr)
    }

    if overrideErr := scope.OverrideInstance("overridden", &plainCloseOnlyService{}, containercontract.OverrideOptions{}); nil != overrideErr {
        t.Fatalf("expected an override its installer keeps admitted, got %v", overrideErr)
    }
}

func TestResolve_ACloseOnlyValueBehindAnInterfaceProviderIsRefusedAtCreationAndClosedOnce(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    built := make([]*plainCloseOnlyService, 0, 2)

    MustRegister[plainCloseOnlyHolder](serviceContainer, "interface.typed", func(_ containercontract.Resolver) (plainCloseOnlyHolder, error) {
        service := &plainCloseOnlyService{}
        built = append(built, service)

        return service, nil
    }, RegisterOptions{TypeRegistrationDisabled: true})

    _, getErr := serviceContainer.Get(t.Context(), "interface.typed")
    if false == errors.Is(getErr, ErrCloseDoorRequired) {
        t.Fatalf("expected the creation refused with ErrCloseDoorRequired, got %v", getErr)
    }

    if 1 != len(built) || 1 != built[0].closeCalls.Load() {
        t.Fatalf("expected the refused value closed exactly once, got %d values", len(built))
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != built[0].closeCalls.Load() {
        t.Fatalf("expected the teardown not to close a refused value again, got %d", built[0].closeCalls.Load())
    }
}

func TestResolve_TheRefusalOfACloseOnlyValueCarriesTheFailureOfItsClose(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    closeFailure := errors.New("the descriptor was already gone")

    MustRegister[plainCloseOnlyHolder](serviceContainer, "interface.typed", func(_ containercontract.Resolver) (plainCloseOnlyHolder, error) {
        return &plainCloseOnlyService{closeErr: closeFailure}, nil
    }, RegisterOptions{TypeRegistrationDisabled: true})

    _, getErr := serviceContainer.Get(t.Context(), "interface.typed")
    if false == errors.Is(getErr, ErrCloseDoorRequired) || false == errors.Is(getErr, closeFailure) {
        t.Fatalf("expected the refusal to carry both the sentinel and the close failure, got %v", getErr)
    }
}

func TestResolve_ACloseOnlyValueBehindAnInterfaceProviderIsAdmittedWhenTheCloserTakesIt(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    service := &plainCloseOnlyService{}

    MustRegister[plainCloseOnlyHolder](serviceContainer, "interface.typed", func(_ containercontract.Resolver) (plainCloseOnlyHolder, error) {
        return service, nil
    }, RegisterOptions{TypeRegistrationDisabled: true, Closer: NewCloser(func(_ context.Context, value plainCloseOnlyHolder) error {
        return value.Close()
    })})

    if _, getErr := serviceContainer.Get(t.Context(), "interface.typed"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != service.closeCalls.Load() {
        t.Fatalf("expected the closer to close the value once, got %d", service.closeCalls.Load())
    }
}

func TestScopeResolve_ACloseOnlyValueBehindAnInterfaceProviderIsRefusedAtCreationAndClosedOnce(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    built := make([]*plainCloseOnlyService, 0, 1)

    MustRegisterScoped[plainCloseOnlyHolder](serviceContainer, "scoped.interface.typed", func(_ containercontract.Resolver) (plainCloseOnlyHolder, error) {
        service := &plainCloseOnlyService{}
        built = append(built, service)

        return service, nil
    }, RegisterOptions{TypeRegistrationDisabled: true})

    scope := serviceContainer.NewScope()

    _, getErr := scope.Get(t.Context(), "scoped.interface.typed")
    if false == errors.Is(getErr, ErrCloseDoorRequired) {
        t.Fatalf("expected the scoped creation refused with ErrCloseDoorRequired, got %v", getErr)
    }

    if closeErr := scope.Close(); nil != closeErr {
        t.Fatalf("unexpected scope close error: %v", closeErr)
    }

    if 1 != len(built) || 1 != built[0].closeCalls.Load() {
        t.Fatalf("expected the refused scoped value closed exactly once, got %d values", len(built))
    }
}

func newDeclarationProbeRecorder() (*closeOrderRecorder, *[]string) {
    var mutex sync.Mutex
    closeSequence := make([]string, 0, 4)

    return &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}, &closeSequence
}

func teardownDependenciesOf(t *testing.T, serviceContainer containercontract.Container, nodeKey string) []string {
    t.Helper()

    for _, entry := range serviceContainer.(teardownPlanner).TeardownPlan() {
        if nodeKey == entry.NodeKey {
            return entry.Dependencies
        }
    }

    t.Fatalf("expected %s in the teardown plan", nodeKey)

    return nil
}

func TestContainer_Close_ADanglingDeclarationClosesSequentiallyAndReportsTheRefusal(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerWaveMatePair(t, serviceContainer, 50*time.Millisecond)

    recorder, _ := newDeclarationProbeRecorder()
    serviceContainer.MustRegister(
        "app.reporter",
        func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{recorder: recorder}, nil },
        RegisterOptions{TypeRegistrationDisabled: true, TeardownDependencyNames: []string{"app.missing"}},
    )
    MustFromResolver[*closeOrderServiceA](t.Context(), serviceContainer, "app.reporter")

    closeErr := serviceContainer.Close()
    if false == errors.Is(closeErr, ErrTeardownDependencyWasNeverRegistered) {
        t.Fatalf("expected the teardown to carry the refusal of the dangling declaration, got %v", closeErr)
    }

    if false == strings.Contains(closeErr.Error(), "failed to close container services") {
        t.Fatalf("expected the mates to have closed one after the other and report it, got %v", closeErr)
    }
}

func TestContainer_Close_ADanglingDeclarationAloneIsTheAnswerAndEveryServiceCloses(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    recorder, closeSequence := newDeclarationProbeRecorder()
    serviceContainer.MustRegister(
        "app.reporter",
        func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{recorder: recorder}, nil },
        RegisterOptions{TypeRegistrationDisabled: true, TeardownDependencyNames: []string{"app.missing"}},
    )
    serviceContainer.MustRegister(
        "app.storage",
        func(_ containercontract.Resolver) (*closeOrderServiceB, error) { return &closeOrderServiceB{recorder: recorder}, nil },
        RegisterOptions{TypeRegistrationDisabled: true},
    )
    MustFromResolver[*closeOrderServiceA](t.Context(), serviceContainer, "app.reporter")
    MustFromResolver[*closeOrderServiceB](t.Context(), serviceContainer, "app.storage")

    closeErr := serviceContainer.Close()
    if false == errors.Is(closeErr, ErrTeardownDependencyWasNeverRegistered) {
        t.Fatalf("expected the refusal as the teardown's answer, got %v", closeErr)
    }

    refusalContext := exception.LogContext(closeErr, exception.LogContextConfig{})
    if "app.reporter" != refusalContext["serviceName"] || "app.missing" != refusalContext["dependency"] {
        t.Fatalf("expected the refusal to name the declaration, got %v", refusalContext)
    }

    if 2 != len(*closeSequence) {
        t.Fatalf("expected both services closed, got %v", *closeSequence)
    }
}

func TestContainer_TeardownSequential_AdmitsADanglingDeclaration(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{TeardownSequential: true})

    recorder, closeSequence := newDeclarationProbeRecorder()
    serviceContainer.MustRegister(
        "app.reporter",
        func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{recorder: recorder}, nil },
        RegisterOptions{TypeRegistrationDisabled: true, TeardownDependencyNames: []string{"app.missing"}},
    )
    MustFromResolver[*closeOrderServiceA](t.Context(), serviceContainer, "app.reporter")

    verifyDeclaredTeardownDependencies(t, serviceContainer)

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != len(*closeSequence) {
        t.Fatalf("expected the service closed, got %v", *closeSequence)
    }
}

func TestContainer_Register_AnOptionalTeardownDependencyOnARegisteredServiceOrdersTheTeardown(t *testing.T) {
    for _, testCase := range []struct {
        name    string
        options RegisterOptions
    }{
        {name: "name", options: RegisterOptions{TypeRegistrationDisabled: true, OptionalTeardownDependencyNames: []string{"app.storage"}}},
        {name: "type", options: RegisterOptions{TypeRegistrationDisabled: true, OptionalTeardownDependencyTypes: []reflect.Type{reflect.TypeFor[*closeOrderServiceB]()}}},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            serviceContainer := NewContainer(ContainerConfig{})

            recorder, closeSequence := newDeclarationProbeRecorder()
            serviceContainer.MustRegister(
                "app.reporter",
                func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{recorder: recorder}, nil },
                testCase.options,
            )
            serviceContainer.MustRegister(
                "app.storage",
                func(_ containercontract.Resolver) (*closeOrderServiceB, error) { return &closeOrderServiceB{recorder: recorder}, nil },
                RegisterOptions{},
            )

            /* the storage is built first, so the creation order alone would close it last; the edge is what this pins */
            MustFromResolver[*closeOrderServiceB](t.Context(), serviceContainer, "app.storage")
            MustFromResolver[*closeOrderServiceA](t.Context(), serviceContainer, "app.reporter")

            verifyDeclaredTeardownDependencies(t, serviceContainer)

            dependencies := teardownDependenciesOf(t, serviceContainer, "service:app.reporter")
            if 1 != len(dependencies) || "service:app.storage" != dependencies[0] {
                t.Fatalf("expected the reporter ordered before the storage, got %v", dependencies)
            }

            if closeErr := serviceContainer.Close(); nil != closeErr {
                t.Fatalf("unexpected close error: %v", closeErr)
            }

            if 2 != len(*closeSequence) || "a" != (*closeSequence)[0] || "b" != (*closeSequence)[1] {
                t.Fatalf("expected the reporter closed before the storage, got %v", *closeSequence)
            }
        })
    }
}

func TestContainer_VerifyDeclaredTeardownDependencies_AdmitsAnOptionalDependencyNothingRegistered(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    recorder, closeSequence := newDeclarationProbeRecorder()
    serviceContainer.MustRegister(
        "app.reporter",
        func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{recorder: recorder}, nil },
        RegisterOptions{
            TypeRegistrationDisabled:        true,
            OptionalTeardownDependencyNames: []string{"app.missing"},
            OptionalTeardownDependencyTypes: []reflect.Type{reflect.TypeFor[*closeOrderServiceB]()},
        },
    )
    MustFromResolver[*closeOrderServiceA](t.Context(), serviceContainer, "app.reporter")

    verifyDeclaredTeardownDependencies(t, serviceContainer)

    if 0 != len(teardownDependenciesOf(t, serviceContainer, "service:app.reporter")) {
        t.Fatalf("expected the optional declarations to order nothing")
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != len(*closeSequence) {
        t.Fatalf("expected the service closed, got %v", *closeSequence)
    }
}

func TestContainer_Register_RefusesWhatTheOptionalTeardownDependenciesRefuse(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        scoped   bool
        options  RegisterOptions
        expected error
    }{
        {name: "empty name", options: RegisterOptions{OptionalTeardownDependencyNames: []string{""}}, expected: ErrTeardownDependencyNameIsRequired},
        {name: "own name", options: RegisterOptions{OptionalTeardownDependencyNames: []string{"app.declarer"}}, expected: ErrTeardownDependencyIsSelf},
        {name: "nil type", options: RegisterOptions{OptionalTeardownDependencyTypes: []reflect.Type{nil}}, expected: ErrTeardownDependencyTypeIsRequired},
        {name: "own type", options: RegisterOptions{OptionalTeardownDependencyTypes: []reflect.Type{reflect.TypeFor[*closeOrderServiceA]()}}, expected: ErrTeardownDependencyIsSelf},
        {name: "scoped name", options: RegisterOptions{OptionalTeardownDependencyNames: []string{"scoped.store"}}, expected: ErrTeardownDependencyIsScoped},
        {name: "on a scoped registration", scoped: true, options: RegisterOptions{OptionalTeardownDependencyNames: []string{"app.other"}}, expected: ErrScopedTeardownDependencyUnsupported},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            serviceContainer := NewContainer(ContainerConfig{})
            serviceContainer.MustRegisterScoped(
                "scoped.store",
                func(_ containercontract.Resolver) (*closeOrderServiceB, error) { return &closeOrderServiceB{}, nil },
                RegisterOptions{TypeRegistrationDisabled: true},
            )

            provider := func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{}, nil }

            var registerErr error
            if true == testCase.scoped {
                registerErr = serviceContainer.RegisterScoped("app.declarer", provider, testCase.options)
            } else {
                registerErr = serviceContainer.Register("app.declarer", provider, testCase.options)
            }

            if false == errors.Is(registerErr, testCase.expected) {
                t.Fatalf("expected %v, got %v", testCase.expected, registerErr)
            }

            if true == serviceContainer.Has("app.declarer") {
                t.Fatalf("expected the refused registration to leave nothing behind")
            }
        })
    }
}

func TestContainer_Register_RefusesARingClosedThroughAnOptionalTeardownDependency(t *testing.T) {
    for _, testCase := range []struct {
        name   string
        first  RegisterOptions
        second RegisterOptions
    }{
        {name: "recorded", first: RegisterOptions{TypeRegistrationDisabled: true, OptionalTeardownDependencyNames: []string{"app.second"}}, second: RegisterOptions{TypeRegistrationDisabled: true, TeardownDependencyNames: []string{"app.first"}}},
        {name: "closing", first: RegisterOptions{TypeRegistrationDisabled: true, TeardownDependencyNames: []string{"app.second"}}, second: RegisterOptions{TypeRegistrationDisabled: true, OptionalTeardownDependencyNames: []string{"app.first"}}},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            serviceContainer := NewContainer(ContainerConfig{})

            serviceContainer.MustRegister(
                "app.first",
                func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{}, nil },
                testCase.first,
            )

            registerErr := serviceContainer.Register(
                "app.second",
                func(_ containercontract.Resolver) (*closeOrderServiceB, error) { return &closeOrderServiceB{}, nil },
                testCase.second,
            )
            if false == errors.Is(registerErr, ErrCircularDependency) {
                t.Fatalf("expected the ring through the optional declaration refused, got %v", registerErr)
            }
        })
    }
}

func TestContainer_VerifyDeclaredTeardownDependencies_RefusesAnOptionalDeclarationThatNamedAScopedServiceRegisteredAfterIt(t *testing.T) {
    for _, options := range []RegisterOptions{
        {TypeRegistrationDisabled: true, OptionalTeardownDependencyNames: []string{"app.scoped"}},
        {TypeRegistrationDisabled: true, OptionalTeardownDependencyTypes: []reflect.Type{reflect.TypeFor[*scopedOnlyService]()}},
    } {
        serviceContainer := NewContainer(ContainerConfig{})

        serviceContainer.MustRegister("app.declarer", func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{}, nil }, options)
        serviceContainer.MustRegisterScoped("app.scoped", func(_ containercontract.Resolver) (*scopedOnlyService, error) { return &scopedOnlyService{label: "scoped"}, nil }, RegisterOptions{})

        if verifyErr := serviceContainer.(declaredTeardownVerifier).VerifyDeclaredTeardownDependencies(); false == errors.Is(verifyErr, ErrTeardownDependencyIsScoped) {
            t.Fatalf("expected the verification to refuse the optional declaration on a scoped service, got %v", verifyErr)
        }

        if closeErr := serviceContainer.Close(); false == errors.Is(closeErr, ErrTeardownDependencyIsScoped) {
            t.Fatalf("expected the teardown to report the optional declaration on a scoped service, got %v", closeErr)
        }
    }
}

func TestContainer_Register_RefusesADeclaredDependencyOnARegisteredScopedType(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegisterScoped("app.scoped", func(_ containercontract.Resolver) (*scopedOnlyService, error) { return &scopedOnlyService{label: "scoped"}, nil }, RegisterOptions{})

    registerErr := serviceContainer.Register(
        "app.declarer",
        func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{}, nil },
        RegisterOptions{TeardownDependencyTypes: []reflect.Type{reflect.TypeFor[*scopedOnlyService]()}},
    )
    if false == errors.Is(registerErr, ErrTeardownDependencyIsScoped) {
        t.Fatalf("expected the type declaration on a registered scoped type to be refused at the registration, got %v", registerErr)
    }
}

type foreignConnectionWithoutCloseDoor struct {
    closed atomic.Int32
}

func TestContainerClose_AClosersRegistrationDoesNotHangOnHowTheNamesOfOneInstanceSort(t *testing.T) {
    for _, names := range [][2]string{{"db", "db.readonly"}, {"zdb", "alias"}} {
        serviceContainer := NewContainer(ContainerConfig{})
        connection := &foreignConnectionWithoutCloseDoor{}
        provider := func(containercontract.Resolver) (*foreignConnectionWithoutCloseDoor, error) {
            return connection, nil
        }

        MustRegister(serviceContainer, names[0], provider, containercontract.RegisterOptions{
            TypeRegistrationDisabled: true,
            Closer: NewCloser(func(closeContext context.Context, value *foreignConnectionWithoutCloseDoor) error {
                value.closed.Add(1)

                return nil
            }),
        })
        MustRegister(serviceContainer, names[1], provider, containercontract.RegisterOptions{TypeRegistrationDisabled: true})

        for _, name := range names {
            if _, getErr := serviceContainer.Get(context.Background(), name); nil != getErr {
                t.Fatalf("unexpected resolve error: %v", getErr)
            }
        }

        if closeErr := serviceContainer.Close(); nil != closeErr {
            t.Fatalf("names %v: unexpected close error: %v", names, closeErr)
        }

        if 1 != connection.closed.Load() {
            t.Fatalf("names %v: expected the declared closer to run once, ran %d", names, connection.closed.Load())
        }
    }
}

func TestContainerResolve_AProviderLeavingThroughGoexitAnswersTheWaiterAndRunsAgain(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    entered := make(chan struct{})
    release := make(chan struct{})
    var calls atomic.Int32

    MustRegister(serviceContainer, "goexit", func(containercontract.Resolver) (*foreignConnectionWithoutCloseDoor, error) {
        if 1 == calls.Add(1) {
            close(entered)
            <-release
            runtime.Goexit()
        }

        return &foreignConnectionWithoutCloseDoor{}, nil
    }, containercontract.RegisterOptions{TypeRegistrationDisabled: true})

    creatorDone := make(chan struct{})
    go func() {
        defer close(creatorDone)
        _, _ = serviceContainer.Get(context.Background(), "goexit")
    }()
    <-entered

    waiterDone := make(chan error, 1)
    go func() {
        _, getErr := serviceContainer.Get(context.Background(), "goexit")
        waiterDone <- getErr
    }()

    time.Sleep(20 * time.Millisecond)
    close(release)
    <-creatorDone

    select {
    case <-waiterDone:
    case <-time.After(5 * time.Second):
        t.Fatal("the waiter must not stay parked on a provider that left through Goexit")
    }

    if _, getErr := serviceContainer.Get(context.Background(), "goexit"); nil != getErr {
        t.Fatalf("expected a later resolve to run the provider again, got %v", getErr)
    }
}
