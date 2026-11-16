package cache

import (
    "context"
    "math"
    "sort"
    "strconv"
    "strings"
    "sync/atomic"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/redis/rueidis"
)

const defaultBackendPrefix = "melody:cache:"

/* BackendConfig declares the backend over one key prefix; its zero value is the default backend. */
type BackendConfig struct {
    /* Prefix is the whole of the backend's isolation: every key it writes carries it, and Clear deletes everything under it. Empty is melody:cache:, the same string in every melody application, so two applications on one redis with it share a namespace, a foreign json document decoding as a hit and a Clear from either emptying the other; the client's SelectDatabase defaults to 0, so databases do not separate them either. Give each application and environment sharing a store its own prefix. */
    Prefix string

    /* ScanCount is the COUNT hint of the SCAN walk Clear and ClearByPrefix run: 1000 by default, at least 1, unlimited refused. */
    ScanCount bound.Count

    /* DeleteBatch bounds how many keys one DEL carries: 500 by default, at least 1, unlimited refused, since the batch bounds how long the server is held. */
    DeleteBatch bound.Count

    /* MaxKeyLength bounds the key the caller hands in, before the prefix is put in front of it, since the prefix is the operator's and must not change which keys are refused: 1024 by default, at least 1, unlimited refused. */
    MaxKeyLength bound.Count

    /* CommandTimeout bounds every round trip on top of the caller's context, each SCAN and DEL of a Clear on its own while the caller's deadline bounds the walk, so a store that accepts connections but stops answering cannot hold a request-path read, which the client retries for as long as its context allows: 1s by default, at least 1ms, unlimited refused. */
    CommandTimeout bound.Duration
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

var backendCommandTimeoutRule = bound.Rule[time.Duration]{
    Name:    "BackendConfig.CommandTimeout",
    Default: time.Second,
    Minimum: bound.TimeoutMinimum,
}

type resolvedBackend struct {
    prefix         string
    scanCount      int
    deleteBatch    int
    maxKeyLength   int
    commandTimeout time.Duration
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

    commandTimeout, commandTimeoutErr := bound.Resolve(config.CommandTimeout, backendCommandTimeoutRule)
    if nil != commandTimeoutErr {
        return resolvedBackend{}, commandTimeoutErr
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
        commandTimeout: commandTimeout.Value,
    }, nil
}

/* NewBackend builds the redis-backed cache; a nil client or a limit outside its rule is the error, naming the field. */
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
        commandTimeout: resolved.commandTimeout,
    }
}

type Backend struct {
    client         rueidis.Client
    prefix         string
    scanCount      int
    deleteBatch    int
    maxKeyLength   int
    commandTimeout time.Duration
    closed         atomic.Bool
}

/* roundTripContext answers the context one store round trip of a walk runs on. */
type roundTripContext func() (context.Context, context.CancelFunc)

/* commandContext bounds one round trip by the command timeout on top of the caller's context, the earlier deadline winning. */
func (instance *Backend) commandContext(ctx context.Context) (context.Context, context.CancelFunc) {
    return context.WithTimeout(ctx, instance.commandTimeout)
}

/* walkRoundTrip gives every round trip of a walk its own command timeout under the caller's one context, whose deadline bounds the walk as a whole. */
func (instance *Backend) walkRoundTrip(ctx context.Context) roundTripContext {
    return func() (context.Context, context.CancelFunc) {
        return instance.commandContext(ctx)
    }
}

func (instance *Backend) refuseBeforeArguments(ctx context.Context) error {
    if nilErr := cachecontract.RefuseNilContext(ctx); nil != nilErr {
        return nilErr
    }

    return instance.refuseWhenClosed()
}

/* refuseWhenClosed answers the refusal every operation gives after CloseWithContext, as the in-memory backend behind the same contract does, so a teardown-ordering bug surfaces at once. */
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

