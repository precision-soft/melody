package cache

import (
    "context"
    "errors"
    "strings"
    "testing"
    "time"
)

func TestRememberInFlightCall_RemoveWaiterDoesNotPoisonAConcurrentJoiner(t *testing.T) {
    shard := &rememberInFlightShard{
        inFlightByKey: make(map[string]*rememberInFlightCall),
    }

    call := newRememberInFlightCall(true)
    call.AddWaiter()
    shard.inFlightByKey["k"] = call

    /* A fresh joiner enters rememberWithStampedeProtection's critical section: it holds the shard mutex, has just found the in-flight call and is about to inspect IsCanceled and AddWaiter. */
    shard.mutex.Lock()

    removeWaiterReturned := make(chan struct{})
    go func() {
        /* The last existing waiter times out and leaves. */
        call.RemoveWaiter(shard)
        close(removeWaiterReturned)
    }()

    /* Give the departing waiter's RemoveWaiter time to run. When the decrement-to-zero and the cancel decision are made outside the shard mutex, it observes zero waiters and cancels the call right here, under the joiner's feet. */
    time.Sleep(50 * time.Millisecond)

    if true == call.IsCanceled() {
        shard.mutex.Unlock()
        <-removeWaiterReturned
        t.Fatalf("joiner observed a canceled in-flight call while still holding the shard mutex")
    }

    /* The joiner commits to joining the healthy call under the same mutex. */
    call.AddWaiter()
    shard.mutex.Unlock()

    <-removeWaiterReturned

    /* Because the joiner incremented the waiter count under the mutex, the departing waiter must not have canceled the call. */
    if true == call.IsCanceled() {
        t.Fatalf("healthy joiner was poisoned by the departing waiter's cancel")
    }
}

func TestRememberInFlightCall_WaitZeroTakesACompletedResult(t *testing.T) {
    completedCall := newRememberInFlightCall(false)
    completedCall.Complete("ready", nil)

    value, waitErr := completedCall.Wait(context.Background(), 0, "k")
    if nil != waitErr {
        t.Fatalf("expected the completed result without waiting, got: %v", waitErr)
    }
    if "ready" != value.(string) {
        t.Fatalf("unexpected value: %v", value)
    }

    pendingCall := newRememberInFlightCall(false)

    _, pendingErr := pendingCall.Wait(context.Background(), 0, "k")
    if nil == pendingErr {
        t.Fatalf("expected the pending flight to answer with the timeout")
    }
}

/* the wait is driven from a goroutine and given a deadline of its own: a wait that does NOT end on the context is the defect under test, so letting it run to the package timeout would report the same failure ten minutes later and with the reason buried in a panic dump */
func waitForRememberCallAnswer(
    t *testing.T,
    call *rememberInFlightCall,
    callerContext context.Context,
    waitTimeout time.Duration,
) error {
    t.Helper()

    answerChannel := make(chan error, 1)

    go func() {
        _, waitErr := call.Wait(callerContext, waitTimeout, "report:daily")
        answerChannel <- waitErr
    }()

    select {
    case waitErr := <-answerChannel:
        return waitErr
    case <-time.After(2 * time.Second):
        t.Fatalf("the wait did not end on the caller context within 2s, wait timeout %s", waitTimeout)
    }

    return nil
}

func TestRememberInFlightCall_WaitEndsOnACanceledCallerContextUnderAnUnboundedWait(t *testing.T) {
    call := newRememberInFlightCall(false)

    canceledContext, cancel := context.WithCancel(context.Background())
    cancel()

    waitErr := waitForRememberCallAnswer(t, call, canceledContext, -1)
    if nil == waitErr {
        t.Fatal("expected the unbounded wait to end on the caller context")
    }

    if false == errors.Is(waitErr, context.Canceled) {
        t.Fatalf("expected context.Canceled to survive as the cause, got %v", waitErr)
    }

    if false == strings.Contains(waitErr.Error(), "canceled by the caller context") {
        t.Fatalf("expected the cancellation to be told apart from the timeout, got %v", waitErr)
    }

    select {
    case <-call.done:
        t.Fatal("the caller leaving must not complete the flight")
    default:
    }
}

func TestRememberInFlightCall_WaitEndsOnACanceledCallerContextUnderATimeout(t *testing.T) {
    call := newRememberInFlightCall(false)

    canceledContext, cancel := context.WithCancel(context.Background())
    cancel()

    waitErr := waitForRememberCallAnswer(t, call, canceledContext, time.Hour)
    if nil == waitErr {
        t.Fatal("expected the timed wait to end on the caller context")
    }

    if false == errors.Is(waitErr, context.Canceled) {
        t.Fatalf("expected context.Canceled to survive as the cause, got %v", waitErr)
    }
}

func TestRememberInFlightCall_WaitTakesAMemoizedResultOverACanceledCallerContext(t *testing.T) {
    call := newRememberInFlightCall(false)
    call.Complete("ready", nil)

    canceledContext, cancel := context.WithCancel(context.Background())
    cancel()

    for _, waitTimeout := range []time.Duration{-1, time.Hour} {
        value, waitErr := call.Wait(canceledContext, waitTimeout, "report:daily")
        if nil != waitErr {
            t.Fatalf("wait timeout %s: expected the memoized result, got: %v", waitTimeout, waitErr)
        }
        if "ready" != value.(string) {
            t.Fatalf("wait timeout %s: unexpected value: %v", waitTimeout, value)
        }
    }
}

func TestRememberInFlightCall_WaitWithoutACallerContextParksUntilTheFlightAnswers(t *testing.T) {
    call := newRememberInFlightCall(false)

    go func() {
        time.Sleep(20 * time.Millisecond)
        call.Complete("late", nil)
    }()

    value, waitErr := call.Wait(context.Background(), -1, "report:daily")
    if nil != waitErr {
        t.Fatalf("expected the flight's answer, got: %v", waitErr)
    }
    if "late" != value.(string) {
        t.Fatalf("unexpected value: %v", value)
    }
}

/* the pre-select above the two blocking selects exists to take the coin out of a tie: with a memoized result AND a lapsed caller context both ready, a select chooses uniformly at random, so the same call answered the value or the cancellation from one run to the next. A single-shot assertion cannot prove that guard — measured, the mutant that removes it is killed four runs in five, which is luck, not proof — so the tie is built sixty-four times and every one of them has to answer the value. */
func TestRememberInFlightCall_AMemoizedResultBeatsALapsedContextEveryTime(t *testing.T) {
    canceledContext, cancel := context.WithCancel(context.Background())
    cancel()

    for attempt := 0; attempt < 64; attempt++ {
        call := newRememberInFlightCall(false)
        call.Complete("ready", nil)

        for _, waitTimeout := range []time.Duration{-1, time.Hour} {
            value, waitErr := call.Wait(canceledContext, waitTimeout, "report:daily")
            if nil != waitErr {
                t.Fatalf("attempt %d, wait timeout %s: expected the memoized result, got: %v", attempt, waitTimeout, waitErr)
            }
            if "ready" != value.(string) {
                t.Fatalf("attempt %d, wait timeout %s: unexpected value: %v", attempt, waitTimeout, value)
            }
        }
    }
}
