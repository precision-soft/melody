package cache

import (
    "container/list"
    "runtime"
    "strconv"
    "strings"
    "sync"
    "time"

    cachecontract "github.com/precision-soft/melody/cache/contract"
    clockcontract "github.com/precision-soft/melody/clock/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
)

type lruEntry struct {
    key         string
    item        *Item
    listElement *list.Element
}

func NewInMemoryBackend(
    maxItems int,
    cleanupInterval time.Duration,
    clockInstance clockcontract.Clock,
) *InMemoryBackend {
    interval := cleanupInterval
    if 0 >= interval {
        interval = time.Minute
    }

    if true == internal.IsNilInterface(clockInstance) {
        exception.Panic(
            exception.NewError(
                "clock is nil",
                nil,
                nil,
            ),
        )
    }

    backend := &InMemoryBackend{
        entries:             make(map[string]*lruEntry),
        lruList:             list.New(),
        maxItems:            maxItems,
        cleanupTickInterval: interval,
        stopCleanup:         make(chan struct{}),
        cleanupDone:         make(chan struct{}),
        clock:               clockInstance,
    }

    runtime.SetFinalizer(
        backend,
        func(backendInstance *InMemoryBackend) {
            if nil == backendInstance {
                return
            }

            backendInstance.stopCleanupLoop()
        },
    )

    go backend.cleanupLoop()

    return backend
}

type InMemoryBackend struct {
    mutex               sync.RWMutex
    entries             map[string]*lruEntry
    lruList             *list.List
    maxItems            int
    cleanupTickInterval time.Duration
    stopCleanup         chan struct{}
    cleanupDone         chan struct{}
    stopCleanupOnce     sync.Once
    clock               clockcontract.Clock
}

func (instance *InMemoryBackend) Get(key string) ([]byte, bool, error) {
    now := instance.clock.Now()

    instance.mutex.RLock()
    entry, exists := instance.entries[key]
    if false == exists || nil == entry || nil == entry.item {
        instance.mutex.RUnlock()
        return nil, false, nil
    }

    if true == instance.isExpiredAt(entry.item, now) {
        instance.mutex.RUnlock()
        return nil, false, nil
    }

    payload := entry.item.Payload()
    instance.mutex.RUnlock()

    instance.mutex.Lock()
    entry, exists = instance.entries[key]
    if true == exists && nil != entry && nil != entry.item {
        if false == instance.isExpiredAt(entry.item, now) {
            entry.item.Touch(now)
            instance.lruList.MoveToFront(entry.listElement)
        } else {
            instance.deleteExpiredLocked(key, now)
        }
    }
    instance.mutex.Unlock()

    return payload, true, nil
}

func (instance *InMemoryBackend) Has(key string) (bool, error) {
    now := instance.clock.Now()

    instance.mutex.RLock()
    entry, exists := instance.entries[key]
    if false == exists || nil == entry || nil == entry.item {
        instance.mutex.RUnlock()
        return false, nil
    }

    if true == instance.isExpiredAt(entry.item, now) {
        instance.mutex.RUnlock()
        return false, nil
    }

    instance.mutex.RUnlock()
    return true, nil
}

func (instance *InMemoryBackend) Set(
    key string,
    payload []byte,
    ttl time.Duration,
) error {
    now := instance.clock.Now()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.upsertLocked(
        key,
        payload,
        now,
        ttl,
    )

    return nil
}

func (instance *InMemoryBackend) Delete(key string) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.deleteLocked(key)

    return nil
}

func (instance *InMemoryBackend) Clear() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.entries = make(map[string]*lruEntry)
    instance.lruList = list.New()

    return nil
}

