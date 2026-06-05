package rueidis

import (
    "context"
    "encoding/json"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
    "github.com/redis/rueidis"
)

const (
    defaultTokenStorePrefix = "melody:token"
    tokenStoreScanCount     = 256
)

/**
 * tokenPutScript stores the claims at the token key and indexes the token under its user. When the
 * token already exists under a different user, the old user's set membership is removed first, so a
 * re-issued token never lingers in a stale user index. KEYS[1] is the token key; ARGV is
 * [claimsJson, pttlMilliseconds, userKeyPrefix, userIdentifier].
 */
var tokenPutScript = rueidis.NewLuaScript(`
local existing = redis.call("get", KEYS[1])
if existing then
    local decoded = cjson.decode(existing)
    local oldUser = decoded["UserIdentifier"]
    if oldUser and oldUser ~= ARGV[4] then
        redis.call("srem", ARGV[3] .. oldUser, KEYS[1])
    end
end
if ARGV[2] == "0" then
    redis.call("set", KEYS[1], ARGV[1])
else
    redis.call("set", KEYS[1], ARGV[1], "PX", tonumber(ARGV[2]))
end
redis.call("sadd", ARGV[3] .. ARGV[4], KEYS[1])
return 1
`)

/**
 * tokenDeleteScript removes the token key and its membership from the owning user's set. KEYS[1] is
 * the token key; ARGV[1] is the user key prefix. Returns 1 when a token was deleted, 0 otherwise.
 */
var tokenDeleteScript = rueidis.NewLuaScript(`
local existing = redis.call("get", KEYS[1])
if not existing then
    return 0
end
local decoded = cjson.decode(existing)
local user = decoded["UserIdentifier"]
redis.call("del", KEYS[1])
if user then
    redis.call("srem", ARGV[1] .. user, KEYS[1])
end
return 1
`)

/**
 * tokenDeleteByUserScript deletes every live token key in the user's set, drops the set, and returns
 * the count of token keys that actually existed (members whose key already expired are not counted but
 * are still cleaned by dropping the set). KEYS[1] is the user set key.
 */
var tokenDeleteByUserScript = rueidis.NewLuaScript(`
local members = redis.call("smembers", KEYS[1])
local removed = 0
for index = 1, #members do
    if redis.call("del", members[index]) == 1 then
        removed = removed + 1
    end
end
redis.call("del", KEYS[1])
return removed
`)

/**
 * tokenPurgeUserScript reconciles a single user set: members whose token key Redis has already expired
 * are removed, and the set itself is dropped when it becomes empty. KEYS[1] is the user set key.
 * Returns the number of stale members pruned.
 */
var tokenPurgeUserScript = rueidis.NewLuaScript(`
local members = redis.call("smembers", KEYS[1])
local pruned = 0
for index = 1, #members do
    if redis.call("exists", members[index]) == 0 then
        redis.call("srem", KEYS[1], members[index])
        pruned = pruned + 1
    end
end
if redis.call("scard", KEYS[1]) == 0 then
    redis.call("del", KEYS[1])
end
return pruned
`)

func NewTokenStore(client rueidis.Client, options ...TokenStoreOption) *RedisTokenStore {
    if nil == client {
        exception.Panic(exception.NewError("redis token store client is nil", nil, nil))
    }

    store := &RedisTokenStore{
        client: client,
        ctx:    context.Background(),
        prefix: defaultTokenStorePrefix,
    }

    for _, option := range options {
        option(store)
    }

    if "" == store.prefix {
        store.prefix = defaultTokenStorePrefix
    }

    if nil == store.ctx {
        store.ctx = context.Background()
    }

    return store
}

type TokenStoreOption func(*RedisTokenStore)

/** WithTokenStorePrefix overrides the key namespace (default "melody:token"). */
func WithTokenStorePrefix(prefix string) TokenStoreOption {
    return func(store *RedisTokenStore) {
        store.prefix = prefix
    }
}

/**
 * WithTokenStoreContext binds the context used by the context-less mutators (Put/Delete/DeleteByUser/
 * PurgeExpired). Lookup always uses the per-request runtime context instead. Defaults to background.
 */
func WithTokenStoreContext(ctx context.Context) TokenStoreOption {
    return func(store *RedisTokenStore) {
        store.ctx = ctx
    }
}

type RedisTokenStore struct {
    client rueidis.Client
    ctx    context.Context
    prefix string
}

