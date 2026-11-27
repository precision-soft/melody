package cache

import (
    "context"
    "errors"
    "fmt"
    "math"
    "strconv"
    "strings"
    "sync"
    "testing"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

func TestInMemoryBackend_SetGet_HappyPath(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{
        MaxItems:        bound.Of(10),
        CleanupInterval: bound.Of(time.Hour),
        Clock:           clockInstance,
    })
    defer backend.CloseWithContext(context.Background())

    err := backend.Set(testRuntime(), "product.1", []byte("payload"), bound.UnlimitedDuration())
    if nil != err {
        t.Fatalf("set error: %v", err)
    }

    value, exists, err := backend.Get(testRuntime(), "product.1")
    if nil != err {
        t.Fatalf("get error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected cache hit")
    }
    if "payload" != string(value) {
        t.Fatalf("unexpected value: %s", string(value))
    }
}

func TestInMemoryBackend_TtlExpiry(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{
        MaxItems:        bound.Of(10),
        CleanupInterval: bound.Of(time.Hour),
        Clock:           clockInstance,
    })
    defer backend.CloseWithContext(context.Background())

    err := backend.Set(testRuntime(), "product.1", []byte("payload"), bound.Of(2*time.Second))
    if nil != err {
        t.Fatalf("set error: %v", err)
    }

    clockInstance.now = time.Unix(20, 0)

    _, exists, err := backend.Get(testRuntime(), "product.1")
    if nil != err {
        t.Fatalf("get error: %v", err)
    }
    if true == exists {
        t.Fatalf("expected cache miss due to ttl")
    }
}

func TestInMemoryBackend_LruEviction(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{
        MaxItems:        bound.Of(2),
        CleanupInterval: bound.Of(time.Hour),
        Clock:           clockInstance,
    })
    defer backend.CloseWithContext(context.Background())

    _ = backend.Set(testRuntime(), "a", []byte("a"), bound.UnlimitedDuration())
    _ = backend.Set(testRuntime(), "b", []byte("b"), bound.UnlimitedDuration())

    clockInstance.now = clockInstance.now.Add(recencyPromotionInterval)

    _, _, _ = backend.Get(testRuntime(), "a")

    _ = backend.Set(testRuntime(), "c", []byte("c"), bound.UnlimitedDuration())

    _, exists, _ := backend.Get(testRuntime(), "b")
    if true == exists {
        t.Fatalf("expected b to be evicted")
    }

    _, exists, _ = backend.Get(testRuntime(), "a")
    if false == exists {
        t.Fatalf("expected a to remain")
    }

    _, exists, _ = backend.Get(testRuntime(), "c")
    if false == exists {
        t.Fatalf("expected c to exist")
    }
}

func TestInMemoryBackend_AReadInsideThePromotionIntervalLeavesTheRecencyOrderWhereItWas(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{
        MaxItems:        bound.Of(2),
        CleanupInterval: bound.Of(time.Hour),
        Clock:           clockInstance,
    })
    defer backend.CloseWithContext(context.Background())

    _ = backend.Set(testRuntime(), "a", []byte("a"), bound.UnlimitedDuration())
    _ = backend.Set(testRuntime(), "b", []byte("b"), bound.UnlimitedDuration())

    clockInstance.now = clockInstance.now.Add(recencyPromotionInterval - time.Nanosecond)

    _, _, _ = backend.Get(testRuntime(), "a")

    _ = backend.Set(testRuntime(), "c", []byte("c"), bound.UnlimitedDuration())

    _, exists, _ := backend.Get(testRuntime(), "a")
    if true == exists {
        t.Fatalf("expected the read inside the promotion interval to leave a at the back, so a is the victim")
    }

    _, exists, _ = backend.Get(testRuntime(), "b")
    if false == exists {
        t.Fatalf("expected b to remain")
    }
}

func TestInMemoryBackend_AReadRefreshesTheAccessMarkEvenWithoutAPromotion(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{
        MaxItems:        bound.Of(10),
        CleanupInterval: bound.Of(time.Hour),
        Clock:           clockInstance,
    })
    defer backend.CloseWithContext(context.Background())

    _ = backend.Set(testRuntime(), "a", []byte("a"), bound.UnlimitedDuration())

    clockInstance.now = clockInstance.now.Add(time.Millisecond)

    _, _, _ = backend.Get(testRuntime(), "a")

    backend.mutex.RLock()
    entry := backend.entries["a"]
    backend.mutex.RUnlock()

    if clockInstance.now.UnixNano() != entry.item.LastAccessedAt().UnixNano() {
        t.Fatalf(
            "expected the read to refresh the access mark under the read lock, mark is %s and the read was at %s",
            entry.item.LastAccessedAt(),
            clockInstance.now,
        )
    }

    if 1 != entry.item.HitCount() {
        t.Fatalf("expected the read to count the hit, got %d", entry.item.HitCount())
    }

    if true == entry.isPromotionDue(clockInstance.now) {
        t.Fatalf("expected no promotion to be due a millisecond after the write")
    }
}

func TestInMemoryBackend_RefusesEmptyKey(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    if setErr := backend.Set(testRuntime(), "", []byte("1"), bound.UnlimitedDuration()); nil == setErr {
        t.Fatalf("expected Set to refuse the empty key")
    }

    if _, _, getErr := backend.Get(testRuntime(), ""); nil == getErr {
        t.Fatalf("expected Get to refuse the empty key")
    }

    if _, hasErr := backend.Has(testRuntime(), ""); nil == hasErr {
        t.Fatalf("expected Has to refuse the empty key")
    }

    if deleteErr := backend.Delete(testRuntime(), ""); nil == deleteErr {
        t.Fatalf("expected Delete to refuse the empty key")
    }

    if _, manyErr := backend.Many(testRuntime(), []string{"a", ""}); nil == manyErr {
        t.Fatalf("expected Many to refuse the empty key")
    }

    if setMultipleErr := backend.SetMultiple(testRuntime(), map[string][]byte{"": []byte("1")}, bound.UnlimitedDuration()); nil == setMultipleErr {
        t.Fatalf("expected SetMultiple to refuse the empty key")
    }

    if deleteMultipleErr := backend.DeleteMultiple(testRuntime(), []string{""}); nil == deleteMultipleErr {
        t.Fatalf("expected DeleteMultiple to refuse the empty key")
    }

    if _, incrementErr := backend.Increment(testRuntime(), "", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil == incrementErr {
        t.Fatalf("expected Increment to refuse the empty key")
    }

    if _, decrementErr := backend.Decrement(testRuntime(), "", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil == decrementErr {
        t.Fatalf("expected Decrement to refuse the empty key")
    }

    if _, exists, _ := backend.Get(testRuntime(), "a"); true == exists {
        t.Fatalf("expected no entry to have been written through a refused key")
    }
}

func TestInMemoryBackend_RefusesOperationsAfterClose(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})

    if setErr := backend.Set(testRuntime(), "a", []byte("1"), bound.UnlimitedDuration()); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    if closeErr := backend.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close error: %v", closeErr)
    }

    if setErr := backend.Set(testRuntime(), "b", []byte("2"), bound.UnlimitedDuration()); nil == setErr {
        t.Fatalf("expected Set to refuse a closed backend")
    }

    if _, _, getErr := backend.Get(testRuntime(), "a"); nil == getErr {
        t.Fatalf("expected Get to refuse a closed backend")
    }

    if _, hasErr := backend.Has(testRuntime(), "a"); nil == hasErr {
        t.Fatalf("expected Has to refuse a closed backend")
    }

    if deleteErr := backend.Delete(testRuntime(), "a"); nil == deleteErr {
        t.Fatalf("expected Delete to refuse a closed backend")
    }

    if clearErr := backend.Clear(testRuntime()); nil == clearErr {
        t.Fatalf("expected Clear to refuse a closed backend")
    }

    if _, manyErr := backend.Many(testRuntime(), []string{"a"}); nil == manyErr {
        t.Fatalf("expected Many to refuse a closed backend")
    }

    if setMultipleErr := backend.SetMultiple(testRuntime(), map[string][]byte{"c": []byte("3")}, bound.UnlimitedDuration()); nil == setMultipleErr {
        t.Fatalf("expected SetMultiple to refuse a closed backend")
    }

    if deleteMultipleErr := backend.DeleteMultiple(testRuntime(), []string{"a"}); nil == deleteMultipleErr {
        t.Fatalf("expected DeleteMultiple to refuse a closed backend")
    }

    if _, incrementErr := backend.Increment(testRuntime(), "n", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil == incrementErr {
        t.Fatalf("expected Increment to refuse a closed backend")
    }

    if _, decrementErr := backend.Decrement(testRuntime(), "n", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil == decrementErr {
        t.Fatalf("expected Decrement to refuse a closed backend")
    }

    if closeErr := backend.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("expected the second Close to stay idempotent, got: %v", closeErr)
    }
}