func (instance *InMemoryBackend) Many(keys []string) (map[string][]byte, error) {
    now := instance.clock.Now()

    result := make(map[string][]byte, len(keys))

    type hit struct {
        key string
    }
    hits := make([]hit, 0, len(keys))

    instance.mutex.RLock()
    for _, key := range keys {
        entry, exists := instance.entries[key]
        if false == exists || nil == entry || nil == entry.item {
            continue
        }

        if true == instance.isExpiredAt(entry.item, now) {
            continue
        }

        result[key] = entry.item.Payload()
        hits = append(
            hits,
            hit{
                key: key,
            },
        )
    }
    instance.mutex.RUnlock()

    if 0 == len(hits) {
        return result, nil
    }

    instance.mutex.Lock()
    for _, currentHit := range hits {
        entry, exists := instance.entries[currentHit.key]
        if false == exists || nil == entry || nil == entry.item {
            continue
        }

        if true == instance.isExpiredAt(entry.item, now) {
            instance.deleteExpiredLocked(currentHit.key, now)
            continue
        }

        entry.item.Touch(now)
        instance.lruList.MoveToFront(entry.listElement)
    }
    instance.mutex.Unlock()

    return result, nil
}

func (instance *InMemoryBackend) SetMultiple(items map[string][]byte, ttl time.Duration) error {
    now := instance.clock.Now()

    normalizedItems := make(map[string][]byte, len(items))
    for key, payload := range items {
        normalizedItems[key] = payload
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for key, payload := range normalizedItems {
        instance.upsertLocked(
            key,
            payload,
            now,
            ttl,
        )
    }

    return nil
}

func (instance *InMemoryBackend) DeleteMultiple(keys []string) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for _, key := range keys {
        instance.deleteLocked(key)
    }

    return nil
}

func (instance *InMemoryBackend) Increment(key string, delta int64) (int64, error) {
    return instance.incrementWithTtl(key, delta, 0)
}

func (instance *InMemoryBackend) Decrement(key string, delta int64) (int64, error) {
    const (
        maxInt64 = int64(^uint64(0) >> 1)
        minInt64 = -maxInt64 - 1
    )

    if minInt64 == delta {
        return 0, exception.NewError(
            "delta overflows int64 when negated",
            exceptioncontract.Context{
                "key": key,
            },
            nil,
        )
    }

    return instance.incrementWithTtl(key, -delta, 0)
}

func (instance *InMemoryBackend) Close() error {
    instance.stopCleanupLoop()

    <-instance.cleanupDone

    return nil
}

func (instance *InMemoryBackend) stopCleanupLoop() {
    instance.stopCleanupOnce.Do(
        func() {
            close(instance.stopCleanup)
        },
    )
}

func (instance *InMemoryBackend) incrementWithTtl(
    key string,
    delta int64,
    ttl time.Duration,
) (int64, error) {
    now := instance.clock.Now()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    entry, exists := instance.getEntryLocked(key, now)

    var currentValue int64 = 0

    if true == exists && nil != entry && nil != entry.item {
        payload := entry.item.Payload()
        trimmedValue := strings.TrimSpace(string(payload))

        if "" != trimmedValue {
            parsedValue, parseIntErr := strconv.ParseInt(trimmedValue, 10, 64)
            if nil != parseIntErr {
                return 0, exception.NewError(
                    "cache value is not a valid int64",
                    exceptioncontract.Context{
                        "key":   key,
                        "value": trimmedValue,
                    },
                    parseIntErr,
                )
            }

            currentValue = parsedValue
        }
    }

    newValue, addInt64WithOverflowCheckErr := instance.addInt64WithOverflowCheck(currentValue, delta)
    if nil != addInt64WithOverflowCheckErr {
        return 0, exception.NewError(
            "cache increment overflow",
            exceptioncontract.Context{
                "key":          key,
                "currentValue": currentValue,
                "delta":        delta,
            },
            addInt64WithOverflowCheckErr,
        )
    }

    instance.upsertLocked(
        key,
        []byte(strconv.FormatInt(newValue, 10)),
        now,
        ttl,
    )

    return newValue, nil
}

