package container

import (
    "context"
    "errors"
    "math/rand"
    "reflect"
    "runtime"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type takeoverCallerKey struct{}

/* takeoverProvider scripts one answer per run: a run listed in blocked waits on its channel first, a run listed in failures answers it, every other run builds the probe; the caller value of each run's runtime is recorded */
type takeoverProvider struct {
    mutex    sync.Mutex
    runs     int
    entered  map[int]chan struct{}
    blocked  map[int]chan struct{}
    failures map[int]func(ctx context.Context) error
    callers  []any
}

func newTakeoverProvider() *takeoverProvider {
    return &takeoverProvider{
        entered:  make(map[int]chan struct{}),
        blocked:  make(map[int]chan struct{}),
        failures: make(map[int]func(ctx context.Context) error),
    }
}

func (instance *takeoverProvider) blockRun(run int) (entered chan struct{}, release chan struct{}) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.entered[run] = make(chan struct{})
    instance.blocked[run] = make(chan struct{})

    return instance.entered[run], instance.blocked[run]
}

func (instance *takeoverProvider) failRun(run int, failure func(ctx context.Context) error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.failures[run] = failure
}

func (instance *takeoverProvider) runCount() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.runs
}

func (instance *takeoverProvider) provide(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
    instance.mutex.Lock()
    instance.runs++
    run := instance.runs
    ctx := resolver.Runtime().Context()
    instance.callers = append(instance.callers, ctx.Value(takeoverCallerKey{}))
    entered := instance.entered[run]
    blocked := instance.blocked[run]
    failure := instance.failures[run]
    instance.mutex.Unlock()

    if nil != entered {
        close(entered)
        <-blocked
    }

    if nil != failure {
        return nil, failure(ctx)
    }

    return &waitingResolverProbe{value: "built"}, nil
}

func takeoverCallerContext(parent context.Context, caller string) (context.Context, context.CancelFunc) {
    return context.WithCancel(context.WithValue(parent, takeoverCallerKey{}, caller))
}

/* the owner's departure is read off its context, whatever its provider answered, so a live waiter builds again under its own context rather than answer another caller's hang-up */
func TestCreationGuard_ALiveWaiterBuildsAgainWhenTheOwnersContextEndedWhateverTheOwnersFailureCarries(t *testing.T) {
    for name, failure := range map[string]func(ctx context.Context) error{
        "the context's error":  func(ctx context.Context) error { return ctx.Err() },
        "a driver error":       func(ctx context.Context) error { return errors.New("driver: bad connection") },
        "a custom cause":       func(ctx context.Context) error { return context.Cause(ctx) },
        "a panic":              func(ctx context.Context) error { panic("the provider panicked") },
    } {
        t.Run(name, func(t *testing.T) {
            serviceContainer := NewContainer(ContainerConfig{}).(*container)
            provider := newTakeoverProvider()
            entered, release := provider.blockRun(1)
            provider.failRun(1, failure)
            MustRegister[*waitingResolverProbe](serviceContainer, "app.takeover", provider.provide, RegisterOptions{})

            ownerContext, cancelOwner := context.WithCancelCause(context.WithValue(t.Context(), takeoverCallerKey{}, "owner"))
            ownerDone := make(chan error, 1)
            go func() {
                _, ownerErr := serviceContainer.Get(ownerContext, "app.takeover")
                ownerDone <- ownerErr
            }()

            <-entered

            waiterContext, cancelWaiter := takeoverCallerContext(t.Context(), "waiter")
            defer cancelWaiter()
            waiterDone := make(chan any, 1)
            go func() {
                value, waiterErr := serviceContainer.Get(waiterContext, "app.takeover")
                if nil != waiterErr {
                    waiterDone <- waiterErr
                    return
                }
                waiterDone <- value
            }()

            awaitCreationWaiter(t, serviceContainer, "app.takeover", 1)

            cancelOwner(errors.New("the owner's client hung up"))
            close(release)

            if ownerErr := <-ownerDone; nil == ownerErr {
                t.Fatalf("expected the owner to answer its own failure")
            }

            answer := <-waiterDone
            if probe, isProbe := answer.(*waitingResolverProbe); false == isProbe || "built" != probe.value {
                t.Fatalf("expected the waiter to hold the service it built again, got %v", answer)
            }

            if 2 != provider.runCount() || "waiter" != provider.callers[1] {
                t.Fatalf("expected a second run under the waiter's context, got %d runs with callers %v", provider.runCount(), provider.callers)
            }
        })
    }
}

