package container

import (
    "context"
    "errors"
    "fmt"
    "reflect"
    "runtime"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
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

type circularServiceA struct{}

type circularServiceB struct{}

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

type panickingErrorMessage struct{}

func (instance panickingErrorMessage) Error() string {
    panic("boom from Error()")
}

type panickingErrorMessageService struct{}

func (instance *panickingErrorMessageService) Close() error {
    return panickingErrorMessage{}
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

type sameTypeZeroSizeCloser struct{}

var sameTypeZeroSizeCloseCount = 0

func (instance *sameTypeZeroSizeCloser) Close() error {
    sameTypeZeroSizeCloseCount = sameTypeZeroSizeCloseCount + 1

    return nil
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

type lazyHoldingService struct {
    recorder *closeOrderRecorder
    handle   *LazyService[*closeOrderServiceA]
}

func (instance *lazyHoldingService) Close() error {
    instance.recorder.record("holder")
    return nil
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

type closeTimeResolvingService struct {
    serviceContainer containercontract.Container
    resolveErr       error
    resolvedValue    any
}

func (instance *closeTimeResolvingService) Close() error {
    instance.resolvedValue, instance.resolveErr = instance.serviceContainer.Get("service.mmm.shared")

    return nil
}

type probeClosableService struct {
    closed *bool
}

func (instance *probeClosableService) Close() error {
    *instance.closed = true

    return nil
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

type labelledCloser struct {
    label    string
    recorder *closeOrderRecorder
}

func (instance *labelledCloser) Close() error {
    instance.recorder.record(instance.label)

    return nil
}

type declaringCloser struct {
    label    string
    recorder *closeOrderRecorder
}

func (instance *declaringCloser) Close() error {
    instance.recorder.record(instance.label)

    return nil
}

func newAmbiguousTypeRecorder() (*closeOrderRecorder, *[]string) {
    mutex := &sync.Mutex{}
    closeSequence := make([]string, 0, 4)

    return &closeOrderRecorder{mutex: mutex, closeSequence: &closeSequence}, &closeSequence
}

func registerAmbiguousTypeWiring(t *testing.T, serviceContainer containercontract.Container, recorder *closeOrderRecorder, oneNameOnly bool) {
    t.Helper()

    if false == oneNameOnly {
        if registerErr := serviceContainer.Register(
            "app.fallback",
            func(_ containercontract.Resolver) (*labelledCloser, error) {
                return &labelledCloser{label: "fallback", recorder: recorder}, nil
            },
            WithTypeRegistration(false),
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }
    }

    if registerErr := serviceContainer.Register(
        "app.primary",
        func(_ containercontract.Resolver) (*labelledCloser, error) {
            return &labelledCloser{label: "primary", recorder: recorder}, nil
        },
        WithTypeRegistration(false),
        WithTeardownDependency("app.router"),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.router",
        func(_ containercontract.Resolver) (*declaringCloser, error) {
            return &declaringCloser{label: "router", recorder: recorder}, nil
        },
        WithTeardownDependencyOfType[*labelledCloser](),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }
}

func buildEveryRegisteredService(t *testing.T, serviceContainer containercontract.Container, serviceNames ...string) {
    t.Helper()

    for _, serviceName := range serviceNames {
        if _, getErr := serviceContainer.Get(serviceName); nil != getErr {
            t.Fatalf("unexpected get error for %s: %v", serviceName, getErr)
        }
    }
}

type concurrentCloser struct {
    running *atomic.Int64
    peak    *atomic.Int64
}

func (instance *concurrentCloser) Close() error {
    now := instance.running.Add(1)

    for {
        peak := instance.peak.Load()
        if now <= peak || true == instance.peak.CompareAndSwap(peak, now) {
            break
        }
    }

    time.Sleep(50 * time.Millisecond)
    instance.running.Add(-1)

    return nil
}

type countingConcurrentCloser struct {
    running *atomic.Int64
    peak    *atomic.Int64
    closed  *atomic.Int64
}

func (instance *countingConcurrentCloser) Close() error {
    now := instance.running.Add(1)

    for {
        peak := instance.peak.Load()
        if now <= peak || true == instance.peak.CompareAndSwap(peak, now) {
            break
        }
    }

    time.Sleep(50 * time.Millisecond)
    instance.running.Add(-1)
    instance.closed.Add(1)

    return nil
}

type mutualConcurrentCloser struct {
    running *atomic.Int64
    peak    *atomic.Int64
    closed  *atomic.Int64
    partner *mutualConcurrentCloser
}

func (instance *mutualConcurrentCloser) Close() error {
    return (&countingConcurrentCloser{running: instance.running, peak: instance.peak, closed: instance.closed}).Close()
}

type mutualWaveMate struct {
    mate    *waveMateCloser
    partner *mutualWaveMate
}

func (instance *mutualWaveMate) Close() error {
    return instance.mate.Close()
}

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

type plainValueService struct {
    label string
}

type failingCloser struct{}

func (instance *failingCloser) Close() error {
    return errors.New("the close refused")
}

type scopedOnlyService struct {
    label string
}

func (instance *scopedOnlyService) Close() error { return nil }

type sharedPoolService struct {
    label string
}

func (instance *sharedPoolService) Close() error { return nil }

type poolDeclarerService struct {
    label string
}

func (instance *poolDeclarerService) Close() error { return nil }

type capturedDeclarerPool struct {
    declarer *poolDeclarerService
}

func (instance *capturedDeclarerPool) Close() error { return nil }

type registerScopedProbe struct {
    value string
}

type registerScopedOtherProbe struct {
    value string
}

type registerProbeService struct{}

type closedGuardCloser struct {
    mutex  sync.Mutex
    closed bool
}

func (instance *closedGuardCloser) Close() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.closed = true

    return nil
}

func (instance *closedGuardCloser) IsClosed() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.closed
}

type panickingCloser struct{}

func (instance *panickingCloser) Close() error {
    panic("close exploded")
}

type scopeCloseRaceService struct {
    mutex  sync.Mutex
    closed bool
}

func (instance *scopeCloseRaceService) Close() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.closed = true

    return nil
}

func (instance *scopeCloseRaceService) wasClosed() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.closed
}

type typedNilPanicError struct {
    detail string
}

func (instance *typedNilPanicError) Error() string {
    return instance.detail
}

type panickingPanicValueError struct{}

func (instance *panickingPanicValueError) Error() string {
    panic("the error message gives up")
}

type overrideRaceBuiltService struct {
    mutex  sync.Mutex
    closed bool
}

func (instance *overrideRaceBuiltService) Close() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.closed = true

    return nil
}

