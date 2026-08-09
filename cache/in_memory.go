package cache

import (
    "container/list"
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

const (
    maxInt64 = int64(^uint64(0) >> 1)
    minInt64 = -maxInt64 - 1
)

type lruEntry struct {
    key         string
    item        *Item
    listElement *list.Element
}

/* NewInMemoryBackend builds the framework's in-memory cache backend and starts its cleanup goroutine, which only Close stops — an instance abandoned without Close keeps the goroutine, its ticker and the whole entry map alive for the rest of the process; there is no finalizer fallback. maxItems bounds the entry count: zero disables the bound and a negative value panics, so a bound computed wrong cannot silently disarm eviction. cleanupInterval is how often the sweep collects lapsed entries, defaulting to a minute when non-positive; it is not a lifetime applied to anything. */
func NewInMemoryBackend(
    maxItems int,
    cleanupInterval time.Duration,
    clockInstance clockcontract.Clock,
) *InMemoryBackend {
    interval := cleanupInterval
    if 0 >= interval {
        interval = time.Minute
    }

    if 0 > maxItems {
        exception.Panic(
            exception.NewError(
                "cache max items is negative",
                exceptioncontract.Context{
                    "maxItems": maxItems,
                },
                nil,
            ),
        )
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
    closed              bool
    clock               clockcontract.Clock
}

/* a closed backend refuses every operation. Serving one would be worse than the error: the cleanup goroutine is stopped by then, so an entry saved after Close is never reclaimed by anything but a read that happens to name it — the map grows for the rest of the process while Close has already reported the backend gone. */
func closedBackendError() error {
    return exception.NewError(
        "cache backend is closed",
        nil,
        nil,
    )
}

const inMemoryBackendMaxKeyLength = 1024

/* validateKey enforces the key grammar the Backend contract states — non-empty, no spaces, no newlines, at most 1024 bytes — with the exact refusals the redis backend answers, so a caller cannot tell the implementations apart by which keys they refuse: a key that works in development against this backend works in production against the store, and the malformed one fails identically in both. */
func validateKey(key string) error {
    if "" == key {
        return emptyKeyError()
    }

    if true == strings.Contains(key, " ") {
        return exception.NewError(
            "cache key contains spaces",
            exceptioncontract.Context{
                "key": key,
            },
            nil,
        )
    }

    if true == strings.Contains(key, "\n") {
        return exception.NewError(
            "cache key contains newlines",
            exceptioncontract.Context{
                "key": key,
            },
            nil,
        )
    }

    if inMemoryBackendMaxKeyLength < len(key) {
        return exception.NewError(
            "cache key is too long",
            exceptioncontract.Context{
                "key":          key,
                "maxKeyLength": inMemoryBackendMaxKeyLength,
                "keyLength":    len(key),
            },
            nil,
        )
    }

    return nil
}

func emptyKeyError() error {
    return exception.NewError(
        "cache key is empty",
        nil,
        nil,
    )
}

func negativeTtlError(ttl time.Duration) error {
    return exception.NewError(
        "cache ttl is negative",
        exceptioncontract.Context{
            "ttl": ttl.String(),
        },
        nil,
    )
}

func refuseEmptyKeyList(keys []string) error {
    for _, key := range keys {
        if keyErr := validateKey(key); nil != keyErr {
            return keyErr
        }
    }

    return nil
}

func (instance *InMemoryBackend) Get(key string) ([]byte, bool, error) {
    now := instance.clock.Now()

    instance.mutex.RLock()
    if true == instance.closed {
        instance.mutex.RUnlock()
        return nil, false, closedBackendError()
    }

    if keyErr := validateKey(key); nil != keyErr {
        instance.mutex.RUnlock()
        return nil, false, keyErr
    }

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
    if true == instance.closed {
        instance.mutex.RUnlock()
        return false, closedBackendError()
    }

    if keyErr := validateKey(key); nil != keyErr {
        instance.mutex.RUnlock()
        return false, keyErr
    }

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

    if true == instance.closed {
        return closedBackendError()
    }

    if keyErr := validateKey(key); nil != keyErr {
        return keyErr
    }

    if 0 > ttl {
        return negativeTtlError(ttl)
    }

    instance.saveLocked(
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

    if true == instance.closed {
        return closedBackendError()
    }

    if keyErr := validateKey(key); nil != keyErr {
        return keyErr
    }

    instance.deleteLocked(key)

    return nil
}

func (instance *InMemoryBackend) Clear() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return closedBackendError()
    }

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
    if true == instance.closed {
        instance.mutex.RUnlock()
        return nil, closedBackendError()
    }

    if refuseErr := refuseEmptyKeyList(keys); nil != refuseErr {
        instance.mutex.RUnlock()
        return nil, refuseErr
    }

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

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return closedBackendError()
    }

    /* the ttl is judged before the keys, the order the redis backend judges them in: a batch carrying both a malformed key and an invalid ttl is refused with the same answer on both implementations */
    if 0 > ttl {
        return negativeTtlError(ttl)
    }

    for key := range items {
        if keyErr := validateKey(key); nil != keyErr {
            return keyErr
        }
    }

    for key, payload := range items {
        instance.saveLocked(
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

    if true == instance.closed {
        return closedBackendError()
    }

    if refuseErr := refuseEmptyKeyList(keys); nil != refuseErr {
        return refuseErr
    }

    for _, key := range keys {
        instance.deleteLocked(key)
    }

    return nil
}

func (instance *InMemoryBackend) Increment(key string, delta int64) (int64, error) {
    return instance.incrementValue(key, delta)
}

func (instance *InMemoryBackend) Decrement(key string, delta int64) (int64, error) {
    if minInt64 == delta {
        return 0, exception.NewError(
            "delta overflows int64 when negated",
            exceptioncontract.Context{
                "key": key,
            },
            nil,
        )
    }

    return instance.incrementValue(key, -delta)
}

func (instance *InMemoryBackend) Close() error {
    instance.mutex.Lock()
    instance.closed = true
    instance.mutex.Unlock()

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

func (instance *InMemoryBackend) incrementValue(
    key string,
    delta int64,
) (int64, error) {
    now := instance.clock.Now()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return 0, closedBackendError()
    }

    if keyErr := validateKey(key); nil != keyErr {
        return 0, keyErr
    }

    entry, exists := instance.getEntryLocked(key, now)

    var currentValue int64 = 0

    /* an existing payload is parsed in its entirety: an empty or blank one is refused the same way a textual one is, instead of being silently adopted as a zero counter and overwritten — a present value that is not a number is the caller mixing keys, and the redis reference answers it with an error too */
    if true == exists && nil != entry && nil != entry.item {
        payload := entry.item.Payload()
        trimmedValue := strings.TrimSpace(string(payload))

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

    var preservedExpiresAt *time.Time
    if true == exists && nil != entry && nil != entry.item {
        preservedExpiresAt = entry.item.ExpiresAt()
    }

    instance.saveItemLocked(
        key,
        []byte(strconv.FormatInt(newValue, 10)),
        now,
        preservedExpiresAt,
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

const cleanupChunkSize = 1024

const evictionProbeLimit = 8

/* the sweep takes the keys once and then expires them in chunks, releasing the lock between chunks: Get takes the same exclusive lock to touch the recency list, so a single whole-map pass under one lock stalls every concurrent request for as long as the map is large. A key deleted meanwhile is simply not found. */
func (instance *InMemoryBackend) cleanupExpired() {
    now := instance.clock.Now()

    instance.mutex.Lock()
    keys := make([]string, 0, len(instance.entries))
    for key := range instance.entries {
        keys = append(keys, key)
    }
    instance.mutex.Unlock()

    for start := 0; start < len(keys); start = start + cleanupChunkSize {
        end := start + cleanupChunkSize
        if len(keys) < end {
            end = len(keys)
        }

        instance.mutex.Lock()
        for _, key := range keys[start:end] {
            instance.deleteExpiredLocked(key, now)
        }
        instance.mutex.Unlock()
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

func (instance *InMemoryBackend) saveLocked(
    key string,
    payload []byte,
    now time.Time,
    ttl time.Duration,
) {
    var expiresAt *time.Time
    if 0 < ttl {
        expiration := now.Add(ttl)
        expiresAt = &expiration
    }

    instance.saveItemLocked(key, payload, now, expiresAt)
}

func (instance *InMemoryBackend) saveItemLocked(
    key string,
    payload []byte,
    now time.Time,
    expiresAt *time.Time,
) {
    entry, exists := instance.entries[key]

    if 0 < instance.maxItems && len(instance.entries) >= instance.maxItems && false == exists {
        instance.evictOneLocked(now)
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

/* the walk toward the front is bounded: it looks for an expired victim before falling back to the least recently used one, and an unbounded search would make every insert into a full cache pay a whole-list scan under the exclusive lock. Expired entries are reclaimed anyway, lazily by the readers and periodically by the sweep. */
func (instance *InMemoryBackend) evictOneLocked(now time.Time) {
    probed := 0
    for element := instance.lruList.Back(); nil != element && evictionProbeLimit > probed; element = element.Prev() {
        probed = probed + 1

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
