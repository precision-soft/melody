package cache

import (
    "context"
    "fmt"
    "hash/fnv"
    "reflect"
    "runtime/debug"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* RememberConfig is what one Remember call declares beyond its context, its cache, its key and its callback; the zero value is the default but for Ttl, which is required: stampede protection on, a 30s wait and a cancelable flight. */
type RememberConfig struct {
    /* Ttl is the lifetime of the stored value, handed to the cache's Set: a value of at least 1s, or unlimited for an entry kept until it is deleted or evicted; absent is refused, since a cached value declares how long it may be served. */
    Ttl bound.Duration

    /* StampedeProtectionDisabled runs the callback on every miss in the caller's goroutine, under the caller's context, with no flight to join. Off, concurrent callers of one key on one cache instance coalesce under a leader. */
    StampedeProtectionDisabled bool

    /* WaitTimeout bounds how long THIS caller waits for the flight, not the computation: 30s by default, 0 takes a memoized answer or none at once, unlimited waits on the flight and the context alone. Under the cancelable flight a wait shorter than the callback never stores the value when the callback honours its context, a callback that ignores the cancellation still storing it; pair a bounded wait on a slow callback with Detached. */
    WaitTimeout bound.Duration

    /* Detached keeps the flight running when its last waiter leaves, so the computation completes and stores its value for the callers after it. Off, the last waiter leaving cancels the flight's context and the next caller leads a fresh computation, so a callback that never returns does not pin its key for the life of the process. */
    Detached bool
}

var rememberWaitTimeoutRule = bound.Rule[time.Duration]{
    Name:              "RememberConfig.WaitTimeout",
    Default:           30 * time.Second,
    Minimum:           0,
    UnlimitedAdmitted: true,
}

/* Remember answers the cached value, computing it through the callback on a miss and storing it under the ttl. The context governs this caller's wait and nothing else under a flight: the computation belongs to the flight, and only the last waiter leaving a cancelable flight cancels it. A flight already finished is answered whatever the context says; otherwise the context's cancellation, the wait timeout and the leader's answer race, the first to arrive ending the wait. The callback receives the flight's context; with no flight, the stampede protection disabled or a cache the flight cannot tell apart by its pointer, it runs under the caller's context, which then governs the computation too. A refused configuration is answered as the error, naming the field, before the cache is read. On a cache exposing its stored shape, a StoredValueNormalizer, the computed value goes through it before it is returned, so the miss answers what every hit answers, while another cache answers the computed value as is; with the JSON serializer an integer beyond 2^53 therefore comes back changed on the computing call too. */
func Remember(
    ctx context.Context,
    cacheInstance cachecontract.Cache,
    key string,
    config RememberConfig,
    callback func(ctx context.Context) (any, error),
) (any, error) {
    /* the guard reads through the interface, since a typed-nil Cache would pass a plain comparison and panic on the first method call */
    if true == internal.IsNilInterface(cacheInstance) {
        return nil, exception.NewError("cache instance is nil", nil, nil)
    }

    if true == internal.IsNilInterface(ctx) {
        return nil, exception.NewError("remember context is nil", nil, nil)
    }

    ttl, ttlErr := resolveRememberTtl(config.Ttl)
    if nil != ttlErr {
        return nil, ttlErr
    }

    resolvedWaitTimeout, waitTimeoutErr := bound.Resolve(config.WaitTimeout, rememberWaitTimeoutRule)
    if nil != waitTimeoutErr {
        return nil, waitTimeoutErr
    }

    /* the flight reads a negative wait as unlimited */
    waitTimeout := resolvedWaitTimeout.Value
    if true == resolvedWaitTimeout.Unlimited {
        waitTimeout = -1
    }

    isCancelable := false == config.Detached

    value, exists, getErr := cacheInstance.Get(key)
    getErr = normalizeThirdPartyError(getErr)
    if nil != getErr {
        /* a payload the serializer cannot decode is a miss: the callback recomputes and its Set overwrites the corrupt payload, so the key heals; every other error means the cache failed */
        if false == IsDeserializationError(getErr) {
            return nil, getErr
        }

        exists = false
    }
    if true == exists {
        return value, nil
    }

    if true == config.StampedeProtectionDisabled {
        return rememberWithoutStampedeProtection(
            cacheInstance,
            key,
            ttl,
            ctx,
            callback,
        )
    }

    singleFlightKey, identifiable := rememberSingleFlightKey(
        cacheInstance,
        key,
        isCancelable,
    )
    if false == identifiable {
        return rememberWithoutStampedeProtection(
            cacheInstance,
            key,
            ttl,
            ctx,
            callback,
        )
    }

    return rememberWithStampedeProtection(
        cacheInstance,
        singleFlightKey,
        key,
        ttl,
        waitTimeout,
        isCancelable,
        ctx,
        callback,
    )
}

/* the ttl is judged before the cache is read, under the configuration's field name, so a miss never computes a value it cannot store */
func resolveRememberTtl(ttl bound.Duration) (bound.Duration, error) {
    rule := cachecontract.TtlRule
    rule.Name = "RememberConfig.Ttl"

    if true == ttl.IsAbsent() {
        return bound.Duration{}, exception.NewError(
            "the cache ttl is required",
            exceptioncontract.Context{
                "field": rule.Name,
            },
            nil,
        )
    }

    if _, err := bound.Resolve(ttl, rule); nil != err {
        return bound.Duration{}, err
    }

    return ttl, nil
}

func rememberWithStampedeProtection(
    cacheInstance cachecontract.Cache,
    singleFlightKey string,
    key string,
    ttl bound.Duration,
    waitTimeout time.Duration,
    isCancelable bool,
    callerContext context.Context,
    callback func(ctx context.Context) (any, error),
) (any, error) {
    shard := getRememberInFlightShard(singleFlightKey)

    shard.mutex.Lock()

    call, exists := shard.inFlightByKey[singleFlightKey]

    /* a cancelable call whose waiters all timed out is doomed to a cancellation error, so a late joiner replaces the entry and leads afresh */
    if true == exists && true == call.IsCanceled() {
        exists = false
    }

    if true == exists {
        call.AddWaiter()
        shard.mutex.Unlock()

        defer call.RemoveWaiter(shard)
        return call.Wait(callerContext, waitTimeout, key)
    }

    call = newRememberInFlightCall(isCancelable)
    call.AddWaiter()
    shard.inFlightByKey[singleFlightKey] = call

    shard.mutex.Unlock()

    go executeRememberInFlightLeader(
        cacheInstance,
        shard,
        singleFlightKey,
        key,
        ttl,
        call,
        callback,
    )

    defer call.RemoveWaiter(shard)
    return call.Wait(callerContext, waitTimeout, key)
}

/* readOwnStoredValue is a waiter's own read of the stored value its flight's leader could not copy: through a Manager each read deserializes a value of its own. An entry gone between the leader's read and this one, or one that fails to decode, is answered as a miss outside a flight is: the waiter computes through the callback and stores what it computed, under the flight's context, which lives while a waiter is in it. */
func readOwnStoredValue(
    cacheInstance cachecontract.Cache,
    key string,
    ttl bound.Duration,
    flightContext context.Context,
    callback func(ctx context.Context) (any, error),
) (any, error) {
    value, exists, getErr := cacheInstance.Get(key)
    getErr = normalizeThirdPartyError(getErr)
    if nil != getErr && false == IsDeserializationError(getErr) {
        return nil, getErr
    }

    if nil != getErr || false == exists {
        return rememberWithoutStampedeProtection(cacheInstance, key, ttl, flightContext, callback)
    }

    return value, nil
}

func executeRememberInFlightLeader(
    cacheInstance cachecontract.Cache,
    shard *rememberInFlightShard,
    singleFlightKey string,
    key string,
    ttl bound.Duration,
    call *rememberInFlightCall,
    callback func(ctx context.Context) (any, error),
) {
    defer func() {
        shard.mutex.Lock()
        if call == shard.inFlightByKey[singleFlightKey] {
            delete(shard.inFlightByKey, singleFlightKey)
        }
        shard.mutex.Unlock()
    }()

    /* the callback's panics are recovered inside executeRememberCallbackSafely, so this recover catches a panicking Get or Set, and the error says so */
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        call.Complete(
            nil,
            exception.NewError(
                "cache remember cache access panicked",
                map[string]any{
                    "key":        key,
                    "panic":      internal.DescribeRecoveredValue(recoveredValue),
                    "panicStack": string(debug.Stack()),
                },
                exception.PanicCause(recoveredValue),
            ),
        )
    }()

    existingValue, existingExists, existingGetErr := cacheInstance.Get(key)
    existingGetErr = normalizeThirdPartyError(existingGetErr)
    if nil != existingGetErr {
        if false == IsDeserializationError(existingGetErr) {
            call.Complete(nil, existingGetErr)
            return
        }

        existingExists = false
        existingGetErr = nil
    }
    copyOf := func(value any) (any, error) {
        return normalizeRememberedValue(cacheInstance, key, value)
    }

    if true == existingExists {
        /* the value read is handed to the first caller, so the others copy one made apart from it; a value that cannot be copied is not shared either: every other caller reads its own from the store, as a hit outside a flight does, so the same stored bytes answer alike whether the callers coalesced or not */
        master, copyErr := copyOf(existingValue)
        if nil != copyErr {
            call.CompleteWithCopies(existingValue, nil, func(any) (any, error) {
                return readOwnStoredValue(cacheInstance, key, ttl, call.Context(), callback)
            })
            return
        }

        call.CompleteWithCopies(existingValue, master, copyOf)
        return
    }

    computedValue, callbackErr := executeRememberCallbackSafely(
        call.Context(),
        callback,
        key,
    )
    if nil != callbackErr {
        call.Complete(nil, callbackErr)
        return
    }

    normalizedValue, normalizeErr := normalizeRememberedValue(cacheInstance, key, computedValue)
    if nil != normalizeErr {
        call.Complete(nil, normalizeErr)
        return
    }

    setErr := normalizeThirdPartyError(cacheInstance.Set(key, computedValue, ttl))
    if nil != setErr {
        call.Complete(nil, setErr)
        return
    }

    /* the callback may keep the value it returned, so every caller past the first copies a master taken here, before the flight completes, which nothing else holds */
    master, copyErr := copyOf(computedValue)
    if nil != copyErr {
        call.Complete(nil, copyErr)
        return
    }

    call.CompleteWithCopies(normalizedValue, master, copyOf)
}

