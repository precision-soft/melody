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

    shard.mutex.Lock()

    removeWaiterReturned := make(chan struct{})
    go func() {
        call.RemoveWaiter(shard)
        close(removeWaiterReturned)
    }()

    time.Sleep(50 * time.Millisecond)

    if true == call.IsCanceled() {
        shard.mutex.Unlock()
        <-removeWaiterReturned
        t.Fatalf("joiner observed a canceled in-flight call while still holding the shard mutex")
    }

    call.AddWaiter()
    shard.mutex.Unlock()

    <-removeWaiterReturned

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
