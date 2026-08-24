package cache

import (
    "strconv"
    "strings"
    "time"

    cachecontract "github.com/precision-soft/melody/v3/cache/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
)

/* NewManager takes a backend it does not own: Close leaves it open, because a backend handed in was built by someone else and is closed by whoever built it. That is what the container path needs — the backend is a registered service the container closes itself, so a manager that closed it too would close it twice, which a backend wrapping a connection typically reports as a failure on the second call and turns a clean shutdown into a reported one. Use NewManagerOwningBackend to get the cascade back. */
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

/* normalizeThirdPartyError reads the error through the interface: a backend or serializer declared with a concrete error type hands back a typed nil boxed into a non-nil interface, which would be treated as the failure it is not — and would panic the first caller that renders it. */
func normalizeThirdPartyError(err error) error {
    if true == internal.IsNilInterface(err) {
        return nil
    }

    return err
}

/* NormalizeStoredValue answers the shape a value stored through this manager reads back as: one serializer round-trip, run locally with no backend involved. Remember consults it so the computing call and the cached calls answer one shape — without it, a callback's int came back float64 and its struct came back a map, but only from the second call on. */
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

/* an entry whose payload does not deserialize is left out of the result the way an absent key is — Get answers the same entry with exists false — and the keys it happened under come back in a DeserializationError beside the values that did decode, so one corrupt entry no longer discards the whole answer and the error names its culprits deterministically. */
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

func (instance *Manager) SetMultiple(items map[string]any, ttl time.Duration) error {
    payloads := make(map[string][]byte, len(items))
    for key, value := range items {
        payload, serializeErr := instance.serializer.Serialize(value)
        serializeErr = normalizeThirdPartyError(serializeErr)
        if nil != serializeErr {
            return exception.NewError(
                "cache value serialization failed",
                exceptioncontract.Context{"key": key},
                serializeErr,
            )
        }

        payloads[key] = payload
    }

    return normalizeThirdPartyError(instance.backend.SetMultiple(payloads, ttl))
}

func (instance *Manager) DeleteMultiple(keys []string) error {
    return normalizeThirdPartyError(instance.backend.DeleteMultiple(keys))
}

/* the counter operations are backend-native so a distributed backend keeps them atomic, which means they bypass the serializer and store the count as decimal text; a counter key must therefore be read with GetCounter rather than Get, and must not be mixed with Set on the same key */
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
