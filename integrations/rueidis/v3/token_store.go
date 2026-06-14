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

var tokenDeleteByUserScript = rueidis.NewLuaScript(`
local members = redis.call("smembers", KEYS[1])
local removed = 0
for index = 1, #members do
    local value = redis.call("get", members[index])
    if value then
        local decoded = cjson.decode(value)
        if decoded["UserIdentifier"] == ARGV[1] then
            redis.call("del", members[index])
            removed = removed + 1
        end
    end
end
redis.call("del", KEYS[1])
return removed
`)

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

func WithTokenStorePrefix(prefix string) TokenStoreOption {
    return func(store *RedisTokenStore) {
        store.prefix = prefix
    }
}

func WithTokenStoreContext(ctx context.Context) TokenStoreOption {
    return func(store *RedisTokenStore) {
        if nil == ctx {
            return
        }

        store.ctx = context.WithoutCancel(ctx)
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
        []string{userIdentifier},
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
            instance.client.B().Scan().Cursor(cursor).Match(escapeRedisGlobMeta(instance.userKeyPrefix())+"*").Count(tokenStoreScanCount).Build(),
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

func (instance *RedisTokenStore) put(tokenString string, claims securitycontract.Claims, ttl time.Duration) {
    payload, marshalErr := json.Marshal(claims)
    if nil != marshalErr {
        exception.Panic(exception.NewError("redis token store could not encode claims", map[string]any{"user": claims.UserIdentifier}, marshalErr))
    }

    pttl := "0"
    if 0 < ttl {
        pttl = strconv.FormatInt(floorPositiveMilliseconds(ttl), 10)
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

func (instance *RedisTokenStore) keyspace() string {
    return "{" + instance.prefix + "}"
}

func (instance *RedisTokenStore) tokenKey(tokenString string) string {
    return instance.keyspace() + ":token:" + tokenString
}

func (instance *RedisTokenStore) userKey(userIdentifier string) string {
    return instance.keyspace() + ":user:" + userIdentifier
}

func (instance *RedisTokenStore) userKeyPrefix() string {
    return instance.keyspace() + ":user:"
}

var _ securitycontract.RevocableTokenStore = (*RedisTokenStore)(nil)