func (instance *overrideRaceBuiltService) wasClosed() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.closed
}

type waitingResolverProbe struct {
    value string
}

func awaitCreationWaiter(t *testing.T, serviceContainer *container, serviceName string, waiterCount int) {
    t.Helper()

    for attempt := 0; attempt < 20000; attempt++ {
        serviceContainer.mutex.RLock()
        state, exists := serviceContainer.creatingByName[serviceName]
        registered := 0
        if true == exists && nil != state {
            registered = len(state.waiterContextIds)
        }
        serviceContainer.mutex.RUnlock()

        if waiterCount <= registered {
            return
        }

        runtime.Gosched()
    }

    t.Fatalf("expected %d waiters on the creation of %q", waiterCount, serviceName)
}

type contextPanickingError struct{}

func (instance *contextPanickingError) Error() string {
    return "the context panicked"
}

func (instance *contextPanickingError) Context() exceptioncontract.Context {
    panic("context rendering gave up")
}

var _ exceptioncontract.ContextProvider = (*contextPanickingError)(nil)

type unwrapPanickingError struct{}

func (instance *unwrapPanickingError) Error() string {
    return "the cause walk gives up"
}

func (instance *unwrapPanickingError) Unwrap() error {
    panic("unwrap gave up")
}

type scopedRegistrarProbe struct {
    value string
}

func scopedNameProbeProvider() containercontract.Provider[*providerContractProbe] {
    return func(resolver containercontract.Resolver) (*providerContractProbe, error) {
        return &providerContractProbe{value: "scoped"}, nil
    }
}

type scopedTeardownProbeService struct{}