func TestInMemoryBackend_RefusesAnAbsentTtlAndOneBelowItsMinimum(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    for _, refusedTtl := range []bound.Duration{{}, bound.Of(time.Duration(0)), bound.Of(-time.Second), bound.Of(time.Second - time.Nanosecond)} {
        setErr := backend.Set(testRuntime(), "a", []byte("1"), refusedTtl)
        if nil == setErr || "ttl" != exception.LogContext(setErr, exception.LogContextConfig{})["field"] {
            t.Fatalf("expected Set to refuse the ttl %s naming it, got %v", refusedTtl, setErr)
        }

        setMultipleErr := backend.SetMultiple(testRuntime(), map[string][]byte{"b": []byte("2")}, refusedTtl)
        if nil == setMultipleErr || "ttl" != exception.LogContext(setMultipleErr, exception.LogContextConfig{})["field"] {
            t.Fatalf("expected SetMultiple to refuse the ttl %s naming it, got %v", refusedTtl, setMultipleErr)
        }
    }

    if _, exists, _ := backend.Get(testRuntime(), "a"); true == exists {
        t.Fatalf("expected the refused write not to have stored anything")
    }

    if setErr := backend.Set(testRuntime(), "a", []byte("1"), bound.Of(time.Second)); nil != setErr {
        t.Fatalf("expected the minimum ttl to be admitted, got %v", setErr)
    }
}

