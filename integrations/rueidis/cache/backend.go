package cache

import (
    "context"
    "strconv"
    "strings"
    "time"

    cachecontract "github.com/precision-soft/melody/cache/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/redis/rueidis"
)

const (
    rueidisBackendDefaultScanCount    = 1000
    rueidisBackendDefaultDeleteBatch  = 500
    rueidisBackendDefaultMaxKeyLength = 1024
)

func NewBackend(
    client rueidis.Client,
    ctx context.Context,
    prefix string,
    scanCount int,
    deleteBatch int,
) (*Backend, error) {
    if nil == client {
        return nil, exception.NewError(
            "redis client is nil",
            nil,
            nil,
        )
    }

    if nil == ctx {
        ctx = context.Background()
    }

    normalizedPrefix := prefix
    if "" == normalizedPrefix {
        normalizedPrefix = "melody:cache:"
    }

    normalizedScanCount := scanCount
    if 0 >= normalizedScanCount {
        normalizedScanCount = rueidisBackendDefaultScanCount
    }

    normalizedDeleteBatch := deleteBatch
    if 0 >= normalizedDeleteBatch {
        normalizedDeleteBatch = rueidisBackendDefaultDeleteBatch
    }

    return &Backend{
        client:      client,
        ctx:         ctx,
        prefix:      normalizedPrefix,
        scanCount:   normalizedScanCount,
        deleteBatch: normalizedDeleteBatch,
    }, nil
}

type Backend struct {
    client      rueidis.Client
    ctx         context.Context
    prefix      string
    scanCount   int
    deleteBatch int
}

func (instance *Backend) Get(key string) ([]byte, bool, error) {
    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return nil, false, normalizeErr
    }

    response := instance.client.Do(
        instance.ctx,
        instance.client.B().Get().Key(normalizedKey).Build(),
    )
    if err := response.Error(); nil != err {
        if true == rueidis.IsRedisNil(err) {
            return nil, false, nil
        }

        return nil, false, err
    }

    payload, err := response.AsBytes()
    if nil != err {
        return nil, false, err
    }

    return payload, true, nil
}

func (instance *Backend) Set(key string, payload []byte, ttl time.Duration) error {
    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return normalizeErr
    }

    var command rueidis.Completed
    if 0 < ttl {
        command = instance.client.B().Set().Key(normalizedKey).Value(rueidis.BinaryString(payload)).Px(ttl).Build()
    } else {
        command = instance.client.B().Set().Key(normalizedKey).Value(rueidis.BinaryString(payload)).Build()
    }

    return instance.client.Do(
        instance.ctx,
        command,
    ).Error()
}

func (instance *Backend) Delete(key string) error {
    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return normalizeErr
    }

    return instance.client.Do(
        instance.ctx,
        instance.client.B().Del().Key(normalizedKey).Build(),
    ).Error()
}

func (instance *Backend) Has(key string) (bool, error) {
    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return false, normalizeErr
    }

    response := instance.client.Do(
        instance.ctx,
        instance.client.B().Exists().Key(normalizedKey).Build(),
    )
    if err := response.Error(); nil != err {
        return false, err
    }

    count, err := response.AsInt64()
    if nil != err {
        return false, err
    }

    return 0 != count, nil
}

func (instance *Backend) Clear() error {
    pattern := instance.prefix + "*"
    keys, scanErr := instance.scanKeys(instance.ctx, pattern)
    if nil != scanErr {
        return scanErr
    }

    if 0 == len(keys) {
        return nil
    }

    return instance.deleteKeysInBatches(instance.ctx, keys)
}

func (instance *Backend) ClearByPrefix(prefix string) error {
    if "" == prefix {
        return instance.Clear()
    }

    normalizedPrefix, normalizeErr := instance.normalizeKey(prefix)
    if nil != normalizeErr {
        return normalizeErr
    }

    pattern := normalizedPrefix + "*"
    keys, scanErr := instance.scanKeys(instance.ctx, pattern)
    if nil != scanErr {
        return scanErr
    }

    if 0 == len(keys) {
        return nil
    }

    return instance.deleteKeysInBatches(instance.ctx, keys)
}

func (instance *Backend) Many(keys []string) (map[string][]byte, error) {
    result := make(map[string][]byte, len(keys))
    if 0 == len(keys) {
        return result, nil
    }

    normalizedKeys := make([]string, 0, len(keys))
    for _, key := range keys {
        normalizedKey, normalizeErr := instance.normalizeKey(key)
        if nil != normalizeErr {
            return nil, normalizeErr
        }

        normalizedKeys = append(normalizedKeys, normalizedKey)
    }

    values, err := rueidis.MGet(
        instance.client,
        instance.ctx,
        normalizedKeys,
    )
    if nil != err {
        return nil, err
    }

    for fullKey, message := range values {
        if true == message.IsNil() {
            continue
        }

        payload, payloadErr := message.AsBytes()
        if nil != payloadErr {
            return nil, payloadErr
        }

        originalKey := instance.stripPrefix(fullKey)
        result[originalKey] = payload
    }

    return result, nil
}