/* a waiter whose own context ended in the instant its owner left answers its own refusal and never becomes a creator */
func TestCreationGuard_AWaiterWhoseOwnContextEndedAnswersItsOwnRefusal(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)
    provider := newTakeoverProvider()
    entered, release := provider.blockRun(1)
    provider.failRun(1, func(ctx context.Context) error { return ctx.Err() })
    MustRegister[*waitingResolverProbe](serviceContainer, "app.takeover", provider.provide, RegisterOptions{})

    ownerContext, cancelOwner := takeoverCallerContext(t.Context(), "owner")
    ownerDone := make(chan error, 1)
    go func() {
        _, ownerErr := serviceContainer.Get(ownerContext, "app.takeover")
        ownerDone <- ownerErr
    }()

    <-entered

    waiterContext, cancelWaiter := context.WithCancelCause(t.Context())
    serviceContainer.afterCreationWaitReleased = func() {
        cancelOwner()
        close(release)
        <-ownerDone
    }

    waiterDone := make(chan error, 1)
    go func() {
        _, waiterErr := serviceContainer.Get(waiterContext, "app.takeover")
        waiterDone <- waiterErr
    }()

    awaitCreationWaiter(t, serviceContainer, "app.takeover", 1)

    waiterCause := errors.New("the waiter's own client hung up")
    cancelWaiter(waiterCause)

    waiterErr := <-waiterDone
    if nil == waiterErr || "service creation wait was cancelled by the caller context" != waiterErr.Error() || false == errors.Is(waiterErr, waiterCause) {
        t.Fatalf("expected the waiter's own refusal carrying its own cause, got %v", waiterErr)
    }

    if 1 != provider.runCount() {
        t.Fatalf("expected no second run for a done waiter, got %d", provider.runCount())
    }
}

/* every live waiter is answered by one takeover: the first to re-take the lock builds, the others wait on it */
func TestCreationGuard_EveryLiveWaiterIsAnsweredByOneTakeover(t *testing.T) {
    const waiterCount = 8

    serviceContainer := NewContainer(ContainerConfig{CreationWaiters: bound.Of(waiterCount)}).(*container)
    provider := newTakeoverProvider()
    entered, release := provider.blockRun(1)
    provider.failRun(1, func(ctx context.Context) error { return ctx.Err() })
    MustRegister[*waitingResolverProbe](serviceContainer, "app.takeover", provider.provide, RegisterOptions{})

    ownerContext, cancelOwner := takeoverCallerContext(t.Context(), "owner")
    ownerDone := make(chan error, 1)
    go func() {
        _, ownerErr := serviceContainer.Get(ownerContext, "app.takeover")
        ownerDone <- ownerErr
    }()

    <-entered

    answers := make(chan any, waiterCount)
    for range waiterCount {
        go func() {
            value, waiterErr := serviceContainer.Get(t.Context(), "app.takeover")
            if nil != waiterErr {
                answers <- waiterErr
                return
            }
            answers <- value
        }()
    }

    awaitCreationWaiter(t, serviceContainer, "app.takeover", waiterCount)

    cancelOwner()
    close(release)
    <-ownerDone

    var first any
    for range waiterCount {
        answer := <-answers
        if _, isProbe := answer.(*waitingResolverProbe); false == isProbe {
            t.Fatalf("expected every waiter to hold the service, got %v", answer)
        }

        if nil == first {
            first = answer
        }

        if first != answer {
            t.Fatalf("expected one instance for every waiter")
        }
    }

    if 2 != provider.runCount() {
        t.Fatalf("expected one takeover run, got %d runs", provider.runCount())
    }

    serviceContainer.mutex.RLock()
    defer serviceContainer.mutex.RUnlock()
    if 0 != len(serviceContainer.creatingByName) {
        t.Fatalf("expected no creation state left, got %v", serviceContainer.creatingByName)
    }
}

