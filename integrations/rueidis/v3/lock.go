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

/* lockAcquireScript answers 1 for a lease TAKEN under ARGV[1], the ARGV index of the matching token for one this handle already owns and whose expiry it has just pushed out, and 0 for a refusal. ARGV[3] onwards are the tokens this handle may own — the lease it holds and the ones whose acquisitions never learned their outcome — so a re-acquisition extends the stored value instead of replacing it, and the caller learns WHICH of its tokens the store carries. A handle owning none passes its new token there, so a comparison can never match a token belonging to somebody else. */
var lockAcquireScript = rueidis.NewLuaScript(`local current = redis.call("get", KEYS[1])
if current == false then
    redis.call("set", KEYS[1], ARGV[1], "PX", tonumber(ARGV[2]))
    return 1
end
for index = 3, #ARGV do
    if current == ARGV[index] then
        redis.call("pexpire", KEYS[1], tonumber(ARGV[2]))
        return index
    end
end
return 0`)

/* lockReleaseScript deletes the key when it carries any of the tokens handed to it, so one round trip gives back the lease this handle holds together with the ones its unresolved acquisitions may have taken. */
var lockReleaseScript = rueidis.NewLuaScript(`local current = redis.call("get", KEYS[1])
for index = 1, #ARGV do
    if current == ARGV[index] then
        return redis.call("del", KEYS[1])
    end
end
return 0`)

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

    /* claim is what this handle may own at the store. It is a single value behind a compare-and-swap because Acquire, Release and Refresh write it from goroutines the framework's own helpers put there — RunExclusive and LeaderGate renew and release off the path that acquired — and an unconditional write would drop whatever landed between the read and the write. */
    claim atomic.Pointer[lockClaim]
}

/* lockClaim separates the lease this handle HOLDS from the tokens of acquisitions that never learned their outcome. Both name a lease this handle may own at the store, neither can be derived from the other, and a token minted per acquisition is what lets a give-back name the attempt rather than the handle. */
type lockClaim struct {
    held    string
    pending []string
}

/* maximumPendingTokens caps the unresolved acquisitions a handle carries. The set is the SECOND door — the detached give-back is the first — and a lease neither reclaims lapses on the ttl this backend documents as its crash safety, so the cap costs a reclaim attempt in a case the ttl already covers. The oldest goes first: its lease is the closest to expiring. */
const maximumPendingTokens = 4

func (instance *redisLock) currentClaim() lockClaim {
    stored := instance.claim.Load()
    if nil == stored {
        return lockClaim{}
    }

    return *stored
}

/* heldToken answers the lease this handle currently holds, or the empty string before its first acquisition and after a release or a lost refresh. */
func (instance *redisLock) heldToken() string {
    return instance.currentClaim().held
}

/* ownedTokens is every token the store may carry for this handle, the held lease first. */
func (instance *redisLock) ownedTokens() []string {
    claim := instance.currentClaim()

    owned := make([]string, 0, len(claim.pending)+1)
    if "" != claim.held {
        owned = append(owned, claim.held)
    }

    return append(owned, claim.pending...)
}

/* updateClaim retries its compare-and-swap until it lands, so concurrent doors ORDER rather than overwrite. The change function is handed a copy and must return a new value. */
func (instance *redisLock) updateClaim(change func(current lockClaim) lockClaim) {
    for {
        loaded := instance.claim.Load()

        current := lockClaim{}
        if nil != loaded {
            current = *loaded
        }

        updated := change(current)

        if true == instance.claim.CompareAndSwap(loaded, &updated) {
            return
        }
    }
}

func claimWithPending(current lockClaim, token string) lockClaim {
    for _, existing := range current.pending {
        if existing == token {
            return current
        }
    }

    pending := make([]string, 0, len(current.pending)+1)
    pending = append(pending, current.pending...)
    pending = append(pending, token)

    if maximumPendingTokens < len(pending) {
        pending = pending[len(pending)-maximumPendingTokens:]
    }

    return lockClaim{held: current.held, pending: pending}
}

