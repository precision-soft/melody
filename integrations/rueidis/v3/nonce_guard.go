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

const defaultNonceGuardCallTimeout = time.Second

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
