package contract

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* FailedAttemptWindowRule is the reading every store gives the window of an Increment: at least 1s, the resolution of a stored ttl. It has no default and no unlimited: a count that never lapses locks its account for good. */
func FailedAttemptWindowRule() bound.Rule[time.Duration] {
    return bound.Rule[time.Duration]{
        Name:    "FailedAttemptStore.Increment.window",
        Minimum: bound.TtlMinimum,
    }
}

/* ResolveFailedAttemptWindow decides the window of an Increment against FailedAttemptWindowRule; an absent window, a value below the minimum and unlimited are refused, naming the window. */
func ResolveFailedAttemptWindow(window bound.Duration) (time.Duration, error) {
    if true == window.IsAbsent() {
        return 0, exception.NewError(
            "the failed attempt window is required",
            exceptioncontract.Context{
                "field": FailedAttemptWindowRule().Name,
            },
            nil,
        )
    }

    resolved, err := bound.Resolve(window, FailedAttemptWindowRule())
    if nil != err {
        return 0, err
    }

    return resolved.Value, nil
}

/* FailedAttemptCount is what a store answers for one counted attempt. */
type FailedAttemptCount struct {
    /* Count is the attempts counted inside the window the first one opened, this one included. */
    Count int

    /* WindowRemaining is how long until that window lapses; zero when the store cannot say. */
    WindowRemaining time.Duration
}

/* FailedAttemptStore counts the second factors an account presented and has not had accepted. Increment adds one under the key inside the window the first count opened, read against FailedAttemptWindowRule; a later count never extends it, and a refused window is answered as the error. Reset forgets the key. Implementations are expected to be safe for concurrent use and, in a multi-instance deployment, to share state between instances. */
type FailedAttemptStore interface {
    Increment(runtimeInstance containercontract.Runtime, key string, window bound.Duration) (FailedAttemptCount, error)

    Reset(runtimeInstance containercontract.Runtime, key string) error
}
