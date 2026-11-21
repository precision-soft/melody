package cache

/* Get answers the value stored under key as a T: the zero T and false on a miss, the zero T and the error on a failure, a payload that does not decode into T answered as a DeserializationError. Get[any] answers the generic form of the store's encoding. */
func Get[T any](boundCache Bound, key string) (T, bool, error) {
    var zero T

    var value T
    exists, getErr := boundCache.Get(key, &value)
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
func Many[T any](boundCache Bound, keys []string) (map[string]T, error) {
    var result map[string]T
    manyErr := normalizeThirdPartyError(boundCache.Many(keys, &result))
    if nil == result && (nil == manyErr || true == IsDeserializationError(manyErr)) {
        result = map[string]T{}
    }

    return result, manyErr
}