func (instance *RedisTokenStore) Put(tokenString string, claims securitycontract.Claims) {
    instance.put(tokenString, claims, 0)
}

func (instance *RedisTokenStore) PutWithTtl(tokenString string, claims securitycontract.Claims, ttl time.Duration) {
    instance.put(tokenString, claims, ttl)
}

func (instance *RedisTokenStore) put(tokenString string, claims securitycontract.Claims, ttl time.Duration) {
    payload, marshalErr := json.Marshal(claims)
    if nil != marshalErr {
        exception.Panic(exception.NewError("redis token store could not encode claims", map[string]any{"user": claims.UserIdentifier}, marshalErr))
    }

    pttl := "0"
    if 0 < ttl {
        pttl = strconv.FormatInt(ttl.Milliseconds(), 10)
    }

    result := tokenPutScript.Exec(
        instance.ctx,
        instance.client,
        []string{instance.tokenKey(tokenString)},
        []string{string(payload), pttl, instance.userKeyPrefix(), claims.UserIdentifier},
    )
    if resultErr := result.Error(); nil != resultErr {
        exception.Panic(exception.NewError("redis token store put failed", map[string]any{"user": claims.UserIdentifier}, resultErr))
    }
}

func (instance *RedisTokenStore) Delete(tokenString string) {
    result := tokenDeleteScript.Exec(
        instance.ctx,
        instance.client,
        []string{instance.tokenKey(tokenString)},
        []string{instance.userKeyPrefix()},
    )
    if resultErr := result.Error(); nil != resultErr {
        exception.Panic(exception.NewError("redis token store delete failed", nil, resultErr))
    }
}

func (instance *RedisTokenStore) DeleteByUser(userIdentifier string) int {
    result := tokenDeleteByUserScript.Exec(
        instance.ctx,
        instance.client,
        []string{instance.userKey(userIdentifier)},
        nil,
    )

    removed, resultErr := result.AsInt64()
    if nil != resultErr {
        exception.Panic(exception.NewError("redis token store delete by user failed", map[string]any{"user": userIdentifier}, resultErr))
    }

    return int(removed)
}

func (instance *RedisTokenStore) PurgeExpired() int {
    pruned := 0
    cursor := uint64(0)

    for {
        scan, scanErr := instance.client.Do(
            instance.ctx,
            instance.client.B().Scan().Cursor(cursor).Match(instance.userKeyPrefix()+"*").Count(tokenStoreScanCount).Build(),
        ).AsScanEntry()
        if nil != scanErr {
            exception.Panic(exception.NewError("redis token store purge scan failed", nil, scanErr))
        }

        for _, setKey := range scan.Elements {
            result := tokenPurgeUserScript.Exec(instance.ctx, instance.client, []string{setKey}, nil)

            count, countErr := result.AsInt64()
            if nil != countErr {
                exception.Panic(exception.NewError("redis token store purge failed", map[string]any{"set": setKey}, countErr))
            }

            pruned += int(count)
        }

        cursor = scan.Cursor
        if 0 == cursor {
            break
        }
    }

    return pruned
}

func (instance *RedisTokenStore) Lookup(
    runtimeInstance runtimecontract.Runtime,
    tokenString string,
) (securitycontract.Claims, bool, error) {
    payload, lookupErr := instance.client.Do(
        runtimeInstance.Context(),
        instance.client.B().Get().Key(instance.tokenKey(tokenString)).Build(),
    ).ToString()

    if nil != lookupErr {
        if true == rueidis.IsRedisNil(lookupErr) {
            return securitycontract.Claims{}, false, nil
        }

        return securitycontract.Claims{}, false, exception.NewError("redis token store lookup failed", nil, lookupErr)
    }

    claims := securitycontract.Claims{}
    if unmarshalErr := json.Unmarshal([]byte(payload), &claims); nil != unmarshalErr {
        return securitycontract.Claims{}, false, exception.NewError("redis token store could not decode claims", nil, unmarshalErr)
    }

    return claims, true, nil
}

func (instance *RedisTokenStore) tokenKey(tokenString string) string {
    return instance.prefix + ":token:" + tokenString
}

func (instance *RedisTokenStore) userKey(userIdentifier string) string {
    return instance.prefix + ":user:" + userIdentifier
}

func (instance *RedisTokenStore) userKeyPrefix() string {
    return instance.prefix + ":user:"
}

var _ securitycontract.RevocableTokenStore = (*RedisTokenStore)(nil)
