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
    defaultTokenStorePrefix    = "melody:token"
    defaultTokenStoreScanCount = 256
)

/* tokenIndexExpiryGraceMilliseconds is how much longer a user's index set lives than the longest-lived token in it.

It has to be positive at all so the set's deadline sits behind the token's instead of on the same millisecond: the set is how DeleteByUser finds a user's tokens, so a set that went first would leave a live token unrevocable. A second is generous for that and still short enough that a set outlives its last token only briefly — long enough for PurgeExpired to see the dead member and drop it on purpose, rather than the entry disappearing with the set and a scheduled purge finding nothing to report. */
const tokenIndexExpiryGraceMilliseconds = 1000

/* tokenPutScript writes the token and adds it to its user's index set, and gives that set an expiry that always covers the longest-lived token it holds.

Without one the set is immortal: only the token keys carry PX, so a user who logs in and out for years leaves an ever-growing set of dead member names behind, and nothing sweeps it unless PurgeExpired is scheduled — which nothing here does.

The expiry can only ever be raised, never shortened, because the set is what DeleteByUser reads to find a user's tokens: a set that expired while a token it lists was still alive would make that token unrevocable. So a token with no expiry at all makes the set persistent, and a token with one extends the set only when its own lifetime reaches past what the set already had. A set that exists with no expiry is therefore left persistent — it either holds a non-expiring token or predates this accounting, and both are answered by keeping it.

The set is given the token's lifetime plus a grace (ARGV[5], never the token's own PX) so its deadline is unambiguously behind the token's rather than landing on the same millisecond, and so a token that has just expired still has an index entry for PurgeExpired to prune deliberately instead of taking the whole set with it. */
var tokenPutScript = rueidis.NewLuaScript(`
local indexKey = ARGV[3] .. ARGV[4]
local existing = redis.call("get", KEYS[1])
if existing then
    local decoded = cjson.decode(existing)
    local oldUser = decoded["UserIdentifier"]
    if oldUser and oldUser ~= ARGV[4] then
        redis.call("srem", ARGV[3] .. oldUser, KEYS[1])
    end
end
local indexExisted = redis.call("exists", indexKey) == 1
if ARGV[2] == "0" then
    redis.call("set", KEYS[1], ARGV[1])
else
    redis.call("set", KEYS[1], ARGV[1], "PX", tonumber(ARGV[2]))
end
redis.call("sadd", indexKey, KEYS[1])
if ARGV[2] == "0" then
    redis.call("persist", indexKey)
else
    local wantedTtl = tonumber(ARGV[5])
    local indexTtl = redis.call("pttl", indexKey)
    if not indexExisted or (indexTtl >= 0 and indexTtl < wantedTtl) then
        redis.call("pexpire", indexKey, wantedTtl)
    end
end
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

/* tokenDeleteByUserScript revokes ONE bounded batch of a user's tokens: KEYS[1] is the index set and KEYS[2..] are the members to settle.

Redis runs a script to completion with every other client blocked, so the batch is the unit that decides how long a revocation stalls the whole server. Reading the entire set inside the script instead makes that stall proportional to how many tokens the user ever held, which for a busy account is a multi-second freeze of every other client of that Redis.

A member whose token is gone is dropped from the set as it is met, so the set shrinks as the batch walks it; the set itself is removed once nothing is left in it, rather than unconditionally, so a token re-issued to another user mid-revocation keeps its own index entry. */
var tokenDeleteByUserScript = rueidis.NewLuaScript(`
local removed = 0
for index = 2, #KEYS do
    local value = redis.call("get", KEYS[index])
    if value then
        local decoded = cjson.decode(value)
        if decoded["UserIdentifier"] == ARGV[1] then
            redis.call("del", KEYS[index])
            redis.call("srem", KEYS[1], KEYS[index])
            removed = removed + 1
        end
    else
        redis.call("srem", KEYS[1], KEYS[index])
    end
end
if redis.call("scard", KEYS[1]) == 0 then
    redis.call("del", KEYS[1])
end
return removed
`)

/* tokenPurgeUserScript prunes ONE bounded batch of a user's index set: KEYS[1] is the set and KEYS[2..] are the members to test.

It carries the same bound as tokenDeleteByUserScript, and for the same reason: a script holds the whole server while it runs, so reading the entire set inside it makes a purge stall every other client of that Redis for as long as the largest user's token history is. Both operations walk the same set, so they walk it the same way.

A member whose token is gone is dropped as it is met, and the set itself is removed once nothing is left in it — checked rather than assumed, so a token written into the set between the scan and this batch keeps its index entry.

What is counted is what SREM actually removed, not what was found dead: a walked set may hand the same member back twice, and counting the finding rather than the removal would report more entries pruned than the set ever held. */
var tokenPurgeUserScript = rueidis.NewLuaScript(`
local pruned = 0
for index = 2, #KEYS do
    if redis.call("exists", KEYS[index]) == 0 then
        pruned = pruned + redis.call("srem", KEYS[1], KEYS[index])
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
        client:    client,
        ctx:       context.Background(),
        prefix:    defaultTokenStorePrefix,
        scanCount: defaultTokenStoreScanCount,
    }

    for _, option := range options {
        option(store)
    }

    if "" == store.prefix {
        store.prefix = defaultTokenStorePrefix
    }

    if 0 >= store.scanCount {
        store.scanCount = defaultTokenStoreScanCount
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

func WithTokenStoreScanCount(scanCount int) TokenStoreOption {
    return func(store *RedisTokenStore) {
        store.scanCount = scanCount
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
    client    rueidis.Client
    ctx       context.Context
    prefix    string
    scanCount int
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

/* DeleteByUser revokes every live token a user holds, walking that user's index set in bounded batches.

The walk is an SSCAN cursor rather than one read of the whole set, and each batch is settled by its own short script, so the time any single script holds the server is a function of the batch size and not of how many tokens the user has accumulated. SSCAN treats its count as a hint and may hand back more than that, so what it returns is sliced down to the batch size before any of it is executed — the bound has to be the one this side enforces.

Removing members while scanning is what SSCAN tolerates: an element present for the whole iteration is returned at least once, and one that is removed may or may not be returned again. Both re-visits and stale members are settled idempotently — the token is already gone, so the member is simply dropped. What a bounded walk gives up against a single atomic script is a token issued to this user after the walk passed the bucket it lands in; that token survives the revocation, exactly as one issued a millisecond after the call returns would. */
func (instance *RedisTokenStore) DeleteByUser(userIdentifier string) int {
    indexKey := instance.userKey(userIdentifier)

    removed := 0
    cursor := uint64(0)

    for {
        scan, scanErr := instance.client.Do(
            instance.ctx,
            instance.client.B().Sscan().Key(indexKey).Cursor(cursor).Count(int64(instance.scanCount)).Build(),
        ).AsScanEntry()
        if nil != scanErr {
            exception.Panic(exception.NewError("redis token store delete by user scan failed", map[string]any{"user": userIdentifier}, scanErr))
        }

        for offset := 0; offset < len(scan.Elements); offset += instance.scanCount {
            limit := offset + instance.scanCount
            if len(scan.Elements) < limit {
                limit = len(scan.Elements)
            }

            removed += instance.deleteTokenBatch(indexKey, userIdentifier, scan.Elements[offset:limit])
        }

        cursor = scan.Cursor
        if 0 == cursor {
            break
        }
    }

    return removed
}

func (instance *RedisTokenStore) deleteTokenBatch(indexKey string, userIdentifier string, members []string) int {
    keys := make([]string, 0, len(members)+1)
    keys = append(keys, indexKey)
    keys = append(keys, members...)

    result := tokenDeleteByUserScript.Exec(instance.ctx, instance.client, keys, []string{userIdentifier})

    removed, resultErr := result.AsInt64()
    if nil != resultErr {
        exception.Panic(exception.NewError("redis token store delete by user failed", map[string]any{"user": userIdentifier}, resultErr))
    }

    return int(removed)
}

/* PurgeExpired drops the index entries of tokens that have already expired, walking every user's index set in bounded batches.

The sets carry an expiry of their own, but that only bounds a set whose members have ALL died: an account that keeps logging in keeps raising its set's deadline, so dead member names pile up inside a set that stays alive indefinitely. This is what removes them.

Both loops are cursor walks and neither reads a whole collection into a script. The outer SCAN finds the user sets, and each set is settled by the same bounded batching DeleteByUser uses, so the time any single script holds the server depends on the batch size and not on how many tokens the largest account ever held. SCAN and SSCAN both treat their count as a hint and may return more, so what comes back is sliced down to the batch size before any of it is executed.

Pruning while scanning is what SSCAN tolerates: a member present throughout is returned at least once, and a removed one may or may not be returned again. A re-visit costs nothing — the member is either still dead and already gone from the set, or alive and left alone. */
func (instance *RedisTokenStore) PurgeExpired() int {
    pruned := 0
    cursor := uint64(0)

    for {
        scan, scanErr := instance.client.Do(
            instance.ctx,
            instance.client.B().Scan().Cursor(cursor).Match(escapeRedisGlobMeta(instance.userKeyPrefix())+"*").Count(int64(instance.scanCount)).Build(),
        ).AsScanEntry()
        if nil != scanErr {
            exception.Panic(exception.NewError("redis token store purge scan failed", nil, scanErr))
        }

        for _, setKey := range scan.Elements {
            pruned += instance.purgeUserIndex(setKey)
        }

        cursor = scan.Cursor
        if 0 == cursor {
            break
        }
    }

    return pruned
}

func (instance *RedisTokenStore) purgeUserIndex(indexKey string) int {
    pruned := 0
    cursor := uint64(0)

    for {
        scan, scanErr := instance.client.Do(
            instance.ctx,
            instance.client.B().Sscan().Key(indexKey).Cursor(cursor).Count(int64(instance.scanCount)).Build(),
        ).AsScanEntry()
        if nil != scanErr {
            exception.Panic(exception.NewError("redis token store purge member scan failed", map[string]any{"set": indexKey}, scanErr))
        }

        for offset := 0; offset < len(scan.Elements); offset += instance.scanCount {
            limit := offset + instance.scanCount
            if len(scan.Elements) < limit {
                limit = len(scan.Elements)
            }

            pruned += instance.pruneTokenBatch(indexKey, scan.Elements[offset:limit])
        }

        cursor = scan.Cursor
        if 0 == cursor {
            break
        }
    }

    return pruned
}

func (instance *RedisTokenStore) pruneTokenBatch(indexKey string, members []string) int {
    keys := make([]string, 0, len(members)+1)
    keys = append(keys, indexKey)
    keys = append(keys, members...)

    result := tokenPurgeUserScript.Exec(instance.ctx, instance.client, keys, nil)

    pruned, resultErr := result.AsInt64()
    if nil != resultErr {
        exception.Panic(exception.NewError("redis token store purge failed", map[string]any{"set": indexKey}, resultErr))
    }

    return int(pruned)
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
    indexPttl := "0"
    if 0 < ttl {
        tokenMilliseconds := floorPositiveMilliseconds(ttl)

        pttl = strconv.FormatInt(tokenMilliseconds, 10)
        indexPttl = strconv.FormatInt(tokenMilliseconds+tokenIndexExpiryGraceMilliseconds, 10)
    }

    result := tokenPutScript.Exec(
        instance.ctx,
        instance.client,
        []string{instance.tokenKey(tokenString)},
        []string{string(payload), pttl, instance.userKeyPrefix(), claims.UserIdentifier, indexPttl},
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
