package rueidis

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "math"
    "strconv"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v4/bound"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/redis/rueidis"
)

const (
    defaultTokenStorePrefix    = "melody:token"
    defaultTokenStoreScanCount = 256
)

const defaultRevocationEpochRetention = 7 * 24 * time.Hour

const defaultTokenStoreCallTimeout = time.Second

const (
    revocationUserField = "user"

    revocationDeviceFieldPrefix = "device:"
)

var revocationEpochLowerBound = time.Unix(0, 0)
var revocationEpochUpperBound = time.Unix(0, math.MaxInt64)

/* the index set outlives its longest-lived token, or that token would become unrevocable */
const tokenIndexExpiryGraceMilliseconds = 1000

/* the index expiry is only ever raised: a set expiring before a listed token would leave that token unrevocable */
var tokenPutScript = rueidis.NewLuaScript(`
local indexKey = ARGV[3] .. ARGV[4]
local existing = redis.call("get", KEYS[1])
if existing then
    local decodedOk, decoded = pcall(cjson.decode, existing)
    if decodedOk and type(decoded) == "table" then
        local oldUser = decoded["userIdentifier"]
        if type(oldUser) == "string" and oldUser ~= ARGV[4] then
            redis.call("srem", ARGV[3] .. oldUser, KEYS[1])
        end
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
local decodedOk, decoded = pcall(cjson.decode, existing)
redis.call("del", KEYS[1])
if decodedOk and type(decoded) == "table" then
    local user = decoded["userIdentifier"]
    if type(user) == "string" then
        redis.call("srem", ARGV[1] .. user, KEYS[1])
    end
end
return 1
`)

