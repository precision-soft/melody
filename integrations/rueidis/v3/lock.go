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

/* defaultLockerCallTimeout is the budget of one round trip, the one the token store and the server-sent event backplane in this package give theirs. A lock round trip is one Lua script — a compare-and-set, a compare-and-delete or a compare-and-extend — so a healthy store answers in a few milliseconds; the budget only has to sit under the client's own connection timeout, which is what bounded the call before this option existed, and under the renewal cadence the framework's lock helpers run Refresh at, which they bound on their own. */
const defaultLockerCallTimeout = time.Second

/* lockAcquireScript answers 1 for a lease TAKEN under ARGV[1] and 2 for one this handle already holds whose expiry it has just pushed out. ARGV[2] is the token this handle currently holds, and a handle holding none passes its new token there, so the second comparison can never match a token belonging to somebody else. A re-acquisition leaves the stored value ALONE, so the token of an attempt that ended without an answer is on the key only when that attempt took a FRESH lease — which is what lets its give-back name the attempt rather than the handle.

   The comparison against ARGV[1] is the incumbent one for a handle holding nothing and CANNOT fire for any other: the acquisition token is minted in the call that runs this script, and the only writer of it to the key is the set on the other side of the branch above. It is written as a disjunction rather than as one comparison because the two arguments carry different questions — the token being offered and the token being held — and a client that ever retried a write would need both read; this one does not, so the second is what answers. */
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
        callTimeout: instance.callTimeout,
    }
}


type redisLock struct {
    client      rueidis.Client
    name        string
    ttl         time.Duration
    callTimeout time.Duration

    /* mutex serialises the three doors, the form the mysql and pgsql backends of this repo use. It is what lets held be a plain field: with the doors serial, no write of it can land between another door's read and its own write. */
    mutex sync.Mutex
    held  string
}

/* heldToken answers the lease this handle currently holds, or the empty string before its first acquisition and after a release or a lost refresh. */
func (instance *redisLock) heldToken() string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.held
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

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    milliseconds := strconv.FormatInt(floorPositiveMilliseconds(instance.ttl), 10)

    /* the token of THIS acquisition, minted before the call so the give-back below can name the attempt rather than the handle. A handle that holds nothing passes its new token as the incumbent too, which makes the script's second comparison compare the new token with itself and so unable to match anybody else's lease. */
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
        /* a context already done before the call refuses the command before it is written, so no lease can have been taken and the give-back would be a round trip against a store this call never reached. It is a narrow test — it says nothing about a dial that failed or a deadline that fired mid-flight, which stay ambiguous and are still given back — but it is the one case the caller's own state proves. */
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
        /* an extension left the stored value untouched, so what this handle holds is the incumbent it offered — which is what it already held, since a handle holding none cannot reach this branch */
        instance.held = incumbentToken

        return true, nil
    }

    /* the store has answered that the key carries a token this handle does not own, which settles what the handle could not settle on its own: the claim is dropped here rather than left to be re-offered as an incumbent that can never match, and to keep Release the round-trip-free no-op its contract promises */
    instance.held = ""

    return false, nil
}

const (
    /* lockAcquireTaken and lockAcquireExtended are the two ways lockAcquireScript says yes: a lease newly written under this acquisition's token, and one this handle already held whose expiry was pushed out without its value changing. */
    lockAcquireTaken    = int64(1)
    lockAcquireExtended = int64(2)
)

/* releaseAmbiguousAcquire gives back a lease one acquisition may have taken without ever learning that it did. An acquire that ends in an error ends AMBIGUOUSLY: the call is bounded, so a store that answers late — or a connection that drops after the server ran the script — leaves the compare-and-set executed and the key holding that acquisition's token for its whole ttl, while the caller is told it did not get the lock. Nothing else can clear it: every later acquisition mints a fresh token, so the compare-and-delete refuses all of them until the ttl lapses with nobody holding the lock — measured on a live store, a one-second budget bought a twenty-nine-second lockout.

   It deletes the token of THAT acquisition and nothing else, which is what makes it safe with no state to consult. A re-acquisition by a handle that already holds the lease never writes its token — the script extends the stored value instead of replacing it — so when the ambiguous attempt was a re-acquisition this delete matches nothing and the lease the caller is still inside is untouched. When it was a fresh take, the token it deletes is exactly the lease nobody believes they hold. No door of this handle can ever adopt that token: every acquisition mints its own and offers only what the handle HOLDS as the incumbent, so the delete can never take a lease a later acquire was granted.

   It runs DETACHED, on a goroutine and on a context of its own, for two reasons that pull the same way: the caller's context is often the very thing that ended the acquire, and a caller that carries a deadline TIGHTER than the call timeout must keep it — charging it a second round trip would take an acquire refused in ten milliseconds to a full budget. A failure is silent, and a process that exits before it lands leaves exactly the lease a crash would leave, which is what the ttl is documented to cover. */
func (instance *redisLock) releaseAmbiguousAcquire(acquisitionToken string) {
    go func() {
        /* a panic on a bare goroutine takes the process down with it, and this one runs for a caller that has already been answered */
        defer func() { _ = recover() }()

        releaseContext, cancel := context.WithTimeout(context.Background(), instance.callTimeout)
        defer cancel()

        _ = lockReleaseScript.Exec(releaseContext, instance.client, []string{instance.name}, []string{acquisitionToken}).Error()
    }()
}

/* Release gives up the lease this handle holds. A handle that holds none returns without a round trip, which is the published contract — releasing a lock this instance no longer holds is a no-op that reports no error — and is also the only correct answer, since there is no token to compare against.

   The claim is dropped only once the store has ANSWERED. A release whose round trip never landed leaves the lease standing under a token nothing else can name, so a claim dropped before the answer would report success over a lease still held on the retry and leave every later acquire refused until the ttl lapsed. */
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

    /* a handle that holds no lease has nothing to extend, and says so with the same error a lost lease gets: Refresh is the authoritative liveness check by published contract, so it must answer "no longer held" rather than a silent success */
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
        /* the store has answered that the key is gone or carries another token, which is the one reading that settles the question this handle cannot settle on its own; a refresh that merely ENDED AMBIGUOUSLY leaves the claim standing, because there the safe reading is that the lease is still ours */
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