type testService struct {
    Value string
}

type testInterface interface {
    Name() string
}

type testImplementation struct {
    name string
}

func (instance *testImplementation) Name() string {
    return instance.name
}

type hasTypeProbe struct {
    value string
}

type containerOverrideOutsideValue struct{}

type armedTypedProbe struct{ label string }

func (instance *armedTypedProbe) Close() error { return nil }

func refusalStageOf(t *testing.T, err error) string {
    t.Helper()

    melodyErr, isMelodyErr := err.(*exception.Error)
    if false == isMelodyErr {
        t.Fatalf("expected a melody error carrying the refusal stage, got %T", err)
    }

    stage, _ := melodyErr.Context()["refusedAt"].(string)

    return stage
}

type contextOnlyCleanup struct { calls int; seen context.Context; failure error }

func (instance *contextOnlyCleanup) CloseWithContext(ctx context.Context) error { instance.calls++; instance.seen = ctx; return instance.failure }

type lazyProbeItem struct {
    name string
}

type lazyTeardownReader struct {
    lazyHandle    *LazyService[*lazyProbeItem]
    directResolve func() (*lazyProbeItem, error)

    lazyErr   error
    directErr error
    ran       bool
}

func (instance *lazyTeardownReader) Close() error {
    instance.ran = true
    _, instance.lazyErr = instance.lazyHandle.Resolve()
    _, instance.directErr = instance.directResolve()

    return nil
}

type providerContractProbe struct {
    value string
}

type providerConcreteError struct {
    detail string
}

func (instance *providerConcreteError) Error() string {
    return instance.detail
}

type scopedProbe struct {
    value string
}

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

type scopedTypeOnlyProbe struct {
    value  string
    closed *int
}

func (instance *scopedTypeOnlyProbe) Close() error {
    *instance.closed = *instance.closed + 1

    return nil
}

type keptResolverHolder struct {
    resolver containercontract.Resolver
    recorder *scopedCloseRecorder
}

func (instance *keptResolverHolder) Close() error {
    instance.recorder.record("holder")

    return nil
}

type resolverRaceProbeFirst struct{}

type resolverRaceProbeSecond struct{}

type resolverRaceRegisteredZero struct{}

type resolverRaceRegisteredOne struct{}

type resolverRaceRegisteredTwo struct{}

type resolverRaceRegisteredThree struct{}

type resolverRaceRegisteredFour struct{}

type resolverRaceRegisteredFive struct{}

type suspensionHasProbe struct {
    value string
}

type resolverContextMustProbe struct {
    value string
}

type resolverContextMustDependent struct {
    dependency *resolverContextMustProbe
}

func contextValueInChain(err error, key string) string {
    for current := err; nil != current; current = errors.Unwrap(current) {
        melodyErr, isMelodyErr := current.(*exception.Error)
        if false == isMelodyErr || nil == melodyErr {
            continue
        }

        value, exists := melodyErr.Context()[key]
        if false == exists {
            continue
        }

        stringValue, isString := value.(string)
        if true == isString {
            return stringValue
        }
    }

    return ""
}

func renderedCauseChain(err error) string {
    rendered := ""
    for current := err; nil != current; current = errors.Unwrap(current) {
        rendered = rendered + current.Error() + "\n"
    }

    return rendered
}

type resolverContextGraphDependency struct{}

type resolverContextGraphParent struct {
    dependency *resolverContextGraphDependency
}

type resolverContextGraphControlParent struct {
    dependency *resolverContextGraphDependency
}

var errFailingProvider = errors.New("the provider failed")

type collectableHandler interface {
    Handle() string
}

type unrelatedContract interface {
    Unrelated()
}

type invoiceHandler struct {
}

func (instance *invoiceHandler) Handle() string {
    return "invoice"
}

type auditHandler struct {
}

func (instance *auditHandler) Handle() string {
    return "audit"
}

type plainService struct {
}

func newCollectionContainer(t *testing.T) containercontract.Container {
    t.Helper()

    serviceContainer := NewContainer()

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*invoiceHandler, error) {
        return &invoiceHandler{}, nil
    })

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*auditHandler, error) {
        return &auditHandler{}, nil
    })

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*plainService, error) {
        return &plainService{}, nil
    })

    return serviceContainer
}

