package cache

import (
    "context"
    "sort"
    "strconv"
    "strings"
    "sync/atomic"
    "time"

    cachecontract "github.com/precision-soft/melody/v2/cache/contract"
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/redis/rueidis"
)

const (
    rueidisBackendDefaultScanCount    = 1000
    rueidisBackendDefaultDeleteBatch  = 500
    rueidisBackendDefaultMaxKeyLength = 1024
)

/* NewBackend builds the redis-backed cache over one key prefix, which is the whole of this backend's isolation: every key it writes carries it, and Clear deletes everything under it. An empty prefix takes the shipped default, the same string in every melody application, so two applications on one redis with it share a namespace, a foreign json document decoding as a hit and a Clear from either emptying the other; the client's SelectDb defaults to 0, so databases do not separate them either. Give each application and environment sharing a store its own prefix. */
func NewBackend(
    client rueidis.Client,
    ctx context.Context,
    prefix string,
    scanCount int,
    deleteBatch int,
) (*Backend, error) {
    return NewBackendWithCommandTimeout(client, ctx, prefix, scanCount, deleteBatch, 0)
}

/* NewBackendWithCommandTimeout additionally bounds every operation dispatched without a caller context, so the ctx-less contract methods do not hang against a store that accepts connections but stops answering; a non-positive value reads as unbounded, the behaviour of NewBackend. */
func NewBackendWithCommandTimeout(
    client rueidis.Client,
    ctx context.Context,
    prefix string,
    scanCount int,
    deleteBatch int,
    commandTimeout time.Duration,
) (*Backend, error) {
    if nil == client {
        return nil, exception.NewError(
            "redis client is nil",
            nil,
            nil,
        )
    }

    if nil == ctx {
        ctx = context.Background()
    }

    normalizedPrefix := prefix
    if "" == normalizedPrefix {
        normalizedPrefix = "melody:cache:"
    }

    normalizedScanCount := scanCount
    if 0 >= normalizedScanCount {
        normalizedScanCount = rueidisBackendDefaultScanCount
    }

    normalizedDeleteBatch := deleteBatch
    if 0 >= normalizedDeleteBatch {
        normalizedDeleteBatch = rueidisBackendDefaultDeleteBatch
    }

    normalizedCommandTimeout := commandTimeout
    if 0 >= normalizedCommandTimeout {
        normalizedCommandTimeout = 0
    }

    return &Backend{
        client:         client,
        ctx:            ctx,
        prefix:         normalizedPrefix,
        scanCount:      normalizedScanCount,
        deleteBatch:    normalizedDeleteBatch,
        commandTimeout: normalizedCommandTimeout,
    }, nil
}

type Backend struct {
    client         rueidis.Client
    ctx            context.Context
    prefix         string
    scanCount      int
    deleteBatch    int
    commandTimeout time.Duration
    closed         atomic.Bool
    /* ownerClosed is the closed flag of the backend this handle was derived from, nil on a backend built directly: a handle WithContext mints per request lives exactly as long as its owner, so the owner's Close must reach it. A sibling backend over the same client stays independent; the client belongs to whoever built it. */
    ownerClosed *atomic.Bool
}

/* operationContext bounds an operation dispatched without a caller context: the constructor's context plus the command timeout when one is configured, the constructor's context alone otherwise. */
func (instance *Backend) operationContext() (context.Context, context.CancelFunc) {
    if 0 < instance.commandTimeout {
        return context.WithTimeout(instance.ctx, instance.commandTimeout)
    }

    return instance.ctx, func() {}
}

/* refuseWhenClosed answers the refusal every operation gives after Close, as the in-memory backend behind the same contract does, so a teardown-ordering bug surfaces at once; a derived handle reads its owner's flag too. */
func (instance *Backend) refuseWhenClosed() error {
    if true == instance.closed.Load() {
        return exception.NewError(
            "cache backend is closed",
            nil,
            nil,
        )
    }

    if nil != instance.ownerClosed && true == instance.ownerClosed.Load() {
        return exception.NewError(
            "cache backend is closed",
            nil,
            nil,
        )
    }

    return nil
}