func (instance *Backend) Get(ctx context.Context, key string) ([]byte, bool, error) {
    if refusalErr := instance.refuseBeforeArguments(ctx); nil != refusalErr {
        return nil, false, refusalErr
    }

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

        return nil, false, err
    }

    payload, err := response.AsBytes()
    if nil != err {
        return nil, false, err
    }

    return payload, true, nil
}

func (instance *Backend) Set(ctx context.Context, key string, payload []byte, ttl bound.Duration) error {
    if refusalErr := instance.refuseBeforeArguments(ctx); nil != refusalErr {
        return refusalErr
    }

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

    return instance.client.Do(
        commandContext,
        command,
    ).Error()
}

func (instance *Backend) Delete(ctx context.Context, key string) error {
    if refusalErr := instance.refuseBeforeArguments(ctx); nil != refusalErr {
        return refusalErr
    }

    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return normalizeErr
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    commandContext, cancel := instance.commandContext(ctx)
    defer cancel()

    return instance.client.Do(
        commandContext,
        instance.client.B().Del().Key(normalizedKey).Build(),
    ).Error()
}

func (instance *Backend) Has(ctx context.Context, key string) (bool, error) {
    if refusalErr := instance.refuseBeforeArguments(ctx); nil != refusalErr {
        return false, refusalErr
    }

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
        return false, err
    }

    count, err := response.AsInt64()
    if nil != err {
        return false, err
    }

    return 0 != count, nil
}