type handlerDispatcher struct {
    handlers []collectableHandler
}

type namedHandler struct {
    name string
}

func (instance *namedHandler) Handle() string {
    return instance.name
}

type compositeDispatcher struct {
    handlers []collectableHandler
}

func (instance *compositeDispatcher) Handle() string {
    return "composite"
}

type requestHandler struct {
}

func (instance *requestHandler) Handle() string {
    return "request"
}

type resolverTestService struct {
    value string
}

type resolverTestResolver struct {
    servicesByName map[string]any
    servicesByType map[reflect.Type]any
}

func (instance *resolverTestResolver) Get(serviceName string) (any, error) {
    value, exists := instance.servicesByName[serviceName]
    if false == exists {
        return nil, errors.New("service missing")
    }

    return value, nil
}

func (instance *resolverTestResolver) MustGet(serviceName string) any {
    value, err := instance.Get(serviceName)
    if nil != err {
        exception.Panic(
            exception.FromError(err),
        )
    }

    return value
}

func (instance *resolverTestResolver) GetByType(targetType reflect.Type) (any, error) {
    value, exists := instance.servicesByType[targetType]
    if false == exists {
        return nil, errors.New("service missing")
    }

    return value, nil
}

func (instance *resolverTestResolver) MustGetByType(targetType reflect.Type) any {
    value, err := instance.GetByType(targetType)
    if nil != err {
        exception.Panic(
            exception.FromError(err),
        )
    }

    return value
}

func (instance *resolverTestResolver) Has(serviceName string) bool {
    _, exists := instance.servicesByName[serviceName]

    return true == exists
}

func (instance *resolverTestResolver) HasType(targetType reflect.Type) bool {
    _, exists := instance.servicesByType[targetType]

    return true == exists
}

var _ containercontract.Resolver = (*resolverTestResolver)(nil)

type melodyErrorResolver struct {
    err *exception.Error
}

func (instance *melodyErrorResolver) Get(serviceName string) (any, error) {
    return nil, instance.err
}

func (instance *melodyErrorResolver) MustGet(serviceName string) any {
    exception.Panic(instance.err)

    return nil
}

func (instance *melodyErrorResolver) GetByType(targetType reflect.Type) (any, error) {
    return nil, instance.err
}

func (instance *melodyErrorResolver) MustGetByType(targetType reflect.Type) any {
    exception.Panic(instance.err)

    return nil
}

func (instance *melodyErrorResolver) Has(serviceName string) bool {
    return false
}

func (instance *melodyErrorResolver) HasType(targetType reflect.Type) bool {
    return false
}

type resolverTestTypedNilError struct{}

func (instance *resolverTestTypedNilError) Error() string {
    return "never reached on a nil receiver"
}

type resolverTestTypedNilErrorResolver struct {
    value any
}

func (instance *resolverTestTypedNilErrorResolver) Get(serviceName string) (any, error) {
    var typedNil *resolverTestTypedNilError
    return instance.value, typedNil
}

func (instance *resolverTestTypedNilErrorResolver) MustGet(serviceName string) any {
    return instance.value
}

func (instance *resolverTestTypedNilErrorResolver) GetByType(targetType reflect.Type) (any, error) {
    var typedNil *resolverTestTypedNilError
    return instance.value, typedNil
}

func (instance *resolverTestTypedNilErrorResolver) MustGetByType(targetType reflect.Type) any {
    return instance.value
}

func (instance *resolverTestTypedNilErrorResolver) Has(serviceName string) bool { return true }

func (instance *resolverTestTypedNilErrorResolver) HasType(targetType reflect.Type) bool {
    return true
}

type scopeContextProbe struct {
    plainCalls int
    contexts   []context.Context
    closeErr   error
    panicValue any
}

func (instance *scopeContextProbe) Close() error {
    instance.plainCalls++
    return nil
}

func (instance *scopeContextProbe) CloseWithContext(closeContext context.Context) error {
    instance.contexts = append(instance.contexts, closeContext)
    if nil != instance.panicValue {
        panic(instance.panicValue)
    }
    return instance.closeErr
}

