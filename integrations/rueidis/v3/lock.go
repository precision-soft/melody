package rueidis

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "strconv"
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
        return false, exception.NewError("redis lock acquire failed", map[string]any{"name": instance.name}, resultErr)
    }

    return 1 == acquired, nil
}

func (instance *redisLock) Release(runtimeInstance runtimecontract.Runtime) error {
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
