package rueidis

import (
    "context"
    "encoding/json"
    "math"
    "strconv"
    "time"

    melodyclock "github.com/precision-soft/melody/v3/clock"
    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/exception"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
    "github.com/redis/rueidis"
)

const (
    defaultTokenStorePrefix    = "melody:token"
    defaultTokenStoreScanCount = 256
)

const defaultRevocationEpochRetentionMilliseconds = int64(7 * 24 * 60 * 60 * 1000)

/* defaultTokenStoreCallTimeout is the budget of one round trip, as the server-sent event backplane in this package gives a publish. A token store round trip is one Lua script or one cursor step, a few milliseconds on a healthy store, and the budget sits under the client's connection timeout. */
const defaultTokenStoreCallTimeout = time.Second

const (
    revocationUserField = "user"

    revocationDeviceFieldPrefix = "device:"
)

var revocationEpochLowerBound = time.Unix(0, 0)
var revocationEpochUpperBound = time.Unix(0, math.MaxInt64)

/* tokenIndexExpiryGraceMilliseconds is how much longer a user's index set lives than the longest-lived token in it. It is positive so the set's deadline sits behind the token's: the set is how DeleteByUser finds a user's tokens, so a set that went first would leave a live token unrevocable, and a set outliving its last token briefly lets PurgeExpired prune the dead member on purpose. */
const tokenIndexExpiryGraceMilliseconds = 1000

/* tokenPutScript writes the token, adds it to its user's index set and gives the set an expiry that covers the longest-lived token it holds, so the set does not grow forever with dead members. The expiry is only ever raised, because a set that expired while a listed token lived would make that token unrevocable: a token with no expiry makes the set persistent, and a set that exists with no expiry stays persistent. The set gets the token's lifetime plus a grace (ARGV[5]) so its deadline is behind the token's and a just-expired token still has an entry for PurgeExpired to prune. */
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

/* tokenDeleteByUserScript revokes one bounded batch of a user's tokens: KEYS[1] is the index set and KEYS[2..] the members to settle. Redis runs a script with every other client blocked, so the batch bounds how long a revocation stalls the server; reading the whole set inside the script would stall it in proportion to the user's token history. A member whose token is gone is dropped as it is met, and the set is removed only once empty, so a token re-issued to another user mid-revocation keeps its index entry. */
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

/* tokenPurgeUserScript prunes one bounded batch of a user's index set: KEYS[1] is the set and KEYS[2..] the members to test. It carries tokenDeleteByUserScript's bound for the same reason, a script holding the whole server while it runs. A member whose token is gone is dropped as it is met, and the set is removed once it is checked empty, so a token written into the set between the scan and this batch keeps its entry. What is counted is what SREM removed, since a walked set may hand the same member back twice. */
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

var tokenLookupScript = rueidis.NewLuaScript(`
local payload = redis.call("get", KEYS[1])
if not payload then
    return {}
end
local userEpoch, deviceEpoch = "", ""
local decoded = nil
local ok, result = pcall(cjson.decode, payload)
if ok and type(result) == "table" then
    decoded = result
end
if decoded then
    local user = decoded["UserIdentifier"]
    if type(user) == "string" and user ~= "" then
        local epochKey = ARGV[1] .. user
        userEpoch = redis.call("hget", epochKey, ARGV[2]) or ""
        local device = decoded["DeviceIdentifier"]
        if type(device) == "string" and device ~= "" then
            deviceEpoch = redis.call("hget", epochKey, ARGV[3] .. device) or ""
        end
    end
end
return {payload, userEpoch, deviceEpoch}
`)
var tokenRevokeEpochScript = rueidis.NewLuaScript(`
local previousTtl = redis.call("pttl", KEYS[1])
local existing = redis.call("hget", KEYS[1], ARGV[1])
if existing == false or #ARGV[2] > #existing or (#ARGV[2] == #existing and ARGV[2] > existing) then
    redis.call("hset", KEYS[1], ARGV[1], ARGV[2])
end
if previousTtl == -1 then
    return 1
end
local indexTtl = redis.call("pttl", KEYS[2])
if indexTtl == -1 then
    redis.call("persist", KEYS[1])
    return 1
end
local wantedTtl = tonumber(ARGV[3])
if indexTtl > wantedTtl then
    wantedTtl = indexTtl
end
if previousTtl == -2 or previousTtl < wantedTtl then
    redis.call("pexpire", KEYS[1], wantedTtl)
end
return 1
`)