func TestInMemoryBackend_IncrementRefusesEmptyPayload(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    if setErr := backend.Set(testRuntime(), "marker", []byte(""), bound.UnlimitedDuration()); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    if _, incrementErr := backend.Increment(testRuntime(), "marker", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil == incrementErr {
        t.Fatalf("expected Increment to refuse an empty payload")
    }

    if setErr := backend.Set(testRuntime(), "blank", []byte("   "), bound.UnlimitedDuration()); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    if _, incrementErr := backend.Increment(testRuntime(), "blank", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil == incrementErr {
        t.Fatalf("expected Increment to refuse a blank payload")
    }

    value, exists, getErr := backend.Get(testRuntime(), "marker")
    if nil != getErr {
        t.Fatalf("get error: %v", getErr)
    }
    if false == exists || "" != string(value) {
        t.Fatalf("expected the refused Increment to have left the payload untouched")
    }
}

func TestInMemoryBackend_Get_ReturnsCopyOfBytes(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    _ = backend.Set(testRuntime(), "a", []byte{1, 2, 3}, bound.UnlimitedDuration())

    value, exists, err := backend.Get(testRuntime(), "a")
    if nil != err {
        t.Fatalf("get error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }

    value[0] = 9

    valueAgain, exists, err := backend.Get(testRuntime(), "a")
    if nil != err {
        t.Fatalf("get error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if byte(1) != valueAgain[0] {
        t.Fatalf("expected stored bytes to be isolated from mutations")
    }
}

func TestInMemoryBackend_ManySetMultipleDeleteMultipleClearDeleteHas(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    _ = backend.Set(testRuntime(), "a", []byte("1"), bound.UnlimitedDuration())
    _ = backend.Set(testRuntime(), "b", []byte("2"), bound.UnlimitedDuration())

    hasValue, err := backend.Has(testRuntime(), "a")
    if nil != err {
        t.Fatalf("has error: %v", err)
    }
    if true != hasValue {
        t.Fatalf("expected has true")
    }

    values, err := backend.Many(testRuntime(), []string{"a", "b", "c"})
    if nil != err {
        t.Fatalf("many error: %v", err)
    }
    if "1" != string(values["a"]) {
        t.Fatalf("unexpected value")
    }
    if "2" != string(values["b"]) {
        t.Fatalf("unexpected value")
    }
    if nil != values["c"] {
        t.Fatalf("expected c missing")
    }

    err = backend.SetMultiple(
        testRuntime(),
        map[string][]byte{
            "c": []byte("3"),
            "d": []byte("4"),
        },
        bound.UnlimitedDuration(),
    )
    if nil != err {
        t.Fatalf("setMultiple error: %v", err)
    }

    values, err = backend.Many(testRuntime(), []string{"c", "d"})
    if nil != err {
        t.Fatalf("many error: %v", err)
    }
    if "3" != string(values["c"]) {
        t.Fatalf("unexpected c")
    }
    if "4" != string(values["d"]) {
        t.Fatalf("unexpected d")
    }

    err = backend.DeleteMultiple(testRuntime(), []string{"c", "d"})
    if nil != err {
        t.Fatalf("deleteMultiple error: %v", err)
    }

    _, exists, err := backend.Get(testRuntime(), "c")
    if nil != err {
        t.Fatalf("get error: %v", err)
    }
    if true == exists {
        t.Fatalf("expected c deleted")
    }

    err = backend.Delete(testRuntime(), "a")
    if nil != err {
        t.Fatalf("delete error: %v", err)
    }

    _, exists, err = backend.Get(testRuntime(), "a")
    if nil != err {
        t.Fatalf("get error: %v", err)
    }
    if true == exists {
        t.Fatalf("expected a deleted")
    }

    err = backend.Clear(testRuntime())
    if nil != err {
        t.Fatalf("clear error: %v", err)
    }

    _, exists, err = backend.Get(testRuntime(), "b")
    if nil != err {
        t.Fatalf("get error: %v", err)
    }
    if true == exists {
        t.Fatalf("expected cleared")
    }
}

func TestInMemoryBackend_IncrementDecrement_HappyPath(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    value, err := backend.Increment(testRuntime(), "n", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil != err {
        t.Fatalf("increment error: %v", err)
    }
    if int64(1) != value {
        t.Fatalf("expected 1")
    }

    value, err = backend.Increment(testRuntime(), "n", 2, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil != err {
        t.Fatalf("increment error: %v", err)
    }
    if int64(3) != value {
        t.Fatalf("expected 3")
    }

    value, err = backend.Decrement(testRuntime(), "n", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil != err {
        t.Fatalf("decrement error: %v", err)
    }
    if int64(2) != value {
        t.Fatalf("expected 2")
    }
}

func TestInMemoryBackend_Increment_AcceptsExactlyTheRedisIntegerGrammar(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    _ = backend.Set(testRuntime(), "n", []byte("10"), bound.UnlimitedDuration())

    value, err := backend.Increment(testRuntime(), "n", 5, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil != err {
        t.Fatalf("increment error: %v", err)
    }
    if int64(15) != value {
        t.Fatalf("expected 15")
    }

    _ = backend.Set(testRuntime(), "negative", []byte("-3"), bound.UnlimitedDuration())

    value, err = backend.Increment(testRuntime(), "negative", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil != err {
        t.Fatalf("increment error: %v", err)
    }
    if int64(-2) != value {
        t.Fatalf("expected -2")
    }

    for _, payload := range []string{"not-a-number", " 10 ", "+10", "007", "-0", ""} {
        _ = backend.Set(testRuntime(), "bad", []byte(payload), bound.UnlimitedDuration())

        if _, err = backend.Increment(testRuntime(), "bad", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil == err {
            t.Fatalf("expected the payload %q to be refused the way redis refuses it", payload)
        }
    }
}

func TestInMemoryBackend_IncrementOverflow_ReturnsError(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    _ = backend.Set(testRuntime(), "n", []byte("9223372036854775807"), bound.UnlimitedDuration())

    _, err := backend.Increment(testRuntime(), "n", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestInMemoryBackend_Decrement_MinInt64Delta_ReturnsError(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    _, err := backend.Decrement(testRuntime(), "n", math.MinInt64, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestInMemoryBackend_IncrementPreservesExistingTtl(t *testing.T) {
    start := time.Unix(10, 0)
    clockInstance := &cacheTestClock{now: start}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})

    if setErr := backend.Set(testRuntime(), "counter", []byte("0"), bound.Of(10*time.Second)); nil != setErr {
        t.Fatalf("Set returned an error: %v", setErr)
    }

    if _, incrementErr := backend.Increment(testRuntime(), "counter", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil != incrementErr {
        t.Fatalf("Increment returned an error: %v", incrementErr)
    }

    clockInstance.now = start.Add(11 * time.Second)

    _, found, getErr := backend.Get(testRuntime(), "counter")
    if nil != getErr {
        t.Fatalf("Get returned an error: %v", getErr)
    }

    if true == found {
        t.Fatalf("Increment cleared the key's existing ttl: the counter outlived its 10s expiration, diverging from the Redis INCRBY contract")
    }
}

func TestInMemoryBackend_IncrementOnFreshKeyHasNoExpiry(t *testing.T) {
    start := time.Unix(10, 0)
    clockInstance := &cacheTestClock{now: start}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})

    if _, incrementErr := backend.Increment(testRuntime(), "fresh", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil != incrementErr {
        t.Fatalf("Increment returned an error: %v", incrementErr)
    }

    clockInstance.now = start.Add(365 * 24 * time.Hour)

    value, found, getErr := backend.Get(testRuntime(), "fresh")
    if nil != getErr {
        t.Fatalf("Get returned an error: %v", getErr)
    }

    if false == found {
        t.Fatalf("Increment on a previously-absent key must create a non-expiring counter, matching Redis INCRBY")
    }

    if "1" != string(value) {
        t.Fatalf("expected counter value 1, got %q", string(value))
    }
}

func TestInMemoryBackend_CleanupExpired_SweepsBeyondOneChunk(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    entryCount := cleanupChunkSize*2 + 7

    for index := 0; index < entryCount; index++ {
        _ = backend.Set(testRuntime(), fmt.Sprintf("expiring-%d", index), []byte("value"), bound.Of(time.Minute))
    }

    _ = backend.Set(testRuntime(), "surviving", []byte("value"), bound.Of(time.Hour))

    clockInstance.now = clockInstance.now.Add(2 * time.Minute)

    backend.cleanupExpired()

    backend.mutex.RLock()
    remaining := len(backend.entries)
    backend.mutex.RUnlock()

    if 1 != remaining {
        t.Fatalf("expected only the unexpired entry to survive the sweep, got %d of %d", remaining, entryCount+1)
    }

    if _, exists, _ := backend.Get(testRuntime(), "surviving"); false == exists {
        t.Fatalf("expected the unexpired entry to survive")
    }
}

func TestInMemoryBackend_Eviction_PrefersAnExpiredVictimWithinTheProbeWindow(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(3), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    _ = backend.Set(testRuntime(), "expiring", []byte("value"), bound.Of(time.Minute))
    _ = backend.Set(testRuntime(), "keep-one", []byte("value"), bound.Of(time.Hour))
    _ = backend.Set(testRuntime(), "keep-two", []byte("value"), bound.Of(time.Hour))

    clockInstance.now = clockInstance.now.Add(2 * time.Minute)

    _ = backend.Set(testRuntime(), "fresh", []byte("value"), bound.Of(time.Hour))

    if _, exists, _ := backend.Get(testRuntime(), "expiring"); true == exists {
        t.Fatalf("expected the expired entry to be the eviction victim")
    }

    for _, key := range []string{"keep-one", "keep-two", "fresh"} {
        if _, exists, _ := backend.Get(testRuntime(), key); false == exists {
            t.Fatalf("expected %q to survive the eviction", key)
        }
    }
}

func TestNewInMemoryBackend_ACleanupIntervalBelowItsMinimumIsRefused(t *testing.T) {
    for _, refusedInterval := range []time.Duration{0, -time.Second, time.Millisecond - time.Nanosecond} {
        assertInMemoryFieldPanic(t, "the limit is below its minimum", "InMemoryBackendConfig.CleanupInterval", func() {
            _ = NewInMemoryBackend(InMemoryBackendConfig{CleanupInterval: bound.Of(refusedInterval), Clock: &cacheTestClock{now: time.Unix(10, 0)}})
        })
    }

    assertInMemoryFieldPanic(t, "the limit does not admit unlimited", "InMemoryBackendConfig.CleanupInterval", func() {
        _ = NewInMemoryBackend(InMemoryBackendConfig{CleanupInterval: bound.UnlimitedDuration(), Clock: &cacheTestClock{now: time.Unix(10, 0)}})
    })

    defaultBackend := NewInMemoryBackend(InMemoryBackendConfig{Clock: &cacheTestClock{now: time.Unix(10, 0)}})
    defer defaultBackend.CloseWithContext(context.Background())

    if time.Minute != defaultBackend.cleanupTickInterval {
        t.Fatalf("expected an absent interval to take the one-minute default, got %v", defaultBackend.cleanupTickInterval)
    }

    backend := NewInMemoryBackend(InMemoryBackendConfig{CleanupInterval: bound.Of(time.Millisecond), Clock: &cacheTestClock{now: time.Unix(10, 0)}})
    defer backend.CloseWithContext(context.Background())

    if time.Millisecond != backend.cleanupTickInterval {
        t.Fatalf("expected the minimum interval to be kept, got %v", backend.cleanupTickInterval)
    }
}

func TestNewInMemoryBackend_ANilClockReadsTheSystemClock(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{})

    if _, isSystemClock := backend.clock.(*clock.SystemClock); false == isSystemClock {
        t.Fatalf("expected an absent clock to read the system clock, got %T", backend.clock)
    }

    if err := backend.CloseWithContext(t.Context()); nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }
}

func TestNewInMemoryBackend_AMaxItemsBelowOneIsRefused(t *testing.T) {
    for _, refusedMaxItems := range []int{0, -1} {
        assertInMemoryFieldPanic(t, "the limit is below its minimum", "InMemoryBackendConfig.MaxItems", func() {
            _ = NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(refusedMaxItems), Clock: &cacheTestClock{now: time.Unix(10, 0)}})
        })
    }
}

func TestNewInMemoryBackend_AnAbsentMaxItemsIsTenThousandAndUnlimitedLiftsTheCeiling(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    defaultBackend := NewInMemoryBackend(InMemoryBackendConfig{Clock: clockInstance})
    defer defaultBackend.CloseWithContext(context.Background())

    for index := 0; index <= 10_000; index++ {
        if setErr := defaultBackend.Set(testRuntime(), "key-"+strconv.Itoa(index), []byte("1"), bound.UnlimitedDuration()); nil != setErr {
            t.Fatalf("set error: %v", setErr)
        }
    }

    if 10_000 != len(defaultBackend.entries) {
        t.Fatalf("expected the default ceiling to hold 10,000 entries, got %d", len(defaultBackend.entries))
    }

    unlimitedBackend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.UnlimitedCount(), Clock: clockInstance})
    defer unlimitedBackend.CloseWithContext(context.Background())

    for index := 0; index <= 10_000; index++ {
        if setErr := unlimitedBackend.Set(testRuntime(), "key-"+strconv.Itoa(index), []byte("1"), bound.UnlimitedDuration()); nil != setErr {
            t.Fatalf("set error: %v", setErr)
        }
    }

    if 10_001 != len(unlimitedBackend.entries) {
        t.Fatalf("expected the unlimited ceiling to evict nothing, got %d entries", len(unlimitedBackend.entries))
    }
}

func assertInMemoryFieldPanic(t *testing.T, expectedMessage string, expectedField string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredErr, isError := recover().(error)
        if false == isError || expectedMessage != recoveredErr.Error() {
            t.Fatalf("expected %q to be refused, got %v", expectedMessage, recoveredErr)
        }

        if expectedField != exception.LogContext(recoveredErr, exception.LogContextConfig{})["field"] {
            t.Fatalf("expected the refusal to name %s, got %v", expectedField, exception.LogContext(recoveredErr, exception.LogContextConfig{}))
        }
    }()

    callback()
}

func assertInMemoryPanic(t *testing.T, expectedMessage string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected %q to be refused, got no panic", expectedMessage)
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic value, got %#v", recoveredValue)
        }

        if expectedMessage != recoveredErr.Error() {
            t.Fatalf("unexpected refusal message: %q", recoveredErr.Error())
        }
    }()

    callback()
}

func TestInMemoryBackend_HasSeparatesAbsentFromLapsedFromLive(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    if err := backend.Set(testRuntime(), "live", []byte("payload"), bound.UnlimitedDuration()); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    if err := backend.Set(testRuntime(), "lapsing", []byte("payload"), bound.Of(2*time.Second)); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    exists, hasErr := backend.Has(testRuntime(), "absent")
    if nil != hasErr || true == exists {
        t.Fatalf("expected an absent key to answer false, got %t, %v", exists, hasErr)
    }

    exists, hasErr = backend.Has(testRuntime(), "live")
    if nil != hasErr || false == exists {
        t.Fatalf("expected a live key to answer true, got %t, %v", exists, hasErr)
    }

    clockInstance.now = time.Unix(20, 0)

    exists, hasErr = backend.Has(testRuntime(), "lapsing")
    if nil != hasErr || true == exists {
        t.Fatalf("expected a lapsed key to answer false, got %t, %v", exists, hasErr)
    }

    exists, hasErr = backend.Has(testRuntime(), "live")
    if nil != hasErr || false == exists {
        t.Fatalf("expected the entry without a ttl to survive the clock, got %t, %v", exists, hasErr)
    }
}

func TestInMemoryBackend_ManySkipsTheKeysItCannotServe(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    if err := backend.Set(testRuntime(), "live", []byte("payload"), bound.UnlimitedDuration()); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    if err := backend.Set(testRuntime(), "lapsing", []byte("payload"), bound.Of(2*time.Second)); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    clockInstance.now = time.Unix(20, 0)

    result, manyErr := backend.Many(testRuntime(), []string{"live", "lapsing", "absent"})
    if nil != manyErr {
        t.Fatalf("unexpected many error: %v", manyErr)
    }

    if 1 != len(result) {
        t.Fatalf("expected only the live key to be served, got %#v", result)
    }

    if "payload" != string(result["live"]) {
        t.Fatalf("unexpected payload: %q", result["live"])
    }

    result, manyErr = backend.Many(testRuntime(), []string{"absent", "lapsing"})
    if nil != manyErr {
        t.Fatalf("unexpected many error: %v", manyErr)
    }

    if nil == result || 0 != len(result) {
        t.Fatalf("expected an empty map rather than nil when nothing is found, got %#v", result)
    }
}

func TestInMemoryBackend_DeletingAnAbsentKeyIsANoOp(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    if err := backend.Set(testRuntime(), "kept", []byte("payload"), bound.UnlimitedDuration()); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    if err := backend.Delete(testRuntime(), "absent"); nil != err {
        t.Fatalf("expected deleting an absent key to succeed, got %v", err)
    }

    if err := backend.DeleteMultiple(testRuntime(), []string{"absent", "other"}); nil != err {
        t.Fatalf("expected deleting absent keys to succeed, got %v", err)
    }

    _, exists, getErr := backend.Get(testRuntime(), "kept")
    if nil != getErr || false == exists {
        t.Fatalf("expected the untouched key to survive, got %t, %v", exists, getErr)
    }

    if 1 != backend.lruList.Len() {
        t.Fatalf("expected the recency list to keep exactly the surviving entry, got %d", backend.lruList.Len())
    }
}

func TestInMemoryBackend_TheExactInstantOfExpiryIsAlreadyLapsed(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    if err := backend.Set(testRuntime(), "lapsing", []byte("payload"), bound.Of(2*time.Second)); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    clockInstance.now = time.Unix(11, 999999999)

    _, exists, getErr := backend.Get(testRuntime(), "lapsing")
    if nil != getErr || false == exists {
        t.Fatalf("expected the entry to be alive a nanosecond before its expiry, got %t, %v", exists, getErr)
    }

    clockInstance.now = time.Unix(12, 0)

    _, exists, getErr = backend.Get(testRuntime(), "lapsing")
    if nil != getErr || true == exists {
        t.Fatalf("expected the entry to be lapsed at the exact instant of its expiry, got %t, %v", exists, getErr)
    }
}

func TestInMemoryBackend_DecrementIsRefusedRatherThanWrappingPastTheSmallestInt64(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    if err := backend.Set(testRuntime(), "counter", []byte(fmt.Sprintf("%d", int64(math.MinInt64)+1)), bound.UnlimitedDuration()); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    if _, decrementErr := backend.Decrement(testRuntime(), "counter", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil != decrementErr {
        t.Fatalf("expected the decrement that exactly reaches the bound to succeed, got %v", decrementErr)
    }

    newValue, decrementErr := backend.Decrement(testRuntime(), "counter", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil == decrementErr {
        t.Fatalf("expected the decrement past the smallest int64 to be refused, got %d", newValue)
    }

    if 0 != newValue {
        t.Fatalf("expected the refused decrement to answer zero, got %d", newValue)
    }

    stored, exists, getErr := backend.Get(testRuntime(), "counter")
    if nil != getErr || false == exists {
        t.Fatalf("unexpected get error: %t, %v", exists, getErr)
    }

    if fmt.Sprintf("%d", int64(math.MinInt64)) != string(stored) {
        t.Fatalf("expected the refused decrement to leave the counter untouched, got %q", stored)
    }
}

func TestInMemoryBackend_IncrementStartsOverOnceTheCounterHasLapsed(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    if _, err := backend.Increment(testRuntime(), "window", 5, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil != err {
        t.Fatalf("unexpected increment error: %v", err)
    }

    if err := backend.Set(testRuntime(), "window", []byte("5"), bound.Of(2*time.Second)); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    clockInstance.now = time.Unix(20, 0)

    newValue, incrementErr := backend.Increment(testRuntime(), "window", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil != incrementErr {
        t.Fatalf("unexpected increment error: %v", incrementErr)
    }

    if 1 != newValue {
        t.Fatalf("expected the lapsed counter to start over, got %d", newValue)
    }
}

func TestInMemoryBackend_TheTickerDrivesTheSweep(t *testing.T) {
    clockInstance := newCacheTickableTestClock(time.Unix(10, 0))

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    if err := backend.Set(testRuntime(), "lapsing", []byte("payload"), bound.Of(2*time.Second)); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    if err := backend.Set(testRuntime(), "kept", []byte("payload"), bound.UnlimitedDuration()); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    clockInstance.setNow(time.Unix(20, 0))

    clockInstance.tick()

    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) {
        backend.mutex.RLock()
        remaining := len(backend.entries)
        backend.mutex.RUnlock()

        if 1 == remaining {
            _, exists, getErr := backend.Get(testRuntime(), "kept")
            if nil != getErr || false == exists {
                t.Fatalf("expected the sweep to leave the entry without a ttl alone, got %t, %v", exists, getErr)
            }

            return
        }

        time.Sleep(time.Millisecond)
    }

    t.Fatalf("expected the ticker to drive the sweep that reclaims the lapsed entry")
}

func newCacheTickableTestClock(now time.Time) *cacheTickableTestClock {
    return &cacheTickableTestClock{
        now:           now,
        tickerChannel: make(chan time.Time),
    }
}

type cacheTickableTestClock struct {
    stateMutex    sync.RWMutex
    now           time.Time
    tickerChannel chan time.Time
}

func (instance *cacheTickableTestClock) Now() time.Time {
    instance.stateMutex.RLock()
    defer instance.stateMutex.RUnlock()

    return instance.now
}

func (instance *cacheTickableTestClock) setNow(now time.Time) {
    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    instance.now = now
}

func (instance *cacheTickableTestClock) tick() {
    instance.tickerChannel <- instance.Now()
}

func (instance *cacheTickableTestClock) NewTicker(interval time.Duration) clockcontract.Ticker {
    return &cacheTestTicker{
        channel: instance.tickerChannel,
    }
}

func (instance *cacheTickableTestClock) NewTimer(duration time.Duration) clockcontract.Timer {
    return clock.NewSystemClock().NewTimer(duration)
}

var _ clockcontract.Clock = (*cacheTickableTestClock)(nil)

func TestInMemoryBackend_EvictionToleratesARecencyListThatLostAgreementWithTheMap(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    t.Run("a list element that is not a key is dropped and the walk goes on", func(t *testing.T) {
        backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
        defer backend.CloseWithContext(context.Background())

        if err := backend.Set(testRuntime(), "real", []byte("payload"), bound.UnlimitedDuration()); nil != err {
            t.Fatalf("unexpected set error: %v", err)
        }

        backend.mutex.Lock()
        backend.lruList.PushBack(42)
        backend.evictOneLocked(clockInstance.now)
        remainingEntries := len(backend.entries)
        remainingElements := backend.lruList.Len()
        backend.mutex.Unlock()

        if 0 != remainingEntries {
            t.Fatalf("expected the walk to go past the intruder and evict the real entry, got %d entries", remainingEntries)
        }

        if 0 != remainingElements {
            t.Fatalf("expected both the intruder and the evicted element to leave the list, got %d", remainingElements)
        }
    })

    t.Run("a key the map has already lost is dropped from both sides", func(t *testing.T) {
        backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
        defer backend.CloseWithContext(context.Background())

        if err := backend.Set(testRuntime(), "real", []byte("payload"), bound.UnlimitedDuration()); nil != err {
            t.Fatalf("unexpected set error: %v", err)
        }

        backend.mutex.Lock()
        backend.lruList.PushBack("orphan")
        backend.evictOneLocked(clockInstance.now)
        _, orphanSurvives := backend.entries["orphan"]
        _, realSurvives := backend.entries["real"]
        remainingElements := backend.lruList.Len()
        backend.mutex.Unlock()

        if true == orphanSurvives {
            t.Fatalf("expected the orphan key to be dropped from the map")
        }

        if false == realSurvives {
            t.Fatalf("expected the eviction to stop at the orphan rather than take a live entry too")
        }

        if 1 != remainingElements {
            t.Fatalf("expected only the orphan element to leave the list, got %d remaining", remainingElements)
        }
    })

    t.Run("an intruder still at the back once the probe gave up is dropped without evicting anything", func(t *testing.T) {
        backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
        defer backend.CloseWithContext(context.Background())

        if err := backend.Set(testRuntime(), "real", []byte("payload"), bound.UnlimitedDuration()); nil != err {
            t.Fatalf("unexpected set error: %v", err)
        }

        backend.mutex.Lock()
        backend.lruList.PushBack(1)
        backend.lruList.PushBack(2)
        backend.evictOneLocked(clockInstance.now)
        _, realSurvives := backend.entries["real"]
        remainingElements := backend.lruList.Len()
        backend.mutex.Unlock()

        if false == realSurvives {
            t.Fatalf("expected the live entry to survive an eviction that only found intruders")
        }

        if 1 != remainingElements {
            t.Fatalf("expected both intruders to be dropped, got %d elements remaining", remainingElements)
        }
    })

    t.Run("a list the walk emptied evicts nothing rather than dereferencing its absent back", func(t *testing.T) {
        backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
        defer backend.CloseWithContext(context.Background())

        backend.mutex.Lock()
        backend.lruList.PushBack(1)
        backend.evictOneLocked(clockInstance.now)
        remainingElements := backend.lruList.Len()
        backend.mutex.Unlock()

        if 0 != remainingElements {
            t.Fatalf("expected the intruder to be dropped, got %d elements remaining", remainingElements)
        }
    })
}

func TestInMemoryBackend_AnEntryWithoutAnItemCountsAsLapsed(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    if false == backend.isExpiredAt(nil, clockInstance.now) {
        t.Fatalf("expected an absent item to count as lapsed")
    }
}

func TestInMemoryBackend_TheExpiringDeleteToleratesAKeyThatIsAlreadyGone(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    if err := backend.Set(testRuntime(), "kept", []byte("payload"), bound.UnlimitedDuration()); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    backend.mutex.Lock()
    backend.deleteExpiredLocked("collected.then.deleted", clockInstance.now)
    remainingEntries := len(backend.entries)
    remainingElements := backend.lruList.Len()
    backend.mutex.Unlock()

    if 1 != remainingEntries || 1 != remainingElements {
        t.Fatalf("expected the absent key to change nothing, got %d entries and %d elements", remainingEntries, remainingElements)
    }
}

func TestInMemoryBackend_ReadsToleratePlacementChangingUnderThem(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(1000), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    const keyCount = 32
    const rounds = 400

    keys := make([]string, 0, keyCount)
    for keyIndex := 0; keyIndex < keyCount; keyIndex = keyIndex + 1 {
        key := fmt.Sprintf("key.%d", keyIndex)
        keys = append(keys, key)

        if err := backend.Set(testRuntime(), key, []byte("payload"), bound.UnlimitedDuration()); nil != err {
            t.Fatalf("unexpected set error: %v", err)
        }
    }

    var waitGroup sync.WaitGroup
    waitGroup.Add(3)

    go func() {
        defer waitGroup.Done()

        for round := 0; round < rounds; round = round + 1 {
            for _, key := range keys {
                _, _, _ = backend.Get(testRuntime(), key)
            }
        }
    }()

    go func() {
        defer waitGroup.Done()

        for round := 0; round < rounds; round = round + 1 {
            _, _ = backend.Many(testRuntime(), keys)
        }
    }()

    go func() {
        defer waitGroup.Done()

        for round := 0; round < rounds; round = round + 1 {
            for _, key := range keys {
                _ = backend.Delete(testRuntime(), key)
                _ = backend.Set(testRuntime(), key, []byte("payload"), bound.UnlimitedDuration())
            }
        }
    }()

    waitGroup.Wait()

    backend.mutex.RLock()
    entryCount := len(backend.entries)
    elementCount := backend.lruList.Len()
    backend.mutex.RUnlock()

    if entryCount != elementCount {
        t.Fatalf("expected the map and the recency list to agree, got %d entries and %d elements", entryCount, elementCount)
    }
}

func TestInMemoryBackend_RefusesTheContractKeyGrammar(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{
        MaxItems:        bound.Of(10),
        CleanupInterval: bound.Of(time.Hour),
        Clock:           &cacheTestClock{now: time.Unix(10, 0)},
    })
    defer backend.CloseWithContext(context.Background())

    longKey := strings.Repeat("k", 1025)

    for name, key := range map[string]string{
        "spaces":   "user profile:alice",
        "newlines": "user\nprofile",
        "too long": longKey,
    } {
        if setErr := backend.Set(testRuntime(), key, []byte("payload"), bound.Of(time.Minute)); nil == setErr {
            t.Fatalf("expected the %s key to be refused by Set", name)
        }

        if _, _, getErr := backend.Get(testRuntime(), key); nil == getErr {
            t.Fatalf("expected the %s key to be refused by Get", name)
        }

        if _, incrementErr := backend.Increment(testRuntime(), key, 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil == incrementErr {
            t.Fatalf("expected the %s key to be refused by Increment", name)
        }
    }

    if setMultipleErr := backend.SetMultiple(testRuntime(), map[string][]byte{"a b": []byte("x")}, bound.Of(time.Minute)); nil == setMultipleErr {
        t.Fatal("expected the spaced key to be refused by SetMultiple")
    }

    if _, manyErr := backend.Many(testRuntime(), []string{"good", "with space"}); nil == manyErr {
        t.Fatal("expected the spaced key to be refused by Many")
    }

    keyAtTheBound := strings.Repeat("k", 1024)
    if setErr := backend.Set(testRuntime(), keyAtTheBound, []byte("payload"), bound.Of(time.Minute)); nil != setErr {
        t.Fatalf("expected a key of exactly 1024 bytes stored, got %v", setErr)
    }

    if payload, found, getErr := backend.Get(testRuntime(), keyAtTheBound); nil != getErr || false == found || "payload" != string(payload) {
        t.Fatalf("expected the key at the bound read back, got %q found %v error %v", payload, found, getErr)
    }
}

func TestInMemoryBackend_RefusalOrderMatchesTheRedisBackend(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    closed := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    if closeErr := closed.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("Close: %v", closeErr)
    }

    _, _, getErr := closed.Get(testRuntime(), "")
    if nil == getErr || false == strings.Contains(getErr.Error(), "cache backend is closed") {
        t.Fatalf("expected the closed refusal to win over the key judgment, got %v", getErr)
    }

    open := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer func() {
        _ = open.CloseWithContext(context.Background())
    }()

    setErr := open.SetMultiple(testRuntime(), map[string][]byte{"bad key": []byte("payload")}, bound.Of(-time.Second))
    if nil == setErr || "ttl" != exception.LogContext(setErr, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected the batch ttl judgment to precede the key judgment, got %v", setErr)
    }
}

func TestInMemoryBackend_ANilPayloadReadsBackEmptyNonNil(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    if setErr := backend.Set(testRuntime(), "k", nil, bound.UnlimitedDuration()); nil != setErr {
        t.Fatalf("set failed: %v", setErr)
    }

    payload, exists, getErr := backend.Get(testRuntime(), "k")
    if nil != getErr || false == exists {
        t.Fatalf("get failed: exists=%v err=%v", exists, getErr)
    }

    if nil == payload || 0 != len(payload) {
        t.Fatalf("expected the stored nil to read back as an empty non-nil slice, the redis identity; got nil=%v len=%d", nil == payload, len(payload))
    }
}

func TestInMemoryBackend_SetMultipleNamesTheSortedFirstMalformedKey(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: &cacheTestClock{now: time.Unix(10, 0)}})
    defer backend.CloseWithContext(context.Background())

    items := map[string][]byte{
        "zz bad": []byte("z"),
        "aa bad": []byte("a"),
        "good":   []byte("g"),
    }

    for attempt := 0; attempt < 8; attempt++ {
        setErr := backend.SetMultiple(testRuntime(), items, bound.UnlimitedDuration())
        if nil == setErr {
            t.Fatal("expected the malformed keys to be refused")
        }

        namedKey := exception.LogContext(setErr, exception.LogContextConfig{})["key"]
        if "aa bad" != namedKey {
            t.Fatalf("expected the sorted-first malformed key named, got %v", namedKey)
        }
    }
}

func TestInMemoryBackend_DecrementRefusesTheClosedBackendBeforeTheDeltaMagnitude(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})

    if closeErr := backend.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("Close returned an error: %v", closeErr)
    }

    _, decrementErr := backend.Decrement(testRuntime(), "counter", math.MinInt64, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil == decrementErr {
        t.Fatalf("expected a refusal from a closed backend")
    }

    if "cache backend is closed" != decrementErr.Error() {
        t.Fatalf("expected the closed refusal the redis sibling answers first, got: %v", decrementErr)
    }
}

func TestInMemoryBackend_DecrementRefusesTheMalformedKeyBeforeTheDeltaMagnitude(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    _, decrementErr := backend.Decrement(testRuntime(), "bad key", math.MinInt64, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil == decrementErr {
        t.Fatalf("expected a refusal for the malformed key")
    }

    if true == strings.Contains(decrementErr.Error(), "delta overflows") {
        t.Fatalf("expected the key refusal ahead of the magnitude one, got: %v", decrementErr)
    }
}

func TestValidateKey_ARefusalNamesAnOversizedKeyByABoundedPrefix(t *testing.T) {
    marker := "...(truncated)"

    for name, key := range map[string]string{
        "too long":          strings.Repeat("k", 1<<20),
        "with a space":      strings.Repeat("k", 1<<20) + " ",
        "with a newline":    strings.Repeat("k", 1<<20) + "\n",
        "cut inside a rune": strings.Repeat("k", 127) + "é" + strings.Repeat("k", 2000),
    } {
        refusal := validateKey(key, inMemoryBackendMaxKeyLengthRule.Default)

        var refusalException *exception.Error
        if false == errors.As(refusal, &refusalException) {
            t.Fatalf("%s: expected an exception, got %v", name, refusal)
        }

        rendered, _ := refusalException.Context()["key"].(string)
        if len(rendered) > 128+len(marker) || false == strings.HasSuffix(rendered, marker) || false == utf8.ValidString(rendered) {
            t.Fatalf("%s: expected the key cut to 128 bytes on a rune boundary with the marker, got %d bytes", name, len(rendered))
        }
    }

    if refusal := validateKey(strings.Repeat("k", 2000), inMemoryBackendMaxKeyLengthRule.Default); len(strings.Repeat("k", 2000)) != refusal.(*exception.Error).Context()["keyLength"] {
        t.Fatalf("expected the whole length beside the cut key, got %v", refusal.(*exception.Error).Context())
    }
}

func TestInMemoryBackend_CloseRefusesEveryLaterDoor(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: &cacheTestClock{now: time.Unix(10, 0)}})
    sibling := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: &cacheTestClock{now: time.Unix(10, 0)}})
    defer sibling.CloseWithContext(context.Background())

    _ = backend.CloseWithContext(t.Context())

    for door, call := range map[string]func() error{
        "Get":    func() error { _, _, err := backend.Get(testRuntime(), "key"); return err },
        "Has":    func() error { _, err := backend.Has(testRuntime(), "key"); return err },
        "Set":    func() error { return backend.Set(testRuntime(), "key", []byte("value"), bound.Of(time.Minute)) },
        "Delete": func() error { return backend.Delete(testRuntime(), "key") },
        "Clear":  func() error { return backend.Clear(testRuntime()) },
        "Many":   func() error { _, err := backend.Many(testRuntime(), []string{"key"}); return err },
        "SetMultiple": func() error {
            return backend.SetMultiple(testRuntime(), map[string][]byte{"key": []byte("value")}, bound.Of(time.Minute))
        },
        "DeleteMultiple": func() error { return backend.DeleteMultiple(testRuntime(), []string{"key"}) },
        "Increment":      func() error { _, err := backend.Increment(testRuntime(), "counter", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); return err },
        "Decrement":      func() error { _, err := backend.Decrement(testRuntime(), "counter", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); return err },
    } {
        if closedErr := call(); nil == closedErr || false == strings.Contains(closedErr.Error(), "cache backend is closed") {
            t.Fatalf("expected %s refused after Close, got %v", door, closedErr)
        }
    }

    if setErr := sibling.Set(testRuntime(), "key", []byte("value"), bound.Of(time.Minute)); nil != setErr {
        t.Fatalf("expected a sibling backend untouched by the close, got %v", setErr)
    }
}