func (instance *Backend) GetCtx(ctx context.Context, key string) ([]byte, bool, error) {
    if closedErr := instance.refuseWhenClosed(); nil != closedErr {
        return nil, false, closedErr
    }

    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return nil, false, normalizeErr
    }

    response := instance.client.Do(
        ctx,
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

/* Deprecated: prefer GetCtx, which takes ctx per call. */
func (instance *Backend) Get(key string) ([]byte, bool, error) {
    ctx, cancel := instance.operationContext()
    defer cancel()

    return instance.GetCtx(ctx, key)
}

func (instance *Backend) SetCtx(ctx context.Context, key string, payload []byte, ttl time.Duration) error {
    if closedErr := instance.refuseWhenClosed(); nil != closedErr {
        return closedErr
    }

    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return normalizeErr
    }

    if 0 > ttl {
        return negativeTtlError(ttl)
    }

    var command rueidis.Completed
    if 0 < ttl {
        command = instance.client.B().Set().Key(normalizedKey).Value(rueidis.BinaryString(payload)).Px(floorPositiveExpiry(ttl)).Build()
    } else {
        command = instance.client.B().Set().Key(normalizedKey).Value(rueidis.BinaryString(payload)).Build()
    }

    return instance.client.Do(
        ctx,
        command,
    ).Error()
}

/* Deprecated: prefer SetCtx, which takes ctx per call. */
func (instance *Backend) Set(key string, payload []byte, ttl time.Duration) error {
    ctx, cancel := instance.operationContext()
    defer cancel()

    return instance.SetCtx(ctx, key, payload, ttl)
}

func (instance *Backend) DeleteCtx(ctx context.Context, key string) error {
    if closedErr := instance.refuseWhenClosed(); nil != closedErr {
        return closedErr
    }

    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return normalizeErr
    }

    return instance.client.Do(
        ctx,
        instance.client.B().Del().Key(normalizedKey).Build(),
    ).Error()
}

/* Deprecated: prefer DeleteCtx, which takes ctx per call. */
func (instance *Backend) Delete(key string) error {
    ctx, cancel := instance.operationContext()
    defer cancel()

    return instance.DeleteCtx(ctx, key)
}