func (instance *InMemoryBackend) cleanupLoop() {
    defer close(instance.cleanupDone)

    ticker := instance.clock.NewTicker(instance.cleanupTickInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.Channel():
            instance.cleanupExpired()
        case <-instance.stopCleanup:
            return
        }
    }
}

func (instance *InMemoryBackend) cleanupExpired() {
    now := instance.clock.Now()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for key := range instance.entries {
        instance.deleteExpiredLocked(key, now)
    }
}

func (instance *InMemoryBackend) deleteLocked(key string) {
    entry, exists := instance.entries[key]
    if false == exists || nil == entry {
        return
    }

    if nil != entry.listElement {
        instance.lruList.Remove(entry.listElement)
    }

    delete(instance.entries, key)
}

func (instance *InMemoryBackend) deleteExpiredLocked(key string, now time.Time) {
    entry, exists := instance.entries[key]
    if false == exists || nil == entry || nil == entry.item {
        return
    }

    if true == instance.isExpiredAt(entry.item, now) {
        instance.deleteLocked(key)
    }
}

func (instance *InMemoryBackend) getEntryLocked(key string, now time.Time) (*lruEntry, bool) {
    entry, exists := instance.entries[key]
    if false == exists || nil == entry || nil == entry.item {
        return nil, false
    }

    if true == instance.isExpiredAt(entry.item, now) {
        instance.deleteLocked(key)
        return nil, false
    }

    return entry, true
}

func (instance *InMemoryBackend) upsertLocked(
    key string,
    payload []byte,
    now time.Time,
    ttl time.Duration,
) {
    entry, exists := instance.entries[key]

    if 0 < instance.maxItems && len(instance.entries) >= instance.maxItems && false == exists {
        instance.evictOneLocked(now)
    }

    var expiresAt *time.Time
    if 0 < ttl {
        expiration := now.Add(ttl)
        expiresAt = &expiration
    }

    item := NewItem(
        key,
        payload,
        now,
        expiresAt,
    )

    if true == exists && nil != entry {
        entry.item = item
        instance.lruList.MoveToFront(entry.listElement)
        return
    }

    element := instance.lruList.PushFront(key)

    instance.entries[key] = &lruEntry{
        key:         key,
        item:        item,
        listElement: element,
    }
}

func (instance *InMemoryBackend) evictOneLocked(now time.Time) {
    for element := instance.lruList.Back(); nil != element; element = element.Prev() {
        key, ok := element.Value.(string)
        if false == ok {
            instance.lruList.Remove(element)
            continue
        }

        entry, exists := instance.entries[key]
        if false == exists || nil == entry || nil == entry.item {
            instance.lruList.Remove(element)
            delete(instance.entries, key)
            return
        }

        if true == instance.isExpiredAt(entry.item, now) {
            instance.deleteLocked(key)
            return
        }
    }

    backElement := instance.lruList.Back()
    if nil == backElement {
        return
    }

    key, ok := backElement.Value.(string)
    if false == ok {
        instance.lruList.Remove(backElement)
        return
    }

    instance.deleteLocked(key)
}

func (instance *InMemoryBackend) isExpiredAt(item *Item, now time.Time) bool {
    if nil == item {
        return true
    }

    expiresAt := item.ExpiresAt()
    if nil == expiresAt {
        return false
    }

    if now.After(*expiresAt) {
        return true
    }

    if now.Equal(*expiresAt) {
        return true
    }

    return false
}

func (instance *InMemoryBackend) addInt64WithOverflowCheck(left int64, right int64) (int64, error) {
    const (
        maxInt64 = int64(^uint64(0) >> 1)
        minInt64 = -maxInt64 - 1
    )

    if 0 < right && left > maxInt64-right {
        return 0, exception.NewError(
            "int64 addition overflow",
            nil,
            nil,
        )
    }

    if 0 > right && left < minInt64-right {
        return 0, exception.NewError(
            "int64 addition underflow",
            nil,
            nil,
        )
    }

    return left + right, nil
}

var _ cachecontract.Backend = (*InMemoryBackend)(nil)
