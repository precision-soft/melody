package cache

import (
    "container/list"
    "context"
    "errors"
    "sort"
    "strconv"
    "strings"
    "sync"
    "sync/atomic"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

const (
    maxInt64 = int64(^uint64(0) >> 1)
    minInt64 = -maxInt64 - 1
)

type lruEntry struct {
    key         string
    item        *Item
    listElement *list.Element
    /* the last promotion, not the last read, which keeps reads off the exclusive lock */
    lastPromotedAtNano atomic.Int64
}

func (instance *lruEntry) isPromotionDue(now time.Time) bool {
    return recencyPromotionInterval <= time.Duration(now.UnixNano()-instance.lastPromotedAtNano.Load())
}

func (instance *lruEntry) markPromoted(now time.Time) {
    instance.lastPromotedAtNano.Store(now.UnixNano())
}

/* InMemoryBackendConfig declares the in-memory backend; the zero value is the default. */
type InMemoryBackendConfig struct {
    /* MaxItems bounds the entry count, evicting expired then least recently promoted: 10,000 by default. */
    MaxItems bound.Count

    /* CleanupInterval is the sweep cadence: 1m by default, at least 1ms, never unlimited. */
    CleanupInterval bound.Duration

    /* Clock is where the backend reads the time; nil is the system clock. */
    Clock clockcontract.Clock
}

var inMemoryBackendMaxItemsRule = bound.Rule[int]{
    Name:              "InMemoryBackendConfig.MaxItems",
    Default:           10_000,
    Minimum:           1,
    UnlimitedAdmitted: true,
}

var inMemoryBackendCleanupIntervalRule = bound.Rule[time.Duration]{
    Name:    "InMemoryBackendConfig.CleanupInterval",
    Default: time.Minute,
    Minimum: bound.TimeoutMinimum,
}

/* NewInMemoryBackend panics on a field below its minimum and starts a cleanup goroutine only CloseWithContext stops. */
func NewInMemoryBackend(config InMemoryBackendConfig) *InMemoryBackend {
    maxItems, maxItemsErr := bound.Resolve(config.MaxItems, inMemoryBackendMaxItemsRule)
    if nil != maxItemsErr {
        exception.Panic(exception.FromError(maxItemsErr))
    }

    cleanupInterval, cleanupIntervalErr := bound.Resolve(config.CleanupInterval, inMemoryBackendCleanupIntervalRule)
    if nil != cleanupIntervalErr {
        exception.Panic(exception.FromError(cleanupIntervalErr))
    }

    clockInstance := config.Clock
    if true == internal.IsNilInterface(clockInstance) {
        clockInstance = clock.NewSystemClock()
    }

    /* 0 is the unlimited ceiling */
    backend := &InMemoryBackend{
        entries:             make(map[string]*lruEntry),
        lruList:             list.New(),
        maxItems:            maxItems.Value,
        cleanupTickInterval: cleanupInterval.Value,
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

/* refused: the cleanup goroutine is stopped, so a saved entry would never be reclaimed */
func closedBackendError() error {
    return exception.NewError(
        "cache backend is closed",
        nil,
        nil,
    )
}

const inMemoryBackendMaxKeyLength = 1024

func validateKey(key string) error {
    if "" == key {
        return emptyKeyError()
    }

    if true == strings.Contains(key, " ") {
        return exception.NewError(
            "cache key contains spaces",
            exceptioncontract.Context{
                "key": renderedCacheKey(key),
            },
            nil,
        )
    }

    if true == strings.Contains(key, "\n") {
        return exception.NewError(
            "cache key contains newlines",
            exceptioncontract.Context{
                "key": renderedCacheKey(key),
            },
            nil,
        )
    }

    if inMemoryBackendMaxKeyLength < len(key) {
        return exception.NewError(
            "cache key is too long",
            exceptioncontract.Context{
                "key":          renderedCacheKey(key),
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

func refuseInvalidKeyList(keys []string) error {
    for _, key := range keys {
        if keyErr := validateKey(key); nil != keyErr {
            return keyErr
        }
    }

    return nil
}

func (instance *InMemoryBackend) Get(ctx context.Context, key string) ([]byte, bool, error) {
    if nilErr := cachecontract.RefuseNilContext(ctx); nil != nilErr {
        return nil, false, nilErr
    }

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

    if doneErr := ctx.Err(); nil != doneErr {
        instance.mutex.RUnlock()
        return nil, false, doneErr
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

    entry.item.Touch(now)
    promotionDue := entry.isPromotionDue(now)

    instance.mutex.RUnlock()

    /* a lapsed entry is answered absent and left to the sweep, which holds the exclusive lock */
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

func (instance *InMemoryBackend) Has(ctx context.Context, key string) (bool, error) {
    if nilErr := cachecontract.RefuseNilContext(ctx); nil != nilErr {
        return false, nilErr
    }

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

    if doneErr := ctx.Err(); nil != doneErr {
        instance.mutex.RUnlock()
        return false, doneErr
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
    ctx context.Context,
    key string,
    payload []byte,
    ttl bound.Duration,
) error {
    if nilErr := cachecontract.RefuseNilContext(ctx); nil != nilErr {
        return nilErr
    }

    now := instance.clock.Now()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return closedBackendError()
    }

    if keyErr := validateKey(key); nil != keyErr {
        return keyErr
    }

    expiry, ttlErr := cachecontract.ResolveTtl(ttl)
    if nil != ttlErr {
        return ttlErr
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    instance.saveLocked(
        key,
        payload,
        now,
        expiry,
    )

    return nil
}

func (instance *InMemoryBackend) Delete(ctx context.Context, key string) error {
    if nilErr := cachecontract.RefuseNilContext(ctx); nil != nilErr {
        return nilErr
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return closedBackendError()
    }

    if keyErr := validateKey(key); nil != keyErr {
        return keyErr
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    instance.deleteLocked(key)

    return nil
}

func (instance *InMemoryBackend) Clear(ctx context.Context) error {
    if nilErr := cachecontract.RefuseNilContext(ctx); nil != nilErr {
        return nilErr
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return closedBackendError()
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    instance.entries = make(map[string]*lruEntry)
    instance.lruList = list.New()

    return nil
}

func (instance *InMemoryBackend) Many(ctx context.Context, keys []string) (map[string][]byte, error) {
    if nilErr := cachecontract.RefuseNilContext(ctx); nil != nilErr {
        return nil, nilErr
    }

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

    if refuseErr := refuseInvalidKeyList(keys); nil != refuseErr {
        instance.mutex.RUnlock()
        return nil, refuseErr
    }

    if doneErr := ctx.Err(); nil != doneErr {
        instance.mutex.RUnlock()
        return nil, doneErr
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

func (instance *InMemoryBackend) SetMultiple(ctx context.Context, items map[string][]byte, ttl bound.Duration) error {
    if nilErr := cachecontract.RefuseNilContext(ctx); nil != nilErr {
        return nilErr
    }

    now := instance.clock.Now()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return closedBackendError()
    }

    /* the ttl is judged before the keys, as the redis backend does */
    expiry, ttlErr := cachecontract.ResolveTtl(ttl)
    if nil != ttlErr {
        return ttlErr
    }

    /* sorted, so a malformed batch answers the same refusal every time */
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

    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    for key, payload := range items {
        instance.saveLocked(
            key,
            payload,
            now,
            expiry,
        )
    }

    return nil
}

func (instance *InMemoryBackend) DeleteMultiple(ctx context.Context, keys []string) error {
    if nilErr := cachecontract.RefuseNilContext(ctx); nil != nilErr {
        return nilErr
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return closedBackendError()
    }

    if refuseErr := refuseInvalidKeyList(keys); nil != refuseErr {
        return refuseErr
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    for _, key := range keys {
        instance.deleteLocked(key)
    }

    return nil
}

func (instance *InMemoryBackend) Increment(ctx context.Context, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    if nilErr := cachecontract.RefuseNilContext(ctx); nil != nilErr {
        return 0, nilErr
    }

    return instance.incrementValue(ctx, key, delta, options)
}

func (instance *InMemoryBackend) Decrement(ctx context.Context, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    if nilErr := cachecontract.RefuseNilContext(ctx); nil != nilErr {
        return 0, nilErr
    }

    /* the delta is judged after the closed check and the key, the contract's order */
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

    return instance.incrementValue(ctx, key, -delta, options)
}

func (instance *InMemoryBackend) refuseClosedOrInvalidKey(key string) error {
    instance.mutex.RLock()
    closed := instance.closed
    instance.mutex.RUnlock()

    if true == closed {
        return closedBackendError()
    }

    return validateKey(key)
}

/* CloseWithContext refuses later calls and waits for the cleanup loop up to the deadline. */
func (instance *InMemoryBackend) CloseWithContext(closeContext context.Context) error {
    instance.mutex.Lock()
    instance.closed = true
    instance.mutex.Unlock()

    instance.stopCleanupLoop()

    return awaitCleanupLoop(closeContext, instance.cleanupDone)
}

/* awaitCleanupLoop answers nil for a loop that left as the deadline passed, since select picks at random. */
func awaitCleanupLoop(closeContext context.Context, cleanupDone <-chan struct{}) error {
    select {
    case <-cleanupDone:
        return nil
    case <-closeContext.Done():
    }

    select {
    case <-cleanupDone:
        return nil
    default:
    }

    return exception.NewError("the in-memory cache backend's cleanup loop did not stop before the close deadline", nil, closeContext.Err())
}

func (instance *InMemoryBackend) stopCleanupLoop() {
    instance.stopCleanupOnce.Do(
        func() {
            close(instance.stopCleanup)
        },
    )
}

func (instance *InMemoryBackend) incrementValue(
    ctx context.Context,
    key string,
    delta int64,
    options cachecontract.CounterOptions,
) (int64, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    /* read under the lock: the instant judges the expiry and starts the written window */
    now := instance.clock.Now()

    if true == instance.closed {
        return 0, closedBackendError()
    }

    if keyErr := validateKey(key); nil != keyErr {
        return 0, keyErr
    }

    expiry, ttlErr := cachecontract.ResolveTtl(options.Ttl)
    if nil != ttlErr {
        return 0, ttlErr
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return 0, doneErr
    }

    entry, exists := instance.getEntryLocked(key, now)

    var currentValue int64 = 0

    /* parsed with redis's integer grammar, not Go's, so both backends agree */
    if true == exists && nil != entry && nil != entry.item {
        payloadValue := string(entry.item.Payload())

        parsedValue, parseIntErr := parseCanonicalCounterPayload(payloadValue)
        if nil != parseIntErr {
            return 0, exception.NewError(
                "cache value is not a valid int64",
                exceptioncontract.Context{
                    "key":           key,
                    "payloadLength": len(payloadValue),
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

    var expiresAt *time.Time
    if true == exists && nil != entry && nil != entry.item {
        expiresAt = entry.item.ExpiresAt()
    }

    if 0 < expiry && (true == options.RefreshTtl || nil == expiresAt) {
        counterExpiresAt := now.Add(expiry)
        expiresAt = &counterExpiresAt
    }

    if 0 == expiry && true == options.RefreshTtl {
        expiresAt = nil
    }

    instance.saveItemLocked(
        key,
        []byte(strconv.FormatInt(newValue, 10)),
        now,
        expiresAt,
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

/* the promotion grain: eviction prefers an expired victim, so the order needs no finer grain */
const recencyPromotionInterval = time.Second

/* swept in chunks with the lock released between them, so a large map does not stall writers */
func (instance *InMemoryBackend) cleanupExpired() {
    now := instance.clock.Now()

    instance.mutex.RLock()
    keys := make([]string, 0, len(instance.entries))
    for key := range instance.entries {
        keys = append(keys, key)
    }
    instance.mutex.RUnlock()

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
    expiry time.Duration,
) {
    /* a nil payload is stored empty, since redis has no nil */
    if nil == payload {
        payload = []byte{}
    }

    var expiresAt *time.Time
    if 0 < expiry {
        expiration := now.Add(expiry)
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

/* parseCanonicalCounterPayload accepts exactly what redis's string2ll accepts. */
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

    parsedValue, parseIntErr := strconv.ParseInt(payload, 10, 64)
    if nil != parseIntErr {
        return 0, counterParseFailure(parseIntErr)
    }

    return parsedValue, nil
}

/* counterParseFailure drops the payload strconv quotes, which a refusal must not carry. */
func counterParseFailure(parseErr error) error {
    if true == errors.Is(parseErr, strconv.ErrRange) {
        return exception.NewError(
            "value is out of the int64 range",
            nil,
            nil,
        )
    }

    return exception.NewError(
        "value is not an integer",
        nil,
        nil,
    )
}

var _ cachecontract.Backend = (*InMemoryBackend)(nil)

func renderedCacheKey(key string) string {
    const renderedCacheKeyLimit = 128

    if renderedCacheKeyLimit >= len(key) {
        return key
    }

    cut := renderedCacheKeyLimit
    for 0 < cut && false == utf8.RuneStart(key[cut]) {
        cut = cut - 1
    }

    return key[:cut] + "...(truncated)"
}
