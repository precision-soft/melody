package cache

import (
    "context"
    "fmt"
    "hash/fnv"
    "reflect"
    "time"

    cachecontract "github.com/precision-soft/melody/cache/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
)

func NewDefaultRememberOption() *RememberOption {
    return &RememberOption{
        enableStampedeProtection: true,
        waitTimeout:              -1,
        isCancelable:             false,
    }
}

/* RememberOption starts from the constructor defaults wherever it is built: the fields are unexported, so outside this package the only writes are the With setters, and a setter called on the exact zero value first reads the receiver as NewDefaultRememberOption before applying its own field. Without that reading, a zero value plus one setter carried a waitTimeout of zero — every miss answered with a timeout while the callback computed in the background — and configuring cancelability alone silently disarmed the stampede protection the caller never asked to configure. A deliberate no-wait or protection-off option is spelled through the constructor: NewDefaultRememberOption().WithWaitTimeout(0). */
type RememberOption struct {
    enableStampedeProtection bool
    waitTimeout              time.Duration
    isCancelable             bool
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
    return instance.waitTimeout
}

func (instance *RememberOption) WithWaitTimeout(waitTimeout time.Duration) *RememberOption {
    instance.normalizeZeroReceiver()
    instance.waitTimeout = waitTimeout
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

func Remember(
    cacheInstance cachecontract.Cache,
    key string,
    ttl time.Duration,
    callback func(ctx context.Context) (any, error),
    option *RememberOption,
) (any, error) {
    /* the guard reads through the interface: a typed-nil Cache is a non-nil interface that would pass a plain comparison and panic on the first method call, on the request path, in place of the error this refusal promises */
    if true == internal.IsNilInterface(cacheInstance) {
        return nil, exception.NewError("cache instance is nil", nil, nil)
    }

    /* the zero-value option is constructible from outside the package and would silently disarm the stampede protection it never asked to configure; it reads as the constructor defaults instead. A deliberate protection-off option built through the constructor carries waitTimeout -1 and stays what it says. */
    effectiveOption := option
    if nil == effectiveOption || (RememberOption{}) == *effectiveOption {
        effectiveOption = NewDefaultRememberOption()
    }

    value, exists, getErr := cacheInstance.Get(key)
    getErr = normalizeThirdPartyError(getErr)
    if nil != getErr {
        /* a payload the serializer cannot decode is a miss, not a failure: the callback recomputes and its Set overwrites the corrupt payload, so the key heals instead of staying poisoned until the entry lapses — which a ttl of zero postpones forever. Every other error keeps meaning the cache itself failed. */
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
    callback func(ctx context.Context) (any, error),
) (any, error) {
    shard := getRememberInFlightShard(singleFlightKey)

    shard.mutex.Lock()

    call, exists := shard.inFlightByKey[singleFlightKey]

    /* a cancelable call whose waiters all timed out is already doomed to a cancellation error; a late joiner must not inherit that poison, so it replaces the entry and becomes a fresh leader. */
    if true == exists && true == call.IsCanceled() {
        exists = false
    }

    if true == exists {
        call.AddWaiter()
        shard.mutex.Unlock()

        defer call.RemoveWaiter(shard)
        return call.Wait(waitTimeout, key)
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
    return call.Wait(waitTimeout, key)
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

    /* the callback's own panics are already recovered inside executeRememberCallbackSafely, so what this recover catches is the cache side — a Get or Set that panicked — and the fabricated error says so, instead of sending the diagnosis after a callback that never misbehaved */
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
                    "key":   key,
                    "panic": fmt.Sprintf("%v", recoveredValue),
                },
                nil,
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

    call.Complete(computedValue, nil)
}

func rememberWithoutStampedeProtection(
    cacheInstance cachecontract.Cache,
    key string,
    ttl time.Duration,
    callback func(ctx context.Context) (any, error),
) (any, error) {
    value, callbackErr := executeRememberCallbackSafely(
        context.Background(),
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

    return value, nil
}

/* rememberSingleFlightKey names the unit callers coalesce under: one cache instance, one key, one cancelability. The instance is told apart by its pointer, so only pointer-kind implementations coalesce — a value-kind Cache has no address to tell two instances apart, and one shared flight would hand a caller the value computed for somebody else's cache, so a value-kind instance gets no coalescing at all, which costs the stampede optimization and never the answer. Two managers over one backend are two units on purpose: the unit is what Remember was handed, not what stands behind it. */
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
                "key":   key,
                "panic": fmt.Sprintf("%v", recoveredValue),
            },
            nil,
        )

        result = nil
    }()

    /* a typed-nil error from the callback reads as the success it means: boxed into a non-nil interface it would be memoized as the flight's failure, handed to every waiter, and would panic the first one that renders it */
    value, computeErr := callback(contextInstance)
    computeErr = normalizeThirdPartyError(computeErr)
    if nil != computeErr {
        return nil, computeErr
    }

    result = value

    return result, nil
}
