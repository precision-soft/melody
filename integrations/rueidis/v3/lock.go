package rueidis

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "strconv"
    "sync/atomic"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/redis/rueidis"
)

/* defaultLockerCallTimeout is the budget of one round trip, the one the token store and the server-sent event backplane in this package give theirs. A lock round trip is one Lua script — a compare-and-set, a compare-and-delete or a compare-and-extend — so a healthy store answers in a few milliseconds; the budget only has to sit under the client's own connection timeout, which is what bounded the call before this option existed, and under the renewal cadence the framework's lock helpers run Refresh at, which they bound on their own. */
const defaultLockerCallTimeout = time.Second

var lockAcquireScript = rueidis.NewLuaScript(`local current = redis.call("get", KEYS[1])
if current == false or current == ARGV[1] then
    redis.call("set", KEYS[1], ARGV[1], "PX", tonumber(ARGV[2]))
    return 1
end
return 0`)

var lockReleaseScript = rueidis.NewLuaScript(`if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`)

var lockRefreshScript = rueidis.NewLuaScript(`if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("pexpire", KEYS[1], ARGV[2]) else return 0 end`)

func NewLocker(client rueidis.Client) *Locker {
    return NewLockerWithOptions(client)
}

/* NewLockerWithOptions is NewLocker with options — WithLockerCallTimeout above all. The option-less door keeps its signature and builds the same locker at the defaults. */
func NewLockerWithOptions(client rueidis.Client, options ...LockerOption) *Locker {
    if nil == client {
        exception.Panic(exception.NewError("redis lock client is nil", nil, nil))
    }

    locker := &Locker{
        client:      client,
        callTimeout: defaultLockerCallTimeout,
    }

    for _, option := range options {
        option(locker)
    }

    return locker
}

type LockerOption func(*Locker)

/* WithLockerCallTimeout bounds one round trip of every door of a lock this locker creates — Acquire, Release and Refresh — by capping the runtime context with it, so a request whose context carries no deadline — melody's http kernel attaches none — still fails fast, while a caller that already carries a tighter deadline keeps it: the framework's RunExclusive and LeaderGate renew and release under deadlines of their own, and those stay the ones that end their calls. Without a bound a store that accepts connections but stops answering holds each of these Lua scripts for the client's own connection timeout, five seconds at the provider's default, which is what a readiness handler taking the lock on the request path, or a leader gate campaigning on the caller's context, then waits on every attempt. A non-positive timeout falls back to the default, following this package's zero-means-default convention, so a config-sourced unset value can never build an already-cancelled context that refuses every acquire; the cache subpackage deliberately reads its command timeout the other way and says so on its own option. */
func WithLockerCallTimeout(timeout time.Duration) LockerOption {
    return func(locker *Locker) {
        if 0 >= timeout {
            timeout = defaultLockerCallTimeout
        }

        locker.callTimeout = timeout
    }
}

type Locker struct {
    client      rueidis.Client
    callTimeout time.Duration
}

func (instance *Locker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &redisLock{
        client:      instance.client,
        name:        name,
        ttl:         ttl,
        token:       newLockToken(),
        callTimeout: instance.callTimeout,
    }
}

type redisLock struct {
    client      rueidis.Client
    name        string
    ttl         time.Duration
    token       string
    callTimeout time.Duration

    /* held records whether this handle believes it currently owns the lease, so the give-back of an AMBIGUOUS acquire can tell a lease this acquire may have taken from one this handle already legitimately owned. Acquire is reentrant on this lock's own token by contract, and the token is minted once per handle, so without this the two are indistinguishable at the store: both are the key carrying this token. It is atomic because the framework's helpers drive one lock from more than one goroutine — RunExclusive and LeaderGate renew and release off the path that acquired. */
    held atomic.Bool
}

/* callContext caps the runtime context with the call timeout: context.WithTimeout keeps whichever deadline is earlier, so a caller that already carries a tighter deadline — the framework's lock helpers renew and release under one of their own — still wins, while a request whose context has no deadline, as melody's http kernel leaves it, is bounded here rather than held for the client's own connection timeout. */
func (instance *redisLock) callContext(runtimeInstance runtimecontract.Runtime) (context.Context, context.CancelFunc) {
    return context.WithTimeout(runtimeInstance.Context(), instance.callTimeout)
}

/* Acquire requires a positive ttl. Redis locks are leases — the key's expiry IS the crash safety — so a non-positive ttl would write a key with no expiry at all, and a holder that dies before releasing would strand the lock forever: every later acquirer, on every instance, would skip its work with no error to show for it. Session-style behavior (hold until the connection drops) belongs to the MySQL GET_LOCK and PostgreSQL advisory lockers, whose Refresh is a liveness probe; this backend fails closed instead of pretending to offer it. */
func (instance *redisLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    if 0 >= instance.ttl {
        return false, exception.NewError(
            "redis lock requires a positive ttl; a redis lock is a lease and a non-positive ttl would never expire (session-style locks are the mysql/pgsql backends)",
            map[string]any{"name": instance.name, "ttl": instance.ttl.String()},
            nil,
        )
    }

    milliseconds := strconv.FormatInt(floorPositiveMilliseconds(instance.ttl), 10)

    callContext, cancel := instance.callContext(runtimeInstance)
    defer cancel()

    result := lockAcquireScript.Exec(callContext, instance.client, []string{instance.name}, []string{instance.token, milliseconds})

    acquired, resultErr := result.AsInt64()
    if nil != resultErr {
        instance.releaseAmbiguousAcquire()

        return false, exception.NewError("redis lock acquire failed", map[string]any{"name": instance.name}, resultErr)
    }

    if 1 == acquired {
        instance.held.Store(true)
    }

    return 1 == acquired, nil
}

