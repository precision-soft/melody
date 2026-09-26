package rueidis

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "strconv"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/redis/rueidis"
)

/* defaultLockerCallTimeout is the budget of one round trip, as the token store and the server-sent event backplane in this package give theirs. A lock round trip is one Lua script, a few milliseconds on a healthy store; the budget sits under the client's connection timeout and under the renewal cadence of the framework's lock helpers, which bound Refresh on their own. */
const defaultLockerCallTimeout = time.Second

/* lockAcquireScript answers 1 for a lease taken under ARGV[1] and 2 for one this handle already holds whose expiry it has just pushed out. ARGV[2] is the token this handle holds, and a handle holding none passes its new token there, so the second comparison can never match another holder's token. A re-acquisition leaves the stored value alone, so the token of an attempt that ended without an answer is on the key only when that attempt took a fresh lease, which lets its give-back name the attempt rather than the handle. The comparison against ARGV[1] cannot fire for a handle that holds a lease, since the acquisition token is minted in the call that runs this script and only the set on the other branch writes it. It is written as a disjunction because the two arguments carry different questions, the token offered and the token held. */
var lockAcquireScript = rueidis.NewLuaScript(`local current = redis.call("get", KEYS[1])
if current == false then
    redis.call("set", KEYS[1], ARGV[1], "PX", tonumber(ARGV[3]))
    return 1
end
if current == ARGV[1] or current == ARGV[2] then
    redis.call("pexpire", KEYS[1], tonumber(ARGV[3]))
    return 2
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

/* WithLockerCallTimeout bounds one round trip of Acquire, Release and Refresh by capping the runtime context with it, so a request whose context carries no deadline, as melody's http kernel leaves it, fails fast. The earlier of the two deadlines ends the call, so a looser budget declared by RunExclusive or LeaderGate is cut to this timeout, and a caller that needs longer raises it here. Without it a store that accepts connections but stops answering holds each script for the client's connection timeout, five seconds at the provider's default. A non-positive timeout falls back to the default, this package's zero-means-default convention; the cache subpackage reads its command timeout the other way and says so on its own option. */
func WithLockerCallTimeout(timeout time.Duration) LockerOption {
    return func(locker *Locker) {
        locker.callTimeout = resolvedCallTimeout(timeout, defaultLockerCallTimeout)
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
        callTimeout: instance.callTimeout,
    }
}

type redisLock struct {
    client      rueidis.Client
    name        string
    ttl         time.Duration
    callTimeout time.Duration

    /* mutex serialises the three doors, as the mysql and pgsql backends do, which is what lets held be a plain field: no write of it can land between another door's read and its own write. */
    mutex sync.Mutex
    held  string
}

/* heldToken answers the lease this handle currently holds, or the empty string before its first acquisition and after a release or a lost refresh. */
func (instance *redisLock) heldToken() string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.held
}

/* callContext caps the runtime context with the call timeout; context.WithTimeout keeps the earlier deadline, so a caller with a tighter one, as the framework's lock helpers renew and release under, still wins. */
func (instance *redisLock) callContext(runtimeInstance runtimecontract.Runtime) (context.Context, context.CancelFunc) {
    return context.WithTimeout(runtimeInstance.Context(), instance.callTimeout)
}

/* Acquire requires a positive ttl: a Redis lock is a lease whose expiry is the crash safety, so a key with no expiry would strand the lock forever when its holder died. Session-style locks that hold until the connection drops belong to the MySQL GET_LOCK and PostgreSQL advisory lockers; this backend fails closed. */
func (instance *redisLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    if 0 >= instance.ttl {
        return false, exception.NewError(
            "redis lock requires a positive ttl; a redis lock is a lease and a non-positive ttl would never expire (session-style locks are the mysql/pgsql backends)",
            map[string]any{"name": instance.name, "ttl": instance.ttl.String()},
            nil,
        )
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    milliseconds := strconv.FormatInt(floorPositiveMilliseconds(instance.ttl), 10)

    /* the token of this acquisition, minted before the call so the give-back below can name the attempt rather than the handle; a handle that holds nothing passes it as the incumbent too, so the script's second comparison cannot match another lease */
    acquisitionToken := newLockToken()

    incumbentToken := instance.held
    if "" == incumbentToken {
        incumbentToken = acquisitionToken
    }

    callContext, cancel := instance.callContext(runtimeInstance)
    defer cancel()

    dispatchContextErr := callContext.Err()

    result := lockAcquireScript.Exec(
        callContext,
        instance.client,
        []string{instance.name},
        []string{acquisitionToken, incumbentToken, milliseconds},
    )

    acquired, resultErr := result.AsInt64()
    if nil != resultErr {
        /* a context already done before the call refuses the command before it is written, so no lease was taken and no give-back is needed; a failed dial or a deadline firing mid-flight stays ambiguous and is still given back */
        if nil == dispatchContextErr {
            instance.releaseAmbiguousAcquire(acquisitionToken)
        }

        return false, exception.NewError("redis lock acquire failed", map[string]any{"name": instance.name}, resultErr)
    }

    if lockAcquireTaken == acquired {
        instance.held = acquisitionToken

        return true, nil
    }

    if lockAcquireExtended == acquired {
        /* an extension leaves the stored value untouched, so this handle holds the incumbent it offered, which it already held, since a handle holding none cannot reach this branch */
        instance.held = incumbentToken

        return true, nil
    }

    /* the store answered that the key carries a token this handle does not own, so the claim is dropped here rather than re-offered as an incumbent that can never match, keeping Release the round-trip-free no-op its contract promises */
    instance.held = ""

    return false, nil
}

const (
    /* lockAcquireTaken and lockAcquireExtended are the two ways lockAcquireScript says yes: a lease newly written under this acquisition's token, and one this handle already held whose expiry was pushed out without its value changing. */
    lockAcquireTaken    = int64(1)
    lockAcquireExtended = int64(2)
)

/* releaseAmbiguousAcquire gives back a lease one acquisition may have taken without learning that it did. An acquire that ends in an error is ambiguous: a late answer or a dropped connection can leave the compare-and-set executed and the key holding that acquisition's token for its whole ttl while the caller is told it did not get the lock, and since every later acquisition mints a fresh token nothing else can clear it until the ttl lapses. It deletes the token of that acquisition and nothing else, so it needs no state: a re-acquisition never writes its token, so the delete matches nothing and the lease the caller holds is untouched, and no door of this handle ever adopts that token, so the delete cannot take a lease a later acquire was granted. It runs detached, on a goroutine with its own context, because the caller's context often ended the acquire and a caller with a deadline tighter than the call timeout keeps it. A failure is silent, and a process that exits before it lands leaves the lease a crash would leave, which the ttl covers. */
func (instance *redisLock) releaseAmbiguousAcquire(acquisitionToken string) {
    go func() {
        /* a panic on a bare goroutine takes the process down with it, and this one runs for a caller that has already been answered */
        defer func() { _ = recover() }()

        releaseContext, cancel := context.WithTimeout(context.Background(), instance.callTimeout)
        defer cancel()

        _ = lockReleaseScript.Exec(releaseContext, instance.client, []string{instance.name}, []string{acquisitionToken}).Error()
    }()
}

/* Release gives up the lease this handle holds. A handle that holds none returns without a round trip, which is the published contract, a no-op reporting no error, and the only correct answer, since there is no token to compare. The claim is dropped only once the store answered, since a release whose round trip never landed leaves the lease standing under a token only this handle can name. */
func (instance *redisLock) Release(runtimeInstance runtimecontract.Runtime) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    heldToken := instance.held
    if "" == heldToken {
        return nil
    }

    callContext, cancel := instance.callContext(runtimeInstance)
    defer cancel()

    result := lockReleaseScript.Exec(callContext, instance.client, []string{instance.name}, []string{heldToken})
    if resultErr := result.Error(); nil != resultErr {
        return exception.NewError("redis lock release failed", map[string]any{"name": instance.name}, resultErr)
    }

    instance.held = ""

    return nil
}

func (instance *redisLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    if 0 >= ttl {
        return exception.NewError("redis lock refresh ttl must be positive", map[string]any{"name": instance.name}, nil)
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    /* a handle that holds no lease has nothing to extend and answers the error a lost lease gets, since Refresh is the authoritative liveness check by contract */
    heldToken := instance.held
    if "" == heldToken {
        return exception.NewError("redis lock is no longer held", map[string]any{"name": instance.name}, nil)
    }

    milliseconds := strconv.FormatInt(floorPositiveMilliseconds(ttl), 10)

    callContext, cancel := instance.callContext(runtimeInstance)
    defer cancel()

    result := lockRefreshScript.Exec(callContext, instance.client, []string{instance.name}, []string{heldToken, milliseconds})

    refreshed, resultErr := result.AsInt64()
    if nil != resultErr {
        return exception.NewError("redis lock refresh failed", map[string]any{"name": instance.name}, resultErr)
    }

    if 0 == refreshed {
        /* the store answered that the key is gone or carries another token, the one reading that settles the question; a refresh that ended ambiguously leaves the claim standing, since there the safe reading is that the lease is still held */
        instance.held = ""

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
