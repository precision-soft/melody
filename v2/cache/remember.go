package cache

import (
    "context"
    "fmt"
    "hash/fnv"
    "reflect"
    "time"

    cachecontract "github.com/precision-soft/melody/v2/cache/contract"
    "github.com/precision-soft/melody/v2/exception"
)

func NewDefaultRememberOption() *RememberOption {
    return &RememberOption{
        enableStampedeProtection: true,
        waitTimeout:              -1,
        isCancelable:             false,
    }
}

type RememberOption struct {
    enableStampedeProtection bool
    waitTimeout              time.Duration
    isCancelable             bool
}

func (instance *RememberOption) EnableStampedeProtection() bool {
    return instance.enableStampedeProtection
}

func (instance *RememberOption) WithStampedeProtectionEnabled(enableStampedeProtection bool) *RememberOption {
    instance.enableStampedeProtection = enableStampedeProtection
    return instance
}

func (instance *RememberOption) WaitTimeout() time.Duration {
    return instance.waitTimeout
}

func (instance *RememberOption) WithWaitTimeout(waitTimeout time.Duration) *RememberOption {
    instance.waitTimeout = waitTimeout
    return instance
}

func (instance *RememberOption) IsCancelable() bool {
    return instance.isCancelable
}

func (instance *RememberOption) WithCancelable(isCancelable bool) *RememberOption {
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
    if nil == cacheInstance {
        return nil, exception.NewError("cache instance is nil", nil, nil)
    }

    effectiveOption := option
    if nil == effectiveOption {
        effectiveOption = NewDefaultRememberOption()
    }

    value, exists, getErr := cacheInstance.Get(key)
    if nil != getErr {
        return nil, getErr
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

    singleFlightKey := buildRememberSingleFlightKey(
        cacheInstance,
        key,
        effectiveOption.IsCancelable(),
    )

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
    if true == exists {
        call.AddWaiter()
        shard.mutex.Unlock()

        defer call.RemoveWaiter()
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

    defer call.RemoveWaiter()
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
        delete(shard.inFlightByKey, singleFlightKey)
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
                "cache remember callback panicked",
                map[string]any{
                    "key":   key,
                    "panic": fmt.Sprintf("%v", recoveredValue),
                },
                nil,
            ),
        )
    }()

    existingValue, existingExists, existingGetErr := cacheInstance.Get(key)
    if nil != existingGetErr {
        call.Complete(nil, existingGetErr)
        return
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

    setErr := cacheInstance.Set(key, computedValue, ttl)
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

    setErr := cacheInstance.Set(key, value, ttl)
    if nil != setErr {
        return nil, setErr
    }

    return value, nil
}

func buildRememberSingleFlightKey(cacheInstance cachecontract.Cache, key string, isCancelable bool) string {
    cancelableSuffix := "cancelable:false"
    if true == isCancelable {
        cancelableSuffix = "cancelable:true"
    }

    if nil == cacheInstance {
        return "nil:" + key + ":" + cancelableSuffix
    }

    typeName := reflect.TypeOf(cacheInstance).String()
    value := reflect.ValueOf(cacheInstance)

    if reflect.Pointer == value.Kind() {
        return fmt.Sprintf(
            "%s:%d:%s:%s",
            typeName,
            value.Pointer(),
            key,
            cancelableSuffix,
        )
    }

    return typeName + ":value:" + key + ":" + cancelableSuffix
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

    value, computeErr := callback(contextInstance)
    if nil != computeErr {
        return nil, computeErr
    }

    result = value

    return result, nil
}
