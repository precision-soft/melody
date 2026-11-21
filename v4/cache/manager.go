package cache

import (
    "context"
    "fmt"
    "reflect"
    "sort"

    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* ManagerConfig is what a manager declares beyond its backend and its serializer; the zero value is the default. */
type ManagerConfig struct {
    /* OwnsBackend closes the backend when the manager is closed, for the caller that builds both by hand and wants one CloseWithContext to end both. Off, CloseWithContext leaves the backend open, since whoever built it closes it. Leave it off for a backend that is also registered as a service: the container closes every service it created, so the backend would be closed once by this manager and once by the container. */
    OwnsBackend bool
}

/* NewManager builds the manager over a backend and a serializer, both required and refused when nil. */
func NewManager(
    backend cachecontract.Backend,
    serializer cachecontract.Serializer,
    config ManagerConfig,
) *Manager {
    if true == internal.IsNilInterface(backend) {
        exception.Panic(exception.NewError("cache backend is nil", nil, nil))
    }

    if true == internal.IsNilInterface(serializer) {
        exception.Panic(exception.NewError("cache serializer is nil", nil, nil))
    }

    return &Manager{
        backend:     backend,
        serializer:  serializer,
        ownsBackend: config.OwnsBackend,
    }
}

type Manager struct {
    backend     cachecontract.Backend
    serializer  cachecontract.Serializer
    ownsBackend bool
}

/* normalizeThirdPartyError reads the error through the interface, since a typed nil boxed into a non-nil interface is no failure and would panic the first caller that renders it. */
func normalizeThirdPartyError(err error) error {
    if true == internal.IsNilInterface(err) {
        return nil
    }

    return err
}

/* a refused target is a programming error, so it is no DeserializationError and Remember does not recompute over it */
func resolveReadTarget(target any, key string) (reflect.Value, error) {
    targetValue := reflect.ValueOf(target)
    if nil == target || reflect.Pointer != targetValue.Kind() || true == targetValue.IsNil() {
        return reflect.Value{}, exception.NewError(
            "cache read target is not a non-nil pointer",
            exceptioncontract.Context{"key": key, "targetType": fmt.Sprintf("%T", target)},
            nil,
        )
    }

    return targetValue, nil
}

func resolveBatchReadTarget(target any) (reflect.Value, error) {
    targetValue := reflect.ValueOf(target)
    if nil == target || reflect.Pointer != targetValue.Kind() || true == targetValue.IsNil() || reflect.Map != targetValue.Elem().Kind() || reflect.String != targetValue.Elem().Type().Key().Kind() {
        return reflect.Value{}, exception.NewError(
            "cache batch read target is not a pointer to a map keyed by string",
            exceptioncontract.Context{"targetType": fmt.Sprintf("%T", target)},
            nil,
        )
    }

    return targetValue, nil
}

/* a value of its own, so a failed decode leaves the caller's target untouched and a successful one never merges into what it held */
func (instance *Manager) decodeFresh(payload []byte, valueType reflect.Type) (reflect.Value, error) {
    fresh := reflect.New(valueType)
    deserializeErr := normalizeThirdPartyError(instance.serializer.Deserialize(payload, fresh.Interface()))

    return fresh.Elem(), deserializeErr
}

/* RoundTrip writes into target what value reads back as through this manager, one local serializer round trip with the backend untouched; either half's refusal is answered as the serializer gave it. */
func (instance *Manager) RoundTrip(value any, target any) error {
    targetValue, targetErr := resolveReadTarget(target, "")
    if nil != targetErr {
        return targetErr
    }

    payload, serializeErr := instance.serializer.Serialize(value)
    serializeErr = normalizeThirdPartyError(serializeErr)
    if nil != serializeErr {
        return serializeErr
    }

    fresh, deserializeErr := instance.decodeFresh(payload, targetValue.Elem().Type())
    if nil != deserializeErr {
        return deserializeErr
    }

    targetValue.Elem().Set(fresh)

    return nil
}

func (instance *Manager) Get(runtimeInstance containercontract.Runtime, key string, target any) (bool, error) {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != nilErr {
        return false, nilErr
    }

    targetValue, targetErr := resolveReadTarget(target, key)
    if nil != targetErr {
        return false, targetErr
    }

    payload, exists, getErr := instance.backend.Get(runtimeInstance, key)
    getErr = normalizeThirdPartyError(getErr)
    if nil != getErr {
        return false, getErr
    }

    if false == exists {
        return false, nil
    }

    fresh, deserializeErr := instance.decodeFresh(payload, targetValue.Elem().Type())
    if nil != deserializeErr {
        return false, NewDeserializationError(
            []string{key},
            deserializeErr,
        )
    }

    targetValue.Elem().Set(fresh)

    return true, nil
}

func (instance *Manager) Set(runtimeInstance containercontract.Runtime, key string, value any, ttl bound.Duration) error {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != nilErr {
        return nilErr
    }

    payload, serializeErr := instance.serializer.Serialize(value)
    serializeErr = normalizeThirdPartyError(serializeErr)
    if nil != serializeErr {
        return exception.NewError(
            "cache value serialization failed",
            exceptioncontract.Context{"key": key},
            serializeErr,
        )
    }

    return normalizeThirdPartyError(instance.backend.Set(runtimeInstance, key, payload, ttl))
}

func (instance *Manager) Delete(runtimeInstance containercontract.Runtime, key string) error {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != nilErr {
        return nilErr
    }

    return normalizeThirdPartyError(instance.backend.Delete(runtimeInstance, key))
}

func (instance *Manager) Has(runtimeInstance containercontract.Runtime, key string) (bool, error) {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != nilErr {
        return false, nilErr
    }

    hasValue, hasErr := instance.backend.Has(runtimeInstance, key)

    return hasValue, normalizeThirdPartyError(hasErr)
}

func (instance *Manager) Clear(runtimeInstance containercontract.Runtime) error {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != nilErr {
        return nilErr
    }

    return normalizeThirdPartyError(instance.backend.Clear(runtimeInstance))
}

/* Many replaces the target's map with the entries of the keys that are present and decode. An entry whose payload does not decode is left out as an absent key is, and its keys come back in a DeserializationError beside the entries that decoded; a backend failure leaves the target untouched. */
func (instance *Manager) Many(runtimeInstance containercontract.Runtime, keys []string, target any) error {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != nilErr {
        return nilErr
    }

    targetValue, targetErr := resolveBatchReadTarget(target)
    if nil != targetErr {
        return targetErr
    }

    payloadsByKey, manyErr := instance.backend.Many(runtimeInstance, keys)
    manyErr = normalizeThirdPartyError(manyErr)
    if nil != manyErr {
        return manyErr
    }

    mapType := targetValue.Elem().Type()
    result := reflect.MakeMapWithSize(mapType, len(payloadsByKey))

    corruptKeys := make([]string, 0)
    var firstCauseErr error

    processed := make(map[string]struct{}, len(keys))
    for _, key := range keys {
        if _, alreadyProcessed := processed[key]; true == alreadyProcessed {
            continue
        }
        processed[key] = struct{}{}

        payload, exists := payloadsByKey[key]
        if false == exists {
            continue
        }

        fresh, deserializeErr := instance.decodeFresh(payload, mapType.Elem())
        if nil != deserializeErr {
            corruptKeys = append(corruptKeys, key)
            if nil == firstCauseErr {
                firstCauseErr = deserializeErr
            }
            continue
        }

        result.SetMapIndex(reflect.ValueOf(key).Convert(mapType.Key()), fresh)
    }

    targetValue.Elem().Set(result)

    if 0 < len(corruptKeys) {
        return NewDeserializationError(corruptKeys, firstCauseErr)
    }

    return nil
}

/* serializationErrorText reads the text of a serializer's refusal under a recover, since Serializer is a public contract and the application's Error() may panic; the marker takes the shape the other foreign-error readers use. */
func serializationErrorText(err error) (text string) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        text = fmt.Sprintf("serialization error message panicked: %v", recoveredValue)
    }()

    return err.Error()
}