/* a taker whose own context ends hands the creation over again */
func TestCreationGuard_ATakeoverWhoseOwnContextEndsHandsOverAgain(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)
    provider := newTakeoverProvider()
    firstEntered, firstRelease := provider.blockRun(1)
    secondEntered, secondRelease := provider.blockRun(2)
    provider.failRun(1, func(ctx context.Context) error { return ctx.Err() })
    provider.failRun(2, func(ctx context.Context) error { return ctx.Err() })
    MustRegister[*waitingResolverProbe](serviceContainer, "app.takeover", provider.provide, RegisterOptions{})

    ownerContext, cancelOwner := takeoverCallerContext(t.Context(), "owner")
    ownerDone := make(chan error, 1)
    go func() {
        _, ownerErr := serviceContainer.Get(ownerContext, "app.takeover")
        ownerDone <- ownerErr
    }()

    <-firstEntered

    takerContext, cancelTaker := takeoverCallerContext(t.Context(), "taker")
    takerDone := make(chan error, 1)
    go func() {
        _, takerErr := serviceContainer.Get(takerContext, "app.takeover")
        takerDone <- takerErr
    }()

    awaitCreationWaiter(t, serviceContainer, "app.takeover", 1)
    cancelOwner()
    close(firstRelease)
    <-ownerDone
    <-secondEntered

    lastDone := make(chan any, 1)
    go func() {
        value, lastErr := serviceContainer.Get(t.Context(), "app.takeover")
        if nil != lastErr {
            lastDone <- lastErr
            return
        }
        lastDone <- value
    }()

    awaitCreationWaiter(t, serviceContainer, "app.takeover", 1)
    cancelTaker()
    close(secondRelease)

    if takerErr := <-takerDone; nil == takerErr {
        t.Fatalf("expected the taker to answer its own failure")
    }

    if answer := <-lastDone; nil == answer {
        t.Fatalf("expected the last waiter to hold the service")
    } else if _, isProbe := answer.(*waitingResolverProbe); false == isProbe {
        t.Fatalf("expected the last waiter to hold the service, got %v", answer)
    }

    if 3 != provider.runCount() {
        t.Fatalf("expected three runs, got %d", provider.runCount())
    }
}

/* a creation that succeeded after its owner left is stored and answered: no takeover */
func TestCreationGuard_ACreationThatSucceededUnderAnEndedContextIsAnswered(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)
    provider := newTakeoverProvider()
    entered, release := provider.blockRun(1)
    MustRegister[*waitingResolverProbe](serviceContainer, "app.takeover", provider.provide, RegisterOptions{})

    ownerContext, cancelOwner := takeoverCallerContext(t.Context(), "owner")
    ownerDone := make(chan error, 1)
    go func() {
        _, ownerErr := serviceContainer.Get(ownerContext, "app.takeover")
        ownerDone <- ownerErr
    }()

    <-entered

    waiterDone := make(chan error, 1)
    go func() {
        _, waiterErr := serviceContainer.Get(t.Context(), "app.takeover")
        waiterDone <- waiterErr
    }()

    awaitCreationWaiter(t, serviceContainer, "app.takeover", 1)
    cancelOwner()
    close(release)
    <-ownerDone

    if waiterErr := <-waiterDone; nil != waiterErr {
        t.Fatalf("expected the waiter to hold the stored service, got %v", waiterErr)
    }

    if 1 != provider.runCount() {
        t.Fatalf("expected one run, got %d", provider.runCount())
    }
}

