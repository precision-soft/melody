package cache

import (
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

/* @info a zero timeout means no waiting, not no answer: a result already memoized is taken without blocking, and only a flight still in the air answers with the timeout */
func TestRememberInFlightCall_WaitZeroTakesACompletedResult(t *testing.T) {
    completedCall := newRememberInFlightCall(false)
    completedCall.Complete("ready", nil)

    value, waitErr := completedCall.Wait(0, "k")
    if nil != waitErr {
        t.Fatalf("expected the completed result without waiting, got: %v", waitErr)
    }
    if "ready" != value.(string) {
        t.Fatalf("unexpected value: %v", value)
    }

    pendingCall := newRememberInFlightCall(false)

    _, pendingErr := pendingCall.Wait(0, "k")
    if nil == pendingErr {
        t.Fatalf("expected the pending flight to answer with the timeout")
    }
}
