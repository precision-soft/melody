package rueidis

import (
    "strconv"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
    "github.com/redis/rueidis"
)

const defaultNonceGuardPrefix = "melody:nonce"

/* nonceRememberScript records a nonce only if it is not already present, with a millisecond expiry, in a single atomic round-trip. It returns 0 when the nonce was newly recorded (first use) and 1 when it already existed (a replay), so the guard never has a check-then-set race between instances. */
var nonceRememberScript = rueidis.NewLuaScript(`if redis.call("set", KEYS[1], "1", "NX", "PX", tonumber(ARGV[1])) then return 0 else return 1 end`)

/* NewNonceGuard returns a Redis-backed securitycontract.NonceGuard. Because the recorded nonces live in Redis, a nonce replayed against any application instance is detected, which the in-process guard cannot do. */
func NewNonceGuard(client rueidis.Client) *NonceGuard {
    return NewNonceGuardWithPrefix(client, defaultNonceGuardPrefix)
}

func NewNonceGuardWithPrefix(client rueidis.Client, keyPrefix string) *NonceGuard {
    if nil == client {
        exception.Panic(exception.NewError("redis nonce guard client is nil", nil, nil))
    }

    prefix := keyPrefix
    if "" == prefix {
        prefix = defaultNonceGuardPrefix
    }

    return &NonceGuard{
        client:    client,
        keyPrefix: prefix,
    }
}

type NonceGuard struct {
    client    rueidis.Client
    keyPrefix string
}

func (instance *NonceGuard) Remember(
    runtimeInstance runtimecontract.Runtime,
    nonce string,
    ttl time.Duration,
) (bool, error) {
    if 0 >= ttl {
        return false, nil
    }

    milliseconds := strconv.FormatInt(floorPositiveMilliseconds(ttl), 10)

    result := nonceRememberScript.Exec(
        runtimeInstance.Context(),
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
