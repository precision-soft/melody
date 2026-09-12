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

/* NewBackend builds the redis-backed cache over one key prefix, which is the whole of this backend's isolation: every key it writes carries it, and Clear scans and deletes everything under it. An empty prefix takes the shipped default, and the shipped default is the same string in every melody application, so two applications pointed at one redis with it share a namespace — a Get answers whatever the other one wrote under the same name, and because a foreign json document of another shape decodes without error it is served as a hit rather than treated as a miss, while a Clear from either empties the other's entries too. Redis databases do not separate them either: the client's SelectDb defaults to 0. Give each application (and each environment sharing a store) its own prefix. */
func NewBackend(
    client rueidis.Client,
    ctx context.Context,
    prefix string,
    scanCount int,
    deleteBatch int,
) (*Backend, error) {
    return NewBackendWithCommandTimeout(client, ctx, prefix, scanCount, deleteBatch, 0)
}

/* NewBackendWithCommandTimeout additionally bounds every operation dispatched without a caller context: the ctx-less half of the contract methods otherwise runs unbounded against a store that accepts connections but stops answering — the same case the rate limiter in the parent package bounds with its own call timeout. A non-positive value reads as unbounded, the exact behaviour of NewBackend. */
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
    /* ownerClosed is the closed flag of the backend this handle was derived from, nil on a backend built directly: a context-bound handle minted by WithContext lives exactly as long as its owner, so the owner's Close must reach it — otherwise the refuse-after-Close guarantee would hold only for the one stored instance while the runtime door mints a fresh, open handle per request. A sibling backend built over the same client stays independent on purpose; the client belongs to whoever built it. */
    ownerClosed *atomic.Bool
}

/* operationContext bounds an operation dispatched without a caller context: the constructor's context plus the command timeout when one is configured, the constructor's context alone otherwise. */
func (instance *Backend) operationContext() (context.Context, context.CancelFunc) {
    if 0 < instance.commandTimeout {
        return context.WithTimeout(instance.ctx, instance.commandTimeout)
    }

    return instance.ctx, func() {}
}

/* refuseWhenClosed answers the refusal every operation gives after Close, the answer the in-memory backend behind the same contract gives: a teardown-ordering bug surfaces immediately instead of quietly serving through a client whose owner already ended this backend. A derived handle also reads its owner's flag, so the owner's Close reaches every handle WithContext minted from it. */
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

/* ClearByPrefixCtx refuses the empty prefix the way every other operation refuses the empty key: a prefix assembled at run time that comes out empty — "tenant:" + an unresolved id — would otherwise select the whole namespace, and wiping everything is the one outcome a prefixed delete exists to prevent. A caller that means the whole namespace has ClearCtx, which says so. */
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

    /* the ttl is judged before the empty early-return, the order the in-memory backend judges it in: an already-invalid ttl is refused whether or not this particular batch happens to be empty */
    if 0 > ttl {
        return negativeTtlError(ttl)
    }

    if 0 == len(items) {
        return nil
    }

    /* the batch is walked over sorted keys, never map order: the validation refusal below names the first key it rejects, and a map-ordered walk named a different key for the same wrong batch on every call — the exact nondeterminism the response reporting further down already refuses. The keys are carried alongside the commands because a failing response is identified by position only. */
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

    /* every failing response is collected before one is reported, the delete sibling's rule: returning on the first failure of a map-ordered walk named a different key for the same failing batch on every call, and hid that the entries after it also failed */
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

/* Close marks the backend closed and refuses every later operation, the answer the in-memory backend behind the same contract gives. The shared client itself belongs to the composition root and is deliberately not closed here — see the Connection wrapper in the parent package for the value the container's teardown closes. */
func (instance *Backend) Close() error {
    instance.closed.Store(true)

    return nil
}

/* counterError wraps a counter refusal with the key it happened on: the raw store error names neither, and the counter path is the one a caller most often logs verbatim.

   The caller's own mistakes are named the way the in-memory sibling names them, because the shared contract makes the grammar of a refusal part of the promise: on an open backend with a valid key, three distinct mistakes used to arrive here under one message that is also the message of a store outage, so neither the operator nor the application could tell a bug in the call from redis being down. Redis refuses all three natively — the guard exists, it is redis's, and it answers in redis's words — so what was missing was never the refusal but its name. A store error matching none of them keeps the generic message, which from here on really does mean the store failed. The redis error stays the cause in every branch, so nothing that reads through the chain loses anything. */
func counterError(key string, causeErr error) error {
    return exception.NewError(
        counterErrorMessage(causeErr),
        exceptioncontract.Context{
            "key": key,
        },
        causeErr,
    )
}

/* counterRefusalMessages maps redis's own wording onto the message the in-memory backend answers for the same mistake. The match is on a fragment rather than the whole line because a redis error carries a prefix that varies by server version and by whether the command travelled through a script.

   The order is load-bearing and the list is walked in it: redis answers a DECRBY that cannot be negated with "decrement would overflow" and a counter driven past the int64 ceiling with "increment or decrement would overflow", and the first of those two is a substring of the second. Written the other way round every ceiling overflow would be reported as a delta that cannot be negated. */
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

/* negativeTtlError refuses the already-lapsed duration the in-memory backend refuses too. Without it a negative ttl falls into the branch that writes no expiry at all, so the one value the caller meant to be unreadable is the one value stored forever. Zero keeps meaning no expiry, as both backends document. */
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

/* scanKeys walks every node rather than the client as a whole. SCAN names no key, so a cluster client routes it to whichever node it happens to pick first, and a clear that scanned one node would delete that node's share of the matching keys and report success — the caller would read a complete invalidation from a partial one. Nodes() answers with the one client itself when the deployment is not a cluster, so the single-node path is the same walk over one node. */
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
            /* a multi-batch wipe that fails part-way is described at the operation's own extent, not the batch's: the batches before this one are irreversibly gone, and an error whose counts covered only the failing batch could not say whether the wipe destroyed nothing or nearly everything — a cancellation mid-clear left exactly that ambiguity. A single-batch operation keeps the batch report, whose counts already are the operation's. */
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

/* firstDeleteFailure names the key that failed. MDel reports per key, and a batch delete that answers with the bare store error tells the caller neither which entry stopped it nor that the entries before it are already gone. The map is unordered, so the reported key is chosen by sorting rather than by iteration, which keeps two identical failures reporting identically. */
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
