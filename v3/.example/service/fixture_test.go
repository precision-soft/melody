package service

import (
    "sync"
    "time"

    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* ttlRecordingCache answers what it was given and remembers under which ttl each write landed, which is
   the whole property under test: the doors cannot be asked "for how long" any other way. It keeps values
   as they are, because these probes are not about serialization. */
type ttlRecordingCache struct {
    mutex  sync.Mutex
    values map[string]any
    writes []cacheWrite
}

type cacheWrite struct {
    key string
    ttl time.Duration
}

func newTtlRecordingCache() *ttlRecordingCache {
    return &ttlRecordingCache{values: map[string]any{}}
}

func (instance *ttlRecordingCache) writesFor(key string) []cacheWrite {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    matched := make([]cacheWrite, 0, len(instance.writes))
    for _, write := range instance.writes {
        if key == write.key {
            matched = append(matched, write)
        }
    }

    return matched
}

func (instance *ttlRecordingCache) Get(key string) (any, bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    value, exists := instance.values[key]

    return value, exists, nil
}

func (instance *ttlRecordingCache) Set(key string, value any, ttl time.Duration) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.values[key] = value
    instance.writes = append(instance.writes, cacheWrite{key: key, ttl: ttl})

    return nil
}

func (instance *ttlRecordingCache) Delete(key string) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    delete(instance.values, key)

    return nil
}

func (instance *ttlRecordingCache) Has(key string) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    _, exists := instance.values[key]

    return exists, nil
}

func (instance *ttlRecordingCache) Clear() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.values = map[string]any{}

    return nil
}

func (instance *ttlRecordingCache) Many(keys []string) (map[string]any, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    result := map[string]any{}
    for _, key := range keys {
        if value, exists := instance.values[key]; true == exists {
            result[key] = value
        }
    }

    return result, nil
}

func (instance *ttlRecordingCache) SetMultiple(items map[string]any, ttl time.Duration) error {
    for key, value := range items {
        if setErr := instance.Set(key, value, ttl); nil != setErr {
            return setErr
        }
    }

    return nil
}

func (instance *ttlRecordingCache) DeleteMultiple(keys []string) error {
    for _, key := range keys {
        if deleteErr := instance.Delete(key); nil != deleteErr {
            return deleteErr
        }
    }

    return nil
}

func (instance *ttlRecordingCache) Increment(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *ttlRecordingCache) Decrement(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *ttlRecordingCache) Close() error {
    return nil
}

var _ melodycachecontract.Cache = (*ttlRecordingCache)(nil)

/* countingUserRepository answers nothing and counts how often it was asked, which is what tells a
   remembered absence from a lookup that reached the directory again. */

/* recordingDispatcher is the framework's own dispatcher with one listener on it, rather than a double of the
   whole six-method contract. It is the shorter thing to write and the stronger thing to assert: what the
   write doors owe the cache is that the invalidation LISTENER runs, and a double that only counted calls
   would answer the same whether the dispatch reached a listener or not. */
type recordingDispatcher struct {
    dispatcher *melodyevent.EventDispatcher
    mutex      sync.Mutex
    observed   []string
}

func newRecordingDispatcher(clockInstance melodyclockcontract.Clock, eventNames ...string) *recordingDispatcher {
    recorder := &recordingDispatcher{dispatcher: melodyevent.NewEventDispatcher(clockInstance)}

    for _, eventName := range eventNames {
        observed := eventName
        recorder.dispatcher.AddListener(
            eventName,
            func(runtimeInstance melodyruntimecontract.Runtime, event melodyeventcontract.Event) error {
                recorder.mutex.Lock()
                recorder.observed = append(recorder.observed, observed)
                recorder.mutex.Unlock()

                return nil
            },
            0,
        )
    }

    return recorder
}

func (instance *recordingDispatcher) names() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string{}, instance.observed...)
}

/* frozenClock names the instant a stamping door writes, rather than letting it come from the wall. The
   ticker half of the contract is not what these doors use, so it answers the real one: a door that started
   a ticker would be a different subject, and a double that returned nothing there would hide it. */
type frozenClock struct {
    instant time.Time
}

func (instance *frozenClock) Now() time.Time {
    return instance.instant
}

func (instance *frozenClock) NewTicker(interval time.Duration) melodyclockcontract.Ticker {
    return melodyclock.NewSystemClock().NewTicker(interval)
}

var _ melodyclockcontract.Clock = (*frozenClock)(nil)
