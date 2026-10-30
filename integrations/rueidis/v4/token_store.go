package rueidis

import (
    "context"
    "encoding/json"
    "math"
    "strconv"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v4/bound"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/redis/rueidis"
)

const (
    defaultTokenStorePrefix    = "melody:token"
    defaultTokenStoreScanCount = 256
)

const defaultRevocationEpochRetention = 7 * 24 * time.Hour

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
if ARGV[3] == "0" then
    redis.call("persist", KEYS[1])
    return 1
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

/* TokenStoreConfig declares the store's tunables, read once at NewTokenStore. */
type TokenStoreConfig struct {
    /* Prefix is the namespace of every token, index set and revocation hash, melody:token when empty. */
    Prefix string

    /* ScanCount is the COUNT hint and the batch of the SSCAN and SCAN walks: 256 by default, at least 1, unlimited refused. */
    ScanCount bound.Count

    /* CallTimeout bounds one round trip of every door: the context-less half (Put, Delete, DeleteByUser, PurgeExpired, RevokeBefore) and the runtime half (Lookup, RevocationEpoch), where it caps the request context so a request without a deadline, as melody's http kernel leaves it, fails fast while a tighter deadline is kept. 1s by default, at least 1ms, unlimited refused, since unbounded a store that stops answering would hold a write for the client's write deadline and a read forever, the client retrying the SSCAN, SCAN and HMGET read-only commands for as long as the context allows. The bound is per round trip, so a walk over a large index gets one budget per batch. */
    CallTimeout bound.Duration

    /* Clock is where the issue stamps are read; nil is the system clock, and the container's when the store is registered through RegisterTokenStoreService. */
    Clock clockcontract.Clock

    /* MaximumClockSkew is how far a token's issue instant may sit ahead of the clock before it is refused, and the slack every revocation boundary is widened by: 0 by default, which is no tolerance, at least 0, unlimited refused, since an unbounded tolerance admits any future issue instant. */
    MaximumClockSkew bound.Duration

    /* RevocationEpochRetention is how long a user's revocation boundary outlives its instant: 7 days by default, at least 1s, unlimited keeping it for ever, as the in-memory store does, at the cost of one hash kept per user that ever revoked. */
    RevocationEpochRetention bound.Duration
}

var tokenStoreScanCountRule = bound.Rule[int]{
    Name:    "TokenStoreConfig.ScanCount",
    Default: defaultTokenStoreScanCount,
    Minimum: 1,
}

var tokenStoreCallTimeoutRule = callTimeoutRule("TokenStoreConfig.CallTimeout", defaultTokenStoreCallTimeout)

var tokenStoreMaximumClockSkewRule = bound.Rule[time.Duration]{
    Name:    "TokenStoreConfig.MaximumClockSkew",
    Default: 0,
    Minimum: 0,
}

var tokenStoreRevocationEpochRetentionRule = bound.Rule[time.Duration]{
    Name:              "TokenStoreConfig.RevocationEpochRetention",
    Default:           defaultRevocationEpochRetention,
    Minimum:           bound.TtlMinimum,
    UnlimitedAdmitted: true,
}

type resolvedTokenStore struct {
    prefix                     string
    scanCount                  int
    callTimeout                time.Duration
    clock                      clockcontract.Clock
    maximumClockSkew           time.Duration
    epochRetentionMilliseconds int64
}

func resolveTokenStoreConfig(config TokenStoreConfig) (resolvedTokenStore, error) {
    scanCount, scanCountErr := bound.Resolve(config.ScanCount, tokenStoreScanCountRule)
    if nil != scanCountErr {
        return resolvedTokenStore{}, scanCountErr
    }

    callTimeout, callTimeoutErr := bound.Resolve(config.CallTimeout, tokenStoreCallTimeoutRule)
    if nil != callTimeoutErr {
        return resolvedTokenStore{}, callTimeoutErr
    }

    maximumClockSkew, maximumClockSkewErr := bound.Resolve(config.MaximumClockSkew, tokenStoreMaximumClockSkewRule)
    if nil != maximumClockSkewErr {
        return resolvedTokenStore{}, maximumClockSkewErr
    }

    epochRetention, epochRetentionErr := bound.Resolve(config.RevocationEpochRetention, tokenStoreRevocationEpochRetentionRule)
    if nil != epochRetentionErr {
        return resolvedTokenStore{}, epochRetentionErr
    }

    prefix := config.Prefix
    if "" == prefix {
        prefix = defaultTokenStorePrefix
    }

    /* 0 is the revoke script's spelling of a boundary kept for ever */
    epochRetentionMilliseconds := int64(0)
    if false == epochRetention.Unlimited {
        epochRetentionMilliseconds = epochRetention.Value.Milliseconds()
    }

    return resolvedTokenStore{
        prefix:                     prefix,
        scanCount:                  scanCount.Value,
        callTimeout:                callTimeout.Value,
        clock:                      config.Clock,
        maximumClockSkew:           maximumClockSkew.Value,
        epochRetentionMilliseconds: epochRetentionMilliseconds,
    }, nil
}

