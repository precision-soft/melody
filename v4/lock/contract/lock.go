package contract

import (
    "errors"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* LockTtlRule is the reading every locker gives CreateLockOptions.Ttl: 30s when left out, at least 1ms, unlimited the session-style lock that holds until its connection drops, which a lease locker refuses through ResolveLeaseTtl and ResolveLeaseRefreshTtl, since its lease would never lapse. */
func LockTtlRule() bound.Rule[time.Duration] {
    return bound.Rule[time.Duration]{
        Name:              "CreateLockOptions.Ttl",
        Default:           30 * time.Second,
        Minimum:           bound.TimeoutMinimum,
        UnlimitedAdmitted: true,
    }
}

/* ErrUnlimitedTtlRefused is the cause of a lease lock's refusal of an unlimited ttl: a configuration no retry can cure, unlike a store outage. */
var ErrUnlimitedTtlRefused = errors.New("a lease lock does not admit an unlimited ttl")

/* ResolveLeaseTtl decides the ttl a lease locker writes at Acquire against LockTtlRule: absent takes the 30s default, a value below 1ms is refused, and unlimited is refused, naming the ttl, since a lease that never lapses outlives a crashed holder. */
func ResolveLeaseTtl(ttl bound.Duration) (time.Duration, error) {
    return resolveLeaseTtl(ttl, LockTtlRule())
}

/* ResolveLeaseRefreshTtl decides the ttl a lease locker writes at Refresh as ResolveLeaseTtl does, a refusal naming RefreshOptions.Ttl; the lock hands it its creation ttl when RefreshOptions.Ttl is left out. */
func ResolveLeaseRefreshTtl(ttl bound.Duration) (time.Duration, error) {
    rule := LockTtlRule()
    rule.Name = "RefreshOptions.Ttl"

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

/* CreateLockOptions is what one CreateLock declares beside the lock's name. */
type CreateLockOptions struct {
    /* Ttl is the lease handed to the locker, read against LockTtlRule: 30s when left out, at least 1ms, bound.UnlimitedDuration() the session-style lock, which a lease locker refuses. */
    Ttl bound.Duration
}

/* RefreshOptions is what one Refresh declares. */
type RefreshOptions struct {
    /* Ttl is the lease written from now: the ttl the lock was created with when left out, at least 1ms, unlimited refused by a lease locker. */
    Ttl bound.Duration
}

/* Lock is the door a locker implements; userland runs under RunExclusive or NewLeaderGate of the lock package instead, which release on a runtime detached from the caller's cancellation. It has no runtime-bound view: a lock outlives the runtime that took it, and Release bound to a cancelled request could not run until the ttl. */
type Lock interface {
    Acquire(runtimeInstance containercontract.Runtime) (bool, error)

    /* Release bounds its own round trip; it may be handed a context that is never done, as the framework's helpers release on one detached from the run's cancellation. */
    Release(runtimeInstance containercontract.Runtime) error

    /* Refresh extends the held lease; a lease lock reads its creation ttl for an absent RefreshOptions.Ttl. */
    Refresh(runtimeInstance containercontract.Runtime, options RefreshOptions) error
}

type Locker interface {
    /* CreateLock answers the lock of a name. A locker whose locks are leases refuses an unlimited CreateLockOptions.Ttl here, its cause ErrUnlimitedTtlRefused. It reaches no store. */
    CreateLock(name string, options CreateLockOptions) (Lock, error)
}

/* SharedLeaseLocker is what a locker answers when its locks are leases in a store every replica reaches: a held lock pins no connection and lapses by its ttl when its holder dies. It is optional and refuses nothing; a locker wrapping another answers what the wrapped one answers, under the runtime handed. */
type SharedLeaseLocker interface {
    Locker

    LeasesAcrossReplicas(runtimeInstance containercontract.Runtime) (bool, error)
}

/* LeasesAcrossReplicas answers what the locker says of itself, false for one that does not answer SharedLeaseLocker; the error is the one a wrapping locker met reaching the wrapped one. */
func LeasesAcrossReplicas(runtimeInstance containercontract.Runtime, locker Locker) (bool, error) {
    if true == internal.IsNilInterface(locker) {
        return false, nil
    }

    sharedLeaseLocker, isSharedLease := locker.(SharedLeaseLocker)
    if false == isSharedLease {
        return false, nil
    }

    return sharedLeaseLocker.LeasesAcrossReplicas(runtimeInstance)
}