func TestInMemoryBackend_IncrementOnAnOutOfRangePayloadKeepsThePayloadOutOfTheChain(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: &cacheTestClock{now: time.Unix(10, 0)}})
    defer backend.CloseWithContext(context.Background())

    payload := "999999999999999999999999999999"
    if setErr := backend.Set(testRuntime(), "counter", []byte(payload), bound.UnlimitedDuration()); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    _, incrementErr := backend.Increment(testRuntime(), "counter", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil == incrementErr {
        t.Fatalf("expected the out-of-range payload to be refused")
    }

    if rendered := fmt.Sprintf("%v %v", incrementErr, exception.LogContext(incrementErr, exception.LogContextConfig{})); true == strings.Contains(rendered, payload) {
        t.Fatalf("expected the payload withheld from the whole chain, got %s", rendered)
    }

    if false == strings.Contains(fmt.Sprintf("%v", exception.LogContext(incrementErr, exception.LogContextConfig{})), "out of the int64 range") {
        t.Fatalf("expected the range named as the cause, got %v", exception.LogContext(incrementErr, exception.LogContextConfig{}))
    }
}

func TestInMemoryBackend_IncrementOnATextPayloadKeepsThePayloadOutOfTheRefusal(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: &cacheTestClock{now: time.Unix(10, 0)}})
    defer backend.CloseWithContext(context.Background())

    if setErr := backend.Set(testRuntime(), "counter", []byte("not-a-number-but-a-secret"), bound.UnlimitedDuration()); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    _, incrementErr := backend.Increment(testRuntime(), "counter", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    melodyErr, isMelodyErr := incrementErr.(*exception.Error)
    if false == isMelodyErr || "cache value is not a valid int64" != melodyErr.Message() {
        t.Fatalf("expected the textual payload to be refused, got %v", incrementErr)
    }

    if _, spelled := melodyErr.Context()["value"]; true == spelled {
        t.Fatalf("expected the payload withheld, got %v", melodyErr.Context())
    }

    if "counter" != melodyErr.Context()["key"] || 25 != melodyErr.Context()["payloadLength"] {
        t.Fatalf("expected the key and the payload length, got %v", melodyErr.Context())
    }
}

