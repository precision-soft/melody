package cache

import (
    "context"
    "sync"
    "sync/atomic"
    "time"

    "github.com/precision-soft/melody/v2/exception"
)

const rememberInFlightShardCount = 64

type rememberInFlightShard struct {
    mutex         sync.Mutex
    inFlightByKey map[string]*rememberInFlightCall
}

var rememberInFlightShardList = buildRememberInFlightShardList()

func newRememberInFlightCall(isCancelable bool) *rememberInFlightCall {
    contextInstance := context.Background()
    var cancelFunc context.CancelFunc = nil

    if true == isCancelable {
        derivedContext, derivedCancelFunc := context.WithCancel(context.Background())
        contextInstance = derivedContext
        cancelFunc = derivedCancelFunc
    }

    return &rememberInFlightCall{
        done:         make(chan struct{}),
        context:      contextInstance,
        cancelFunc:   cancelFunc,
        isCancelable: isCancelable,
    }
}

type rememberInFlightCall struct {
    doneOnce sync.Once
    done     chan struct{}
    result   any
    err      error

    waitersCount atomic.Int64

    context      context.Context
    cancelOnce   sync.Once
    cancelFunc   context.CancelFunc
    isCancelable bool
}

func (instance *rememberInFlightCall) AddWaiter() {
    instance.waitersCount.Add(1)
}

/* RemoveWaiter takes the shard mutex around the decrement and the cancel decision so a late joiner can never slip in between the zero observation and cancelFunc; joiners inspect IsCanceled and call AddWaiter under the same mutex, so serializing here keeps a healthy joiner from inheriting a spurious cancellation. */
func (instance *rememberInFlightCall) RemoveWaiter(shard *rememberInFlightShard) {
    shard.mutex.Lock()
    defer shard.mutex.Unlock()

    remainingWaiters := instance.waitersCount.Add(-1)
    if 0 != remainingWaiters {
        return
    }

    if false == instance.isCancelable {
        return
    }

    if nil == instance.cancelFunc {
        return
    }

    instance.cancelOnce.Do(
        func() {
            instance.cancelFunc()
        },
    )
}

func (instance *rememberInFlightCall) Context() context.Context {
    return instance.context
}

func (instance *rememberInFlightCall) IsCanceled() bool {
    if false == instance.isCancelable {
        return false
    }

    return nil != instance.context.Err()
}

/* Wait ends on whichever comes first: the flight, the caller's context, or the timeout. The context door is what makes the wait belong to the request that opened it — a waiter without one parks for as long as the callback takes, so a client that disconnected an hour ago still holds the goroutine that served it, and with the goroutine its scope, its session and everything the request reached. It is the caller that leaves, never the flight: the value is still being computed for whoever else is coalesced onto it, and the cancelable option's own rule — the last waiter out cancels the computation — settles what happens when the last one leaves this way too. A background context contributes a nil channel, which no select ever chooses, so a caller that names none waits exactly as it always did. */
func (instance *rememberInFlightCall) Wait(callerContext context.Context, waitTimeout time.Duration, key string) (any, error) {
    /* a zero timeout means no waiting, not no answer: a result that is already memoized is taken without blocking, and only a flight still in the air answers with the timeout. There is nothing for a context to end here — the branch never blocks. */
    if 0 == waitTimeout {
        select {
        case <-instance.done:
            return instance.result, instance.err
        default:
        }

        return nil, newRememberWaitTimedOutError(key, waitTimeout)
    }

    /* an answer that is already memoized is taken whatever the caller's context says, the same way the zero-timeout branch above takes it: the work is done, handing it over costs nothing, and without this the select below would toss a coin between a finished flight and a context that lapsed in the same instant */
    select {
    case <-instance.done:
        return instance.result, instance.err
    default:
    }

    if 0 > waitTimeout {
        select {
        case <-instance.done:
            return instance.result, instance.err
        case <-callerContext.Done():
            return nil, newRememberWaitCanceledError(callerContext, key)
        }
    }

    timer := time.NewTimer(waitTimeout)
    defer timer.Stop()

    select {
    case <-instance.done:
        return instance.result, instance.err
    case <-callerContext.Done():
        return nil, newRememberWaitCanceledError(callerContext, key)
    case <-timer.C:
        return nil, newRememberWaitTimedOutError(key, waitTimeout)
    }
}

func newRememberWaitTimedOutError(key string, waitTimeout time.Duration) error {
    return exception.NewError(
        "cache remember callback timed out",
        map[string]any{
            "key":     key,
            "timeout": waitTimeout.String(),
        },
        nil,
    )
}

/* the cancellation is told apart from the timeout by message and by cause: one says the caller went away, the other says the callback is too slow, and an operator reading a spike of either needs to know which. The context error travels as the cause so errors.Is reaches context.Canceled and context.DeadlineExceeded. */
func newRememberWaitCanceledError(callerContext context.Context, key string) error {
    return exception.NewError(
        "cache remember wait canceled by the caller context",
        map[string]any{
            "key": key,
        },
        callerContext.Err(),
    )
}

func (instance *rememberInFlightCall) Complete(result any, err error) {
    instance.doneOnce.Do(
        func() {
            instance.result = result
            instance.err = err
            close(instance.done)
        },
    )
}
