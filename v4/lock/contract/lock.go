package contract

import (
    "errors"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* LockTtlRule is the reading every locker gives the ttl it is handed: 30s when left out, at least 1ms, unlimited the session-style lock that holds until its connection drops, which a lease locker refuses through ResolveLeaseTtl and ResolveLeaseRefreshTtl, since its lease would never lapse. */
var LockTtlRule = bound.Rule[time.Duration]{
    Name:              "Locker.CreateLock.ttl",
    Default:           30 * time.Second,
    Minimum:           bound.TimeoutMinimum,
    UnlimitedAdmitted: true,
}

/* ErrUnlimitedTtlRefused is the cause of a lease lock's refusal of an unlimited ttl: a configuration no retry can cure, unlike a store outage. */
var ErrUnlimitedTtlRefused = errors.New("a lease lock does not admit an unlimited ttl")

/* ResolveLeaseTtl decides the ttl a lease locker writes at Acquire against LockTtlRule: absent takes the 30s default, a value below 1ms is refused, and unlimited is refused, naming the ttl, since a lease that never lapses outlives a crashed holder. */
func ResolveLeaseTtl(ttl bound.Duration) (time.Duration, error) {
    return resolveLeaseTtl(ttl, LockTtlRule)
}

/* ResolveLeaseRefreshTtl decides the ttl a lease locker writes at Refresh as ResolveLeaseTtl does, a refusal naming Lock.Refresh.ttl. */
func ResolveLeaseRefreshTtl(ttl bound.Duration) (time.Duration, error) {
    rule := LockTtlRule
    rule.Name = "Lock.Refresh.ttl"

    return resolveLeaseTtl(ttl, rule)
}

func resolveLeaseTtl(ttl bound.Duration, rule bound.Rule[time.Duration]) (time.Duration, error) {
    resolved, err := bound.Resolve(ttl, rule)
    if nil != err {
        return 0, err
    }

    if true == resolved.Unlimited {
        return 0, exception.NewError(
            "a lease lock does not admit an unlimited ttl",
            exceptioncontract.Context{
                "field": rule.Name,
            },
            ErrUnlimitedTtlRefused,
        )
    }

    return resolved.Value, nil
}

type Lock interface {
    Acquire(runtimeInstance runtimecontract.Runtime) (bool, error)

    Release(runtimeInstance runtimecontract.Runtime) error

    Refresh(runtimeInstance runtimecontract.Runtime, ttl bound.Duration) error
}

type Locker interface {
    CreateLock(name string, ttl bound.Duration) Lock
}

/* LeaseLocker marks a Locker whose every lock is a lease, so an unlimited ttl can be refused where the locker is handed over rather than at the first Acquire. */
type LeaseLocker interface {
    Locker

    LeaseOnly()
}
