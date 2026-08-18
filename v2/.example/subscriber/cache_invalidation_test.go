package subscriber

import (
    "errors"
    "testing"
    "time"

    cachecontract "github.com/precision-soft/melody/v2/cache/contract"
)

type recordingCache struct {
    deleted []string
    failOn  map[string]error
}

func (instance *recordingCache) Get(key string) (any, bool, error) {
    return nil, false, nil
}

func (instance *recordingCache) Set(key string, value any, ttl time.Duration) error {
    return nil
}

func (instance *recordingCache) Delete(key string) error {
    instance.deleted = append(instance.deleted, key)

    if failure, exists := instance.failOn[key]; true == exists {
        return failure
    }

    return nil
}

func (instance *recordingCache) Has(key string) (bool, error) {
    return false, nil
}

func (instance *recordingCache) Clear() error {
    return nil
}

func (instance *recordingCache) Many(keys []string) (map[string]any, error) {
    return map[string]any{}, nil
}

func (instance *recordingCache) SetMultiple(items map[string]any, ttl time.Duration) error {
    return nil
}

func (instance *recordingCache) DeleteMultiple(keys []string) error {
    return nil
}

func (instance *recordingCache) Increment(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *recordingCache) Decrement(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *recordingCache) Close() error {
    return nil
}

var _ cachecontract.Cache = (*recordingCache)(nil)

/* the early return this replaces skipped every delete after the first failure — the list entry above all — leaving a ttl-less cache serving a catalogue the database no longer holds */
func TestDeleteCacheEntriesAttemptsEveryKeyDespiteAFailure(t *testing.T) {
    firstFailure := errors.New("first delete refused")
    cacheInstance := &recordingCache{failOn: map[string]error{"first": firstFailure}}

    joinedErr := deleteCacheEntries(cacheInstance, "first", "second", "third")
    if nil == joinedErr {
        t.Fatal("expected the failure to surface")
    }

    if false == errors.Is(joinedErr, firstFailure) {
        t.Fatalf("expected the first failure inside the joined answer, got %v", joinedErr)
    }

    if 3 != len(cacheInstance.deleted) {
        t.Fatalf("expected every key attempted, got %v", cacheInstance.deleted)
    }
}

func TestDeleteCacheEntriesSkipsBlankKeys(t *testing.T) {
    cacheInstance := &recordingCache{}

    if deleteErr := deleteCacheEntries(cacheInstance, "", "kept"); nil != deleteErr {
        t.Fatalf("unexpected error: %v", deleteErr)
    }

    if 1 != len(cacheInstance.deleted) || "kept" != cacheInstance.deleted[0] {
        t.Fatalf("expected only the non-blank key attempted, got %v", cacheInstance.deleted)
    }
}