/* with no flight nobody else waits on the computation, so the callback runs under the caller's context */
func rememberWithoutStampedeProtection(
    cacheInstance cachecontract.Cache,
    key string,
    ttl bound.Duration,
    callerContext context.Context,
    callback func(ctx context.Context) (any, error),
) (any, error) {
    value, callbackErr := executeRememberCallbackSafely(
        callerContext,
        callback,
        key,
    )
    if nil != callbackErr {
        return nil, callbackErr
    }

    normalizedValue, normalizeErr := normalizeRememberedValue(cacheInstance, key, value)
    if nil != normalizeErr {
        return nil, normalizeErr
    }

    setErr := normalizeThirdPartyError(cacheInstance.Set(key, value, ttl))
    if nil != setErr {
        return nil, setErr
    }

    return normalizedValue, nil
}

/* normalizeRememberedValue gives the computing call the shape every cached call answers; a cache that does not expose its stored shape answers the value unchanged. It runs before the store, so a value the serializer encodes but cannot decode is refused once instead of being rewritten and missed on every call. */
func normalizeRememberedValue(cacheInstance cachecontract.Cache, key string, value any) (any, error) {
    normalizer, isNormalizer := cacheInstance.(cachecontract.StoredValueNormalizer)
    if false == isNormalizer {
        return value, nil
    }

    normalized, normalizeErr := normalizer.NormalizeStoredValue(value)
    if nil != normalizeErr {
        /* the refusal names the key and the store's own message, so a value the serializer cannot carry through the store reads as one class whichever half of the round-trip failed; the cause says which */
        return nil, exception.NewError(
            "cache value serialization failed",
            map[string]any{"key": key},
            normalizeErr,
        )
    }

    return normalized, nil
}