func TestAwaitCleanupLoop_ALoopStillRunningPastTheDeadlineAnswersTheDeadline(t *testing.T) {
    closeContext, cancel := context.WithCancel(t.Context())
    cancel()

    waitErr := awaitCleanupLoop(closeContext, make(chan struct{}))
    if false == errors.Is(waitErr, context.Canceled) {
        t.Fatalf("expected the deadline's error, got %v", waitErr)
    }
}

func TestAwaitCleanupLoop_ALoopThatLeftAnswersNilEvenPastTheDeadline(t *testing.T) {
    closeContext, cancel := context.WithCancel(t.Context())
    cancel()

    cleanupDone := make(chan struct{})
    close(cleanupDone)

    for attempt := 0; attempt < 64; attempt++ {
        if waitErr := awaitCleanupLoop(closeContext, cleanupDone); nil != waitErr {
            t.Fatalf("expected a loop that left to answer nil, got %v", waitErr)
        }
    }
}

func TestAwaitCleanupLoop_ALoopThatLeavesBeforeTheDeadlineAnswersNil(t *testing.T) {
    cleanupDone := make(chan struct{})
    close(cleanupDone)

    if waitErr := awaitCleanupLoop(t.Context(), cleanupDone); nil != waitErr {
        t.Fatalf("expected nil, got %v", waitErr)
    }
}