type scopeLegacyContextControl struct {
    calls int
}

func (instance *scopeLegacyContextControl) Close() error {
    instance.calls++
    return nil
}

type scopeRegistrarProbe struct {
    value string
}

func scopeRegistrarProvider(value string) containercontract.Provider[*scopeRegistrarProbe] {
    return func(resolver containercontract.Resolver) (*scopeRegistrarProbe, error) {
        return &scopeRegistrarProbe{value: value}, nil
    }
}

type scopedEarlyHandler struct {
}

func (instance *scopedEarlyHandler) Handle() string {
    return "early"
}

type scopedLateHandler struct {
}

func (instance *scopedLateHandler) Handle() string {
    return "late"
}

type scopeTestService struct {
    value string
}

type closeCountingScopeService struct {
    value      string
    closeCalls *int32
    closeErr   error
}

func (instance *closeCountingScopeService) Close() error {
    atomic.AddInt32(instance.closeCalls, 1)

    return instance.closeErr
}

type scopeLifetimeProbe struct {
    closed *atomic.Int64
}

func (instance *scopeLifetimeProbe) Close() error {
    instance.closed.Add(1)

    return nil
}

type aliasRepositoryService struct {
    recorder *scopedCloseRecorder
}

func (instance *aliasRepositoryService) Close() error {
    instance.recorder.record("repository")

    return nil
}

type dualFiledValueService struct {
    closeCount *int32
    padding    []string
}

func (instance dualFiledValueService) Close() error {
    atomic.AddInt32(instance.closeCount, 1)

    return nil
}

type secondEvictedFailingService struct {
    failure error
}

func (instance *secondEvictedFailingService) Close() error {
    return instance.failure
}

type scopeCloseFailingService struct {
    failure error
}

func (instance *scopeCloseFailingService) Close() error {
    return instance.failure
}

type scopeOverrideGreeter interface {
    Greet() string
}

type containerScopeGreeter struct{}

func (instance *containerScopeGreeter) Greet() string {
    return "container"
}

type overrideScopeGreeter struct{}

func (instance *overrideScopeGreeter) Greet() string {
    return "override"
}

type scopeOverrideOutsider struct{}

type aliasNameHeldService struct {
    recorder *scopedCloseRecorder
}

func (instance *aliasNameHeldService) Close() error {
    instance.recorder.record("dependency")

    return nil
}

type aliasKeptResolverHolder struct {
    resolver containercontract.Resolver
    recorder *scopedCloseRecorder
}

func (instance *aliasKeptResolverHolder) Close() error {
    instance.recorder.record("holder")

    return nil
}

type aliasGroupSpanningService struct {
    label    string
    recorder *scopedCloseRecorder
}

func (instance *aliasGroupSpanningService) Close() error {
    instance.recorder.record(instance.label)

    return nil
}

type aliasGroupMiddleService struct {
    recorder *scopedCloseRecorder
}

func (instance *aliasGroupMiddleService) Close() error {
    instance.recorder.record("middle")

    return nil
}

type scopeContextDoorService struct {
    closeCalls            int
    closeWithContextCalls int
    contextErr            error
}

func (instance *scopeContextDoorService) Close() error {
    instance.closeCalls = instance.closeCalls + 1

    return nil
}

func (instance *scopeContextDoorService) CloseWithContext(closeContext context.Context) error {
    instance.closeWithContextCalls = instance.closeWithContextCalls + 1
    instance.contextErr = closeContext.Err()

    return nil
}

type scopeContextDoorPanickingService struct{}

func (instance *scopeContextDoorPanickingService) Close() error {
    return nil
}

func (instance *scopeContextDoorPanickingService) CloseWithContext(closeContext context.Context) error {
    panic("the context door panicked")
}

