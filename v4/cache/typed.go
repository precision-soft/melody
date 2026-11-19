package cache

import (
    "context"

    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
)

/* Get answers the value stored under key as a T: the zero T and false on a miss, the zero T and the error on a failure, a payload that does not decode into T answered as a DeserializationError. Get[any] answers the generic form of the store's encoding. */
func Get[T any](ctx context.Context, cacheInstance cachecontract.Cache, key string) (T, bool, error) {
    var zero T
    if true == internal.IsNilInterface(cacheInstance) {
        return zero, false, exception.NewError("cache instance is nil", nil, nil)
    }

    var value T
    exists, getErr := cacheInstance.Get(ctx, key, &value)
    getErr = normalizeThirdPartyError(getErr)
    if nil != getErr {
        return zero, false, getErr
    }

    if false == exists {
        return zero, false, nil
    }

    return value, true, nil
}

/* Many answers the present, decodable entries of keys as a map of T, never nil once the cache was read; the keys that did not decode come back in a DeserializationError beside the entries that did. */
func Many[T any](ctx context.Context, cacheInstance cachecontract.Cache, keys []string) (map[string]T, error) {
    if true == internal.IsNilInterface(cacheInstance) {
        return nil, exception.NewError("cache instance is nil", nil, nil)
    }

    var result map[string]T
    manyErr := normalizeThirdPartyError(cacheInstance.Many(ctx, keys, &result))
    if nil == result && (nil == manyErr || true == IsDeserializationError(manyErr)) {
        result = map[string]T{}
    }

    return result, manyErr
}
