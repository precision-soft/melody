package cache

import (
    "context"
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
    /* Ttl is the lifetime of the stored value unless the callback chooses its own through RememberItem.SetTtl, handed to the cache's Set: a value of at least 1s, or unlimited for an entry kept until it is deleted or evicted; absent is refused, also where every callback chooses, since the configuration is judged before the cache is read. */
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

/* Remember answers the value cached under key as a T, computing it through the callback on a miss and storing it under the configuration's ttl, unless the callback, through its item, chose another ttl or asked that nothing is stored. The context governs this caller's wait and nothing else under a flight: the computation belongs to the flight, and only the last waiter leaving a cancelable flight cancels it. A flight already finished is answered whatever the context says; otherwise the context's cancellation, the wait timeout and the leader's answer race, the first to arrive ending the wait. The callback receives the flight's context; with no flight, the stampede protection disabled or a cache the flight cannot tell apart by its pointer, it runs under the caller's context, which then governs the computation too. The cache is read and a computed value stored without the cancellation of the context they run under, its values kept, so a done context never refuses a hit and never loses a value already computed. A refused configuration is answered as the error, naming the field, before the cache is read. A ttl the callback chose that the cache refuses is answered as the error once the callback returned, naming RememberItem.Ttl, with nothing stored. A value the callback asked not to store is answered to this caller and to every waiter of its flight, and the next call computes again: under a skipped store, coalescing is the only stampede protection left. Every caller is answered a T of its own, the one the cache reads back: the computed value goes through the cache's RoundTrip before it is stored, so the miss answers what every hit answers, and a value the cache can write but not read back as a T is refused once, naming the key, with nothing stored. An entry the serializer refuses to decode into T, a corrupt payload or one of a kind T cannot hold, is a miss: the callback recomputes and the first computation that stores overwrites it. One key holds one type: callers naming different types under one key never share a flight, and under json a struct written as another struct reads as a hit with the fields they do not share at zero. Remember[any] answers the generic form of the store's encoding, so under json an integer beyond 2^53 comes back changed. */
func Remember[T any](
    ctx context.Context,
    cacheInstance cachecontract.Cache,
    key string,
    config RememberConfig,
    callback func(ctx context.Context, item *RememberItem) (T, error),
) (T, error) {
    var zero T

    /* the guard reads through the interface, since a typed-nil Cache would pass a plain comparison and panic on the first method call */
    if true == internal.IsNilInterface(cacheInstance) {
        return zero, exception.NewError("cache instance is nil", nil, nil)
    }

    if true == internal.IsNilInterface(ctx) {
        return zero, exception.NewError("remember context is nil", nil, nil)
    }

    ttl, ttlErr := resolveRememberTtl(config.Ttl)
    if nil != ttlErr {
        return zero, ttlErr
    }

    resolvedWaitTimeout, waitTimeoutErr := bound.Resolve(config.WaitTimeout, rememberWaitTimeoutRule)
    if nil != waitTimeoutErr {
        return zero, waitTimeoutErr
    }

    /* the flight reads a negative wait as unlimited */
    waitTimeout := resolvedWaitTimeout.Value
    if true == resolvedWaitTimeout.Unlimited {
        waitTimeout = -1
    }

    isCancelable := false == config.Detached

    operations := newRememberOperations(cacheInstance, key, callback)

    value, exists, getErr := operations.read(context.WithoutCancel(ctx))
    getErr = normalizeThirdPartyError(getErr)
    if nil != getErr {
        /* a payload that does not decode into T is a miss: the callback recomputes and the first Set overwrites the payload, so the key heals once a computation stores; every other error means the cache failed */
        if false == IsDeserializationError(getErr) {
            return zero, getErr
        }

        exists = false
    }
    if true == exists {
        return unboxRememberedValue[T](value, nil, key)
    }

    flightKey, identifiable := rememberFlightKeyOf(
        cacheInstance,
        key,
        operations.valueType,
        isCancelable,
    )

    var answer any
    var answerErr error
    if true == config.StampedeProtectionDisabled || false == identifiable {
        answer, answerErr = rememberWithoutStampedeProtection(
            key,
            ttl,
            ctx,
            operations,
        )
    } else {
        answer, answerErr = rememberWithStampedeProtection(
            flightKey,
            key,
            ttl,
            waitTimeout,
            isCancelable,
            ctx,
            operations,
        )
    }

    return unboxRememberedValue[T](answer, answerErr, key)
}

/* the flight carries any, and a nil T of an interface type, unboxed, would be the nil any it reads as no value */
type rememberedValue[T any] struct {
    value T
}

/* every any is a rememberedValue of the call's T */
type rememberOperations struct {
    valueType reflect.Type
    read      func(ctx context.Context) (any, bool, error)
    roundTrip func(value any) (any, error)
    store     func(ctx context.Context, value any, ttl bound.Duration) error
    compute   func(ctx context.Context, item *RememberItem) (any, error)
}

func newRememberOperations[T any](
    cacheInstance cachecontract.Cache,
    key string,
    callback func(ctx context.Context, item *RememberItem) (T, error),
) rememberOperations {
    return rememberOperations{
        valueType: reflect.TypeFor[T](),
        read: func(ctx context.Context) (any, bool, error) {
            var value T
            exists, getErr := cacheInstance.Get(ctx, key, &value)

            return rememberedValue[T]{value: value}, exists, getErr
        },
        roundTrip: func(value any) (any, error) {
            boxed, isBoxed := value.(rememberedValue[T])
            if false == isBoxed {
                return nil, newRememberFlightTypeError[T](key)
            }

            copyOfValue, roundTripErr := roundTripRememberedValue(cacheInstance, key, boxed.value)
            if nil != roundTripErr {
                return nil, roundTripErr
            }

            return rememberedValue[T]{value: copyOfValue}, nil
        },
        store: func(ctx context.Context, value any, ttl bound.Duration) error {
            boxed, isBoxed := value.(rememberedValue[T])
            if false == isBoxed {
                return newRememberFlightTypeError[T](key)
            }

            return cacheInstance.Set(ctx, key, boxed.value, ttl)
        },
        compute: func(ctx context.Context, item *RememberItem) (any, error) {
            value, computeErr := callback(ctx, item)

            return rememberedValue[T]{value: value}, computeErr
        },
    }
}

/* a flight keyed by T cannot carry another box, so one is refused as an error rather than asserted into a panic */
func unboxRememberedValue[T any](value any, valueErr error, key string) (T, error) {
    var zero T
    if nil != valueErr {
        return zero, valueErr
    }

    boxed, isBoxed := value.(rememberedValue[T])
    if false == isBoxed {
        return zero, newRememberFlightTypeError[T](key)
    }

    return boxed.value, nil
}

func newRememberFlightTypeError[T any](key string) error {
    return exception.NewError(
        "cache remember flight answered another type",
        exceptioncontract.Context{"key": key, "expectedType": reflect.TypeFor[T]().String()},
        nil,
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

/* the callback's choice is judged once it returned, under the item's field name: a skip wins over any ttl, an absent ttl leaves the configuration's */
func resolveRememberStoreChoice(configTtl bound.Duration, storeChoice rememberStoreChoice) (bound.Duration, bool, error) {
    if true == storeChoice.isStoreSkipped {
        return bound.Duration{}, false, nil
    }

    if true == storeChoice.ttl.IsAbsent() {
        return configTtl, true, nil
    }

    rule := cachecontract.TtlRule
    rule.Name = "RememberItem.Ttl"

    if _, err := bound.Resolve(storeChoice.ttl, rule); nil != err {
        return bound.Duration{}, false, err
    }

    return storeChoice.ttl, true, nil
}

func rememberWithStampedeProtection(
    flightKey rememberFlightKey,
    key string,
    ttl bound.Duration,
    waitTimeout time.Duration,
    isCancelable bool,
    callerContext context.Context,
    operations rememberOperations,
) (any, error) {
    shard := getRememberInFlightShard(flightKey)

    shard.mutex.Lock()

    call, exists := shard.inFlightByKey[flightKey]

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
    shard.inFlightByKey[flightKey] = call

    shard.mutex.Unlock()

    go executeRememberInFlightLeader(
        shard,
        flightKey,
        key,
        ttl,
        call,
        operations,
    )

    defer call.RemoveWaiter(shard)
    return call.Wait(callerContext, waitTimeout, key)
}

/* readOwnStoredValue is a waiter's own read of the stored value its flight's leader could not copy: each read decodes a value of its own. An entry gone between the leader's read and this one, or one that fails to decode, is answered as a miss outside a flight is: the waiter computes through the callback, with an item of its own, and stores what it computed as that item says, under the flight's context, which lives while a waiter is in it. */
func readOwnStoredValue(
    key string,
    ttl bound.Duration,
    flightContext context.Context,
    operations rememberOperations,
) (any, error) {
    value, exists, getErr := operations.read(flightContext)
    getErr = normalizeThirdPartyError(getErr)
    if nil != getErr && false == IsDeserializationError(getErr) {
        return nil, getErr
    }

    if nil != getErr || false == exists {
        return rememberWithoutStampedeProtection(key, ttl, flightContext, operations)
    }

    return value, nil
}

func executeRememberInFlightLeader(
    shard *rememberInFlightShard,
    flightKey rememberFlightKey,
    key string,
    ttl bound.Duration,
    call *rememberInFlightCall,
    operations rememberOperations,
) {
    defer func() {
        shard.mutex.Lock()
        if call == shard.inFlightByKey[flightKey] {
            delete(shard.inFlightByKey, flightKey)
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

    existingValue, existingExists, existingGetErr := operations.read(call.Context())
    existingGetErr = normalizeThirdPartyError(existingGetErr)
    if nil != existingGetErr {
        if false == IsDeserializationError(existingGetErr) {
            call.Complete(nil, existingGetErr)
            return
        }

        existingExists = false
        existingGetErr = nil
    }

    if true == existingExists {
        /* the value read is handed to the first caller, so the others copy one made apart from it; a value that cannot be copied is not shared either: every other caller reads its own from the store, as a hit outside a flight does, so the same stored bytes answer alike whether the callers coalesced or not */
        master, copyErr := operations.roundTrip(existingValue)
        if nil != copyErr {
            call.CompleteWithCopies(existingValue, nil, func(any) (any, error) {
                return readOwnStoredValue(key, ttl, call.Context(), operations)
            })
            return
        }

        call.CompleteWithCopies(existingValue, master, operations.roundTrip)
        return
    }

    computedValue, storeChoice, callbackErr := executeRememberCallbackSafely(
        call.Context(),
        operations.compute,
        key,
    )
    if nil != callbackErr {
        call.Complete(nil, callbackErr)
        return
    }

    storeTtl, isStored, storeChoiceErr := resolveRememberStoreChoice(ttl, storeChoice)
    if nil != storeChoiceErr {
        call.Complete(nil, storeChoiceErr)
        return
    }

    normalizedValue, normalizeErr := operations.roundTrip(computedValue)
    if nil != normalizeErr {
        call.Complete(nil, normalizeErr)
        return
    }

    if true == isStored {
        setErr := normalizeThirdPartyError(operations.store(context.WithoutCancel(call.Context()), computedValue, storeTtl))
        if nil != setErr {
            call.Complete(nil, setErr)
            return
        }
    }

    /* the callback may keep the value it returned, so every caller past the first copies a master taken here, before the flight completes, which nothing else holds */
    master, copyErr := operations.roundTrip(computedValue)
    if nil != copyErr {
        call.Complete(nil, copyErr)
        return
    }

    /* a skipped value must be computed again by the next call, so its finished flight leaves the shard before it completes, where a call started after it would otherwise join it until the deferred delete */
    if false == isStored {
        shard.mutex.Lock()
        if call == shard.inFlightByKey[flightKey] {
            delete(shard.inFlightByKey, flightKey)
        }
        shard.mutex.Unlock()
    }

    call.CompleteWithCopies(normalizedValue, master, operations.roundTrip)
}

/* with no flight nobody else waits on the computation, so the callback runs under the caller's context */
func rememberWithoutStampedeProtection(
    key string,
    ttl bound.Duration,
    callerContext context.Context,
    operations rememberOperations,
) (any, error) {
    value, storeChoice, callbackErr := executeRememberCallbackSafely(
        callerContext,
        operations.compute,
        key,
    )
    if nil != callbackErr {
        return nil, callbackErr
    }

    storeTtl, isStored, storeChoiceErr := resolveRememberStoreChoice(ttl, storeChoice)
    if nil != storeChoiceErr {
        return nil, storeChoiceErr
    }

    normalizedValue, normalizeErr := operations.roundTrip(value)
    if nil != normalizeErr {
        return nil, normalizeErr
    }

    if true == isStored {
        setErr := normalizeThirdPartyError(operations.store(context.WithoutCancel(callerContext), value, storeTtl))
        if nil != setErr {
            return nil, setErr
        }
    }

    return normalizedValue, nil
}

/* run before the store, so a value the cache writes but cannot read back as a T is refused once instead of being rewritten and missed on every call */
func roundTripRememberedValue[T any](cacheInstance cachecontract.Cache, key string, value T) (T, error) {
    var copyOfValue T

    roundTripErr := normalizeThirdPartyError(cacheInstance.RoundTrip(value, &copyOfValue))
    if nil != roundTripErr {
        var zero T

        /* the refusal names the key and the store's own message, so a value the cache cannot carry through the store reads as one class whichever half of the round trip failed; the cause says which */
        return zero, exception.NewError(
            "cache value serialization failed",
            map[string]any{"key": key},
            roundTripErr,
        )
    }

    return copyOfValue, nil
}

/* an instance is told apart by its pointer, so a value-kind Cache gets no coalescing; reflect.Type is identity where its String() is not, since two packages may each declare a "model.User" */
type rememberFlightKey struct {
    cacheType    reflect.Type
    cachePointer uintptr
    valueType    reflect.Type
    key          string
    isCancelable bool
}

func rememberFlightKeyOf(cacheInstance cachecontract.Cache, key string, valueType reflect.Type, isCancelable bool) (rememberFlightKey, bool) {
    value := reflect.ValueOf(cacheInstance)
    if reflect.Pointer != value.Kind() {
        return rememberFlightKey{}, false
    }

    return rememberFlightKey{
        cacheType:    value.Type(),
        cachePointer: value.Pointer(),
        valueType:    valueType,
        key:          key,
        isCancelable: isCancelable,
    }, true
}

func buildRememberInFlightShardList() []rememberInFlightShard {
    shardList := make([]rememberInFlightShard, rememberInFlightShardCount)

    for shardIndex := 0; shardIndex < len(shardList); shardIndex = shardIndex + 1 {
        shardList[shardIndex] = rememberInFlightShard{
            inFlightByKey: make(map[rememberFlightKey]*rememberInFlightCall, 64),
        }
    }

    return shardList
}

func getRememberInFlightShard(flightKey rememberFlightKey) *rememberInFlightShard {
    hasher := fnv.New32a()
    _, _ = hasher.Write([]byte(flightKey.key))

    shardIndex := int(hasher.Sum32() % uint32(len(rememberInFlightShardList)))

    return &rememberInFlightShardList[shardIndex]
}

func executeRememberCallbackSafely(
    contextInstance context.Context,
    callback func(ctx context.Context, item *RememberItem) (any, error),
    key string,
) (result any, storeChoice rememberStoreChoice, callbackErr error) {
    result = nil
    storeChoice = rememberStoreChoice{}
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
        storeChoice = rememberStoreChoice{}
    }()

    /* a typed-nil error from the callback is the success it means; boxed, it would be memoized as the flight's failure and panic the first waiter that renders it */
    /* an item of its own per computation, read once here, so what a callback tells a kept item later changes no store */
    item := &RememberItem{}

    value, computeErr := callback(contextInstance, item)
    computeErr = normalizeThirdPartyError(computeErr)
    if nil != computeErr {
        return nil, rememberStoreChoice{}, computeErr
    }

    result = value
    storeChoice = item.storeChoice()

    return result, storeChoice, nil
}