type parkingCacheClock struct {
    tickerChannel chan time.Time
    parked        chan struct{}
    release       chan struct{}
    parkOnce      sync.Once
}

func (instance *parkingCacheClock) Now() time.Time {
    instance.parkOnce.Do(func() {
        close(instance.parked)
        <-instance.release
    })

    return time.Unix(10, 0)
}

func (instance *parkingCacheClock) NewTicker(interval time.Duration) clockcontract.Ticker {
    return &cacheTestTicker{channel: instance.tickerChannel}
}

func (instance *parkingCacheClock) NewTimer(duration time.Duration) clockcontract.Timer {
    return clock.NewSystemClock().NewTimer(duration)
}

func TestInMemoryBackend_CloseWithContextAnswersTheDeadlineOverAParkedCleanupLoopAndALaterCloseFinishes(t *testing.T) {
    clockInstance := &parkingCacheClock{tickerChannel: make(chan time.Time), parked: make(chan struct{}), release: make(chan struct{})}
    backend := NewInMemoryBackend(InMemoryBackendConfig{CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})

    clockInstance.tickerChannel <- time.Unix(10, 0)
    <-clockInstance.parked

    closeContext, cancel := context.WithCancel(t.Context())
    cancel()

    if closeErr := backend.CloseWithContext(closeContext); false == errors.Is(closeErr, context.Canceled) {
        t.Fatalf("expected the caller's deadline over a parked cleanup loop, got %v", closeErr)
    }

    close(clockInstance.release)

    if closeErr := backend.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("expected the released loop to let a later close finish, got %v", closeErr)
    }
}