func newScopeWithContextDoorService(t *testing.T) (containercontract.Scope, *scopeContextDoorService) {
    t.Helper()

    serviceContainer := NewContainer()
    service := &scopeContextDoorService{}

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.scoped.contextDoor",
        func(resolver containercontract.Resolver) (*scopeContextDoorService, error) {
            return service, nil
        },
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if _, getErr := scopeInstance.Get("app.scoped.contextDoor"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    return scopeInstance, service
}

type reflectionGraphNode struct {
    left  *reflectionGraphNode
    right *reflectionGraphNode
    value int
}

func (instance *reflectionGraphNode) Close() error { return nil }

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

    if _, getErr := FromResolver[*capturingHolder](serviceContainer, "app.holder"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := FromResolver[*closeOrderServiceB](serviceContainer, "app.storage"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }
}

func holdsIdentity(identities []heldPointer, wanted pointerIdentity) bool {
    for _, held := range identities {
        if held.identity == wanted {
            return true
        }
    }

    return false
}

type mutualParentService struct {
    recorder *closeOrderRecorder
    child    *mutualChildService
}

func (instance *mutualParentService) Close() error {
    instance.recorder.record("parent")

    return nil
}

type mutualChildService struct {
    recorder *closeOrderRecorder
    parent   *mutualParentService
}

func (instance *mutualChildService) Close() error {
    instance.recorder.record("child")

    return nil
}

type foreignHub struct {
    mutex     sync.Mutex
    backplane fmt.Stringer
}

func (instance *foreignHub) swap(backplane fmt.Stringer) {
    instance.mutex.Lock()
    instance.backplane = backplane
    instance.mutex.Unlock()
}

func (instance *foreignHub) Close() error { return nil }

type foreignBackplane struct{ label string }

func (instance *foreignBackplane) String() string { return instance.label }

type hubHolder struct {
    hub *foreignHub
    ctx context.Context
}

func (instance *hubHolder) Close() error { return nil }

type intermediateStruct struct{ service *closeOrderServiceB }

type outerHolder struct{ middle *intermediateStruct }

func (instance *outerHolder) Close() error { return nil }

type chainLink struct {
    next *chainLink
    held *closeOrderServiceB
}

type chainFirstRoot struct {
    chain  *chainLink
    direct *chainLink
}

type directFirstRoot struct {
    direct *chainLink
    chain  *chainLink
}

func chainOfLinks(length int, tail *chainLink) *chainLink {
    head := tail

    for count := 0; count < length; count = count + 1 {
        head = &chainLink{next: head}
    }

    return head
}

type oddDepthTail struct {
    nested struct{ held *closeOrderServiceB }
}

type oddDepthLink struct {
    next *oddDepthLink
    tail *oddDepthTail
}

type oddDepthRoot struct {
    chain *oddDepthLink
}

func (instance *oddDepthRoot) Close() error { return nil }

func oddDepthChain(length int, held *closeOrderServiceB) *oddDepthLink {
    tail := &oddDepthTail{}
    tail.nested.held = held

    head := &oddDepthLink{tail: tail}

    for count := 1; count < length; count = count + 1 {
        head = &oddDepthLink{next: head}
    }

    return head
}

type wideHolder struct {
    cells [256][256]struct{ service *closeOrderServiceB }
}

func (instance *wideHolder) Close() error { return nil }

type wideHolderWithPeer struct {
    cells [256][256]struct{ service *closeOrderServiceB }
    peer  *closeOrderServiceB
}

func (instance *wideHolderWithPeer) Close() error { return nil }

type boundedHolder struct {
    cells [64][256]struct{ service *closeOrderServiceB }
}

func (instance *boundedHolder) Close() error { return nil }

type ringNode struct {
    next     *ringNode
    recorder *closeOrderRecorder
    label    string
}

func (instance *ringNode) Close() error {
    instance.recorder.record(instance.label)

    return nil
}

type resolvingHolder struct {
    recorder *closeOrderRecorder
}

func (instance *resolvingHolder) Close() error {
    instance.recorder.record("holder")

    return nil
}

type backHolder struct {
    recorder *closeOrderRecorder
    back     *resolvingHolder
}

func (instance *backHolder) Close() error {
    instance.recorder.record("collaborator")

    return nil
}

type foreignBag struct{ items []*closeOrderServiceB }

type bagHolder struct {
    bag   *foreignBag
    items []*closeOrderServiceB
}

func (instance *bagHolder) Close() error { return nil }

type rewritingRegistry struct {
    mutex sync.Mutex
    items []*closeOrderServiceB
}

func (instance *rewritingRegistry) All() []*closeOrderServiceB {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.items
}

func (instance *rewritingRegistry) Rewrite(value *closeOrderServiceB) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.items[0] = value
}

