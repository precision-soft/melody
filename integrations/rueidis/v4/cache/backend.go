package cache

import (
    "context"
    "errors"
    "math"
    "sort"
    "strconv"
    "strings"
    "sync/atomic"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/redis/rueidis"
)

const defaultBackendPrefix = "melody:cache:"

const roundTripCauseGrace = 100 * time.Millisecond

/* ErrCallTimeout marks a round trip cut by CallTimeout; it unwraps to context.DeadlineExceeded. */
var ErrCallTimeout error = callTimeoutCause{}

type callTimeoutCause struct{}

func (callTimeoutCause) Error() string {
    return "the round trip ran past BackendConfig.CallTimeout"
}

func (callTimeoutCause) Unwrap() error {
    return context.DeadlineExceeded
}

func (callTimeoutCause) Timeout() bool {
    return true
}

/* BackendConfig declares the backend; its zero value is the default. */
type BackendConfig struct {
    /* Prefix isolates the backend, melody:cache: by default; each application sharing a store needs its own. */
    Prefix string

    /* ScanCount is the COUNT hint of the SCAN walk: 1000 by default, at least 1. */
    ScanCount bound.Count

    /* DeleteBatch bounds the keys one DEL carries: 500 by default, at least 1. */
    DeleteBatch bound.Count

    /* MaxKeyLength bounds the key before the prefix: 1024 by default, at least 1. */
    MaxKeyLength bound.Count

    /* CallTimeout bounds every round trip on top of the runtime's context: 1s by default, at least 1ms. */
    CallTimeout bound.Duration
}

var backendScanCountRule = bound.Rule[int]{
    Name:    "BackendConfig.ScanCount",
    Default: 1000,
    Minimum: 1,
}

var backendDeleteBatchRule = bound.Rule[int]{
    Name:    "BackendConfig.DeleteBatch",
    Default: 500,
    Minimum: 1,
}

var backendMaxKeyLengthRule = bound.Rule[int]{
    Name:    "BackendConfig.MaxKeyLength",
    Default: 1024,
    Minimum: 1,
}

var backendCallTimeoutRule = bound.Rule[time.Duration]{
    Name:    "BackendConfig.CallTimeout",
    Default: time.Second,
    Minimum: bound.TimeoutMinimum,
}

type resolvedBackend struct {
    prefix         string
    scanCount      int
    deleteBatch    int
    maxKeyLength   int
    callTimeout    time.Duration
}

func resolveBackendConfig(config BackendConfig) (resolvedBackend, error) {
    scanCount, scanCountErr := bound.Resolve(config.ScanCount, backendScanCountRule)
    if nil != scanCountErr {
        return resolvedBackend{}, scanCountErr
    }

    deleteBatch, deleteBatchErr := bound.Resolve(config.DeleteBatch, backendDeleteBatchRule)
    if nil != deleteBatchErr {
        return resolvedBackend{}, deleteBatchErr
    }

    maxKeyLength, maxKeyLengthErr := bound.Resolve(config.MaxKeyLength, backendMaxKeyLengthRule)
    if nil != maxKeyLengthErr {
        return resolvedBackend{}, maxKeyLengthErr
    }

    callTimeout, callTimeoutErr := bound.Resolve(config.CallTimeout, backendCallTimeoutRule)
    if nil != callTimeoutErr {
        return resolvedBackend{}, callTimeoutErr
    }

    prefix := config.Prefix
    if "" == prefix {
        prefix = defaultBackendPrefix
    }

    return resolvedBackend{
        prefix:         prefix,
        scanCount:      scanCount.Value,
        deleteBatch:    deleteBatch.Value,
        maxKeyLength:   maxKeyLength.Value,
        callTimeout:    callTimeout.Value,
    }, nil
}

/* NewBackend builds the redis-backed cache; a nil client or a limit outside its rule is refused. */
func NewBackend(client rueidis.Client, config BackendConfig) (*Backend, error) {
    if nil == client {
        return nil, exception.NewError(
            "redis client is nil",
            nil,
            nil,
        )
    }

    resolved, resolveErr := resolveBackendConfig(config)
    if nil != resolveErr {
        return nil, resolveErr
    }

    return newBackendFromResolved(client, resolved), nil
}

func newBackendFromResolved(client rueidis.Client, resolved resolvedBackend) *Backend {
    return &Backend{
        client:         client,
        prefix:         resolved.prefix,
        scanCount:      resolved.scanCount,
        deleteBatch:    resolved.deleteBatch,
        maxKeyLength:   resolved.maxKeyLength,
        callTimeout:    resolved.callTimeout,
    }
}