func claimWithout(current lockClaim, tokens ...string) lockClaim {
    dropped := make(map[string]struct{}, len(tokens))
    for _, token := range tokens {
        dropped[token] = struct{}{}
    }

    pending := make([]string, 0, len(current.pending))
    for _, existing := range current.pending {
        if _, isDropped := dropped[existing]; true == isDropped {
            continue
        }

        pending = append(pending, existing)
    }

    held := current.held
    if _, isDropped := dropped[held]; true == isDropped {
        held = ""
    }

    return lockClaim{held: held, pending: pending}
}

func (instance *redisLock) dropPending(token string) {
    instance.updateClaim(func(current lockClaim) lockClaim {
        return claimWithout(current, token)
    })
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

    acquired, offeredTokens, acquireErr := instance.attemptAcquire(runtimeInstance)
    if nil != acquireErr || true == acquired {
        return acquired, acquireErr
    }

    /* a refusal is definitive about the tokens that attempt OFFERED, not about the handle. An acquire racing this one on the same handle takes the lease between the read of the owned tokens and the script, and the in-memory backend answers such a caller yes rather than refusing it. The retry is gated on a fact rather than on timing: a refusal proves the other attempt's script ran, its token is recorded before its script runs, so a token this attempt did not offer is on the claim by the time the refusal lands. A genuine refusal by another holder grows nothing and pays nothing. */
    if false == containsUnoffered(instance.ownedTokens(), offeredTokens) {
        return false, nil
    }

    acquired, _, acquireErr = instance.attemptAcquire(runtimeInstance)

    return acquired, acquireErr
}

func containsUnoffered(owned []string, offered []string) bool {
    for _, token := range owned {
        wasOffered := false

        for _, candidate := range offered {
            if candidate == token {
                wasOffered = true

                break
            }
        }

        if false == wasOffered {
            return true
        }
    }

    return false
}

func (instance *redisLock) attemptAcquire(runtimeInstance runtimecontract.Runtime) (bool, []string, error) {
    milliseconds := strconv.FormatInt(floorPositiveMilliseconds(instance.ttl), 10)

    acquisitionToken := newLockToken()

    /* recorded BEFORE the call: an attempt whose outcome is lost must still have a name a later door can reclaim, and after the call there may be no answer to record */
    instance.updateClaim(func(current lockClaim) lockClaim {
        return claimWithPending(current, acquisitionToken)
    })

    offeredTokens := instance.ownedTokens()
    arguments := append([]string{acquisitionToken, milliseconds}, offeredTokens...)

    callContext, cancel := instance.callContext(runtimeInstance)
    defer cancel()

    /* a context already done here refuses the command before it is written, so no lease can have been taken and the give-back below would be a round trip against a store this call never reached */
    dispatched := nil == callContext.Err()

    result := lockAcquireScript.Exec(callContext, instance.client, []string{instance.name}, arguments)

    outcome, resultErr := result.AsInt64()
    if nil != resultErr {
        if false == dispatched {
            instance.dropPending(acquisitionToken)
        } else {
            instance.releaseAmbiguousAcquire(acquisitionToken)
        }

        return false, offeredTokens, exception.NewError("redis lock acquire failed", map[string]any{"name": instance.name}, resultErr)
    }

    if lockAcquireTaken == outcome {
        instance.updateClaim(func(current lockClaim) lockClaim {
            claimed := claimWithout(current, acquisitionToken)
            claimed.held = acquisitionToken

            return claimed
        })

        return true, offeredTokens, nil
    }

    if lockAcquireRefused == outcome {
        instance.dropPending(acquisitionToken)

        return false, offeredTokens, nil
    }

    /* the store carries one of this handle's own tokens and has named which: that token IS the lease this handle holds, whether it was the recorded one or an acquisition that never learned it had taken it */
    extendedToken := arguments[outcome-1]

    instance.updateClaim(func(current lockClaim) lockClaim {
        claimed := claimWithout(current, acquisitionToken, extendedToken)
        claimed.held = extendedToken

        return claimed
    })

    return true, offeredTokens, nil
}

const (
    /* lockAcquireScript answers 0 for a refusal, 1 for a lease newly written under this acquisition's token, and otherwise the ARGV index of the owned token the store carries. */
    lockAcquireRefused = int64(0)
    lockAcquireTaken   = int64(1)
)