/* SetMultiple serializes every entry before the backend sees any, so a refusal writes nothing. The refusal names every refused key under "keys", sorted, with "key" the first of them, and each key's own reason under "causeByKey"; every entry is serialized even after the first refusal, so the list is whole. */
func (instance *Manager) SetMultiple(runtimeInstance containercontract.Runtime, items map[string]any, ttl bound.Duration) error {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != nilErr {
        return nilErr
    }

    payloads := make(map[string][]byte, len(items))
    var refusedKeys []string
    var refusalByKey map[string]error
    for key, value := range items {
        payload, serializeErr := instance.serializer.Serialize(value)
        serializeErr = normalizeThirdPartyError(serializeErr)
        if nil != serializeErr {
            if nil == refusalByKey {
                refusalByKey = make(map[string]error)
            }
            refusedKeys = append(refusedKeys, key)
            refusalByKey[key] = serializeErr
            continue
        }

        payloads[key] = payload
    }

    if 0 < len(refusedKeys) {
        sort.Strings(refusedKeys)

        causeByKey := make(map[string]string, len(refusedKeys))
        for _, refusedKey := range refusedKeys {
            causeByKey[refusedKey] = serializationErrorText(refusalByKey[refusedKey])
        }

        return exception.NewError(
            "cache value serialization failed",
            exceptioncontract.Context{"key": refusedKeys[0], "keys": refusedKeys, "causeByKey": causeByKey},
            refusalByKey[refusedKeys[0]],
        )
    }

    return normalizeThirdPartyError(instance.backend.SetMultiple(runtimeInstance, payloads, ttl))
}