type Backend struct {
    client         rueidis.Client
    prefix         string
    scanCount      int
    deleteBatch    int
    maxKeyLength   int
    callTimeout    time.Duration
    closed         atomic.Bool
}

type roundTripContext func() (context.Context, context.CancelFunc, func(error) error)

func (instance *Backend) commandContext(ctx context.Context) (context.Context, context.CancelFunc) {
    return context.WithTimeoutCause(ctx, instance.callTimeout, ErrCallTimeout)
}

func (instance *Backend) roundTripError(commandContext context.Context, operation string, roundTripErr error) error {
    if nil == roundTripErr || false == errors.Is(roundTripErr, context.DeadlineExceeded) {
        return roundTripErr
    }

    /* the client can see the deadline before the timer records its cause, so a past deadline is waited out */
    if nil == commandContext.Err() {
        deadline, hasDeadline := commandContext.Deadline()
        if false == hasDeadline || true == time.Now().Before(deadline) {
            return roundTripErr
        }

        /* a context reporting a deadline it never signals is answered unnamed past the grace */
        causeGraceTimer := time.NewTimer(roundTripCauseGrace)
        defer causeGraceTimer.Stop()

        select {
        case <-commandContext.Done():
        case <-causeGraceTimer.C:
            return roundTripErr
        }
    }

    if ErrCallTimeout != context.Cause(commandContext) {
        return roundTripErr
    }

    return exception.NewError(
        "cache round trip was cut by BackendConfig.CallTimeout",
        exceptioncontract.Context{
            "field":       "BackendConfig.CallTimeout",
            "callTimeout": instance.callTimeout.String(),
            "operation":   operation,
        },
        ErrCallTimeout,
    )
}

func (instance *Backend) walkRoundTrip(ctx context.Context, operation string) roundTripContext {
    return func() (context.Context, context.CancelFunc, func(error) error) {
        commandContext, cancel := instance.commandContext(ctx)

        return commandContext, cancel, func(roundTripErr error) error {
            return instance.roundTripError(commandContext, operation, roundTripErr)
        }
    }
}

func (instance *Backend) refuseBeforeArguments(runtimeInstance containercontract.Runtime, door string) error {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance, door); nil != nilErr {
        return nilErr
    }

    return instance.refuseWhenClosed()
}

func (instance *Backend) refuseWhenClosed() error {
    if true == instance.closed.Load() {
        return exception.NewError(
            "cache backend is closed",
            nil,
            nil,
        )
    }

    return nil
}

func (instance *Backend) Get(runtimeInstance containercontract.Runtime, key string) ([]byte, bool, error) {
    if refusalErr := instance.refuseBeforeArguments(runtimeInstance, "cache.Backend.Get"); nil != refusalErr {
        return nil, false, refusalErr
    }

    ctx := runtimeInstance.Context()

    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return nil, false, normalizeErr
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return nil, false, doneErr
    }

    commandContext, cancel := instance.commandContext(ctx)
    defer cancel()

    response := instance.client.Do(
        commandContext,
        instance.client.B().Get().Key(normalizedKey).Build(),
    )
    if err := response.Error(); nil != err {
        if true == rueidis.IsRedisNil(err) {
            return nil, false, nil
        }

        return nil, false, instance.roundTripError(commandContext, "Get", err)
    }

    payload, err := response.AsBytes()
    if nil != err {
        return nil, false, err
    }

    return payload, true, nil
}

func (instance *Backend) Set(runtimeInstance containercontract.Runtime, key string, payload []byte, ttl bound.Duration) error {
    if refusalErr := instance.refuseBeforeArguments(runtimeInstance, "cache.Backend.Set"); nil != refusalErr {
        return refusalErr
    }

    ctx := runtimeInstance.Context()

    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return normalizeErr
    }

    expiry, ttlErr := cachecontract.ResolveTtl(ttl)
    if nil != ttlErr {
        return ttlErr
    }

    var command rueidis.Completed
    if 0 < expiry {
        command = instance.client.B().Set().Key(normalizedKey).Value(rueidis.BinaryString(payload)).Px(expiry).Build()
    } else {
        command = instance.client.B().Set().Key(normalizedKey).Value(rueidis.BinaryString(payload)).Build()
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    commandContext, cancel := instance.commandContext(ctx)
    defer cancel()

    return instance.roundTripError(commandContext, "Set", instance.client.Do(
        commandContext,
        command,
    ).Error())
}