/* releaseAmbiguousAcquire gives back a lease one acquisition may have taken without ever learning that it did. An acquire that ends in an error ends AMBIGUOUSLY: the call is bounded, so a store that answers late — or a connection that drops after the server ran the script — leaves the compare-and-set executed and the key holding that acquisition's token for its whole ttl, while the caller is told it did not get the lock. Nothing else can clear it: every later acquisition mints a fresh token, so the compare-and-delete refuses all of them until the ttl lapses with nobody holding the lock — measured on a live store, a one-second budget bought a twenty-nine-second lockout.

   It deletes the token of THAT acquisition and nothing else, which is what makes it safe with no state to consult. A re-acquisition by a handle that already holds the lease never writes its token — the script extends the stored value instead of replacing it — so when the ambiguous attempt was a re-acquisition this delete matches nothing and the lease the caller is still inside is untouched. When it was a fresh take, the token it deletes is exactly the lease nobody believes they hold. The two cases used to be indistinguishable, because one token served the whole handle, and were told apart by a flag mirroring the store; the flag could be read before a concurrent acquisition moved it, was never cleared when the store answered that another token held the key, and had no ordering between the round trip that produced a verdict and the store that recorded it. Naming the acquisition removes the question the flag was answering.

   It runs DETACHED, on a goroutine and on a context of its own, for two reasons that pull the same way: the caller's context is often the very thing that ended the acquire, and a caller that carries a deadline TIGHTER than the call timeout must keep it — charging it a second round trip would take an acquire refused in ten milliseconds to a full budget. It is not the only door, though: the attempt's token stays on the handle's claim until something reclaims it, so a later Acquire extends that lease instead of being refused by it and a later Release gives it back. A process that exits before either lands leaves exactly the lease a crash would leave, which is what the ttl is documented to cover. */
func (instance *redisLock) releaseAmbiguousAcquire(acquisitionToken string) {
    go func() {
        /* a panic on a bare goroutine takes the process down with it, and this one runs for a caller that has already been answered */
        defer func() { _ = recover() }()

        releaseContext, cancel := context.WithTimeout(context.Background(), instance.callTimeout)
        defer cancel()

        if releaseErr := lockReleaseScript.Exec(releaseContext, instance.client, []string{instance.name}, []string{acquisitionToken}).Error(); nil != releaseErr {
            return
        }

        instance.dropPending(acquisitionToken)
    }()
}

/* Release gives back every lease this handle may own — the one it holds and the ones its unresolved acquisitions may have taken — in one round trip. A handle that owns none returns without one, which is the published contract: releasing a lock this instance no longer holds is a no-op that reports no error.

   The claim is dropped only once the store has ANSWERED. A release whose round trip never landed leaves the lease standing under a token nothing else can name, so a claim dropped before the answer would report success over a lease still held and leave every later acquire refused until the ttl lapsed. */
func (instance *redisLock) Release(runtimeInstance runtimecontract.Runtime) error {
    ownedTokens := instance.ownedTokens()
    if 0 == len(ownedTokens) {
        return nil
    }

    callContext, cancel := instance.callContext(runtimeInstance)
    defer cancel()

    result := lockReleaseScript.Exec(callContext, instance.client, []string{instance.name}, ownedTokens)
    if resultErr := result.Error(); nil != resultErr {
        return exception.NewError("redis lock release failed", map[string]any{"name": instance.name}, resultErr)
    }

    instance.updateClaim(func(current lockClaim) lockClaim {
        return claimWithout(current, ownedTokens...)
    })

    return nil
}

func (instance *redisLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    if 0 >= ttl {
        return exception.NewError("redis lock refresh ttl must be positive", map[string]any{"name": instance.name}, nil)
    }

    /* a handle that holds no lease has nothing to extend, and says so with the same error a lost lease gets: Refresh is the authoritative liveness check by published contract, so it must answer "no longer held" rather than a silent success */
    heldToken := instance.heldToken()
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
        /* the store has answered that the key is gone or carries another token, which is the one reading that settles the question this handle cannot settle on its own; a refresh that merely ENDED AMBIGUOUSLY leaves the claim standing, because there the safe reading is that the lease is still ours. The drop is a compare-and-swap on the token that was refreshed, so an acquire that took a fresh lease meanwhile is not stepped on. */
        instance.updateClaim(func(current lockClaim) lockClaim {
            if heldToken != current.held {
                return current
            }

            return claimWithout(current, heldToken)
        })

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
