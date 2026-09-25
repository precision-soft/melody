package cache

import (
    "context"
    "fmt"
    "hash/fnv"
    "reflect"
    "runtime/debug"
    "time"

    cachecontract "github.com/precision-soft/melody/cache/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
)

/* NewDefaultRememberOption arms stampede protection with an unbounded wait and a flight that is not cancelable, so a callback that never returns pins its key for the life of the process: the leader runs under a background context and only its own return deletes the entry, and every later caller of the key parks behind it. A callback that can hang wants its own deadline, WithWaitTimeout on the waiters, or WithCancelable, so an abandoned flight is canceled and replaced. */
func NewDefaultRememberOption() *RememberOption {
    defaultWaitTimeout := time.Duration(-1)

    return &RememberOption{
        enableStampedeProtection: true,
        waitTimeout:              &defaultWaitTimeout,
        isCancelable:             false,
    }
}

/* RememberOption starts from the constructor defaults wherever it is built: a setter called on the zero value first reads the receiver as NewDefaultRememberOption. waitTimeout is a pointer, so a deliberate zero wait is told apart from an unspoken one. The caller's context lives here as one more per-call setting; its interplay with waitTimeout and isCancelable is written on Context. */
type RememberOption struct {
    enableStampedeProtection bool
    waitTimeout              *time.Duration
    isCancelable             bool
    callerContext            context.Context
}

func (instance *RememberOption) normalizeZeroReceiver() {
    if (RememberOption{}) == *instance {
        *instance = *NewDefaultRememberOption()
    }
}

func (instance *RememberOption) EnableStampedeProtection() bool {
    return instance.enableStampedeProtection
}

func (instance *RememberOption) WithStampedeProtectionEnabled(enableStampedeProtection bool) *RememberOption {
    instance.normalizeZeroReceiver()
    instance.enableStampedeProtection = enableStampedeProtection
    return instance
}

func (instance *RememberOption) WaitTimeout() time.Duration {
    if nil == instance.waitTimeout {
        return -1
    }

    return *instance.waitTimeout
}

func (instance *RememberOption) WithWaitTimeout(waitTimeout time.Duration) *RememberOption {
    instance.normalizeZeroReceiver()
    instance.waitTimeout = &waitTimeout
    return instance
}

func (instance *RememberOption) IsCancelable() bool {
    return instance.isCancelable
}

func (instance *RememberOption) WithCancelable(isCancelable bool) *RememberOption {
    instance.normalizeZeroReceiver()
    instance.isCancelable = isCancelable
    return instance
}

/* Context answers the context that governs this caller's wait, context.Background when none was given. It ends the wait and nothing else: the computation belongs to the flight, and only the last waiter leaving a cancelable option cancels it. A canceled context ends the wait first, then the wait timeout, then the leader; a zero wait timeout never consults it. The callback receives the flight's context, not this one. */
func (instance *RememberOption) Context() context.Context {
    if nil == instance.callerContext {
        return context.Background()
    }

    return instance.callerContext
}

func (instance *RememberOption) WithContext(callerContext context.Context) *RememberOption {
    instance.normalizeZeroReceiver()
    instance.callerContext = callerContext
    return instance
}

/* Remember answers the cached value, computing it through the callback on a miss and storing it. The computed value goes through the stored shape before it is returned, so the miss answers what every hit answers; with the JSON serializer an integer beyond 2^53 therefore comes back changed on the computing call too. */
func Remember(
    cacheInstance cachecontract.Cache,
    key string,
    ttl time.Duration,
    callback func(ctx context.Context) (any, error),
    option *RememberOption,
) (any, error) {
    /* the guard reads through the interface, since a typed-nil Cache would pass a plain comparison and panic on the first method call */
    if true == internal.IsNilInterface(cacheInstance) {
        return nil, exception.NewError("cache instance is nil", nil, nil)
    }

    /* the zero-value option reads as the constructor defaults, so it does not disarm the stampede protection; a deliberate protection-off option carries waitTimeout -1 */
    effectiveOption := option
    if nil == effectiveOption || (RememberOption{}) == *effectiveOption {
        effectiveOption = NewDefaultRememberOption()
    }

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

    if false == effectiveOption.EnableStampedeProtection() {
        return rememberWithoutStampedeProtection(
            cacheInstance,
            key,
            ttl,
            effectiveOption.Context(),
            callback,
        )
    }

    singleFlightKey, identifiable := rememberSingleFlightKey(
        cacheInstance,
        key,
        effectiveOption.IsCancelable(),
    )
    if false == identifiable {
        return rememberWithoutStampedeProtection(
            cacheInstance,
            key,
            ttl,
            effectiveOption.Context(),
            callback,
        )
    }

    return rememberWithStampedeProtection(
        cacheInstance,
        singleFlightKey,
        key,
        ttl,
        effectiveOption.WaitTimeout(),
        effectiveOption.IsCancelable(),
        effectiveOption.Context(),
        callback,
    )
}

func rememberWithStampedeProtection(
    cacheInstance cachecontract.Cache,
    singleFlightKey string,
    key string,
    ttl time.Duration,
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

func executeRememberInFlightLeader(
    cacheInstance cachecontract.Cache,
    shard *rememberInFlightShard,
    singleFlightKey string,
    key string,
    ttl time.Duration,
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
                    "panic":      fmt.Sprintf("%v", recoveredValue),
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
    if true == existingExists {
        call.Complete(existingValue, nil)
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

    call.Complete(normalizedValue, nil)
}

/* with no flight nobody else waits on the computation, so the callback runs under the caller's context */
func rememberWithoutStampedeProtection(
    cacheInstance cachecontract.Cache,
    key string,
    ttl time.Duration,
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

/* storedValueNormalizer is the optional door through which Remember learns the shape a stored value reads back as; the manager implements it with one local serializer round-trip. It is asked of the Cache value Remember receives, so a decorator over the manager forwards NormalizeStoredValue by name, or the miss and the hit answer different shapes. It stays unexported here, since a patch admits no new public symbol. */
type storedValueNormalizer interface {
    NormalizeStoredValue(value any) (any, error)
}

/* normalizeRememberedValue gives the computing call the shape every cached call answers; a cache that does not expose its stored shape answers the value unchanged. It runs before the store, so a value the serializer encodes but cannot decode is refused once instead of being rewritten and missed on every call. */
func normalizeRememberedValue(cacheInstance cachecontract.Cache, key string, value any) (any, error) {
    normalizer, isNormalizer := cacheInstance.(storedValueNormalizer)
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
                "panic":      fmt.Sprintf("%v", recoveredValue),
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
