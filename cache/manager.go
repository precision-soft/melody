package cache

import (
    "fmt"
    "sort"
    "strconv"
    "strings"
    "time"

    cachecontract "github.com/precision-soft/melody/cache/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
)

/* NewManager takes a backend it does not own: Close leaves it open, since whoever built it closes it, as the container does for a registered backend. Use NewManagerOwningBackend for the cascade. */
func NewManager(
    backend cachecontract.Backend,
    serializer cachecontract.Serializer,
) *Manager {
    return newManager(backend, serializer, false)
}

/* NewManagerOwningBackend takes a backend it closes when it is closed itself, for the caller that builds both by hand and wants one Close to end both. Do not use it for a backend that is also registered as a service: the container closes every service it created, so the backend would be closed once by this manager and once by the container. */
func NewManagerOwningBackend(
    backend cachecontract.Backend,
    serializer cachecontract.Serializer,
) *Manager {
    return newManager(backend, serializer, true)
}

func newManager(
    backend cachecontract.Backend,
    serializer cachecontract.Serializer,
    ownsBackend bool,
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
        ownsBackend: ownsBackend,
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

/* NormalizeStoredValue answers the shape a value stored through this manager reads back as, through one local serializer round-trip, so Remember's computing call and its cached calls answer one shape. */
func (instance *Manager) NormalizeStoredValue(value any) (any, error) {
    payload, serializeErr := instance.serializer.Serialize(value)
    serializeErr = normalizeThirdPartyError(serializeErr)
    if nil != serializeErr {
        return nil, serializeErr
    }

    normalized, deserializeErr := instance.serializer.Deserialize(payload)
    deserializeErr = normalizeThirdPartyError(deserializeErr)
    if nil != deserializeErr {
        return nil, deserializeErr
    }

    return normalized, nil
}

func (instance *Manager) Get(key string) (any, bool, error) {
    payload, exists, getErr := instance.backend.Get(key)
    getErr = normalizeThirdPartyError(getErr)
    if nil != getErr {
        return nil, false, getErr
    }

    if false == exists {
        return nil, false, nil
    }

    value, deserializeErr := instance.serializer.Deserialize(payload)
    deserializeErr = normalizeThirdPartyError(deserializeErr)
    if nil != deserializeErr {
        return nil, false, NewDeserializationError(
            []string{key},
            deserializeErr,
        )
    }

    return value, true, nil
}

func (instance *Manager) Set(key string, value any, ttl time.Duration) error {
    payload, serializeErr := instance.serializer.Serialize(value)
    serializeErr = normalizeThirdPartyError(serializeErr)
    if nil != serializeErr {
        return exception.NewError(
            "cache value serialization failed",
            exceptioncontract.Context{"key": key},
            serializeErr,
        )
    }

    return normalizeThirdPartyError(instance.backend.Set(key, payload, ttl))
}

func (instance *Manager) Delete(key string) error {
    return normalizeThirdPartyError(instance.backend.Delete(key))
}

func (instance *Manager) Has(key string) (bool, error) {
    hasValue, hasErr := instance.backend.Has(key)

    return hasValue, normalizeThirdPartyError(hasErr)
}

func (instance *Manager) Clear() error {
    return normalizeThirdPartyError(instance.backend.Clear())
}

/* Many answers the values of the keys that are present. An entry whose payload does not deserialize is left out as an absent key is, and its keys come back in a DeserializationError beside the values that decoded. */
func (instance *Manager) Many(keys []string) (map[string]any, error) {
    payloadsByKey, manyErr := instance.backend.Many(keys)
    manyErr = normalizeThirdPartyError(manyErr)
    if nil != manyErr {
        return nil, manyErr
    }

    result := make(map[string]any, len(payloadsByKey))

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

        value, deserializeErr := instance.serializer.Deserialize(payload)
        deserializeErr = normalizeThirdPartyError(deserializeErr)
        if nil != deserializeErr {
            corruptKeys = append(corruptKeys, key)
            if nil == firstCauseErr {
                firstCauseErr = deserializeErr
            }
            continue
        }

        result[key] = value
    }

    if 0 < len(corruptKeys) {
        return result, NewDeserializationError(corruptKeys, firstCauseErr)
    }

    return result, nil
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
func (instance *Manager) SetMultiple(items map[string]any, ttl time.Duration) error {
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

    return normalizeThirdPartyError(instance.backend.SetMultiple(payloads, ttl))
}

func (instance *Manager) DeleteMultiple(keys []string) error {
    return normalizeThirdPartyError(instance.backend.DeleteMultiple(keys))
}

/* Increment adds delta to a counter through the backend's native operation, so a distributed backend keeps it atomic. The count bypasses the serializer and is stored as decimal text, so a counter key is read with GetCounter and never mixed with Set. */
func (instance *Manager) Increment(key string, delta int64) (int64, error) {
    newValue, incrementErr := instance.backend.Increment(key, delta)

    return newValue, normalizeThirdPartyError(incrementErr)
}

/* GetCounter reads a key written by Increment or Decrement. Those operations are backend-native and store the count as decimal text rather than through the serializer, so Get would hand the raw payload to a deserializer that does not expect it. */
func (instance *Manager) GetCounter(key string) (int64, bool, error) {
    payload, exists, getErr := instance.backend.Get(key)
    getErr = normalizeThirdPartyError(getErr)
    if nil != getErr {
        return 0, false, getErr
    }

    if false == exists {
        return 0, false, nil
    }

    parsedValue, parseErr := strconv.ParseInt(strings.TrimSpace(string(payload)), 10, 64)
    if nil != parseErr {
        return 0, false, exception.NewError(
            "cache value is not a valid int64",
            exceptioncontract.Context{"key": key},
            parseErr,
        )
    }

    return parsedValue, true, nil
}

func (instance *Manager) Decrement(key string, delta int64) (int64, error) {
    newValue, decrementErr := instance.backend.Decrement(key, delta)

    return newValue, normalizeThirdPartyError(decrementErr)
}

func (instance *Manager) Close() error {
    if false == instance.ownsBackend {
        return nil
    }

    return normalizeThirdPartyError(instance.backend.Close())
}

var _ cachecontract.Cache = (*Manager)(nil)