func (instance *Manager) DeleteMultiple(runtimeInstance containercontract.Runtime, keys []string) error {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != nilErr {
        return nilErr
    }

    return normalizeThirdPartyError(instance.backend.DeleteMultiple(runtimeInstance, keys))
}

/* Increment adds delta to a counter through the backend's native operation, so a distributed backend keeps it atomic, the counter's ttl written in the same operation as options says. The count bypasses the serializer and is stored as decimal text, so a counter key is read with GetCounter and never mixed with Set. */
func (instance *Manager) Increment(runtimeInstance containercontract.Runtime, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != nilErr {
        return 0, nilErr
    }

    newValue, incrementErr := instance.backend.Increment(runtimeInstance, key, delta, options)

    return newValue, normalizeThirdPartyError(incrementErr)
}

/* GetCounter reads a key written by Increment or Decrement. Those operations are backend-native and store the count as decimal text rather than through the serializer, so Get would hand the raw payload to a deserializer that does not expect it. */
func (instance *Manager) GetCounter(runtimeInstance containercontract.Runtime, key string) (int64, bool, error) {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != nilErr {
        return 0, false, nilErr
    }

    payload, exists, getErr := instance.backend.Get(runtimeInstance, key)
    getErr = normalizeThirdPartyError(getErr)
    if nil != getErr {
        return 0, false, getErr
    }

    if false == exists {
        return 0, false, nil
    }

    parsedValue, parseErr := parseCanonicalCounterPayload(string(payload))
    if nil != parseErr {
        return 0, false, exception.NewError(
            "cache value is not a valid int64",
            exceptioncontract.Context{
                "key":           key,
                "payloadLength": len(payload),
            },
            counterParseFailure(parseErr),
        )
    }

    return parsedValue, true, nil
}

func (instance *Manager) Decrement(runtimeInstance containercontract.Runtime, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != nilErr {
        return 0, nilErr
    }

    newValue, decrementErr := instance.backend.Decrement(runtimeInstance, key, delta, options)

    return newValue, normalizeThirdPartyError(decrementErr)
}

/* CloseWithContext closes the backend the manager owns under the teardown's deadline; a backend built by someone else is closed by whoever built it. */
func (instance *Manager) CloseWithContext(closeContext context.Context) error {
    if false == instance.ownsBackend {
        return nil
    }

    return normalizeThirdPartyError(instance.backend.CloseWithContext(closeContext))
}

var _ cachecontract.Cache = (*Manager)(nil)