/* runtime.Goexit is not a context ending: the waiter inherits the exit even with the owner's context done */
func TestCreationGuard_AnOwnerLeavingThroughGoexitIsNotTakenOver(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)
    provider := newTakeoverProvider()
    entered, release := provider.blockRun(1)
    provider.failRun(1, func(ctx context.Context) error {
        runtime.Goexit()

        return nil
    })
    MustRegister[*waitingResolverProbe](serviceContainer, "app.takeover", provider.provide, RegisterOptions{})

    ownerContext, cancelOwner := takeoverCallerContext(t.Context(), "owner")
    ownerGone := make(chan struct{})
    go func() {
        defer close(ownerGone)
        _, _ = serviceContainer.Get(ownerContext, "app.takeover")
    }()

    <-entered

    waiterDone := make(chan error, 1)
    go func() {
        _, waiterErr := serviceContainer.Get(t.Context(), "app.takeover")
        waiterDone <- waiterErr
    }()

    awaitCreationWaiter(t, serviceContainer, "app.takeover", 1)
    cancelOwner()
    close(release)
    <-ownerGone

    waiterErr := <-waiterDone
    if nil == waiterErr || false == strings.Contains(renderedCauseChain(waiterErr), "service provider left through runtime.Goexit") {
        t.Fatalf("expected the waiter to inherit the Goexit, got %v", waiterErr)
    }

    if 1 != provider.runCount() {
        t.Fatalf("expected one run, got %d", provider.runCount())
    }
}

/* the type-keyed container creation takes over as the name-keyed one does */
func TestCreationGuard_ATypeKeyedContainerCreationIsTakenOver(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)
    provider := newTakeoverProvider()
    entered, release := provider.blockRun(1)
    provider.failRun(1, func(ctx context.Context) error { return ctx.Err() })
    MustRegister[*waitingResolverProbe](serviceContainer, "app.takeover", provider.provide, RegisterOptions{})

    probeType := reflect.TypeFor[*waitingResolverProbe]()

    ownerContext, cancelOwner := takeoverCallerContext(t.Context(), "owner")
    ownerDone := make(chan error, 1)
    go func() {
        _, ownerErr := serviceContainer.GetByType(ownerContext, probeType)
        ownerDone <- ownerErr
    }()

    <-entered

    waiterDone := make(chan error, 1)
    go func() {
        _, waiterErr := serviceContainer.GetByType(t.Context(), probeType)
        waiterDone <- waiterErr
    }()

    awaitCreationWaiterOnAnyKey(t, serviceContainer, 1)
    cancelOwner()
    close(release)
    <-ownerDone

    if waiterErr := <-waiterDone; nil != waiterErr {
        t.Fatalf("expected the type-keyed waiter to build again, got %v", waiterErr)
    }

    if 2 != provider.runCount() {
        t.Fatalf("expected a takeover run, got %d", provider.runCount())
    }
}

/* a scoped creation is taken over by a live waiter of the scope, and a waiter whose scope closed meanwhile refuses without building */
func TestCreationGuard_AScopedCreationIsTakenOverAndAClosedScopeIsNot(t *testing.T) {
    for _, closeScope := range []bool{false, true} {
        serviceContainer := NewContainer(ContainerConfig{}).(*container)
        provider := newTakeoverProvider()
        entered, release := provider.blockRun(1)
        provider.failRun(1, func(ctx context.Context) error { return ctx.Err() })
        MustRegisterScoped[*waitingResolverProbe](serviceContainer, "app.scoped.takeover", provider.provide, RegisterOptions{})

        scopeInstance := serviceContainer.NewScope()

        ownerContext, cancelOwner := takeoverCallerContext(t.Context(), "owner")
        ownerDone := make(chan error, 1)
        go func() {
            _, ownerErr := scopeInstance.Get(ownerContext, "app.scoped.takeover")
            ownerDone <- ownerErr
        }()

        <-entered

        waiterDone := make(chan error, 1)
        go func() {
            _, waiterErr := scopeInstance.Get(t.Context(), "app.scoped.takeover")
            waiterDone <- waiterErr
        }()

        awaitScopedCreationWaiter(t, serviceContainer, scopeInstance.(*scope), "app.scoped.takeover", 1)

        /* the scope reads as closed without its done channel closing, so the waiter is released by the creation's end and meets the closed scope on the takeover path */
        restoreScope := func() {}
        if true == closeScope {
            restoreScope = markScopeClosedForTest(scopeInstance)
        }

        cancelOwner()
        close(release)
        <-ownerDone

        waiterErr := <-waiterDone
        restoreScope()
        if false == closeScope && nil != waiterErr {
            t.Fatalf("expected the scoped waiter to build again, got %v", waiterErr)
        }

        if true == closeScope && false == errors.Is(waiterErr, ErrScopeClosed) {
            t.Fatalf("expected the waiter of a closed scope refused, got %v", waiterErr)
        }

        expectedRuns := 2
        if true == closeScope {
            expectedRuns = 1
        }

        if expectedRuns != provider.runCount() {
            t.Fatalf("close scope %v: expected %d runs, got %d", closeScope, expectedRuns, provider.runCount())
        }

        _ = scopeInstance.Close()
    }
}