func (instance *Backend) Clear(ctx context.Context) error {
    if refusalErr := instance.refuseBeforeArguments(ctx); nil != refusalErr {
        return refusalErr
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    return instance.deleteMatching(instance.walkRoundTrip(ctx), escapeRedisGlobMeta(instance.prefix)+"*")
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

/* ClearByPrefix refuses the empty prefix as every other operation refuses the empty key: a run-time prefix that comes out empty, "tenant:" plus an unresolved id, would select the whole namespace. Clear is the door for the whole namespace. */
func (instance *Backend) ClearByPrefix(ctx context.Context, prefix string) error {
    if refusalErr := instance.refuseBeforeArguments(ctx); nil != refusalErr {
        return refusalErr
    }

    normalizedPrefix, normalizeErr := instance.normalizeKey(prefix)
    if nil != normalizeErr {
        return normalizeErr
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    return instance.deleteMatching(instance.walkRoundTrip(ctx), escapeRedisGlobMeta(normalizedPrefix)+"*")
}

func (instance *Backend) Many(ctx context.Context, keys []string) (map[string][]byte, error) {
    if refusalErr := instance.refuseBeforeArguments(ctx); nil != refusalErr {
        return nil, refusalErr
    }

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
        return nil, err
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

func (instance *Backend) SetMultiple(ctx context.Context, items map[string][]byte, ttl bound.Duration) error {
    if refusalErr := instance.refuseBeforeArguments(ctx); nil != refusalErr {
        return refusalErr
    }

    /* the ttl is judged before the keys, as the in-memory backend judges it, so an invalid ttl is refused whether or not the batch is empty */
    expiry, ttlErr := cachecontract.ResolveTtl(ttl)
    if nil != ttlErr {
        return ttlErr
    }

    /* the batch is walked over sorted keys, so the validation refusal names the same first key for the same batch on every call; the keys travel beside the commands because a failing response is identified by position only */
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

    /* every failing response is collected before one is reported, as the delete sibling does, so the report is deterministic and shows how much of the batch failed */
    setErrors := make(map[string]error, len(commandKeys))
    for index, response := range instance.client.DoMulti(commandContext, commands...) {
        if err := response.Error(); nil != err {
            setErrors[commandKeys[index]] = err
        }
    }

    return instance.firstSetFailure(setErrors, len(commandKeys))
}

/* firstSetFailure names the key that failed, chosen by sorting rather than by map iteration — the firstDeleteFailure convention — so two identical failures report identically, and the counts tell the caller how much of the batch they describe. */
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

func (instance *Backend) DeleteMultiple(ctx context.Context, keys []string) error {
    if refusalErr := instance.refuseBeforeArguments(ctx); nil != refusalErr {
        return refusalErr
    }

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

    return instance.firstDeleteFailure(rueidis.MDel(
        instance.client,
        commandContext,
        normalizedKeys,
    ))
}

func (instance *Backend) Increment(ctx context.Context, key string, delta int64) (int64, error) {
    if refusalErr := instance.refuseBeforeArguments(ctx); nil != refusalErr {
        return 0, refusalErr
    }

    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return 0, normalizeErr
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return 0, doneErr
    }

    commandContext, cancel := instance.commandContext(ctx)
    defer cancel()

    response := instance.client.Do(
        commandContext,
        instance.client.B().Incrby().Key(normalizedKey).Increment(delta).Build(),
    )
    if err := response.Error(); nil != err {
        return 0, counterError(key, err)
    }

    value, err := response.AsInt64()
    if nil != err {
        return 0, counterError(key, err)
    }

    return value, nil
}

func (instance *Backend) Decrement(ctx context.Context, key string, delta int64) (int64, error) {
    if refusalErr := instance.refuseBeforeArguments(ctx); nil != refusalErr {
        return 0, refusalErr
    }

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

    if doneErr := ctx.Err(); nil != doneErr {
        return 0, doneErr
    }

    commandContext, cancel := instance.commandContext(ctx)
    defer cancel()

    response := instance.client.Do(
        commandContext,
        instance.client.B().Decrby().Key(normalizedKey).Decrement(delta).Build(),
    )
    if err := response.Error(); nil != err {
        return 0, counterError(key, err)
    }

    value, err := response.AsInt64()
    if nil != err {
        return 0, counterError(key, err)
    }

    return value, nil
}

/* CloseWithContext marks the backend closed and refuses every later operation, as the in-memory backend does; it waits for nothing. The shared client belongs to the composition root and is not closed here; the parent package's Connection wrapper is what the container's teardown closes. */
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

/* counterRefusalMessages maps redis's wording onto the message the in-memory backend answers for the same mistake, matched on a fragment because redis prefixes its errors by server version and by whether the command went through a script. The order is load-bearing: "decrement would overflow", a DECRBY that cannot be negated, is a substring of "increment or decrement would overflow", the int64 ceiling, so it is checked second. */
var counterRefusalMessages = []struct {
    fragment string
    message  string
}{
    {fragment: "increment or decrement would overflow", message: "cache increment overflow"},
    {fragment: "decrement would overflow", message: "delta overflows int64 when negated"},
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

/* normalizeKey names the offending key in every refusal: a batch call validates keys the caller handed in as a set, and a refusal that does not say which of them is malformed leaves nothing to act on. */
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

/* scanKeys walks every node of the client rather than the one connection a single Do reaches: against a cluster a scan on one node reports only that node's slice of the keyspace, so a prefix wipe would leave the rest behind and report success. Nodes answers a single-node client with itself. */
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
        ctx, cancel := roundTrip()
        response := node.Do(
            ctx,
            node.B().Scan().Cursor(cursor).Match(pattern).Count(int64(scanCount)).Build(),
        )
        cancel()

        if err := response.Error(); nil != err {
            return nil, err
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

        ctx, cancel := roundTrip()
        batchErr := instance.firstDeleteFailure(rueidis.MDel(instance.client, ctx, batch))
        cancel()

        if nil != batchErr {
            /* a multi-batch wipe that fails part-way is reported at the operation's own extent, since the earlier batches are irreversibly gone and batch-only counts could not say how much was destroyed; a single-batch operation keeps the batch report, which already is the operation's */
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

/* firstDeleteFailure names the key that failed, chosen by sorting rather than by map iteration, so two identical failures report identically, and the counts tell the caller how much of the batch they describe. */
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

/* renderedCacheKey bounds a key a refusal carries in its context to its first 128 bytes, cut on a rune boundary and closed by a marker, so a key of a megabyte does not put a megabyte into the record; the refusal of a key past the length limit carries its whole length beside it */
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