func (instance *Backend) SetMultiple(items map[string][]byte, ttl time.Duration) error {
    if 0 == len(items) {
        return nil
    }

    cmds := make(rueidis.Commands, 0, len(items))
    for key, payload := range items {
        normalizedKey, normalizeErr := instance.normalizeKey(key)
        if nil != normalizeErr {
            return normalizeErr
        }

        var command rueidis.Completed
        if 0 < ttl {
            command = instance.client.B().Set().Key(normalizedKey).Value(rueidis.BinaryString(payload)).Px(ttl).Build()
        } else {
            command = instance.client.B().Set().Key(normalizedKey).Value(rueidis.BinaryString(payload)).Build()
        }

        cmds = append(cmds, command)
    }

    for _, response := range instance.client.DoMulti(instance.ctx, cmds...) {
        if err := response.Error(); nil != err {
            return err
        }
    }

    return nil
}

func (instance *Backend) DeleteMultiple(keys []string) error {
    if 0 == len(keys) {
        return nil
    }

    normalizedKeys := make([]string, 0, len(keys))
    for _, key := range keys {
        normalizedKey, normalizeErr := instance.normalizeKey(key)
        if nil != normalizeErr {
            return normalizeErr
        }

        normalizedKeys = append(normalizedKeys, normalizedKey)
    }

    deleteErrors := rueidis.MDel(
        instance.client,
        instance.ctx,
        normalizedKeys,
    )
    for _, deleteErr := range deleteErrors {
        if nil != deleteErr {
            return deleteErr
        }
    }

    return nil
}

func (instance *Backend) Increment(key string, delta int64) (int64, error) {
    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return 0, normalizeErr
    }

    response := instance.client.Do(
        instance.ctx,
        instance.client.B().Incrby().Key(normalizedKey).Increment(delta).Build(),
    )
    if err := response.Error(); nil != err {
        return 0, err
    }

    value, err := response.AsInt64()
    if nil != err {
        return 0, err
    }

    return value, nil
}

func (instance *Backend) Decrement(key string, delta int64) (int64, error) {
    normalizedKey, normalizeErr := instance.normalizeKey(key)
    if nil != normalizeErr {
        return 0, normalizeErr
    }

    response := instance.client.Do(
        instance.ctx,
        instance.client.B().Decrby().Key(normalizedKey).Decrement(delta).Build(),
    )
    if err := response.Error(); nil != err {
        return 0, err
    }

    value, err := response.AsInt64()
    if nil != err {
        return 0, err
    }

    return value, nil
}

func (instance *Backend) Close() error {
    instance.client.Close()
    return nil
}

func (instance *Backend) normalizeKey(key string) (string, error) {
    if "" == key {
        return "", exception.NewError(
            "cache key is empty",
            nil,
            nil,
        )
    }

    if true == strings.Contains(key, " ") {
        return "", exception.NewError(
            "cache key contains spaces",
            nil,
            nil,
        )
    }

    if true == strings.Contains(key, "\n") {
        return "", exception.NewError(
            "cache key contains newlines",
            nil,
            nil,
        )
    }

    if rueidisBackendDefaultMaxKeyLength < len(key) {
        return "", exception.NewError(
            "cache key is too long",
            exceptioncontract.Context{
                "maxKeyLength": rueidisBackendDefaultMaxKeyLength,
                "keyLength":    len(key),
            },
            nil,
        )
    }

    return instance.prefix + key, nil
}

func (instance *Backend) stripPrefix(fullKey string) string {
    return strings.TrimPrefix(fullKey, instance.prefix)
}

func (instance *Backend) scanKeys(ctx context.Context, pattern string) ([]string, error) {
    cursor := uint64(0)
    keys := make([]string, 0)

    for {
        response := instance.client.Do(
            ctx,
            instance.client.B().Scan().Cursor(cursor).Match(pattern).Count(int64(instance.scanCount)).Build(),
        )
        if err := response.Error(); nil != err {
            return nil, err
        }

        array, arrayErr := response.ToArray()
        if nil != arrayErr {
            return nil, arrayErr
        }

        if 2 != len(array) {
            return nil, exception.NewError(
                "unexpected scan response length",
                exceptioncontract.Context{
                    "length": len(array),
                },
                nil,
            )
        }

        cursorString, cursorErr := array[0].ToString()
        if nil != cursorErr {
            return nil, cursorErr
        }

        parsedCursor, parseErr := strconv.ParseUint(cursorString, 10, 64)
        if nil != parseErr {
            return nil, parseErr
        }
        cursor = parsedCursor

        keysArray, keysArrayErr := array[1].ToArray()
        if nil != keysArrayErr {
            return nil, keysArrayErr
        }

        for _, keyMessage := range keysArray {
            keyString, keyErr := keyMessage.ToString()
            if nil != keyErr {
                return nil, keyErr
            }
            if "" == keyString {
                continue
            }

            keys = append(keys, keyString)
        }

        if 0 == cursor {
            break
        }
    }

    return keys, nil
}

func (instance *Backend) deleteKeysInBatches(ctx context.Context, keys []string) error {
    if 0 == len(keys) {
        return nil
    }

    for startIndex := 0; startIndex < len(keys); startIndex += instance.deleteBatch {
        endIndex := startIndex + instance.deleteBatch
        if endIndex > len(keys) {
            endIndex = len(keys)
        }

        batch := keys[startIndex:endIndex]
        deleteErrors := rueidis.MDel(instance.client, ctx, batch)
        for _, deleteErr := range deleteErrors {
            if nil != deleteErr {
                return deleteErr
            }
        }
    }

    return nil
}

var _ cachecontract.Backend = (*Backend)(nil)