/* rememberSingleFlightKey names the unit callers coalesce under: one cache instance, one key, one cancelability. An instance is told apart by its pointer, so a value-kind Cache gets no coalescing, which costs the optimization and never the answer; two managers over one backend are two units. */
func rememberSingleFlightKey(cacheInstance cachecontract.Cache, key string, isCancelable bool) (string, bool) {
    cancelableSuffix := "cancelable:false"
    if true == isCancelable {
        cancelableSuffix = "cancelable:true"
    }

    value := reflect.ValueOf(cacheInstance)
    if reflect.Pointer != value.Kind() {
        return "", false
    }

    return fmt.Sprintf(
        "%s:%d:%s:%s",
        reflect.TypeOf(cacheInstance).String(),
        value.Pointer(),
        key,
        cancelableSuffix,
    ), true
}

func buildRememberInFlightShardList() []rememberInFlightShard {
    shardList := make([]rememberInFlightShard, rememberInFlightShardCount)

    for shardIndex := 0; shardIndex < len(shardList); shardIndex = shardIndex + 1 {
        shardList[shardIndex] = rememberInFlightShard{
            inFlightByKey: make(map[string]*rememberInFlightCall, 64),
        }
    }

    return shardList
}

func getRememberInFlightShard(key string) *rememberInFlightShard {
    hasher := fnv.New32a()
    _, _ = hasher.Write([]byte(key))

    shardIndex := int(hasher.Sum32() % uint32(len(rememberInFlightShardList)))

    return &rememberInFlightShardList[shardIndex]
}

func executeRememberCallbackSafely(
    contextInstance context.Context,
    callback func(ctx context.Context) (any, error),
    key string,
) (result any, callbackErr error) {
    result = nil
    callbackErr = nil

    /* the panic value travels as the cause, with the stack captured on the goroutine that raised it, so an error-shaped panic keeps its context and chain */
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        callbackErr = exception.NewError(
            "cache remember callback panicked",
            map[string]any{
                "key":        key,
                "panic":      internal.DescribeRecoveredValue(recoveredValue),
                "panicStack": string(debug.Stack()),
            },
            exception.PanicCause(recoveredValue),
        )

        result = nil
    }()

    /* a typed-nil error from the callback is the success it means; boxed, it would be memoized as the flight's failure and panic the first waiter that renders it */
    value, computeErr := callback(contextInstance)
    computeErr = normalizeThirdPartyError(computeErr)
    if nil != computeErr {
        return nil, computeErr
    }

    result = value

    return result, nil
}