/* a member may name a verbatim key an earlier release wrote with PascalCase claims, so both spellings are read; an undecodable record has no owner to check and is removed */
var tokenDeleteByUserScript = rueidis.NewLuaScript(`
local removed = 0
for index = 2, #KEYS do
    local value = redis.call("get", KEYS[index])
    if value then
        local decodedOk, decoded = pcall(cjson.decode, value)
        if not decodedOk or type(decoded) ~= "table" or (decoded["userIdentifier"] or decoded["UserIdentifier"]) == ARGV[1] then
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

/* SREM is counted, since a walked set may hand the same member back twice */
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
    local user = decoded["userIdentifier"]
    if type(user) == "string" and user ~= "" then
        local epochKey = ARGV[1] .. user
        userEpoch = redis.call("hget", epochKey, ARGV[2]) or ""
        local device = decoded["deviceIdentifier"]
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

    /* CallTimeout bounds one round trip of every door, capping the caller's context so a request without a deadline, as melody's http kernel leaves it, fails fast while a tighter deadline is kept; a round trip it cut answers an infrastructure failure naming this field. 1s by default, at least 1ms, unlimited refused, since unbounded a store that stops answering would hold a write for the client's write deadline and a read forever, the client retrying the SSCAN, SCAN and HMGET read-only commands for as long as the context allows. The bound is per round trip, so a walk over a large index gets one budget per batch. */
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

func (instance *RedisTokenStore) runtimeCallContext(parentContext context.Context) (context.Context, context.CancelFunc) {
    return context.WithTimeout(parentContext, instance.callTimeout)
}

/* a nil call context and a nil cause are a call refused before its round trip because the caller's context had already ended */
func storeFailure(message string, errorContext map[string]any, parentContext context.Context, callContext context.Context, cause error) error {
    if nil == errorContext {
        errorContext = map[string]any{}
    }

    parentErr := parentContext.Err()
    if nil == parentErr && true == deadlinePassed(parentContext) {
        parentErr = context.DeadlineExceeded
    }

    if nil != parentErr {
        if nil == cause {
            cause = parentErr
        } else if false == exception.ChainIs(cause, parentErr) {
            cause = fmt.Errorf("%w: %w", parentErr, cause)
        }

        return exception.NewError(message, errorContext, exception.MarkInfrastructureFailure(cause))
    }

    if nil != callContext && true == deadlinePassed(callContext) && true == exception.ChainIs(cause, context.DeadlineExceeded) {
        errorContext["bound"] = tokenStoreCallTimeoutRule.Name
    }

    return exception.NewError(message, errorContext, exception.MarkInfrastructureFailure(cause))
}

/* the store can answer a deadline a moment before the context records it, and a call context is read after its cancel */
func deadlinePassed(ctx context.Context) bool {
    deadline, hasDeadline := ctx.Deadline()

    return true == hasDeadline && false == time.Now().Before(deadline)
}

/* Put stores the token under the ttl, read against the contract's TokenTtlRule: an absent ttl and one under a second are refused naming the ttl, since the likeliest caller of a lapsed one computed a remaining lifetime that already elapsed, and unlimited stores it with no expiry. The user identifier must be valid UTF-8. The token is keyed by its SHA-256 digest, which an unkeyed hash makes safe only for a token of at least 128 bits of entropy: a guessable token is found from its digest by whoever reads the keyspace. */
func (instance *RedisTokenStore) Put(
    runtimeInstance containercontract.Runtime,
    tokenString string,
    claims securitycontract.Claims,
    ttl bound.Duration,
) error {
    if runtimeErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != runtimeErr {
        return runtimeErr
    }

    expiry, err := securitycontract.ResolveTokenTtl(ttl)
    if nil != err {
        return err
    }

    return instance.put(runtimeInstance.Context(), tokenString, claims, expiry)
}

func (instance *RedisTokenStore) Delete(runtimeInstance containercontract.Runtime, tokenString string) error {
    if runtimeErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != runtimeErr {
        return runtimeErr
    }

    parentContext := runtimeInstance.Context()
    if nil != parentContext.Err() {
        return storeFailure("redis token store delete failed", nil, parentContext, nil, nil)
    }

    callContext, cancel := instance.runtimeCallContext(parentContext)
    defer cancel()

    result := tokenDeleteScript.Exec(
        callContext,
        instance.client,
        []string{instance.tokenKey(tokenString)},
        []string{instance.userKeyPrefix()},
    )
    if resultErr := result.Error(); nil != resultErr {
        return storeFailure("redis token store delete failed", nil, parentContext, callContext, resultErr)
    }

    return nil
}

/* DeleteByUser reclaims what a revocation made unusable; it is not the revocation itself. SSCAN may miss a member added while the walk runs, so a token issued during the call survives it — RevokeBefore is what ends a user's sessions. A walk the store or the caller's context cut answers what it removed before beside the error. */
func (instance *RedisTokenStore) DeleteByUser(runtimeInstance containercontract.Runtime, userIdentifier string) (int, error) {
    if runtimeErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != runtimeErr {
        return 0, runtimeErr
    }

    if utf8Err := refuseNonUtf8UserIdentifier(userIdentifier); nil != utf8Err {
        return 0, utf8Err
    }

    parentContext := runtimeInstance.Context()
    indexKey := instance.userKey(userIdentifier)

    removed := 0
    cursor := uint64(0)

    for {
        if nil != parentContext.Err() {
            return removed, storeFailure("redis token store delete by user scan failed", map[string]any{"user": userIdentifier}, parentContext, nil, nil)
        }

        scanContext, scanCancel := instance.runtimeCallContext(parentContext)
        scan, scanErr := instance.client.Do(
            scanContext,
            instance.client.B().Sscan().Key(indexKey).Cursor(cursor).Count(int64(instance.scanCount)).Build(),
        ).AsScanEntry()
        scanCancel()
        if nil != scanErr {
            return removed, storeFailure("redis token store delete by user scan failed", map[string]any{"user": userIdentifier}, parentContext, scanContext, scanErr)
        }

        for offset := 0; offset < len(scan.Elements); offset += instance.scanCount {
            limit := offset + instance.scanCount
            if len(scan.Elements) < limit {
                limit = len(scan.Elements)
            }

            batchRemoved, batchErr := instance.deleteTokenBatch(parentContext, indexKey, userIdentifier, scan.Elements[offset:limit])
            removed += batchRemoved
            if nil != batchErr {
                return removed, batchErr
            }
        }

        cursor = scan.Cursor
        if 0 == cursor {
            break
        }
    }

    return removed, nil
}

func (instance *RedisTokenStore) deleteTokenBatch(parentContext context.Context, indexKey string, userIdentifier string, members []string) (int, error) {
    if nil != parentContext.Err() {
        return 0, storeFailure("redis token store delete by user failed", map[string]any{"user": userIdentifier}, parentContext, nil, nil)
    }

    keys := make([]string, 0, len(members)+1)
    keys = append(keys, indexKey)
    keys = append(keys, members...)

    callContext, cancel := instance.runtimeCallContext(parentContext)
    defer cancel()

    result := tokenDeleteByUserScript.Exec(callContext, instance.client, keys, []string{userIdentifier})

    removed, resultErr := result.AsInt64()
    if nil != resultErr {
        return 0, storeFailure("redis token store delete by user failed", map[string]any{"user": userIdentifier}, parentContext, callContext, resultErr)
    }

    return int(removed), nil
}

/* PurgeExpired drops the index entries of tokens that already expired, walking every user's index set in bounded batches. A set's own expiry bounds only a set whose members all died, since an account that keeps logging in keeps raising its deadline. The outer SCAN finds the user sets, walked one at a time, and each set is pruned by its own SSCAN, the results sliced to the batch size since the count is a hint, each slice tested by one purge script under DeleteByUser's bound, so no script's hold on the server depends on the largest account; the script drops only the members whose token is gone, a live token keeping its entry. Pruning while scanning is what SSCAN tolerates: a present member is returned at least once, and a re-visit is harmless. A walk the store or the caller's context cut answers what it pruned before beside the error. */
func (instance *RedisTokenStore) PurgeExpired(runtimeInstance containercontract.Runtime) (int, error) {
    if runtimeErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != runtimeErr {
        return 0, runtimeErr
    }

    parentContext := runtimeInstance.Context()
    if nil != parentContext.Err() {
        return 0, storeFailure("redis token store purge scan failed", nil, parentContext, nil, nil)
    }

    pruned := 0
    pattern := escapeRedisGlobMeta(instance.userKeyPrefix()) + "*"

    /* a keyless SCAN on a cluster client lands on an arbitrary node, so each node is walked on its own cursor */
    for _, node := range instance.client.Nodes() {
        cursor := uint64(0)

        for {
            if nil != parentContext.Err() {
                return pruned, storeFailure("redis token store purge scan failed", nil, parentContext, nil, nil)
            }

            scanContext, scanCancel := instance.runtimeCallContext(parentContext)
            scan, scanErr := node.Do(
                scanContext,
                node.B().Scan().Cursor(cursor).Match(pattern).Count(int64(instance.scanCount)).Build(),
            ).AsScanEntry()
            scanCancel()
            if nil != scanErr {
                return pruned, storeFailure("redis token store purge scan failed", nil, parentContext, scanContext, scanErr)
            }

            for _, setKey := range scan.Elements {
                setPruned, setErr := instance.purgeUserIndex(parentContext, setKey)
                pruned += setPruned
                if nil != setErr {
                    return pruned, setErr
                }
            }

            cursor = scan.Cursor
            if 0 == cursor {
                break
            }
        }
    }

    return pruned, nil
}

func (instance *RedisTokenStore) purgeUserIndex(parentContext context.Context, indexKey string) (int, error) {
    pruned := 0
    cursor := uint64(0)

    for {
        if nil != parentContext.Err() {
            return pruned, storeFailure("redis token store purge member scan failed", map[string]any{"set": indexKey}, parentContext, nil, nil)
        }

        scanContext, scanCancel := instance.runtimeCallContext(parentContext)
        scan, scanErr := instance.client.Do(
            scanContext,
            instance.client.B().Sscan().Key(indexKey).Cursor(cursor).Count(int64(instance.scanCount)).Build(),
        ).AsScanEntry()
        scanCancel()
        if nil != scanErr {
            return pruned, storeFailure("redis token store purge member scan failed", map[string]any{"set": indexKey}, parentContext, scanContext, scanErr)
        }

        for offset := 0; offset < len(scan.Elements); offset += instance.scanCount {
            limit := offset + instance.scanCount
            if len(scan.Elements) < limit {
                limit = len(scan.Elements)
            }

            batchPruned, batchErr := instance.pruneTokenBatch(parentContext, indexKey, scan.Elements[offset:limit])
            pruned += batchPruned
            if nil != batchErr {
                return pruned, batchErr
            }
        }

        cursor = scan.Cursor
        if 0 == cursor {
            break
        }
    }

    return pruned, nil
}

func (instance *RedisTokenStore) pruneTokenBatch(parentContext context.Context, indexKey string, members []string) (int, error) {
    if nil != parentContext.Err() {
        return 0, storeFailure("redis token store purge failed", map[string]any{"set": indexKey}, parentContext, nil, nil)
    }

    keys := make([]string, 0, len(members)+1)
    keys = append(keys, indexKey)
    keys = append(keys, members...)

    callContext, cancel := instance.runtimeCallContext(parentContext)
    defer cancel()

    result := tokenPurgeUserScript.Exec(callContext, instance.client, keys, nil)

    pruned, resultErr := result.AsInt64()
    if nil != resultErr {
        return 0, storeFailure("redis token store purge failed", map[string]any{"set": indexKey}, parentContext, callContext, resultErr)
    }

    return int(pruned), nil
}

func (instance *RedisTokenStore) Lookup(
    runtimeInstance containercontract.Runtime,
    tokenString string,
) (securitycontract.Claims, bool, error) {
    if runtimeErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != runtimeErr {
        return securitycontract.Claims{}, false, runtimeErr
    }

    parentContext := runtimeInstance.Context()
    if nil != parentContext.Err() {
        return securitycontract.Claims{}, false, storeFailure("redis token store lookup failed", nil, parentContext, nil, nil)
    }

    callContext, cancel := instance.runtimeCallContext(parentContext)
    defer cancel()

    values, lookupErr := tokenLookupScript.Exec(
        callContext,
        instance.client,
        []string{instance.tokenKey(tokenString)},
        []string{instance.epochKeyPrefix(), revocationUserField, revocationDeviceFieldPrefix},
    ).ToArray()

    if nil != lookupErr {
        return securitycontract.Claims{}, false, storeFailure("redis token store lookup failed", nil, parentContext, callContext, lookupErr)
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

func (instance *RedisTokenStore) RevokeBefore(
    runtimeInstance containercontract.Runtime,
    userIdentifier string,
    deviceIdentifier string,
    instant time.Time,
) error {
    if runtimeErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != runtimeErr {
        return runtimeErr
    }

    if "" == userIdentifier {
        return exception.NewError("redis token store revocation needs a user identifier", nil, nil)
    }

    if utf8Err := refuseNonUtf8UserIdentifier(userIdentifier); nil != utf8Err {
        return utf8Err
    }

    if true == instant.IsZero() || true == instant.Before(revocationEpochLowerBound) || true == instant.After(revocationEpochUpperBound) {
        return exception.NewError(
            "redis token store revocation instant is not representable",
            map[string]any{"user": userIdentifier, "instant": instant.String()},
            nil,
        )
    }

    parentContext := runtimeInstance.Context()
    if nil != parentContext.Err() {
        return storeFailure("redis token store revoke failed", map[string]any{"user": userIdentifier}, parentContext, nil, nil)
    }

    callContext, cancel := instance.runtimeCallContext(parentContext)
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
        return storeFailure("redis token store revoke failed", map[string]any{"user": userIdentifier}, parentContext, callContext, resultErr)
    }

    return nil
}

func (instance *RedisTokenStore) RevocationEpoch(
    runtimeInstance containercontract.Runtime,
    userIdentifier string,
    deviceIdentifier string,
) (time.Time, error) {
    if runtimeErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != runtimeErr {
        return time.Time{}, runtimeErr
    }

    fields := []string{revocationUserField}
    if "" != deviceIdentifier {
        fields = append(fields, revocationDeviceFieldPrefix+deviceIdentifier)
    }

    parentContext := runtimeInstance.Context()
    if nil != parentContext.Err() {
        return time.Time{}, storeFailure("redis token store revocation epoch read failed", map[string]any{"user": userIdentifier}, parentContext, nil, nil)
    }

    callContext, cancel := instance.runtimeCallContext(parentContext)
    defer cancel()

    values, readErr := instance.client.Do(
        callContext,
        instance.client.B().Hmget().Key(instance.epochKey(userIdentifier)).Field(fields...).Build(),
    ).ToArray()

    if nil != readErr {
        if true == rueidis.IsRedisNil(readErr) {
            return time.Time{}, nil
        }

        return time.Time{}, storeFailure(
            "redis token store revocation epoch read failed",
            map[string]any{"user": userIdentifier},
            parentContext,
            callContext,
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

/* json rewrites an invalid byte, so the scripts would never match a non-UTF-8 user and its tokens would stand */
func refuseNonUtf8UserIdentifier(userIdentifier string) error {
    if false == utf8.ValidString(userIdentifier) {
        return exception.NewError(
            "redis token store user identifier must be valid UTF-8",
            map[string]any{"userLength": len(userIdentifier)},
            nil,
        )
    }

    return nil
}

func (instance *RedisTokenStore) put(parentContext context.Context, tokenString string, claims securitycontract.Claims, ttl time.Duration) error {
    if utf8Err := refuseNonUtf8UserIdentifier(claims.UserIdentifier); nil != utf8Err {
        return utf8Err
    }

    /* stamped from the injected clock, not redis's; a RevokeBefore slipping into the round trip fails closed */
    claims.IssuedAt = instance.clock.Now()

    payload, marshalErr := json.Marshal(claims)
    if nil != marshalErr {
        return exception.NewError("redis token store could not encode claims", map[string]any{"user": claims.UserIdentifier}, marshalErr)
    }

    pttl := "0"
    indexPttl := "0"
    if 0 < ttl {
        tokenMilliseconds := floorPositiveMilliseconds(ttl)

        pttl = strconv.FormatInt(tokenMilliseconds, 10)
        indexPttl = strconv.FormatInt(tokenMilliseconds+tokenIndexExpiryGraceMilliseconds, 10)
    }

    if nil != parentContext.Err() {
        return storeFailure("redis token store put failed", map[string]any{"user": claims.UserIdentifier}, parentContext, nil, nil)
    }

    callContext, cancel := instance.runtimeCallContext(parentContext)
    defer cancel()

    result := tokenPutScript.Exec(
        callContext,
        instance.client,
        []string{instance.tokenKey(tokenString)},
        []string{string(payload), pttl, instance.userKeyPrefix(), claims.UserIdentifier, indexPttl},
    )
    if resultErr := result.Error(); nil != resultErr {
        return storeFailure("redis token store put failed", map[string]any{"user": claims.UserIdentifier}, parentContext, callContext, resultErr)
    }

    return nil
}

func (instance *RedisTokenStore) keyspace() string {
    return "{" + instance.prefix + "}"
}

func (instance *RedisTokenStore) tokenKey(tokenString string) string {
    digest := sha256.Sum256([]byte(tokenString))

    return instance.keyspace() + ":token:" + hex.EncodeToString(digest[:])
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
