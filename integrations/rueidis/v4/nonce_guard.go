package rueidis

import (
    "context"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/redis/rueidis"
)

const defaultNonceGuardPrefix = "melody:nonce"

/* defaultNonceGuardCallTimeout is the budget of one round trip, as the token store on the same authentication path gives its Lookup. A guard round trip is one Lua script, a few milliseconds on a healthy store, and the budget sits under the client's connection timeout. */
const defaultNonceGuardCallTimeout = time.Second

/* nonceRememberScript records a nonce only if it is not already present, with a millisecond expiry, in a single atomic round-trip. It returns 0 when the nonce was newly recorded (first use) and 1 when it already existed (a replay), so the guard never has a check-then-set race between instances. */
var nonceRememberScript = rueidis.NewLuaScript(`if redis.call("set", KEYS[1], "1", "NX", "PX", tonumber(ARGV[1])) then return 0 else return 1 end`)

type NonceGuardConfig struct {
    /* KeyPrefix is the namespace every recorded nonce is keyed under, melody:nonce when empty. */
    KeyPrefix string

    /* CallTimeout bounds one round trip of Remember by capping the request context with it, so a request whose context carries no deadline, as melody's http kernel leaves it on the authentication path, fails fast, while a tighter deadline is kept. 1s by default, at least 1ms, unlimited refused, since unbounded a store that stops answering would hold the Lua record for the client's write deadline. */
    CallTimeout bound.Duration
}

var nonceGuardCallTimeoutRule = callTimeoutRule("NonceGuardConfig.CallTimeout", defaultNonceGuardCallTimeout)

/* NewNonceGuard returns a Redis-backed securitycontract.NonceGuard. Because the recorded nonces live in Redis, a nonce replayed against any application instance is detected, which the in-process guard cannot do. A nil client or a limit outside its rule panics naming it. */
func NewNonceGuard(client rueidis.Client, config NonceGuardConfig) *NonceGuard {
    if nil == client {
        exception.Panic(exception.NewError("redis nonce guard client is nil", nil, nil))
    }

    callTimeout, callTimeoutErr := bound.Resolve(config.CallTimeout, nonceGuardCallTimeoutRule)
    if nil != callTimeoutErr {
        exception.Panic(exception.FromError(callTimeoutErr))
    }

    keyPrefix := config.KeyPrefix
    if "" == keyPrefix {
        keyPrefix = defaultNonceGuardPrefix
    }

    return &NonceGuard{
        client:      client,
        keyPrefix:   keyPrefix,
        callTimeout: callTimeout.Value,
    }
}

type NonceGuard struct {
    client      rueidis.Client
    keyPrefix   string
    callTimeout time.Duration
}

/* callContext caps the request context with the call timeout; context.WithTimeout keeps the earlier deadline, so a tighter request deadline still wins. */
func (instance *NonceGuard) callContext(runtimeInstance runtimecontract.Runtime) (context.Context, context.CancelFunc) {
    return context.WithTimeout(runtimeInstance.Context(), instance.callTimeout)
}

func (instance *NonceGuard) Remember(
    runtimeInstance runtimecontract.Runtime,
    nonce string,
    ttl bound.Duration,
) (bool, error) {
    resolvedTtl, ttlErr := securitycontract.ResolveNonceTtl(ttl)
    if nil != ttlErr {
        return false, ttlErr
    }

    callContext, cancel := instance.callContext(runtimeInstance)
    defer cancel()

    milliseconds := strconv.FormatInt(floorPositiveMilliseconds(resolvedTtl), 10)

    result := nonceRememberScript.Exec(
        callContext,
        instance.client,
        []string{instance.key(nonce)},
        []string{milliseconds},
    )

    seen, resultErr := result.AsInt64()
    if nil != resultErr {
        return false, exception.NewError("redis nonce guard failed", map[string]any{"nonceLength": len(nonce), "keyPrefix": instance.keyPrefix}, resultErr)
    }

    return 1 == seen, nil
}

/* key is the record of a nonce; the nonce is a credential to the guard's callers, a second factor's code among them, and is never journaled, so a refusal names its length and the key prefix instead */
func (instance *NonceGuard) key(nonce string) string {
    return instance.keyPrefix + ":" + nonce
}

var _ securitycontract.NonceGuard = (*NonceGuard)(nil)