func (instance *Backend) Delete(runtimeInstance containercontract.Runtime, key string) error {
    if refusalErr := instance.refuseBeforeArguments(runtimeInstance, "cache.Backend.Delete"); nil != refusalErr {
        return refusalErr
    }

    ctx := runtimeInstance.Context()

    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return normalizeErr
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    commandContext, cancel := instance.commandContext(ctx)
    defer cancel()

    return instance.roundTripError(commandContext, "Delete", instance.client.Do(
        commandContext,
        instance.client.B().Del().Key(normalizedKey).Build(),
    ).Error())
}

func (instance *Backend) Has(runtimeInstance containercontract.Runtime, key string) (bool, error) {
    if refusalErr := instance.refuseBeforeArguments(runtimeInstance, "cache.Backend.Has"); nil != refusalErr {
        return false, refusalErr
    }

    ctx := runtimeInstance.Context()

    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return false, normalizeErr
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return false, doneErr
    }

    commandContext, cancel := instance.commandContext(ctx)
    defer cancel()

    response := instance.client.Do(
        commandContext,
        instance.client.B().Exists().Key(normalizedKey).Build(),
    )
    if err := response.Error(); nil != err {
        return false, instance.roundTripError(commandContext, "Has", err)
    }

    count, err := response.AsInt64()
    if nil != err {
        return false, err
    }

    return 0 != count, nil
}

func (instance *Backend) Clear(runtimeInstance containercontract.Runtime) error {
    if refusalErr := instance.refuseBeforeArguments(runtimeInstance, "cache.Backend.Clear"); nil != refusalErr {
        return refusalErr
    }

    ctx := runtimeInstance.Context()

    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    return instance.deleteMatching(instance.walkRoundTrip(ctx, "Clear"), escapeRedisGlobMeta(instance.prefix)+"*")
}

func (instance *Backend) deleteMatching(roundTrip roundTripContext, pattern string) error {
    keys, scanErr := instance.scanKeys(roundTrip, pattern)
    if nil != scanErr {
        return scanErr
    }

    if 0 == len(keys) {
        return nil
    }

    return instance.deleteKeysInBatches(roundTrip, keys)
}