func NewTokenStore(client rueidis.Client, options ...TokenStoreOption) *RedisTokenStore {
    if nil == client {
        exception.Panic(exception.NewError("redis token store client is nil", nil, nil))
    }

    store := &RedisTokenStore{
        client:                     client,
        ctx:                        context.Background(),
        prefix:                     defaultTokenStorePrefix,
        scanCount:                  defaultTokenStoreScanCount,
        clock:                      melodyclock.NewSystemClock(),
        epochRetentionMilliseconds: defaultRevocationEpochRetentionMilliseconds,
        callTimeout:                defaultTokenStoreCallTimeout,
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

    if nil == store.clock {
        store.clock = melodyclock.NewSystemClock()
    }

    if 0 >= store.epochRetentionMilliseconds {
        store.epochRetentionMilliseconds = defaultRevocationEpochRetentionMilliseconds
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

/* WithTokenStoreContext supplies the context the contract's context-less doors derive theirs from. Its cancellation and deadline are dropped deliberately, since the store lives as long as the application and a lapsed boot context would fail every later Put and Delete; only its values are kept, and one round trip lives by the call timeout. */
func WithTokenStoreContext(ctx context.Context) TokenStoreOption {
    return func(store *RedisTokenStore) {
        if nil == ctx {
            return
        }

        store.ctx = context.WithoutCancel(ctx)
    }
}

/* WithTokenStoreCallTimeout bounds one round trip of every door: the context-less half (Put, PutWithTtl, Delete, DeleteByUser, PurgeExpired, RevokeBefore) and the runtime half (Lookup, RevocationEpoch), where it caps the request context so a request without a deadline, as melody's http kernel leaves it, fails fast while a tighter deadline is kept. Without it a store that stops answering holds a write for the client's connection timeout, five seconds at the provider's default, and a read forever, since the client retries the SSCAN, SCAN and HMGET read-only commands for as long as the context allows. The bound is per round trip, so a walk over a large index gets one budget per batch; a non-positive timeout falls back to the default, and the cache subpackage reads its command timeout the other way. */
func WithTokenStoreCallTimeout(timeout time.Duration) TokenStoreOption {
    return func(store *RedisTokenStore) {
        store.callTimeout = resolvedCallTimeout(timeout, defaultTokenStoreCallTimeout)
    }
}

func WithTokenStoreClock(clockInstance clockcontract.Clock) TokenStoreOption {
    return func(store *RedisTokenStore) {
        if nil == clockInstance {
            return
        }

        store.clock = clockInstance
    }
}

func WithTokenStoreMaximumClockSkew(skew time.Duration) TokenStoreOption {
    return func(store *RedisTokenStore) {
        /* a negative skew is refused rather than ignored: ignored, the operator would believe a tighter policy is in force, and carried it would narrow every revocation boundary, a bypass */
        if 0 > skew {
            exception.Panic(exception.NewError(
                "redis token store maximum clock skew may not be negative",
                map[string]any{"skew": skew.String()},
                nil,
            ))
        }

        store.maximumClockSkew = skew
    }
}

func WithRevocationEpochRetention(retention time.Duration) TokenStoreOption {
    return func(store *RedisTokenStore) {
        /* a negative retention is refused rather than replaced by the default, since a boundary expiring earlier than configured is a revocation bypass; zero keeps the default retention */
        if 0 > retention {
            exception.Panic(exception.NewError(
                "redis token store revocation epoch retention may not be negative",
                map[string]any{"retention": retention.String()},
                nil,
            ))
        }

        if 0 == retention {
            return
        }

        store.epochRetentionMilliseconds = floorPositiveMilliseconds(retention)
    }
}

type RedisTokenStore struct {
    client                     rueidis.Client
    ctx                        context.Context
    prefix                     string
    scanCount                  int
    clock                      clockcontract.Clock
    epochRetentionMilliseconds int64
    maximumClockSkew           time.Duration
    callTimeout                time.Duration
}

/* callContext bounds one round trip of a door that carries no caller context. */
func (instance *RedisTokenStore) callContext() (context.Context, context.CancelFunc) {
    return context.WithTimeout(instance.ctx, instance.callTimeout)
}

/* runtimeCallContext caps the request context with the call timeout; context.WithTimeout keeps the earlier deadline, so a tighter request deadline still wins, while one without a deadline is not retried against an unresponsive store for as long as the client's retry policy allows. */
func (instance *RedisTokenStore) runtimeCallContext(runtimeInstance runtimecontract.Runtime) (context.Context, context.CancelFunc) {
    return context.WithTimeout(runtimeInstance.Context(), instance.callTimeout)
}

func (instance *RedisTokenStore) Put(tokenString string, claims securitycontract.Claims) {
    instance.put(tokenString, claims, 0)
}

func (instance *RedisTokenStore) PutWithTtl(tokenString string, claims securitycontract.Claims, ttl time.Duration) {
    /* a non-positive ttl is refused instead of falling through to the store-forever spelling, since its likeliest caller computed a remaining lifetime that already elapsed; the token string never joins the context, being the credential */
    if 0 >= ttl {
        exception.Panic(exception.NewError(
            "redis token store ttl must be positive",
            map[string]any{"ttl": ttl.String(), "user": claims.UserIdentifier},
            nil,
        ))
    }

    instance.put(tokenString, claims, ttl)
}

func (instance *RedisTokenStore) Delete(tokenString string) {
    callContext, cancel := instance.callContext()
    defer cancel()

    result := tokenDeleteScript.Exec(
        callContext,
        instance.client,
        []string{instance.tokenKey(tokenString)},
        []string{instance.userKeyPrefix()},
    )
    if resultErr := result.Error(); nil != resultErr {
        exception.Panic(exception.NewError("redis token store delete failed", nil, resultErr))
    }
}

/* DeleteByUser reclaims what a revocation made unusable; it is not the revocation itself. SSCAN may miss a member added while the walk runs, so a token issued during the call survives it — RevokeBefore is what ends a user's sessions. */
func (instance *RedisTokenStore) DeleteByUser(userIdentifier string) int {
    indexKey := instance.userKey(userIdentifier)

    removed := 0
    cursor := uint64(0)

    for {
        scanContext, scanCancel := instance.callContext()
        scan, scanErr := instance.client.Do(
            scanContext,
            instance.client.B().Sscan().Key(indexKey).Cursor(cursor).Count(int64(instance.scanCount)).Build(),
        ).AsScanEntry()
        scanCancel()
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

    callContext, cancel := instance.callContext()
    defer cancel()

    result := tokenDeleteByUserScript.Exec(callContext, instance.client, keys, []string{userIdentifier})

    removed, resultErr := result.AsInt64()
    if nil != resultErr {
        exception.Panic(exception.NewError("redis token store delete by user failed", map[string]any{"user": userIdentifier}, resultErr))
    }

    return int(removed)
}

/* PurgeExpired drops the index entries of tokens that already expired, walking every user's index set in bounded batches. A set's own expiry bounds only a set whose members all died, since an account that keeps logging in keeps raising its deadline. The outer SCAN finds the user sets and each is settled by DeleteByUser's bounded batching, SCAN and SSCAN results sliced to the batch size since their count is a hint, so no script's hold on the server depends on the largest account. Pruning while scanning is what SSCAN tolerates: a present member is returned at least once, and a re-visit is harmless. */
func (instance *RedisTokenStore) PurgeExpired() int {
    pruned := 0
    cursor := uint64(0)

    for {
        scanContext, scanCancel := instance.callContext()
        scan, scanErr := instance.client.Do(
            scanContext,
            instance.client.B().Scan().Cursor(cursor).Match(escapeRedisGlobMeta(instance.userKeyPrefix())+"*").Count(int64(instance.scanCount)).Build(),
        ).AsScanEntry()
        scanCancel()
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
        scanContext, scanCancel := instance.callContext()
        scan, scanErr := instance.client.Do(
            scanContext,
            instance.client.B().Sscan().Key(indexKey).Cursor(cursor).Count(int64(instance.scanCount)).Build(),
        ).AsScanEntry()
        scanCancel()
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

    callContext, cancel := instance.callContext()
    defer cancel()

    result := tokenPurgeUserScript.Exec(callContext, instance.client, keys, nil)

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
    callContext, cancel := instance.runtimeCallContext(runtimeInstance)
    defer cancel()

    values, lookupErr := tokenLookupScript.Exec(
        callContext,
        instance.client,
        []string{instance.tokenKey(tokenString)},
        []string{instance.epochKeyPrefix(), revocationUserField, revocationDeviceFieldPrefix},
    ).ToArray()

    if nil != lookupErr {
        return securitycontract.Claims{}, false, exception.NewError("redis token store lookup failed", nil, lookupErr)
    }

    if 0 == len(values) {
        return securitycontract.Claims{}, false, nil
    }

    payload, payloadErr := values[0].ToString()
    if nil != payloadErr {
        return securitycontract.Claims{}, false, exception.NewError("redis token store lookup returned no payload", nil, payloadErr)
    }

    claims := securitycontract.Claims{}
    if unmarshalErr := json.Unmarshal([]byte(payload), &claims); nil != unmarshalErr {
        return securitycontract.Claims{}, false, exception.NewError("redis token store could not decode claims", nil, unmarshalErr)
    }

    revoked, revokedErr := instance.tokenIsRevoked(claims.IssuedAt, epochValueAt(values, 1), epochValueAt(values, 2))
    if nil != revokedErr {
        return securitycontract.Claims{}, false, revokedErr
    }

    if true == revoked {
        return securitycontract.Claims{}, false, nil
    }

    return claims, true, nil
}

func (instance *RedisTokenStore) RevokeBefore(userIdentifier string, deviceIdentifier string, instant time.Time) {
    if "" == userIdentifier {
        exception.Panic(exception.NewError("redis token store revocation needs a user identifier", nil, nil))
    }

    if true == instant.IsZero() || true == instant.Before(revocationEpochLowerBound) || true == instant.After(revocationEpochUpperBound) {
        exception.Panic(exception.NewError(
            "redis token store revocation instant is not representable",
            map[string]any{"user": userIdentifier, "instant": instant.String()},
            nil,
        ))
    }

    callContext, cancel := instance.callContext()
    defer cancel()

    result := tokenRevokeEpochScript.Exec(
        callContext,
        instance.client,
        []string{instance.epochKey(userIdentifier), instance.userKey(userIdentifier)},
        []string{
            revocationField(deviceIdentifier),
            strconv.FormatInt(instant.UnixNano(), 10),
            strconv.FormatInt(instance.epochRetentionMilliseconds, 10),
        },
    )
    if resultErr := result.Error(); nil != resultErr {
        exception.Panic(exception.NewError("redis token store revoke failed", map[string]any{"user": userIdentifier}, resultErr))
    }
}

func (instance *RedisTokenStore) RevocationEpoch(
    runtimeInstance runtimecontract.Runtime,
    userIdentifier string,
    deviceIdentifier string,
) (time.Time, error) {
    fields := []string{revocationUserField}
    if "" != deviceIdentifier {
        fields = append(fields, revocationDeviceFieldPrefix+deviceIdentifier)
    }

    callContext, cancel := instance.runtimeCallContext(runtimeInstance)
    defer cancel()

    values, readErr := instance.client.Do(
        callContext,
        instance.client.B().Hmget().Key(instance.epochKey(userIdentifier)).Field(fields...).Build(),
    ).ToArray()

    if nil != readErr {
        if true == rueidis.IsRedisNil(readErr) {
            return time.Time{}, nil
        }

        return time.Time{}, exception.NewError(
            "redis token store revocation epoch read failed",
            map[string]any{"user": userIdentifier},
            readErr,
        )
    }

    latest := int64(0)
    for index := range values {
        parsed, parseErr := parseRevocationEpoch(epochValueAt(values, index))
        if nil != parseErr {
            return time.Time{}, parseErr
        }

        if latest < parsed {
            latest = parsed
        }
    }

    if 0 == latest {
        return time.Time{}, nil
    }

    return time.Unix(0, latest), nil
}

func revocationField(deviceIdentifier string) string {
    if "" == deviceIdentifier {
        return revocationUserField
    }

    return revocationDeviceFieldPrefix + deviceIdentifier
}

func epochValueAt(values []rueidis.RedisMessage, index int) string {
    if index >= len(values) {
        return ""
    }

    value, valueErr := values[index].ToString()
    if nil != valueErr {
        return ""
    }

    return value
}

func parseRevocationEpoch(value string) (int64, error) {
    if "" == value {
        return 0, nil
    }

    parsed, parseErr := strconv.ParseInt(value, 10, 64)
    if nil != parseErr {
        return 0, exception.NewError(
            "redis token store could not decode a revocation epoch",
            map[string]any{"epoch": value},
            parseErr,
        )
    }

    return parsed, nil
}

func (instance *RedisTokenStore) tokenIsRevoked(issuedAt time.Time, epochValues ...string) (bool, error) {
    if 0 < instance.maximumClockSkew && true == issuedAt.After(instance.clock.Now().Add(instance.maximumClockSkew)) {
        return true, nil
    }

    latest := int64(0)

    for _, epochValue := range epochValues {
        parsed, parseErr := parseRevocationEpoch(epochValue)
        if nil != parseErr {
            return true, parseErr
        }

        if latest < parsed {
            latest = parsed
        }
    }

    if 0 == latest {
        return false, nil
    }

    return false == issuedAt.After(time.Unix(0, latest).Add(instance.maximumClockSkew)), nil
}

func (instance *RedisTokenStore) put(tokenString string, claims securitycontract.Claims, ttl time.Duration) {
    /* the IssuedAt stamp is read client-side, one round trip before the script lands, by design: the stamps come from the injected clock (WithTokenStoreClock), which a server-side stamp would replace with redis's. The window a RevokeBefore can slip into is one marshal plus one round trip and fails closed, the fresh token reading as pre-boundary; WithTokenStoreMaximumClockSkew absorbs the same interleaving between instances. */
    claims.IssuedAt = instance.clock.Now()

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

    callContext, cancel := instance.callContext()
    defer cancel()

    result := tokenPutScript.Exec(
        callContext,
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

func (instance *RedisTokenStore) epochKey(userIdentifier string) string {
    return instance.keyspace() + ":epoch:" + userIdentifier
}

func (instance *RedisTokenStore) epochKeyPrefix() string {
    return instance.keyspace() + ":epoch:"
}

var _ securitycontract.RevocableTokenStore = (*RedisTokenStore)(nil)
var _ securitycontract.EpochRevocableTokenStore = (*RedisTokenStore)(nil)
