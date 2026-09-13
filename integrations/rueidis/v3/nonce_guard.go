package rueidis

import (
    "context"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
    "github.com/redis/rueidis"
)

const defaultNonceGuardPrefix = "melody:nonce"

/* defaultNonceGuardCallTimeout is the budget of one round trip, the one the token store on the same authentication path gives its Lookup. A guard round trip is one Lua script or one EXISTS, so a healthy store answers in a few milliseconds; the budget only has to sit under the client's own connection timeout, which is what bounded the record before this option existed — nothing bounded the existence check at all. */
const defaultNonceGuardCallTimeout = time.Second

/* nonceRememberScript records a nonce only if it is not already present, with a millisecond expiry, in a single atomic round-trip. It returns 0 when the nonce was newly recorded (first use) and 1 when it already existed (a replay), so the guard never has a check-then-set race between instances. */
var nonceRememberScript = rueidis.NewLuaScript(`if redis.call("set", KEYS[1], "1", "NX", "PX", tonumber(ARGV[1])) then return 0 else return 1 end`)

/* NewNonceGuard returns a Redis-backed securitycontract.NonceGuard. Because the recorded nonces live in Redis, a nonce replayed against any application instance is detected, which the in-process guard cannot do. */
func NewNonceGuard(client rueidis.Client) *NonceGuard {
    return NewNonceGuardWithOptions(client)
}

func NewNonceGuardWithPrefix(client rueidis.Client, keyPrefix string) *NonceGuard {
    return NewNonceGuardWithOptions(client, WithNonceGuardKeyPrefix(keyPrefix))
}

/* NewNonceGuardWithOptions is NewNonceGuard with options — WithNonceGuardCallTimeout above all. The two older doors keep their signatures and build the same guard, at the defaults and with the prefix respectively. */
func NewNonceGuardWithOptions(client rueidis.Client, options ...NonceGuardOption) *NonceGuard {
    if nil == client {
        exception.Panic(exception.NewError("redis nonce guard client is nil", nil, nil))
    }

    guard := &NonceGuard{
        client:      client,
        keyPrefix:   defaultNonceGuardPrefix,
        callTimeout: defaultNonceGuardCallTimeout,
    }

    for _, option := range options {
        option(guard)
    }

    return guard
}

type NonceGuardOption func(*NonceGuard)

/* WithNonceGuardKeyPrefix overrides the default melody:nonce key prefix; an empty prefix keeps the default, the way NewNonceGuardWithPrefix has always read it. */
func WithNonceGuardKeyPrefix(keyPrefix string) NonceGuardOption {
    return func(guard *NonceGuard) {
        prefix := keyPrefix
        if "" == prefix {
            prefix = defaultNonceGuardPrefix
        }

        guard.keyPrefix = prefix
    }
}

/* WithNonceGuardCallTimeout bounds one round trip of Remember by capping the request context with it, so a request whose context carries no deadline — melody's http kernel attaches none, and every authenticator that consults this guard runs on the request path — still fails fast, while a request that already carries a tighter deadline keeps it. Without a bound a store that accepts connections but stops answering holds the Lua record for the client's own connection timeout, five seconds at the provider's default, and holds the read-only EXISTS of a non-positive ttl for good: the client retries a read-only command on a fresh connection for as long as the context allows, and a context without deadline allows forever. A non-positive timeout falls back to the default, following this package's zero-means-default convention, so a config-sourced unset value can never build an already-cancelled context that refuses every envelope; the cache subpackage deliberately reads its command timeout the other way and says so on its own option. */
func WithNonceGuardCallTimeout(timeout time.Duration) NonceGuardOption {
    return func(guard *NonceGuard) {
        if 0 >= timeout {
            timeout = defaultNonceGuardCallTimeout
        }

        guard.callTimeout = timeout
    }
}

type NonceGuard struct {
    client      rueidis.Client
    keyPrefix   string
    callTimeout time.Duration
}

/* callContext caps the request context with the call timeout: context.WithTimeout keeps whichever deadline is earlier, so a request that already carries a tighter deadline still wins, while a request whose context has no deadline — as melody's http kernel leaves it — is bounded here rather than held for the client's connection timeout on the record, or retried against an unresponsive store for as long as the client's retry policy allows on the existence check. */
func (instance *NonceGuard) callContext(runtimeInstance runtimecontract.Runtime) (context.Context, context.CancelFunc) {
    return context.WithTimeout(runtimeInstance.Context(), instance.callTimeout)
}

func (instance *NonceGuard) Remember(
    runtimeInstance runtimecontract.Runtime,
    nonce string,
    ttl time.Duration,
) (bool, error) {
    callContext, cancel := instance.callContext(runtimeInstance)
    defer cancel()

    if 0 >= ttl {
        /* a non-positive ttl carries no lifetime to record, but the NonceGuard contract still reports whether the nonce is currently recorded — mirror MemoryNonceGuard, which performs the existence check before its own ttl short-circuit, with a read-only EXISTS rather than silently reporting the nonce as fresh (which would let a replay through at the acceptance-window edge). EXISTS is a read-only command, which the client retries on a fresh connection for as long as its context allows, so the call timeout is the only thing that ends this check against a store that stopped answering. */
        existsResult := instance.client.Do(
            callContext,
            instance.client.B().Exists().Key(instance.key(nonce)).Build(),
        )

        seen, existsErr := existsResult.AsInt64()
        if nil != existsErr {
            return false, exception.NewError("redis nonce guard failed", map[string]any{"nonce": nonce}, existsErr)
        }

        return 1 == seen, nil
    }

    milliseconds := strconv.FormatInt(floorPositiveMilliseconds(ttl), 10)

    result := nonceRememberScript.Exec(
        callContext,
        instance.client,
        []string{instance.key(nonce)},
        []string{milliseconds},
    )

    seen, resultErr := result.AsInt64()
    if nil != resultErr {
        return false, exception.NewError("redis nonce guard failed", map[string]any{"nonce": nonce}, resultErr)
    }

    return 1 == seen, nil
}

func (instance *NonceGuard) key(nonce string) string {
    return instance.keyPrefix + ":" + nonce
}

var _ securitycontract.NonceGuard = (*NonceGuard)(nil)