/* ClearByPrefix refuses the empty prefix; Clear is the door for the whole namespace. */
func (instance *Backend) ClearByPrefix(runtimeInstance containercontract.Runtime, prefix string) error {
    if refusalErr := instance.refuseBeforeArguments(runtimeInstance, "cache.Backend.ClearByPrefix"); nil != refusalErr {
        return refusalErr
    }

    ctx := runtimeInstance.Context()

    normalizedPrefix, normalizeErr := instance.normalizeKey(prefix)
    if nil != normalizeErr {
        return normalizeErr
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    return instance.deleteMatching(instance.walkRoundTrip(ctx, "ClearByPrefix"), escapeRedisGlobMeta(normalizedPrefix)+"*")
}

func (instance *Backend) Many(runtimeInstance containercontract.Runtime, keys []string) (map[string][]byte, error) {
    if refusalErr := instance.refuseBeforeArguments(runtimeInstance, "cache.Backend.Many"); nil != refusalErr {
        return nil, refusalErr
    }

    ctx := runtimeInstance.Context()

    result := make(map[string][]byte, len(keys))

    normalizedKeys := make([]string, 0, len(keys))
    for _, key := range keys {
        normalizedKey, normalizeErr := instance.normalizeKey(key)
        if nil != normalizeErr {
            return nil, normalizeErr
        }

        normalizedKeys = append(normalizedKeys, normalizedKey)
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return nil, doneErr
    }

    if 0 == len(normalizedKeys) {
        return result, nil
    }

    commandContext, cancel := instance.commandContext(ctx)
    defer cancel()

    values, err := rueidis.MGet(
        instance.client,
        commandContext,
        normalizedKeys,
    )
    if nil != err {
        return nil, instance.roundTripError(commandContext, "Many", err)
    }

    for fullKey, message := range values {
        if true == message.IsNil() {
            continue
        }

        originalKey := instance.stripPrefix(fullKey)

        payload, payloadErr := message.AsBytes()
        if nil != payloadErr {
            return nil, exception.NewError(
                "cache entry could not be read as a payload",
                exceptioncontract.Context{
                    "key": originalKey,
                },
                payloadErr,
            )
        }

        result[originalKey] = payload
    }

    return result, nil
}

func (instance *Backend) SetMultiple(runtimeInstance containercontract.Runtime, items map[string][]byte, ttl bound.Duration) error {
    if refusalErr := instance.refuseBeforeArguments(runtimeInstance, "cache.Backend.SetMultiple"); nil != refusalErr {
        return refusalErr
    }

    ctx := runtimeInstance.Context()

    /* the ttl is judged before the keys, as the in-memory backend judges it */
    expiry, ttlErr := cachecontract.ResolveTtl(ttl)
    if nil != ttlErr {
        return ttlErr
    }

    /* sorted for a deterministic refusal; a failing response is identified by position only */
    sortedKeys := make([]string, 0, len(items))
    for key := range items {
        sortedKeys = append(sortedKeys, key)
    }
    sort.Strings(sortedKeys)

    commandKeys := make([]string, 0, len(items))
    commands := make(rueidis.Commands, 0, len(items))
    for _, key := range sortedKeys {
        payload := items[key]

        normalizedKey, normalizeErr := instance.normalizeKey(key)
        if nil != normalizeErr {
            return normalizeErr
        }

        var command rueidis.Completed
        if 0 < expiry {
            command = instance.client.B().Set().Key(normalizedKey).Value(rueidis.BinaryString(payload)).Px(expiry).Build()
        } else {
            command = instance.client.B().Set().Key(normalizedKey).Value(rueidis.BinaryString(payload)).Build()
        }

        commandKeys = append(commandKeys, key)
        commands = append(commands, command)
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    if 0 == len(commands) {
        return nil
    }

    commandContext, cancel := instance.commandContext(ctx)
    defer cancel()

    /* every failure is collected first, so the report is deterministic */
    setErrors := make(map[string]error, len(commandKeys))
    for index, response := range instance.client.DoMulti(commandContext, commands...) {
        if err := response.Error(); nil != err {
            setErrors[commandKeys[index]] = instance.roundTripError(commandContext, "SetMultiple", err)
        }
    }

    return instance.firstSetFailure(setErrors, len(commandKeys))
}

func (instance *Backend) firstSetFailure(setErrors map[string]error, requestedCount int) error {
    if 0 == len(setErrors) {
        return nil
    }

    failedKeys := make([]string, 0, len(setErrors))
    for key := range setErrors {
        failedKeys = append(failedKeys, key)
    }

    sort.Strings(failedKeys)

    return exception.NewError(
        "cache set failed",
        exceptioncontract.Context{
            "key":            failedKeys[0],
            "failedKeyCount": len(failedKeys),
            "requestedCount": requestedCount,
        },
        setErrors[failedKeys[0]],
    )
}

func (instance *Backend) DeleteMultiple(runtimeInstance containercontract.Runtime, keys []string) error {
    if refusalErr := instance.refuseBeforeArguments(runtimeInstance, "cache.Backend.DeleteMultiple"); nil != refusalErr {
        return refusalErr
    }

    ctx := runtimeInstance.Context()

    normalizedKeys := make([]string, 0, len(keys))
    for _, key := range keys {
        normalizedKey, normalizeErr := instance.normalizeKey(key)
        if nil != normalizeErr {
            return normalizeErr
        }

        normalizedKeys = append(normalizedKeys, normalizedKey)
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    if 0 == len(normalizedKeys) {
        return nil
    }

    commandContext, cancel := instance.commandContext(ctx)
    defer cancel()

    return instance.firstDeleteFailure(instance.namedDeleteErrors(commandContext, "DeleteMultiple", rueidis.MDel(
        instance.client,
        commandContext,
        normalizedKeys,
    )))
}

/* counterScript writes the count and its expiry atomically, answering text: Lua doubles round past 2^53. */
var counterScript = rueidis.NewLuaScript(`redis.call("incrby", KEYS[1], ARGV[1])
local ttl = tonumber(ARGV[2])
if ttl > 0 and (ARGV[3] == "1" or redis.call("pttl", KEYS[1]) < 0) then
    redis.call("pexpire", KEYS[1], ttl)
elseif ttl == 0 and ARGV[3] == "1" then
    redis.call("persist", KEYS[1])
end
return redis.call("get", KEYS[1])`)

func (instance *Backend) Increment(runtimeInstance containercontract.Runtime, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    if refusalErr := instance.refuseBeforeArguments(runtimeInstance, "cache.Backend.Increment"); nil != refusalErr {
        return 0, refusalErr
    }

    ctx := runtimeInstance.Context()

    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return 0, normalizeErr
    }

    return instance.addToCounter(ctx, "Increment", key, normalizedKey, delta, options)
}

func (instance *Backend) addToCounter(ctx context.Context, operation string, key string, normalizedKey string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    expiry, ttlErr := cachecontract.ResolveTtl(options.Ttl)
    if nil != ttlErr {
        return 0, ttlErr
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return 0, doneErr
    }

    refresh := "0"
    if true == options.RefreshTtl {
        refresh = "1"
    }

    commandContext, cancel := instance.commandContext(ctx)
    defer cancel()

    value, err := counterScript.Exec(
        commandContext,
        instance.client,
        []string{normalizedKey},
        []string{strconv.FormatInt(delta, 10), strconv.FormatInt(expiry.Milliseconds(), 10), refresh},
    ).AsInt64()
    if nil != err {
        /* named before counterError, whose message would bury the call timeout */
        if namedErr := instance.roundTripError(commandContext, operation, err); true == errors.Is(namedErr, ErrCallTimeout) {
            return 0, namedErr
        }

        return 0, counterError(key, err)
    }

    return value, nil
}

func (instance *Backend) Decrement(runtimeInstance containercontract.Runtime, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    if refusalErr := instance.refuseBeforeArguments(runtimeInstance, "cache.Backend.Decrement"); nil != refusalErr {
        return 0, refusalErr
    }

    ctx := runtimeInstance.Context()

    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return 0, normalizeErr
    }

    if math.MinInt64 == delta {
        return 0, exception.NewError(
            "delta overflows int64 when negated",
            exceptioncontract.Context{
                "key": key,
            },
            nil,
        )
    }

    return instance.addToCounter(ctx, "Decrement", key, normalizedKey, -delta, options)
}

/* CloseWithContext refuses every later operation; the shared client is not closed here. */
func (instance *Backend) CloseWithContext(closeContext context.Context) error {
    instance.closed.Store(true)

    return nil
}

func counterError(key string, causeErr error) error {
    return exception.NewError(
        counterErrorMessage(causeErr),
        exceptioncontract.Context{
            "key": key,
        },
        causeErr,
    )
}

/* counterRefusalMessages maps redis's wording onto the in-memory backend's messages, matched on a fragment. */
var counterRefusalMessages = []struct {
    fragment string
    message  string
}{
    {fragment: "increment or decrement would overflow", message: "cache increment overflow"},
    {fragment: "not an integer or out of range", message: "cache value is not a valid int64"},
}

func counterErrorMessage(causeErr error) string {
    if nil == causeErr {
        return counterStoreFailureMessage
    }

    causeText := strings.ToLower(causeErr.Error())
    for _, refusal := range counterRefusalMessages {
        if true == strings.Contains(causeText, refusal.fragment) {
            return refusal.message
        }
    }

    return counterStoreFailureMessage
}

const counterStoreFailureMessage = "cache counter operation failed"

func (instance *Backend) normalizeKey(key string) (string, error) {
    if "" == key {
        return "", exception.NewError(
            "cache key is empty",
            exceptioncontract.Context{
                "key": renderedCacheKey(key),
            },
            nil,
        )
    }

    if true == strings.Contains(key, " ") {
        return "", exception.NewError(
            "cache key contains spaces",
            exceptioncontract.Context{
                "key": renderedCacheKey(key),
            },
            nil,
        )
    }

    if true == strings.Contains(key, "\n") {
        return "", exception.NewError(
            "cache key contains newlines",
            exceptioncontract.Context{
                "key": renderedCacheKey(key),
            },
            nil,
        )
    }

    if instance.maxKeyLength < len(key) {
        return "", exception.NewError(
            "cache key is too long",
            exceptioncontract.Context{
                "key":          renderedCacheKey(key),
                "maxKeyLength": instance.maxKeyLength,
                "keyLength":    len(key),
            },
            nil,
        )
    }

    return instance.prefix + key, nil
}

func (instance *Backend) stripPrefix(fullKey string) string {
    return strings.TrimPrefix(fullKey, instance.prefix)
}

func escapeRedisGlobMeta(value string) string {
    var builder strings.Builder
    for index := 0; index < len(value); index++ {
        switch value[index] {
        case '*', '?', '[', ']', '\\':
            builder.WriteByte('\\')
        }
        builder.WriteByte(value[index])
    }

    return builder.String()
}

/* every node is scanned: on a cluster one node holds only its slice of the keyspace */
func (instance *Backend) scanKeys(roundTrip roundTripContext, pattern string) ([]string, error) {
    keys := make([]string, 0)

    nodes := instance.client.Nodes()
    for _, node := range nodes {
        nodeKeys, nodeErr := scanNodeKeys(roundTrip, node, pattern, instance.scanCount)
        if nil != nodeErr {
            return nil, nodeErr
        }

        keys = append(keys, nodeKeys...)
    }

    return keys, nil
}

func scanNodeKeys(roundTrip roundTripContext, node rueidis.Client, pattern string, scanCount int) ([]string, error) {
    cursor := uint64(0)
    keys := make([]string, 0)

    for {
        ctx, cancel, name := roundTrip()
        response := node.Do(
            ctx,
            node.B().Scan().Cursor(cursor).Match(pattern).Count(int64(scanCount)).Build(),
        )
        responseErr := name(response.Error())
        cancel()

        if nil != responseErr {
            return nil, responseErr
        }

        array, arrayErr := response.ToArray()
        if nil != arrayErr {
            return nil, arrayErr
        }

        if 2 != len(array) {
            return nil, exception.NewError(
                "unexpected scan response length",
                exceptioncontract.Context{
                    "length": len(array),
                },
                nil,
            )
        }

        cursorString, cursorErr := array[0].ToString()
        if nil != cursorErr {
            return nil, cursorErr
        }

        parsedCursor, parseErr := strconv.ParseUint(cursorString, 10, 64)
        if nil != parseErr {
            return nil, parseErr
        }
        cursor = parsedCursor

        keysArray, keysArrayErr := array[1].ToArray()
        if nil != keysArrayErr {
            return nil, keysArrayErr
        }

        for _, keyMessage := range keysArray {
            keyString, keyErr := keyMessage.ToString()
            if nil != keyErr {
                return nil, keyErr
            }
            if "" == keyString {
                continue
            }

            keys = append(keys, keyString)
        }

        if 0 == cursor {
            break
        }
    }

    return keys, nil
}

func (instance *Backend) deleteKeysInBatches(roundTrip roundTripContext, keys []string) error {
    if 0 == len(keys) {
        return nil
    }

    for startIndex := 0; startIndex < len(keys); startIndex += instance.deleteBatch {
        endIndex := startIndex + instance.deleteBatch
        if endIndex > len(keys) {
            endIndex = len(keys)
        }

        batch := keys[startIndex:endIndex]

        ctx, cancel, name := roundTrip()
        batchErr := instance.firstDeleteFailure(namedEachError(name, rueidis.MDel(instance.client, ctx, batch)))
        cancel()

        if nil != batchErr {
            /* a multi-batch wipe failing part-way reports the operation's extent, as earlier batches are gone */
            if len(keys) <= instance.deleteBatch {
                return batchErr
            }

            return exception.NewError(
                "cache clear failed part-way through its batches",
                exceptioncontract.Context{
                    "deletedBeforeFailure": startIndex,
                    "requestedTotal":       len(keys),
                },
                batchErr,
            )
        }
    }

    return nil
}

func (instance *Backend) namedDeleteErrors(commandContext context.Context, operation string, deleteErrors map[string]error) map[string]error {
    return namedEachError(func(roundTripErr error) error {
        return instance.roundTripError(commandContext, operation, roundTripErr)
    }, deleteErrors)
}

func namedEachError(name func(error) error, deleteErrors map[string]error) map[string]error {
    for key, deleteErr := range deleteErrors {
        if nil != deleteErr {
            deleteErrors[key] = name(deleteErr)
        }
    }

    return deleteErrors
}

func (instance *Backend) firstDeleteFailure(deleteErrors map[string]error) error {
    failedKeys := make([]string, 0, len(deleteErrors))
    for key, deleteErr := range deleteErrors {
        if nil != deleteErr {
            failedKeys = append(failedKeys, key)
        }
    }

    if 0 == len(failedKeys) {
        return nil
    }

    sort.Strings(failedKeys)

    return exception.NewError(
        "cache delete failed",
        exceptioncontract.Context{
            "key":            instance.stripPrefix(failedKeys[0]),
            "failedKeyCount": len(failedKeys),
            "requestedCount": len(deleteErrors),
        },
        deleteErrors[failedKeys[0]],
    )
}

var _ cachecontract.Backend = (*Backend)(nil)

func renderedCacheKey(key string) string {
    const renderedCacheKeyLimit = 128

    if renderedCacheKeyLimit >= len(key) {
        return key
    }

    cut := renderedCacheKeyLimit
    for 0 < cut && false == utf8.RuneStart(key[cut]) {
        cut = cut - 1
    }

    return key[:cut] + "...(truncated)"
}