func inMemoryContextOperations(backend *InMemoryBackend) map[string]func(runtimeInstance containercontract.Runtime, key string) error {
    return map[string]func(runtimeInstance containercontract.Runtime, key string) error{
        "Get": func(runtimeInstance containercontract.Runtime, key string) error {
            _, _, err := backend.Get(runtimeInstance, key)
            return err
        },
        "Set": func(runtimeInstance containercontract.Runtime, key string) error {
            return backend.Set(runtimeInstance, key, []byte("written"), bound.Of(time.Minute))
        },
        "Delete": func(runtimeInstance containercontract.Runtime, key string) error {
            return backend.Delete(runtimeInstance, key)
        },
        "Has": func(runtimeInstance containercontract.Runtime, key string) error {
            _, err := backend.Has(runtimeInstance, key)
            return err
        },
        "Clear": func(runtimeInstance containercontract.Runtime, key string) error {
            return backend.Clear(runtimeInstance)
        },
        "Many": func(runtimeInstance containercontract.Runtime, key string) error {
            _, err := backend.Many(runtimeInstance, []string{key})
            return err
        },
        "SetMultiple": func(runtimeInstance containercontract.Runtime, key string) error {
            return backend.SetMultiple(runtimeInstance, map[string][]byte{key: []byte("written")}, bound.Of(time.Minute))
        },
        "DeleteMultiple": func(runtimeInstance containercontract.Runtime, key string) error {
            return backend.DeleteMultiple(runtimeInstance, []string{key})
        },
        "Increment": func(runtimeInstance containercontract.Runtime, key string) error {
            _, err := backend.Increment(runtimeInstance, key, 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
            return err
        },
        "Decrement": func(runtimeInstance containercontract.Runtime, key string) error {
            _, err := backend.Decrement(runtimeInstance, key, 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
            return err
        },
    }
}

func TestInMemoryBackend_EveryOperationRefusesAnAbsentRuntimeFirst(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour)})
    if closeErr := backend.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    for name, operation := range inMemoryContextOperations(backend) {
        for _, refusedRuntime := range refusedTestRuntimes() {
            operationErr := operation(refusedRuntime, "")
            if nil == operationErr || refusedRuntimeMessage != operationErr.Error() {
                t.Fatalf("%s: expected the absent runtime refused first, got %v", name, operationErr)
            }
        }
    }
}

func TestInMemoryBackend_ADoneContextIsAnsweredAfterTheArgumentsAndTouchesNothing(t *testing.T) {
    canceledContext, cancel := context.WithCancel(context.Background())
    cancel()

    for name := range inMemoryContextOperations(nil) {
        backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour)})

        if setErr := backend.Set(testRuntime(), "held", []byte("7"), bound.Of(time.Minute)); nil != setErr {
            t.Fatalf("planting: %v", setErr)
        }

        operation := inMemoryContextOperations(backend)[name]

        if "Clear" != name {
            if keyErr := operation(testRuntimeUnder(canceledContext), ""); nil == keyErr || true == errors.Is(keyErr, context.Canceled) {
                t.Fatalf("%s: expected the malformed key refused before the done context, got %v", name, keyErr)
            }
        }

        for _, key := range []string{"held", "fresh"} {
            if doneErr := operation(testRuntimeUnder(canceledContext), key); false == errors.Is(doneErr, context.Canceled) {
                t.Fatalf("%s %s: expected the done context's error, got %v", name, key, doneErr)
            }
        }

        payload, exists, getErr := backend.Get(testRuntime(), "held")
        if nil != getErr || false == exists || "7" != string(payload) {
            t.Fatalf("%s: expected the held entry untouched, got %q, %v, %v", name, payload, exists, getErr)
        }

        if freshExists, hasErr := backend.Has(testRuntime(), "fresh"); nil != hasErr || true == freshExists {
            t.Fatalf("%s: expected nothing written under the done context, got %v, %v", name, freshExists, hasErr)
        }

        _ = backend.CloseWithContext(context.Background())
    }
}

func TestInMemoryBackend_AnEmptyBatchUnderADoneContextAnswersItsError(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour)})
    defer backend.CloseWithContext(context.Background())

    canceledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if _, manyErr := backend.Many(testRuntimeUnder(canceledContext), nil); false == errors.Is(manyErr, context.Canceled) {
        t.Fatalf("Many: expected the done context's error, got %v", manyErr)
    }

    if setErr := backend.SetMultiple(testRuntimeUnder(canceledContext), nil, bound.Of(time.Minute)); false == errors.Is(setErr, context.Canceled) {
        t.Fatalf("SetMultiple: expected the done context's error, got %v", setErr)
    }

    if deleteErr := backend.DeleteMultiple(testRuntimeUnder(canceledContext), nil); false == errors.Is(deleteErr, context.Canceled) {
        t.Fatalf("DeleteMultiple: expected the done context's error, got %v", deleteErr)
    }
}

func TestInMemoryBackend_UnderADoneContextTheTtlAndTheDeltaAreJudgedFirst(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour)})
    defer backend.CloseWithContext(context.Background())

    canceledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if setErr := backend.Set(testRuntimeUnder(canceledContext), "key", []byte("value"), bound.Duration{}); nil == setErr || true == errors.Is(setErr, context.Canceled) {
        t.Fatalf("Set: expected the absent ttl refused before the done context, got %v", setErr)
    }

    if setErr := backend.SetMultiple(testRuntimeUnder(canceledContext), map[string][]byte{"key": []byte("value")}, bound.Duration{}); nil == setErr || true == errors.Is(setErr, context.Canceled) {
        t.Fatalf("SetMultiple: expected the absent ttl refused before the done context, got %v", setErr)
    }

    if _, decrementErr := backend.Decrement(testRuntimeUnder(canceledContext), "key", math.MinInt64, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil == decrementErr || true == errors.Is(decrementErr, context.Canceled) {
        t.Fatalf("Decrement: expected the delta refused before the done context, got %v", decrementErr)
    }
}

func counterTestBackend() (*InMemoryBackend, *cacheTestClock) {
    clockInstance := &cacheTestClock{now: time.Unix(1000, 0)}

    return NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance}), clockInstance
}

func TestInMemoryBackend_AFixedWindowCounterExpiresAtTheTtlFromItsFirstIncrement(t *testing.T) {
    backend, clockInstance := counterTestBackend()
    defer backend.CloseWithContext(context.Background())

    options := cachecontract.CounterOptions{Ttl: bound.Of(time.Minute)}
    if _, incrementErr := backend.Increment(testRuntime(), "hits", 1, options); nil != incrementErr {
        t.Fatal(incrementErr)
    }

    clockInstance.now = clockInstance.now.Add(40 * time.Second)
    if value, _ := backend.Increment(testRuntime(), "hits", 1, options); 2 != value {
        t.Fatalf("expected 2, got %d", value)
    }

    clockInstance.now = clockInstance.now.Add(21 * time.Second)
    if _, exists, _ := backend.Get(testRuntime(), "hits"); true == exists {
        t.Fatal("expected the fixed window gone at the ttl from the first increment")
    }

    if value, _ := backend.Increment(testRuntime(), "hits", 5, options); 5 != value {
        t.Fatalf("expected an expired counter to start from its delta, got %d", value)
    }
}

func TestInMemoryBackend_ASlidingCounterMovesItsExpiryOnEveryCall(t *testing.T) {
    backend, clockInstance := counterTestBackend()
    defer backend.CloseWithContext(context.Background())

    options := cachecontract.CounterOptions{Ttl: bound.Of(time.Minute), RefreshTtl: true}
    backend.Increment(testRuntime(), "hits", 1, options)

    clockInstance.now = clockInstance.now.Add(40 * time.Second)
    backend.Decrement(testRuntime(), "hits", 3, options)

    clockInstance.now = clockInstance.now.Add(40 * time.Second)
    if value, exists, _ := backend.Get(testRuntime(), "hits"); false == exists || "-2" != string(value) {
        t.Fatalf("expected the sliding counter alive at -2, got %q %v", value, exists)
    }
}