/* the windows no deterministic pin holds open: callers cancelled at random points, a provider failing only when its own context is done; one build at a time, one success, every live caller holds it, nothing left behind */
func TestCreationGuard_TakeoverUnderConcurrentCancellationsKeepsOneBuildAtATime(t *testing.T) {
    const callerCount = 64

    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    var concurrent atomic.Int32
    var successes atomic.Int32
    MustRegister[*waitingResolverProbe](serviceContainer, "app.stress", func(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
        if 1 < concurrent.Add(1) {
            t.Errorf("two builds ran at once")
        }
        defer concurrent.Add(-1)

        ctx := resolver.Runtime().Context()
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        case <-time.After(time.Duration(rand.Intn(300)) * time.Microsecond):
        }

        if nil != ctx.Err() {
            return nil, ctx.Err()
        }

        successes.Add(1)

        return &waitingResolverProbe{value: "built"}, nil
    }, RegisterOptions{})

    type answer struct {
        value any
        err   error
        live  bool
    }

    answers := make(chan answer, callerCount)
    for index := range callerCount {
        go func() {
            ctx, cancel := context.WithCancel(t.Context())
            defer cancel()

            if 0 == index%3 {
                go func() {
                    time.Sleep(time.Duration(rand.Intn(200)) * time.Microsecond)
                    cancel()
                }()
            }

            value, getErr := serviceContainer.Get(ctx, "app.stress")
            answers <- answer{value: value, err: getErr, live: nil == ctx.Err()}
        }()
    }

    var instance any
    for range callerCount {
        result := <-answers
        if nil == result.err {
            if nil == instance {
                instance = result.value
            }

            if instance != result.value {
                t.Fatalf("expected one instance")
            }

            continue
        }

        if true == result.live {
            t.Fatalf("a live caller answered %v", result.err)
        }
    }

    if 1 < successes.Load() {
        t.Fatalf("expected at most one successful build, got %d", successes.Load())
    }

    serviceContainer.mutex.RLock()
    defer serviceContainer.mutex.RUnlock()
    if 0 != len(serviceContainer.creatingByName) {
        t.Fatalf("expected no creation state left")
    }
}

func markScopeClosedForTest(scopeInstance containercontract.Scope) func() {
    concrete := scopeInstance.(*scope)
    saved := concrete.container.Load()
    concrete.container.Store(nil)

    return func() {
        concrete.container.Store(saved)
    }
}

func awaitCreationWaiterOnAnyKey(t *testing.T, serviceContainer *container, waiterCount int) {
    t.Helper()

    deadline := time.Now().Add(creationWaiterBudget)
    for {
        serviceContainer.mutex.RLock()
        waiters := 0
        for _, state := range serviceContainer.creatingByType {
            waiters += len(state.waiterContextIds)
        }
        for _, state := range serviceContainer.creatingByName {
            waiters += len(state.waiterContextIds)
        }
        serviceContainer.mutex.RUnlock()

        if waiterCount <= waiters {
            return
        }

        if true == time.Now().After(deadline) {
            t.Fatalf("expected %d waiters on a type-keyed creation within %s", waiterCount, creationWaiterBudget)
        }

        time.Sleep(time.Millisecond)
    }
}