/* releaseAmbiguousAcquire gives back a lease this acquire may have taken without ever learning that it did. An acquire that ends in an error ends AMBIGUOUSLY: the call is bounded, so a store that answers late — or a connection that drops after the server ran the script — leaves the compare-and-set executed and the key holding THIS lock's token for its whole ttl, while the caller is told it did not get the lock. Nothing else can clear it: every later campaign mints a fresh token, deliberately, so the compare-and-set refuses all of them until the ttl lapses with nobody holding the lock — measured on a live store, a one-second budget bought a twenty-nine-second lockout.

   It runs DETACHED, on a goroutine and on a context of its own, for two reasons that pull the same way: the caller's context is often the very thing that ended the acquire, and a caller that carries a deadline TIGHTER than the call timeout must keep it — charging it a second round trip would take an acquire refused in ten milliseconds to a full budget. A failure is silent, and a process that exits before it lands leaves exactly the lease a crash would leave, which is what the ttl is documented to cover.

   It is refused outright when this handle ALREADY holds the lease. The compare-and-delete removes a key carrying this lock's own token, and the holder of such a key can be this very handle: acquire is reentrant on its own token — LOCK.md states it as contract and both backends are pinned to it — so a caller that holds the lock and re-acquires it, as a renewal loop does, meets a give-back that would delete the lease it is still inside. At the store the two cases are one and the same key, and only the handle knows which; the flag is what it knows. The direction of the remaining error matters and is chosen: a stale true skips a give-back and leaves the lease to lapse on its ttl, which is the lockout this door was built to shorten, while a stale false takes a live lease and lets two callers into one critical section. */
func (instance *redisLock) releaseAmbiguousAcquire() {
    if true == instance.held.Load() {
        return
    }

    go func() {
        /* a panic on a bare goroutine takes the process down with it, and this one runs for a caller that has already been answered */
        defer func() { _ = recover() }()

        releaseContext, cancel := context.WithTimeout(context.Background(), instance.callTimeout)
        defer cancel()

        _ = lockReleaseScript.Exec(releaseContext, instance.client, []string{instance.name}, []string{instance.token}).Error()
    }()
}

/* Release gives up this handle's claim BEFORE the round trip rather than after it, so an ambiguous release cannot leave the handle claiming a lease it may no longer hold: the caller is done with the lock either way, and a claim kept past that point would silence the give-back of a later ambiguous acquire and strand exactly the lease that door exists to reclaim. */
func (instance *redisLock) Release(runtimeInstance runtimecontract.Runtime) error {
    instance.held.Store(false)

    callContext, cancel := instance.callContext(runtimeInstance)
    defer cancel()

    result := lockReleaseScript.Exec(callContext, instance.client, []string{instance.name}, []string{instance.token})
    if resultErr := result.Error(); nil != resultErr {
        return exception.NewError("redis lock release failed", map[string]any{"name": instance.name}, resultErr)
    }

    return nil
}

func (instance *redisLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    if 0 >= ttl {
        return exception.NewError("redis lock refresh ttl must be positive", map[string]any{"name": instance.name}, nil)
    }

    milliseconds := strconv.FormatInt(floorPositiveMilliseconds(ttl), 10)

    callContext, cancel := instance.callContext(runtimeInstance)
    defer cancel()

    result := lockRefreshScript.Exec(callContext, instance.client, []string{instance.name}, []string{instance.token, milliseconds})

    refreshed, resultErr := result.AsInt64()
    if nil != resultErr {
        return exception.NewError("redis lock refresh failed", map[string]any{"name": instance.name}, resultErr)
    }

    if 0 == refreshed {
        /* the store has answered that the key is gone or carries another token, which is the one reading that settles the question this handle cannot settle on its own; a refresh that merely ENDED AMBIGUOUSLY leaves the claim standing, because there the safe reading is that the lease is still ours */
        instance.held.Store(false)

        return exception.NewError("redis lock is no longer held", map[string]any{"name": instance.name}, nil)
    }

    return nil
}

/* floorPositiveMilliseconds guarantees a positive window never collapses to a 0 PEXPIRE argument, which Redis rejects. */
func floorPositiveMilliseconds(ttl time.Duration) int64 {
    milliseconds := ttl.Milliseconds()
    if 0 == milliseconds {
        return 1
    }

    return milliseconds
}

func newLockToken() string {
    buffer := make([]byte, 16)

    _, readErr := rand.Read(buffer)
    if nil != readErr {
        exception.Panic(exception.NewError("could not generate a lock token", nil, readErr))
    }

    return hex.EncodeToString(buffer)
}

var _ lockcontract.Locker = (*Locker)(nil)
var _ lockcontract.Lock = (*redisLock)(nil)