func TestInMemoryBackend_ASlidingCounterExpiresItsSpanAfterItsLastCall(t *testing.T) {
    backend, clockInstance := counterTestBackend()
    defer backend.CloseWithContext(context.Background())

    options := cachecontract.CounterOptions{Ttl: bound.Of(time.Minute), RefreshTtl: true}
    backend.Increment(testRuntime(), "hits", 1, options)
    clockInstance.now = clockInstance.now.Add(40 * time.Second)
    backend.Increment(testRuntime(), "hits", 1, options)

    clockInstance.now = clockInstance.now.Add(61 * time.Second)
    if _, exists, _ := backend.Get(testRuntime(), "hits"); true == exists {
        t.Fatal("expected the sliding counter gone a span after its last call")
    }
}

func TestInMemoryBackend_AnUnlimitedFixedCounterKeepsAnExpiryTheKeyCarries(t *testing.T) {
    backend, clockInstance := counterTestBackend()
    defer backend.CloseWithContext(context.Background())

    backend.Increment(testRuntime(), "hits", 1, cachecontract.CounterOptions{Ttl: bound.Of(time.Minute)})
    backend.Increment(testRuntime(), "hits", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})

    clockInstance.now = clockInstance.now.Add(61 * time.Second)
    if _, exists, _ := backend.Get(testRuntime(), "hits"); true == exists {
        t.Fatal("expected an unlimited call to leave the minute expiry in place")
    }

    backend.Increment(testRuntime(), "forever", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    clockInstance.now = clockInstance.now.Add(1000 * time.Hour)
    if _, exists, _ := backend.Get(testRuntime(), "forever"); false == exists {
        t.Fatal("expected an unlimited counter kept")
    }
}

func TestInMemoryBackend_AnUnlimitedSlidingCounterRemovesAnExpiryTheKeyCarries(t *testing.T) {
    backend, clockInstance := counterTestBackend()
    defer backend.CloseWithContext(context.Background())

    backend.Set(testRuntime(), "hits", []byte("4"), bound.Of(time.Hour))
    if value, _ := backend.Increment(testRuntime(), "hits", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration(), RefreshTtl: true}); 5 != value {
        t.Fatalf("expected 5, got %d", value)
    }

    clockInstance.now = clockInstance.now.Add(2 * time.Hour)
    if value, exists, _ := backend.Get(testRuntime(), "hits"); false == exists || "5" != string(value) {
        t.Fatalf("expected the unlimited sliding counter kept past the hour it carried, got %q %v", value, exists)
    }
}

func TestInMemoryBackend_AnUnlimitedSlidingDecrementRemovesTheExpiry(t *testing.T) {
    backend, clockInstance := counterTestBackend()
    defer backend.CloseWithContext(context.Background())

    backend.Increment(testRuntime(), "hits", 4, cachecontract.CounterOptions{Ttl: bound.Of(time.Minute)})
    if value, _ := backend.Decrement(testRuntime(), "hits", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration(), RefreshTtl: true}); 3 != value {
        t.Fatalf("expected 3, got %d", value)
    }

    clockInstance.now = clockInstance.now.Add(1000 * time.Hour)
    if value, exists, _ := backend.Get(testRuntime(), "hits"); false == exists || "3" != string(value) {
        t.Fatalf("expected the unlimited sliding decrement to remove the minute expiry, got %q %v", value, exists)
    }
}

func TestInMemoryBackend_AFixedWindowWritesItsTtlOnAKeyWithoutExpiry(t *testing.T) {
    backend, clockInstance := counterTestBackend()
    defer backend.CloseWithContext(context.Background())

    backend.Set(testRuntime(), "hits", []byte("4"), bound.UnlimitedDuration())
    if value, _ := backend.Increment(testRuntime(), "hits", 1, cachecontract.CounterOptions{Ttl: bound.Of(time.Minute)}); 5 != value {
        t.Fatalf("expected 5, got %d", value)
    }

    clockInstance.now = clockInstance.now.Add(61 * time.Second)
    if _, exists, _ := backend.Get(testRuntime(), "hits"); true == exists {
        t.Fatal("expected the fixed window written on a key found without an expiry")
    }
}

func TestInMemoryBackend_ACounterWithoutATtlIsRefusedAfterTheKeyAndTheDelta(t *testing.T) {
    backend, _ := counterTestBackend()
    defer backend.CloseWithContext(context.Background())

    if _, incrementErr := backend.Increment(testRuntime(), "hits", 1, cachecontract.CounterOptions{}); nil == incrementErr || false == strings.Contains(incrementErr.Error(), "ttl is required") {
        t.Fatalf("expected the absent ttl refused, got %v", incrementErr)
    }

    if _, decrementErr := backend.Decrement(testRuntime(), "hits", math.MinInt64, cachecontract.CounterOptions{}); nil == decrementErr || false == strings.Contains(decrementErr.Error(), "overflows") {
        t.Fatalf("expected the delta refused before the ttl, got %v", decrementErr)
    }

    if _, keyErr := backend.Increment(testRuntime(), "", 1, cachecontract.CounterOptions{}); nil == keyErr || true == strings.Contains(keyErr.Error(), "ttl is required") {
        t.Fatalf("expected the key refused before the ttl, got %v", keyErr)
    }

    if _, exists, _ := backend.Get(testRuntime(), "hits"); true == exists {
        t.Fatal("expected a refused increment to write nothing")
    }
}

func TestInMemoryBackend_ACounterRefusesAnAbsentTtlBeforeACancelledContext(t *testing.T) {
    backend, _ := counterTestBackend()
    defer backend.CloseWithContext(context.Background())

    cancelled, cancel := context.WithCancel(context.Background())
    cancel()

    if _, ttlErr := backend.Increment(testRuntimeUnder(cancelled), "hits", 1, cachecontract.CounterOptions{}); nil == ttlErr || false == strings.Contains(ttlErr.Error(), "ttl is required") {
        t.Fatalf("expected the absent ttl refused before the cancelled context, got %v", ttlErr)
    }
}

func TestInMemoryBackend_MaxKeyLengthDefaultsRaisesAndIsRefusedOutsideItsRule(t *testing.T) {
    defaulted := NewInMemoryBackend(InMemoryBackendConfig{})
    defer defaulted.CloseWithContext(context.Background())

    if 1024 != defaulted.maxKeyLength {
        t.Fatalf("expected a left-out key length to resolve to 1024, got %d", defaulted.maxKeyLength)
    }

    if setErr := defaulted.Set(testRuntime(), strings.Repeat("k", 1025), []byte("v"), bound.UnlimitedDuration()); nil == setErr || "cache key is too long" != setErr.Error() {
        t.Fatalf("expected a key past 1024 bytes refused under the default, got %v", setErr)
    }

    raised := NewInMemoryBackend(InMemoryBackendConfig{MaxKeyLength: bound.Of(4096)})
    defer raised.CloseWithContext(context.Background())

    if setErr := raised.Set(testRuntime(), strings.Repeat("k", 4096), []byte("v"), bound.UnlimitedDuration()); nil != setErr {
        t.Fatalf("expected a raised key length to admit a 4096-byte key, got %v", setErr)
    }

    if _, _, getErr := raised.Get(testRuntime(), strings.Repeat("k", 4097)); nil == getErr || 4096 != getErr.(*exception.Error).Context()["maxKeyLength"] {
        t.Fatalf("expected a key past the raised length refused naming it, got %v", getErr)
    }

    if deleteErr := raised.DeleteMultiple(testRuntime(), []string{strings.Repeat("k", 4096)}); nil != deleteErr {
        t.Fatalf("expected a raised key length to admit a 4096-byte key in a key list, got %v", deleteErr)
    }

    for name, configured := range map[string]bound.Count{"below its minimum": bound.Of(0), "unlimited": bound.UnlimitedCount()} {
        t.Run(name, func(t *testing.T) {
            defer func() {
                recoveredErr, isError := recover().(error)
                if false == isError || "InMemoryBackendConfig.MaxKeyLength" != exception.LogContext(recoveredErr, exception.LogContextConfig{})["field"] {
                    t.Fatalf("expected the refusal naming InMemoryBackendConfig.MaxKeyLength, got %v", recoveredErr)
                }
            }()

            NewInMemoryBackend(InMemoryBackendConfig{MaxKeyLength: configured})
        })
    }
}
