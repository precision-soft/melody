package cache

import (
    "context"
    "fmt"
    "hash/fnv"
    "reflect"
    "runtime/debug"
    "time"

    cachecontract "github.com/precision-soft/melody/v3/cache/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
)

/* NewDefaultRememberOption arms stampede protection with an unbounded wait and a cancelable flight. Cancelable is what keeps a hung callback from owning its key forever: when the last waiter leaves — by timeout, by its caller context, or by giving up — the flight's context is canceled, and the next caller of the key finds a canceled flight, replaces it, and leads a fresh computation. What the default does not bound is the wait itself: a waiter that brings neither a caller context nor a wait timeout parks for as long as the callback takes, so a callback that can hang still wants its own deadline inside, WithWaitTimeout on the waiters, or WithContext so an abandoned request takes its waiter out. */
func NewDefaultRememberOption() *RememberOption {
    defaultWaitTimeout := time.Duration(-1)

    return &RememberOption{
        enableStampedeProtection: true,
        waitTimeout:              &defaultWaitTimeout,
        isCancelable:             true,
    }
}

/* RememberOption uses constructor defaults for its exact zero value, including when a With setter is called. An explicitly set zero wait timeout means no waiting and differs from an unset timeout. Context controls this caller’s wait; cancelability controls whether the last departing waiter cancels the shared computation. */
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

/* Context governs this caller’s wait and defaults to context.Background. It does not independently cancel shared computation; only the last departing waiter can do that when cancellation is enabled. Cancellation is checked before timeout and completion. A zero wait timeout returns without waiting or consulting context; an unbounded wait can still be cancelled. */
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

/* Remember answers the cached value, computing it through the callback on a miss and storing it. The computed value is run through the backend's stored shape before it is returned, so the miss answers exactly what every later hit answers — a callback's int comes back a float64 and its struct a map, and this makes that true from the first call rather than only from the second. The cost is that the round-trip is uniform: with the default JSON serializer an integer beyond 2^53 comes back changed on the computing call as well, not only on the cached reads. Where a cached value carries an integer that large, carry a version inside it or key it so it never decodes through JSON. No major escapes this: every default serializer in the tree decodes through the same json.Unmarshal into any. */
func Remember(
    cacheInstance cachecontract.Cache,
    key string,
    ttl time.Duration,
    callback func(ctx context.Context) (any, error),
    option *RememberOption,
) (any, error) {

    if true == internal.IsNilInterface(cacheInstance) {
        return nil, exception.NewError("cache instance is nil", nil, nil)
    }

    effectiveOption := option
    if nil == effectiveOption || (RememberOption{}) == *effectiveOption {
        effectiveOption = NewDefaultRememberOption()
    }

    value, exists, getErr := cacheInstance.Get(key)
    getErr = normalizeThirdPartyError(getErr)
    if nil != getErr {

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

    setErr := normalizeThirdPartyError(cacheInstance.Set(key, computedValue, ttl))
    if nil != setErr {
        call.Complete(nil, setErr)
        return
    }

    normalizedValue, normalizeErr := normalizeRememberedValue(cacheInstance, computedValue)
    if nil != normalizeErr {
        call.Complete(nil, normalizeErr)
        return
    }

    call.Complete(normalizedValue, nil)
}

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

    setErr := normalizeThirdPartyError(cacheInstance.Set(key, value, ttl))
    if nil != setErr {
        return nil, setErr
    }

    return normalizeRememberedValue(cacheInstance, value)
}

type storedValueNormalizer interface {
    NormalizeStoredValue(value any) (any, error)
}

func normalizeRememberedValue(cacheInstance cachecontract.Cache, value any) (any, error) {
    normalizer, isNormalizer := cacheInstance.(storedValueNormalizer)
    if false == isNormalizer {
        return value, nil
    }

    return normalizer.NormalizeStoredValue(value)
}

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

    value, computeErr := callback(contextInstance)
    computeErr = normalizeThirdPartyError(computeErr)
    if nil != computeErr {
        return nil, computeErr
    }

    result = value

    return result, nil
}
