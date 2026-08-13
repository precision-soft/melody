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

func NewDefaultRememberOption() *RememberOption {
    defaultWaitTimeout := time.Duration(-1)

    return &RememberOption{
        enableStampedeProtection: true,
        waitTimeout:              &defaultWaitTimeout,
        isCancelable:             false,
    }
}

/* RememberOption starts from the constructor defaults wherever it is built: the fields are unexported, so outside this package the only writes are the With setters, and a setter called on the exact zero value first reads the receiver as NewDefaultRememberOption before applying its own field. Without that reading, a zero value plus one setter carried a waitTimeout of zero — every miss answered with a timeout while the callback computed in the background — and configuring cancelability alone silently disarmed the stampede protection the caller never asked to configure.

waitTimeout is a pointer so that a deliberate zero — no-wait, spelled NewDefaultRememberOption().WithWaitTimeout(0) — is told apart from the field left unspoken: a nil answers the default wait, a set pointer answers exactly what it holds. Without the distinction, a protection-off option that also asked for no wait was the zero struct itself, which the guard below reads as the constructor defaults, so the caller who spelled "no coalescing and no wait" got protection armed with an unbounded wait — the opposite, on both fields. The guard now equals the zero struct only for a value built outside the constructor, which never sets the pointer, and that value is what should read as the defaults.

The caller's context lives here rather than in a signature of its own because it is one more optional thing about this call, and because it governs the wait together with waitTimeout and isCancelable: what the three do to each other is written once, on Context below. The zero-value guard survives the interface field — comparing against the zero struct puts a nil interface on one side, so the operands never share a dynamic type and the comparison answers false instead of panicking on an uncomparable one. */
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

/* Context answers the context that governs THIS caller's wait, context.Background when none was given. It ends the wait and nothing else: the computation belongs to whoever leads the flight and is awaited by everybody coalesced onto it, so a client that disconnects takes its own caller out and leaves the value being computed for the others. What cancels the computation is still the last waiter leaving, and only on a cancelable option.

The three settings answer in this order: a canceled context ends the wait first, then the wait timeout, then the leader. A zero wait timeout means no waiting at all, so the context never gets to be consulted, and an unbounded wait — the shipped default — is exactly where a context matters most, since without one the waiter parks for as long as the callback takes however long ago its own request was abandoned. Where the callback is the one that should stop, hand its own deadline to the callback: the context it receives is the flight's, not this one. */
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

/* Remember answers the cached value, computing it through the callback on a miss and storing it. The computed value is run through the backend's stored shape before it is returned, so the miss answers exactly what every later hit answers — a callback's int comes back a float64 and its struct a map, and this makes that true from the first call rather than only from the second. The cost is that the round-trip is uniform: with the default JSON serializer an integer beyond 2^53 comes back changed on the computing call as well, not only on the cached reads. Where a cached value carries an integer that large, carry a version inside it or key it so it never decodes through JSON; the major still under development fixes this in its default serializer. */
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

    /* a cancelable call whose waiters all timed out is already doomed to a cancellation error; a late joiner must not inherit that poison, so it replaces the entry and becomes a fresh leader. */
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

/* the callback runs under the CALLER's context here, unlike the coalesced path: without a flight there is nobody else waiting on this computation, so ending it ends nothing that anyone still wants. */
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

/* storedValueNormalizer is the optional door through which Remember learns what shape a stored value reads back as; the cache manager implements it with one local serializer round-trip. */
type storedValueNormalizer interface {
    NormalizeStoredValue(value any) (any, error)
}

/* normalizeRememberedValue makes the computing call answer the exact shape every cached call will answer: without it one key had two shapes — the callback's own value on the miss, the decoded generic form on every hit — so a type assertion worked on the cold path and failed on the warm one, from the second call on. A cache that does not expose its stored shape answers the callback's value unchanged. */
func normalizeRememberedValue(cacheInstance cachecontract.Cache, value any) (any, error) {
    normalizer, isNormalizer := cacheInstance.(storedValueNormalizer)
    if false == isNormalizer {
        return value, nil
    }

    return normalizer.NormalizeStoredValue(value)
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

    /* the panic value travels as the CAUSE and the stack is captured here, on the goroutine whose frames raised it. Stringified into the context alone, an error-shaped panic collapsed to its bare message: the context naming the parameter it could not read, the chain naming the connection that refused, and any file and line were all gone, so a handler's own bug reached the operator as a message and a cache key. */
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

    /* a typed-nil error from the callback reads as the success it means: boxed into a non-nil interface it would be memoized as the flight's failure, handed to every waiter, and would panic the first one that renders it */
    value, computeErr := callback(contextInstance)
    computeErr = normalizeThirdPartyError(computeErr)
    if nil != computeErr {
        return nil, computeErr
    }

    result = value

    return result, nil
}