func (instance *Backend) HasCtx(ctx context.Context, key string) (bool, error) {
    if closedErr := instance.refuseWhenClosed(); nil != closedErr {
        return false, closedErr
    }

    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return false, normalizeErr
    }

    response := instance.client.Do(
        ctx,
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

/* Deprecated: prefer HasCtx, which takes ctx per call. */
func (instance *Backend) Has(key string) (bool, error) {
    ctx, cancel := instance.operationContext()
    defer cancel()

    return instance.HasCtx(ctx, key)
}

func (instance *Backend) ClearCtx(ctx context.Context) error {
    if closedErr := instance.refuseWhenClosed(); nil != closedErr {
        return closedErr
    }

    pattern := escapeRedisGlobMeta(instance.prefix) + "*"
    keys, scanErr := instance.scanKeys(ctx, pattern)
    if nil != scanErr {
        return scanErr
    }

    if 0 == len(keys) {
        return nil
    }

    return instance.deleteKeysInBatches(ctx, keys)
}

/* Deprecated: prefer ClearCtx, which takes ctx per call. */
func (instance *Backend) Clear() error {
    ctx, cancel := instance.operationContext()
    defer cancel()

    return instance.ClearCtx(ctx)
}

/* ClearByPrefixCtx refuses the empty prefix as every other operation refuses the empty key: a run-time prefix that comes out empty, "tenant:" plus an unresolved id, would select the whole namespace. ClearCtx is the door for the whole namespace. */
func (instance *Backend) ClearByPrefixCtx(ctx context.Context, prefix string) error {
    if closedErr := instance.refuseWhenClosed(); nil != closedErr {
        return closedErr
    }

    normalizedPrefix, normalizeErr := instance.normalizeKey(prefix)
    if nil != normalizeErr {
        return normalizeErr
    }

    pattern := escapeRedisGlobMeta(normalizedPrefix) + "*"
    keys, scanErr := instance.scanKeys(ctx, pattern)
    if nil != scanErr {
        return scanErr
    }

    if 0 == len(keys) {
        return nil
    }

    return instance.deleteKeysInBatches(ctx, keys)
}

/* Deprecated: prefer ClearByPrefixCtx, which takes ctx per call. */
func (instance *Backend) ClearByPrefix(prefix string) error {
    ctx, cancel := instance.operationContext()
    defer cancel()

    return instance.ClearByPrefixCtx(ctx, prefix)
}

func (instance *Backend) ManyCtx(ctx context.Context, keys []string) (map[string][]byte, error) {
    if closedErr := instance.refuseWhenClosed(); nil != closedErr {
        return nil, closedErr
    }

    result := make(map[string][]byte, len(keys))
    if 0 == len(keys) {
        return result, nil
    }

    normalizedKeys := make([]string, 0, len(keys))
    for _, key := range keys {
        normalizedKey, normalizeErr := instance.normalizeKey(key)
        if nil != normalizeErr {
            return nil, normalizeErr
        }

        normalizedKeys = append(normalizedKeys, normalizedKey)
    }

    values, err := rueidis.MGet(
        instance.client,
        ctx,
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

/* Deprecated: prefer ManyCtx, which takes ctx per call. */
func (instance *Backend) Many(keys []string) (map[string][]byte, error) {
    ctx, cancel := instance.operationContext()
    defer cancel()

    return instance.ManyCtx(ctx, keys)
}

func (instance *Backend) SetMultipleCtx(ctx context.Context, items map[string][]byte, ttl time.Duration) error {
    if closedErr := instance.refuseWhenClosed(); nil != closedErr {
        return closedErr
    }

    /* the ttl is judged before the empty early-return, as the in-memory backend judges it, so an invalid ttl is refused whether or not the batch is empty */
    if 0 > ttl {
        return negativeTtlError(ttl)
    }

    if 0 == len(items) {
        return nil
    }

    /* the batch is walked over sorted keys, so the validation refusal names the same first key for the same batch on every call; the keys travel beside the commands because a failing response is identified by position only */
    sortedKeys := make([]string, 0, len(items))
    for key := range items {
        sortedKeys = append(sortedKeys, key)
    }
    sort.Strings(sortedKeys)

    commandKeys := make([]string, 0, len(items))
    cmds := make(rueidis.Commands, 0, len(items))
    for _, key := range sortedKeys {
        payload := items[key]

        normalizedKey, normalizeErr := instance.normalizeKey(key)
        if nil != normalizeErr {
            return normalizeErr
        }

        var command rueidis.Completed
        if 0 < ttl {
            command = instance.client.B().Set().Key(normalizedKey).Value(rueidis.BinaryString(payload)).Px(floorPositiveExpiry(ttl)).Build()
        } else {
            command = instance.client.B().Set().Key(normalizedKey).Value(rueidis.BinaryString(payload)).Build()
        }

        commandKeys = append(commandKeys, key)
        cmds = append(cmds, command)
    }

    /* every failing response is collected before one is reported, as the delete sibling does, so the report is deterministic and shows how much of the batch failed */
    setErrors := make(map[string]error, len(commandKeys))
    for index, response := range instance.client.DoMulti(ctx, cmds...) {
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

/* Deprecated: prefer SetMultipleCtx, which takes ctx per call. */
func (instance *Backend) SetMultiple(items map[string][]byte, ttl time.Duration) error {
    ctx, cancel := instance.operationContext()
    defer cancel()

    return instance.SetMultipleCtx(ctx, items, ttl)
}

func (instance *Backend) DeleteMultipleCtx(ctx context.Context, keys []string) error {
    if closedErr := instance.refuseWhenClosed(); nil != closedErr {
        return closedErr
    }

    if 0 == len(keys) {
        return nil
    }

    normalizedKeys := make([]string, 0, len(keys))
    for _, key := range keys {
        normalizedKey, normalizeErr := instance.normalizeKey(key)
        if nil != normalizeErr {
            return normalizeErr
        }

        normalizedKeys = append(normalizedKeys, normalizedKey)
    }

    return instance.firstDeleteFailure(rueidis.MDel(
        instance.client,
        ctx,
        normalizedKeys,
    ))
}

/* Deprecated: prefer DeleteMultipleCtx, which takes ctx per call. */
func (instance *Backend) DeleteMultiple(keys []string) error {
    ctx, cancel := instance.operationContext()
    defer cancel()

    return instance.DeleteMultipleCtx(ctx, keys)
}

func (instance *Backend) IncrementCtx(ctx context.Context, key string, delta int64) (int64, error) {
    if closedErr := instance.refuseWhenClosed(); nil != closedErr {
        return 0, closedErr
    }

    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return 0, normalizeErr
    }

    response := instance.client.Do(
        ctx,
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

/* Deprecated: prefer IncrementCtx, which takes ctx per call. */
func (instance *Backend) Increment(key string, delta int64) (int64, error) {
    ctx, cancel := instance.operationContext()
    defer cancel()

    return instance.IncrementCtx(ctx, key, delta)
}

func (instance *Backend) DecrementCtx(ctx context.Context, key string, delta int64) (int64, error) {
    if closedErr := instance.refuseWhenClosed(); nil != closedErr {
        return 0, closedErr
    }

    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return 0, normalizeErr
    }

    response := instance.client.Do(
        ctx,
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

/* Deprecated: prefer DecrementCtx, which takes ctx per call. */
func (instance *Backend) Decrement(key string, delta int64) (int64, error) {
    ctx, cancel := instance.operationContext()
    defer cancel()

    return instance.DecrementCtx(ctx, key, delta)
}

/* Close marks the backend closed and refuses every later operation, as the in-memory backend does. The shared client belongs to the composition root and is not closed here; the parent package's Connection wrapper is what the container's teardown closes. */
func (instance *Backend) Close() error {
    instance.closed.Store(true)

    return nil
}

/* counterError wraps a counter refusal with the key it happened on, which the raw store error does not name. The caller's own mistakes are named as the in-memory sibling names them, so a bug in the call reads apart from a store outage: redis refuses all three natively and this names its refusal, a store error matching none keeps the generic message, and the redis error stays the cause in every branch. */
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

/* negativeTtlError refuses an already-lapsed duration, as the in-memory backend does, since a negative ttl would otherwise fall into the branch that writes no expiry and store forever the value meant to be unreadable. Zero means no expiry on both backends. */
func negativeTtlError(ttl time.Duration) error {
    return exception.NewError(
        "cache ttl is negative",
        exceptioncontract.Context{
            "ttl": ttl.String(),
        },
        nil,
    )
}

/* normalizeKey names the offending key in every refusal: a batch call validates keys the caller handed in as a set, and a refusal that does not say which of them is malformed leaves nothing to act on. */
func (instance *Backend) normalizeKey(key string) (string, error) {
    if "" == key {
        return "", exception.NewError(
            "cache key is empty",
            nil,
            nil,
        )
    }

    if true == strings.Contains(key, " ") {
        return "", exception.NewError(
            "cache key contains spaces",
            exceptioncontract.Context{
                "key": key,
            },
            nil,
        )
    }

    if true == strings.Contains(key, "\n") {
        return "", exception.NewError(
            "cache key contains newlines",
            exceptioncontract.Context{
                "key": key,
            },
            nil,
        )
    }

    if rueidisBackendDefaultMaxKeyLength < len(key) {
        return "", exception.NewError(
            "cache key is too long",
            exceptioncontract.Context{
                "key":          key,
                "maxKeyLength": rueidisBackendDefaultMaxKeyLength,
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

/* scanKeys walks every node rather than the client as a whole: SCAN names no key, so a cluster client routes it to one node, and a clear that scanned one node would delete that node's share and report success. Nodes() answers a single-node client with itself, so the single-node path is the same walk. */
func (instance *Backend) scanKeys(ctx context.Context, pattern string) ([]string, error) {
    keys := make([]string, 0)

    nodes := instance.client.Nodes()
    for _, node := range nodes {
        nodeKeys, nodeErr := scanNodeKeys(ctx, node, pattern, instance.scanCount)
        if nil != nodeErr {
            return nil, nodeErr
        }

        keys = append(keys, nodeKeys...)
    }

    return keys, nil
}

func scanNodeKeys(ctx context.Context, node rueidis.Client, pattern string, scanCount int) ([]string, error) {
    cursor := uint64(0)
    keys := make([]string, 0)

    for {
        response := node.Do(
            ctx,
            node.B().Scan().Cursor(cursor).Match(pattern).Count(int64(scanCount)).Build(),
        )
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

func (instance *Backend) deleteKeysInBatches(ctx context.Context, keys []string) error {
    if 0 == len(keys) {
        return nil
    }

    for startIndex := 0; startIndex < len(keys); startIndex += instance.deleteBatch {
        endIndex := startIndex + instance.deleteBatch
        if endIndex > len(keys) {
            endIndex = len(keys)
        }

        batch := keys[startIndex:endIndex]
        if batchErr := instance.firstDeleteFailure(rueidis.MDel(instance.client, ctx, batch)); nil != batchErr {
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

func floorPositiveExpiry(ttl time.Duration) time.Duration {
    if 0 < ttl && ttl < time.Millisecond {
        return time.Millisecond
    }

    return ttl
}

var _ cachecontract.Backend = (*Backend)(nil)
