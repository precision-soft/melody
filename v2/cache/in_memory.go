package cache

import (
    "container/list"
    "sort"
    "strconv"
    "strings"
    "sync"
    "sync/atomic"
    "time"

    cachecontract "github.com/precision-soft/melody/v2/cache/contract"
    clockcontract "github.com/precision-soft/melody/v2/clock/contract"
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/internal"
)

const (
    maxInt64 = int64(^uint64(0) >> 1)
    minInt64 = -maxInt64 - 1
)

/* lastPromotedAtNano is when this entry was last moved to the front, not when it was last read, which keeps the read path off the exclusive lock; it is atomic because Get reads it under the read lock */
type lruEntry struct {
    key                string
    item               *Item
    listElement        *list.Element
    lastPromotedAtNano atomic.Int64
}

func (instance *lruEntry) isPromotionDue(now time.Time) bool {
    return recencyPromotionInterval <= time.Duration(now.UnixNano()-instance.lastPromotedAtNano.Load())
}

func (instance *lruEntry) markPromoted(now time.Time) {
    instance.lastPromotedAtNano.Store(now.UnixNano())
}

/* NewInMemoryBackend builds the in-memory cache backend and starts its cleanup goroutine, which only Close stops; there is no finalizer fallback. maxItems bounds the entry count: zero disables the bound and a negative value panics. cleanupInterval is how often the sweep collects lapsed entries, a minute when non-positive. */
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

/* a closed backend refuses every operation, since its cleanup goroutine is stopped and an entry saved after Close would never be reclaimed */
func closedBackendError() error {
    return exception.NewError(
        "cache backend is closed",
        nil,
        nil,
    )
}

const inMemoryBackendMaxKeyLength = 1024

/* validateKey enforces the Backend key grammar with the exact refusals the redis backend answers, so the implementations cannot be told apart by which keys they refuse */
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

    /* the access mark is atomic, so the read path refreshes it without ever leaving the read lock */
    entry.item.Touch(now)
    promotionDue := entry.isPromotionDue(now)

    instance.mutex.RUnlock()

    /* a key promoted recently answers under the read lock alone; a lapsed entry found here is answered absent and left to the sweep, since deleting it needs the exclusive lock */
    if false == promotionDue {
        return payload, true, nil
    }

    instance.mutex.Lock()
    entry, exists = instance.entries[key]
    if true == exists && nil != entry && nil != entry.item {
        if false == instance.isExpiredAt(entry.item, now) {
            entry.markPromoted(now)
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

        entry.item.Touch(now)

        if false == entry.isPromotionDue(now) {
            continue
        }

        hits = append(
            hits,
            hit{
                key: key,
            },
        )
    }
    instance.mutex.RUnlock()

    /* only the keys whose place in the list is stale reach the exclusive lock, so a batch of hot keys costs one read lock */
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

        entry.markPromoted(now)
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

    /* the ttl is judged before the keys, the order the redis backend judges them in */
    if 0 > ttl {
        return negativeTtlError(ttl)
    }

    /* the keys are validated in sorted order, so the same malformed batch answers the same refusal every time, as the redis backend's batch reporting does */
    keys := make([]string, 0, len(items))
    for key := range items {
        keys = append(keys, key)
    }
    sort.Strings(keys)

    for _, key := range keys {
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
    /* the magnitude of the delta is judged after the closed backend and the key, the order the shared contract fixes for every implementation */
    if refusalErr := instance.refuseClosedOrInvalidKey(key); nil != refusalErr {
        return 0, refusalErr
    }

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

/* refuseClosedOrInvalidKey answers the two refusals incrementValue makes under its own lock, in the same order. The flag is read under the lock every writer takes, and a Close landing after the read is caught by incrementValue itself. */
func (instance *InMemoryBackend) refuseClosedOrInvalidKey(key string) error {
    instance.mutex.RLock()
    closed := instance.closed
    instance.mutex.RUnlock()

    if true == closed {
        return closedBackendError()
    }

    return validateKey(key)
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

    /* an existing payload is parsed against the redis integer grammar, not Go's lenient one, so a payload increments or errors the same way on both backends */
    if true == exists && nil != entry && nil != entry.item {
        payloadValue := string(entry.item.Payload())

        parsedValue, parseIntErr := parseCanonicalCounterPayload(payloadValue)
        if nil != parseIntErr {
            return 0, exception.NewError(
                "cache value is not a valid int64",
                exceptioncontract.Context{
                    "key":   key,
                    "value": payloadValue,
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

/* how often one entry may cost the exclusive lock for its place in the recency list: eviction picks an expired victim first and falls back to the least recently promoted one, so the order only has to hold at this coarser grain. The access mark is still refreshed on every read. */
const recencyPromotionInterval = time.Second

/* the sweep takes the keys once and expires them in chunks, releasing the lock between chunks, since every write and every due promotion takes the same exclusive lock and one whole-map pass would stall them all; a key deleted meanwhile is not found */
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
    /* a nil payload is stored as the empty payload, the contract's rule, since redis has no nil to store */
    if nil == payload {
        payload = []byte{}
    }

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
        entry.markPromoted(now)
        instance.lruList.MoveToFront(entry.listElement)
        return
    }

    element := instance.lruList.PushFront(key)

    freshEntry := &lruEntry{
        key:         key,
        item:        item,
        listElement: element,
    }
    freshEntry.markPromoted(now)

    instance.entries[key] = freshEntry
}

/* evictOneLocked walks toward the front a bounded distance for an expired victim before falling back to the least recently promoted one, so an insert into a full cache never scans the whole list under the exclusive lock. */
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

/* parseCanonicalCounterPayload accepts exactly what redis's integer reader (string2ll) accepts: an optional minus and a first digit of 1-9 unless the number is 0, with no whitespace, plus sign, leading zeros or minus zero. */
func parseCanonicalCounterPayload(payload string) (int64, error) {
    digits := payload
    if "" != digits && '-' == digits[0] {
        digits = digits[1:]
    }

    canonical := "" != digits
    if true == canonical && 1 < len(digits) && '0' == digits[0] {
        canonical = false
    }
    if true == canonical && "-0" == payload {
        canonical = false
    }
    if true == canonical {
        for index := 0; index < len(digits); index++ {
            if digits[index] < '0' || digits[index] > '9' {
                canonical = false

                break
            }
        }
    }

    if false == canonical {
        return 0, exception.NewError(
            "value is not a canonical integer",
            nil,
            nil,
        )
    }

    return strconv.ParseInt(payload, 10, 64)
}

var _ cachecontract.Backend = (*InMemoryBackend)(nil)