/* NewTokenStore resolves the configuration once; a nil client or a limit outside its rule panics naming it. */
func NewTokenStore(client rueidis.Client, config TokenStoreConfig) *RedisTokenStore {
    if nil == client {
        exception.Panic(exception.NewError("redis token store client is nil", nil, nil))
    }

    resolved, resolveErr := resolveTokenStoreConfig(config)
    if nil != resolveErr {
        exception.Panic(exception.FromError(resolveErr))
    }

    return newTokenStoreFromResolved(client, resolved)
}

func newTokenStoreFromResolved(client rueidis.Client, resolved resolvedTokenStore) *RedisTokenStore {
    clockInstance := resolved.clock
    if nil == clockInstance {
        clockInstance = melodyclock.NewSystemClock()
    }

    return &RedisTokenStore{
        client:                     client,
        prefix:                     resolved.prefix,
        scanCount:                  resolved.scanCount,
        clock:                      clockInstance,
        epochRetentionMilliseconds: resolved.epochRetentionMilliseconds,
        maximumClockSkew:           resolved.maximumClockSkew,
        callTimeout:                resolved.callTimeout,
    }
}

/* RedisTokenStore holds a redis client and the settings of the store — the key prefix, the scan batch, the clock, the retention and skew bounds and the per-call timeout — all fixed at construction. The tokens and the revocation epochs live in redis, so one store serves concurrent callers without a lock. */
type RedisTokenStore struct {
    client                     rueidis.Client
    prefix                     string
    scanCount                  int
    clock                      clockcontract.Clock
    epochRetentionMilliseconds int64
    maximumClockSkew           time.Duration
    callTimeout                time.Duration
}

/* callContext bounds one round trip of a door that carries no caller context. */
func (instance *RedisTokenStore) callContext() (context.Context, context.CancelFunc) {
    return context.WithTimeout(context.Background(), instance.callTimeout)
}

/* runtimeCallContext caps the request context with the call timeout; context.WithTimeout keeps the earlier deadline, so a tighter request deadline still wins, while one without a deadline is not retried against an unresponsive store for as long as the client's retry policy allows. */
func (instance *RedisTokenStore) runtimeCallContext(runtimeInstance runtimecontract.Runtime) (context.Context, context.CancelFunc) {
    return context.WithTimeout(runtimeInstance.Context(), instance.callTimeout)
}

/* Put stores the token under the ttl, read against the contract's TokenTtlRule: an absent ttl and one under a second panic naming the ttl, since the likeliest caller of a lapsed one computed a remaining lifetime that already elapsed, and unlimited stores it with no expiry. The user identifier must be valid UTF-8. */
func (instance *RedisTokenStore) Put(tokenString string, claims securitycontract.Claims, ttl bound.Duration) {
    expiry, err := securitycontract.ResolveTokenTtl(ttl)
    if nil != err {
        exception.Panic(exception.FromError(err))
    }

    instance.put(tokenString, claims, expiry)
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
    refuseNonUtf8UserIdentifier(userIdentifier)

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

/* PurgeExpired drops the index entries of tokens that already expired, walking every user's index set in bounded batches. A set's own expiry bounds only a set whose members all died, since an account that keeps logging in keeps raising its deadline. The outer SCAN finds the user sets, walked one at a time, and each set is pruned by its own SSCAN, the results sliced to the batch size since the count is a hint, each slice tested by one purge script under DeleteByUser's bound, so no script's hold on the server depends on the largest account; the script drops only the members whose token is gone, a live token keeping its entry. Pruning while scanning is what SSCAN tolerates: a present member is returned at least once, and a re-visit is harmless. */
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

    refuseNonUtf8UserIdentifier(userIdentifier)

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

/* refuseNonUtf8UserIdentifier refuses a user identifier that is not valid UTF-8 at every door that writes or matches it: the stored claims are json, which writes an invalid byte as the replacement character, so the scripts that compare the stored user with the raw identifier would never match it and RevokeBefore and DeleteByUser would leave the user's tokens standing. The refusal names the length, never the bytes. */
func refuseNonUtf8UserIdentifier(userIdentifier string) {
    if false == utf8.ValidString(userIdentifier) {
        exception.Panic(exception.NewError(
            "redis token store user identifier must be valid UTF-8",
            map[string]any{"userLength": len(userIdentifier)},
            nil,
        ))
    }
}

func (instance *RedisTokenStore) put(tokenString string, claims securitycontract.Claims, ttl time.Duration) {
    refuseNonUtf8UserIdentifier(claims.UserIdentifier)

    /* the IssuedAt stamp is read client-side, one round trip before the script lands, by design: the stamps come from the injected clock (TokenStoreConfig.Clock), which a server-side stamp would replace with redis's. The window a RevokeBefore can slip into is one marshal plus one round trip and fails closed, the fresh token reading as pre-boundary; TokenStoreConfig.MaximumClockSkew absorbs the same interleaving between instances. */
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