type handedSliceHolder struct {
    items []*closeOrderServiceB
}

func (instance *handedSliceHolder) Close() error { return nil }

type mutualPairA struct {
    other    *mutualPairB
    recorder *closeOrderRecorder
}

func (instance *mutualPairA) Close() error {
    instance.recorder.record("a")

    return nil
}

type mutualPairB struct {
    other    *mutualPairA
    recorder *closeOrderRecorder
}

func (instance *mutualPairB) Close() error {
    instance.recorder.record("b")

    return nil
}

type pairMemberHolder struct {
    a        *mutualPairA
    recorder *closeOrderRecorder
}

func (instance *pairMemberHolder) Close() error {
    instance.recorder.record("c")

    return nil
}

type aliasedResolvingService struct {
    recorder *closeOrderRecorder
}

func (instance *aliasedResolvingService) Close() error {
    instance.recorder.record("resolving")

    return nil
}

type aliasedCapturingService struct {
    recorder *closeOrderRecorder
    back     *aliasedResolvingService
}

func (instance *aliasedCapturingService) Close() error {
    instance.recorder.record("capturing")

    return nil
}

type fatHub struct {
    cells [64][256]struct{ left, right *closeOrderServiceB }
}

type hubLink struct {
    next *hubLink
    hub  *fatHub
}

func newHubChain(hub *fatHub, length int) *hubLink {
    var head *hubLink

    for index := 0; index < length; index = index + 1 {
        head = &hubLink{next: head, hub: hub}
    }

    return head
}

type collaboratorLink struct {
    next *collaboratorLink
    held *closeOrderServiceB
}

func newCollaboratorChain(held *closeOrderServiceB, length int) *collaboratorLink {
    head := &collaboratorLink{held: held}

    for index := 1; index < length; index = index + 1 {
        head = &collaboratorLink{next: head}
    }

    return head
}

type chainFirstHolder struct {
    chain  *hubLink
    hub    *fatHub
    collaborator *collaboratorLink
}

func (instance *chainFirstHolder) Close() error { return nil }

type collaboratorFirstHolder struct {
    collaborator *collaboratorLink
    hub    *fatHub
    chain  *hubLink
}

func (instance *collaboratorFirstHolder) Close() error { return nil }

type tableFirstCodec struct {
    table [256][256]byte
    peer  *collaboratorLink
}

func (instance *tableFirstCodec) Close() error { return nil }

type peerFirstCodec struct {
    peer  *collaboratorLink
    table [256][256]byte
}

func (instance *peerFirstCodec) Close() error { return nil }

func structOfScalarsAsWideAsTheBudget() reflect.Type {
    integerFields := make([]reflect.StructField, 0, teardownWalkElementLimit)
    for index := 0; index < teardownWalkElementLimit; index = index + 1 {
        integerFields = append(integerFields, reflect.StructField{Name: reflectStructFieldName("Scalar", index), Type: reflect.TypeOf(0)})
    }

    inner := reflect.StructOf(integerFields)

    innerFields := make([]reflect.StructField, 0, teardownWalkElementLimit)
    for index := 0; index < teardownWalkElementLimit; index = index + 1 {
        innerFields = append(innerFields, reflect.StructField{Name: reflectStructFieldName("Inner", index), Type: inner})
    }

    return reflect.StructOf(innerFields)
}

func reflectStructFieldName(prefix string, index int) string {
    return prefix + string(rune('A'+index/26)) + string(rune('A'+index%26))
}

type cellTable struct {
    cells [16][256][256]struct{ pointer *closeOrderServiceB }
    peer  *collaboratorLink
}

type cycleMemberLink struct {
    next *cycleMemberLink
    head *cycleMemberHead
}

type cycleMemberHead struct {
    leaf *cycleMemberLeaf
    tail *cycleMemberTail
}

type cycleMemberTail struct {
    head *cycleMemberHead
}

type cycleMemberLeaf struct {
    next   *cycleMemberLeaf
    collaborator *closeOrderServiceB
}

type chainThenTailHolder struct {
    chain *cycleMemberLink
    tail  *cycleMemberTail
}

func (instance *chainThenTailHolder) Close() error { return nil }

type tailThenChainHolder struct {
    tail  *cycleMemberTail
    chain *cycleMemberLink
}

func (instance *tailThenChainHolder) Close() error { return nil }

func newCycleMemberShape(held *closeOrderServiceB) (*cycleMemberLink, *cycleMemberTail) {
    head := &cycleMemberHead{leaf: &cycleMemberLeaf{next: &cycleMemberLeaf{next: &cycleMemberLeaf{collaborator: held}}}}
    tail := &cycleMemberTail{head: head}
    head.tail = tail

    return &cycleMemberLink{next: &cycleMemberLink{head: head}}, tail
}

type cyclicHub struct {
    cells [64][256]struct{ left, right *closeOrderServiceB }
    back  *cyclicHubLink
}

type cyclicHubLink struct {
    next *cyclicHubLink
    hub  *cyclicHub
}

type chainFirstCyclicHubHolder struct {
    chain  *cyclicHubLink
    hub    *cyclicHub
    collaborator *closeOrderServiceB
}

func (instance *chainFirstCyclicHubHolder) Close() error { return nil }

type crossEdgeNode struct {
    next  *crossEdgeNode
    cross *crossEdgeNode
    back  *crossEdgeNode
    held  *closeOrderServiceB
}

type crossEdgeHolder struct {
    chain *crossEdgeNode
    short *crossEdgeNode
}

func (instance *crossEdgeHolder) Close() error { return nil }

func newCrossEdgeShape(held *closeOrderServiceB) (chain *crossEdgeNode, holderOfMember *crossEdgeNode) {
    beyondTheLimit := &crossEdgeNode{held: held}
    underTheRoot := &crossEdgeNode{next: beyondTheLimit}
    root := &crossEdgeNode{next: underTheRoot}
    member := &crossEdgeNode{back: root}
    holderOfMember = &crossEdgeNode{cross: member}
    root.cross = member
    root.back = holderOfMember

    chain = root
    for link := 0; link < 3; link = link + 1 {
        chain = &crossEdgeNode{next: chain}
    }

    return chain, holderOfMember
}

type anyTableFirstCodec struct {
    table [256][256]any
    peer  *collaboratorLink
}

func (instance *anyTableFirstCodec) Close() error { return nil }

type sliceTableFirstCodec struct {
    table [256][256][]byte
    peer  *collaboratorLink
}

func (instance *sliceTableFirstCodec) Close() error { return nil }

type selfReferringHolder struct {
    self   *selfReferringHolder
    collaborator *closeOrderServiceB
}

func (instance *selfReferringHolder) Close() error { return nil }

type listenerBus struct {
    listeners []*busListener
}

func (instance *listenerBus) Close() error { return nil }

type busListener struct {
    back     *listenerBus
    inFlight *atomic.Int32
    peak     *atomic.Int32
}

func (instance *busListener) Close() error {
    now := instance.inFlight.Add(1)

    for {
        peak := instance.peak.Load()
        if now <= peak || true == instance.peak.CompareAndSwap(peak, now) {
            break
        }
    }

    deadline := time.Now().Add(2 * time.Second)
    for 2 > instance.peak.Load() && true == time.Now().Before(deadline) {
        time.Sleep(time.Millisecond)
    }

    instance.inFlight.Add(-1)

    return nil
}

type selfHoldingService struct {
    self  *selfHoldingService
    label string
}

func (instance *selfHoldingService) Close() error {
    return nil
}

type utilityProbe struct{}

type utilityProbeInterface interface {
    Probe()
}

type fitsProbeGreeter interface {
    Greet() string
}

type fitsProbeImplementer struct{}

func (instance *fitsProbeImplementer) Greet() string {
    return "fits"
}

type fitsProbeOutsider struct{}
