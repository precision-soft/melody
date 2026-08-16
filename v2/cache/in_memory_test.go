package cache

import (
    "fmt"
    "math"
    "strings"
    "sync"
    "testing"
    "time"

    clockcontract "github.com/precision-soft/melody/v2/clock/contract"
    "github.com/precision-soft/melody/v2/exception"
)

func TestInMemoryBackend_SetGet_HappyPath(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(
        10,
        time.Hour,
        clockInstance,
    )
    defer backend.Close()

    err := backend.Set("product.1", []byte("payload"), 0)
    if nil != err {
        t.Fatalf("set error: %v", err)
    }

    value, exists, err := backend.Get("product.1")
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

    backend := NewInMemoryBackend(
        10,
        time.Hour,
        clockInstance,
    )
    defer backend.Close()

    err := backend.Set("product.1", []byte("payload"), 2*time.Second)
    if nil != err {
        t.Fatalf("set error: %v", err)
    }

    clockInstance.now = time.Unix(20, 0)

    _, exists, err := backend.Get("product.1")
    if nil != err {
        t.Fatalf("get error: %v", err)
    }
    if true == exists {
        t.Fatalf("expected cache miss due to ttl")
    }
}

func TestInMemoryBackend_LruEviction(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(
        2,
        time.Hour,
        clockInstance,
    )
    defer backend.Close()

    _ = backend.Set("a", []byte("a"), 0)
    _ = backend.Set("b", []byte("b"), 0)

    clockInstance.now = clockInstance.now.Add(recencyPromotionInterval)

    _, _, _ = backend.Get("a")

    _ = backend.Set("c", []byte("c"), 0)

    _, exists, _ := backend.Get("b")
    if true == exists {
        t.Fatalf("expected b to be evicted")
    }

    _, exists, _ = backend.Get("a")
    if false == exists {
        t.Fatalf("expected a to remain")
    }

    _, exists, _ = backend.Get("c")
    if false == exists {
        t.Fatalf("expected c to exist")
    }
}

func TestInMemoryBackend_AReadInsideThePromotionIntervalLeavesTheRecencyOrderWhereItWas(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(
        2,
        time.Hour,
        clockInstance,
    )
    defer backend.Close()

    _ = backend.Set("a", []byte("a"), 0)
    _ = backend.Set("b", []byte("b"), 0)

    clockInstance.now = clockInstance.now.Add(recencyPromotionInterval - time.Nanosecond)

    _, _, _ = backend.Get("a")

    _ = backend.Set("c", []byte("c"), 0)

    _, exists, _ := backend.Get("a")
    if true == exists {
        t.Fatalf("expected the read inside the promotion interval to leave a at the back, so a is the victim")
    }

    _, exists, _ = backend.Get("b")
    if false == exists {
        t.Fatalf("expected b to remain")
    }
}

func TestInMemoryBackend_AReadRefreshesTheAccessMarkEvenWithoutAPromotion(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(
        10,
        time.Hour,
        clockInstance,
    )
    defer backend.Close()

    _ = backend.Set("a", []byte("a"), 0)

    clockInstance.now = clockInstance.now.Add(time.Millisecond)

    _, _, _ = backend.Get("a")

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

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    if setErr := backend.Set("", []byte("1"), 0); nil == setErr {
        t.Fatalf("expected Set to refuse the empty key")
    }

    if _, _, getErr := backend.Get(""); nil == getErr {
        t.Fatalf("expected Get to refuse the empty key")
    }

    if _, hasErr := backend.Has(""); nil == hasErr {
        t.Fatalf("expected Has to refuse the empty key")
    }

    if deleteErr := backend.Delete(""); nil == deleteErr {
        t.Fatalf("expected Delete to refuse the empty key")
    }

    if _, manyErr := backend.Many([]string{"a", ""}); nil == manyErr {
        t.Fatalf("expected Many to refuse the empty key")
    }

    if setMultipleErr := backend.SetMultiple(map[string][]byte{"": []byte("1")}, 0); nil == setMultipleErr {
        t.Fatalf("expected SetMultiple to refuse the empty key")
    }

    if deleteMultipleErr := backend.DeleteMultiple([]string{""}); nil == deleteMultipleErr {
        t.Fatalf("expected DeleteMultiple to refuse the empty key")
    }

    if _, incrementErr := backend.Increment("", 1); nil == incrementErr {
        t.Fatalf("expected Increment to refuse the empty key")
    }

    if _, decrementErr := backend.Decrement("", 1); nil == decrementErr {
        t.Fatalf("expected Decrement to refuse the empty key")
    }

    if _, exists, _ := backend.Get("a"); true == exists {
        t.Fatalf("expected no entry to have been written through a refused key")
    }
}

func TestInMemoryBackend_RefusesOperationsAfterClose(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)

    if setErr := backend.Set("a", []byte("1"), 0); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    if closeErr := backend.Close(); nil != closeErr {
        t.Fatalf("close error: %v", closeErr)
    }

    if setErr := backend.Set("b", []byte("2"), 0); nil == setErr {
        t.Fatalf("expected Set to refuse a closed backend")
    }

    if _, _, getErr := backend.Get("a"); nil == getErr {
        t.Fatalf("expected Get to refuse a closed backend")
    }

    if _, hasErr := backend.Has("a"); nil == hasErr {
        t.Fatalf("expected Has to refuse a closed backend")
    }

    if deleteErr := backend.Delete("a"); nil == deleteErr {
        t.Fatalf("expected Delete to refuse a closed backend")
    }

    if clearErr := backend.Clear(); nil == clearErr {
        t.Fatalf("expected Clear to refuse a closed backend")
    }

    if _, manyErr := backend.Many([]string{"a"}); nil == manyErr {
        t.Fatalf("expected Many to refuse a closed backend")
    }

    if setMultipleErr := backend.SetMultiple(map[string][]byte{"c": []byte("3")}, 0); nil == setMultipleErr {
        t.Fatalf("expected SetMultiple to refuse a closed backend")
    }

    if deleteMultipleErr := backend.DeleteMultiple([]string{"a"}); nil == deleteMultipleErr {
        t.Fatalf("expected DeleteMultiple to refuse a closed backend")
    }

    if _, incrementErr := backend.Increment("n", 1); nil == incrementErr {
        t.Fatalf("expected Increment to refuse a closed backend")
    }

    if _, decrementErr := backend.Decrement("n", 1); nil == decrementErr {
        t.Fatalf("expected Decrement to refuse a closed backend")
    }

    if closeErr := backend.Close(); nil != closeErr {
        t.Fatalf("expected the second Close to stay idempotent, got: %v", closeErr)
    }
}

func TestInMemoryBackend_RefusesNegativeTtl(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    if setErr := backend.Set("a", []byte("1"), -time.Second); nil == setErr {
        t.Fatalf("expected Set to refuse a negative ttl")
    }

    if setMultipleErr := backend.SetMultiple(map[string][]byte{"b": []byte("2")}, -time.Second); nil == setMultipleErr {
        t.Fatalf("expected SetMultiple to refuse a negative ttl")
    }

    if _, exists, _ := backend.Get("a"); true == exists {
        t.Fatalf("expected the refused write not to have stored anything")
    }
}

func TestNewInMemoryBackend_PanicsOnNegativeMaxItems(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic on negative maxItems")
        }
    }()

    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    _ = NewInMemoryBackend(-1, time.Hour, clockInstance)
}

func TestInMemoryBackend_IncrementRefusesEmptyPayload(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    if setErr := backend.Set("marker", []byte(""), 0); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    if _, incrementErr := backend.Increment("marker", 1); nil == incrementErr {
        t.Fatalf("expected Increment to refuse an empty payload")
    }

    if setErr := backend.Set("blank", []byte("   "), 0); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    if _, incrementErr := backend.Increment("blank", 1); nil == incrementErr {
        t.Fatalf("expected Increment to refuse a blank payload")
    }

    value, exists, getErr := backend.Get("marker")
    if nil != getErr {
        t.Fatalf("get error: %v", getErr)
    }
    if false == exists || "" != string(value) {
        t.Fatalf("expected the refused Increment to have left the payload untouched")
    }
}

func TestInMemoryBackend_Get_ReturnsCopyOfBytes(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    _ = backend.Set("a", []byte{1, 2, 3}, 0)

    value, exists, err := backend.Get("a")
    if nil != err {
        t.Fatalf("get error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }

    value[0] = 9

    valueAgain, exists, err := backend.Get("a")
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

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    _ = backend.Set("a", []byte("1"), 0)
    _ = backend.Set("b", []byte("2"), 0)

    hasValue, err := backend.Has("a")
    if nil != err {
        t.Fatalf("has error: %v", err)
    }
    if true != hasValue {
        t.Fatalf("expected has true")
    }

    values, err := backend.Many([]string{"a", "b", "c"})
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
        map[string][]byte{
            "c": []byte("3"),
            "d": []byte("4"),
        },
        0,
    )
    if nil != err {
        t.Fatalf("setMultiple error: %v", err)
    }

    values, err = backend.Many([]string{"c", "d"})
    if nil != err {
        t.Fatalf("many error: %v", err)
    }
    if "3" != string(values["c"]) {
        t.Fatalf("unexpected c")
    }
    if "4" != string(values["d"]) {
        t.Fatalf("unexpected d")
    }

    err = backend.DeleteMultiple([]string{"c", "d"})
    if nil != err {
        t.Fatalf("deleteMultiple error: %v", err)
    }

    _, exists, err := backend.Get("c")
    if nil != err {
        t.Fatalf("get error: %v", err)
    }
    if true == exists {
        t.Fatalf("expected c deleted")
    }

    err = backend.Delete("a")
    if nil != err {
        t.Fatalf("delete error: %v", err)
    }

    _, exists, err = backend.Get("a")
    if nil != err {
        t.Fatalf("get error: %v", err)
    }
    if true == exists {
        t.Fatalf("expected a deleted")
    }

    err = backend.Clear()
    if nil != err {
        t.Fatalf("clear error: %v", err)
    }

    _, exists, err = backend.Get("b")
    if nil != err {
        t.Fatalf("get error: %v", err)
    }
    if true == exists {
        t.Fatalf("expected cleared")
    }
}

func TestInMemoryBackend_IncrementDecrement_HappyPath(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    value, err := backend.Increment("n", 1)
    if nil != err {
        t.Fatalf("increment error: %v", err)
    }
    if int64(1) != value {
        t.Fatalf("expected 1")
    }

    value, err = backend.Increment("n", 2)
    if nil != err {
        t.Fatalf("increment error: %v", err)
    }
    if int64(3) != value {
        t.Fatalf("expected 3")
    }

    value, err = backend.Decrement("n", 1)
    if nil != err {
        t.Fatalf("decrement error: %v", err)
    }
    if int64(2) != value {
        t.Fatalf("expected 2")
    }
}

func TestInMemoryBackend_Increment_AcceptsExactlyTheRedisIntegerGrammar(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    _ = backend.Set("n", []byte("10"), 0)

    value, err := backend.Increment("n", 5)
    if nil != err {
        t.Fatalf("increment error: %v", err)
    }
    if int64(15) != value {
        t.Fatalf("expected 15")
    }

    _ = backend.Set("negative", []byte("-3"), 0)

    value, err = backend.Increment("negative", 1)
    if nil != err {
        t.Fatalf("increment error: %v", err)
    }
    if int64(-2) != value {
        t.Fatalf("expected -2")
    }

    for _, payload := range []string{"not-a-number", " 10 ", "+10", "007", "-0", ""} {
        _ = backend.Set("bad", []byte(payload), 0)

        if _, err = backend.Increment("bad", 1); nil == err {
            t.Fatalf("expected the payload %q to be refused the way redis refuses it", payload)
        }
    }
}

func TestInMemoryBackend_IncrementOverflow_ReturnsError(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    _ = backend.Set("n", []byte("9223372036854775807"), 0)

    _, err := backend.Increment("n", 1)
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestInMemoryBackend_Decrement_MinInt64Delta_ReturnsError(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    _, err := backend.Decrement("n", math.MinInt64)
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestInMemoryBackend_IncrementPreservesExistingTtl(t *testing.T) {
    start := time.Unix(10, 0)
    clockInstance := &cacheTestClock{now: start}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)

    if setErr := backend.Set("counter", []byte("0"), 10*time.Second); nil != setErr {
        t.Fatalf("Set returned an error: %v", setErr)
    }

    if _, incrementErr := backend.Increment("counter", 1); nil != incrementErr {
        t.Fatalf("Increment returned an error: %v", incrementErr)
    }

    clockInstance.now = start.Add(11 * time.Second)

    _, found, getErr := backend.Get("counter")
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

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)

    if _, incrementErr := backend.Increment("fresh", 1); nil != incrementErr {
        t.Fatalf("Increment returned an error: %v", incrementErr)
    }

    clockInstance.now = start.Add(365 * 24 * time.Hour)

    value, found, getErr := backend.Get("fresh")
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

    backend := NewInMemoryBackend(0, time.Hour, clockInstance)
    defer backend.Close()

    entryCount := cleanupChunkSize*2 + 7

    for index := 0; index < entryCount; index++ {
        _ = backend.Set(fmt.Sprintf("expiring-%d", index), []byte("value"), time.Minute)
    }

    _ = backend.Set("surviving", []byte("value"), time.Hour)

    clockInstance.now = clockInstance.now.Add(2 * time.Minute)

    backend.cleanupExpired()

    backend.mutex.RLock()
    remaining := len(backend.entries)
    backend.mutex.RUnlock()

    if 1 != remaining {
        t.Fatalf("expected only the unexpired entry to survive the sweep, got %d of %d", remaining, entryCount+1)
    }

    if _, exists, _ := backend.Get("surviving"); false == exists {
        t.Fatalf("expected the unexpired entry to survive")
    }
}

func TestInMemoryBackend_Eviction_PrefersAnExpiredVictimWithinTheProbeWindow(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(3, time.Hour, clockInstance)
    defer backend.Close()

    _ = backend.Set("expiring", []byte("value"), time.Minute)
    _ = backend.Set("keep-one", []byte("value"), time.Hour)
    _ = backend.Set("keep-two", []byte("value"), time.Hour)

    clockInstance.now = clockInstance.now.Add(2 * time.Minute)

    _ = backend.Set("fresh", []byte("value"), time.Hour)

    if _, exists, _ := backend.Get("expiring"); true == exists {
        t.Fatalf("expected the expired entry to be the eviction victim")
    }

    for _, key := range []string{"keep-one", "keep-two", "fresh"} {
        if _, exists, _ := backend.Get(key); false == exists {
            t.Fatalf("expected %q to survive the eviction", key)
        }
    }
}

/* the cache sweep is the one thing in this package that reads a clock, so its doubles live beside the storage that runs it; the other test files of the package reach them from here. */
type cacheTestTicker struct {
    channel chan time.Time
}

func (instance *cacheTestTicker) Channel() <-chan time.Time {
    return instance.channel
}

/* the Ticker contract forbids closing the channel on Stop — a consumer selecting on a stopped ticker's channel would spin on the zero value from a closed one — and demands idempotence; the previous close(instance.channel) survived only because the cleanup loop stops reading before Stop runs */
func (instance *cacheTestTicker) Stop() {}

type cacheTestClock struct {
    now time.Time
}

func (instance *cacheTestClock) Now() time.Time {
    return instance.now
}

func (instance *cacheTestClock) NewTicker(interval time.Duration) clockcontract.Ticker {
    return &cacheTestTicker{
        channel: make(chan time.Time),
    }
}

func TestNewInMemoryBackend_ANonPositiveCleanupIntervalBecomesTheDefault(t *testing.T) {
    for _, refusedInterval := range []time.Duration{0, -time.Second, -time.Hour} {
        backend := NewInMemoryBackend(10, refusedInterval, &cacheTestClock{now: time.Unix(10, 0)})

        if time.Minute != backend.cleanupTickInterval {
            t.Fatalf("expected %v to become the one-minute default, got %v", refusedInterval, backend.cleanupTickInterval)
        }

        if err := backend.Close(); nil != err {
            t.Fatalf("unexpected close error: %v", err)
        }
    }

    backend := NewInMemoryBackend(10, 5*time.Second, &cacheTestClock{now: time.Unix(10, 0)})
    defer backend.Close()

    if 5*time.Second != backend.cleanupTickInterval {
        t.Fatalf("expected a positive interval to be kept, got %v", backend.cleanupTickInterval)
    }
}

func TestNewInMemoryBackend_RefusesANilClock(t *testing.T) {
    assertInMemoryPanic(t, "clock is nil", func() {
        _ = NewInMemoryBackend(10, time.Hour, nil)
    })

    assertInMemoryPanic(t, "clock is nil", func() {
        var typedNilClock *cacheTestClock
        _ = NewInMemoryBackend(10, time.Hour, typedNilClock)
    })
}

func TestNewInMemoryBackend_RefusesANegativeMaxItems(t *testing.T) {
    assertInMemoryPanic(t, "cache max items is negative", func() {
        _ = NewInMemoryBackend(-1, time.Hour, &cacheTestClock{now: time.Unix(10, 0)})
    })
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

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    if err := backend.Set("live", []byte("payload"), 0); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    if err := backend.Set("lapsing", []byte("payload"), 2*time.Second); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    exists, hasErr := backend.Has("absent")
    if nil != hasErr || true == exists {
        t.Fatalf("expected an absent key to answer false, got %t, %v", exists, hasErr)
    }

    exists, hasErr = backend.Has("live")
    if nil != hasErr || false == exists {
        t.Fatalf("expected a live key to answer true, got %t, %v", exists, hasErr)
    }

    clockInstance.now = time.Unix(20, 0)

    exists, hasErr = backend.Has("lapsing")
    if nil != hasErr || true == exists {
        t.Fatalf("expected a lapsed key to answer false, got %t, %v", exists, hasErr)
    }

    exists, hasErr = backend.Has("live")
    if nil != hasErr || false == exists {
        t.Fatalf("expected the entry without a ttl to survive the clock, got %t, %v", exists, hasErr)
    }
}

func TestInMemoryBackend_ManySkipsTheKeysItCannotServe(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    if err := backend.Set("live", []byte("payload"), 0); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    if err := backend.Set("lapsing", []byte("payload"), 2*time.Second); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    clockInstance.now = time.Unix(20, 0)

    result, manyErr := backend.Many([]string{"live", "lapsing", "absent"})
    if nil != manyErr {
        t.Fatalf("unexpected many error: %v", manyErr)
    }

    if 1 != len(result) {
        t.Fatalf("expected only the live key to be served, got %#v", result)
    }

    if "payload" != string(result["live"]) {
        t.Fatalf("unexpected payload: %q", result["live"])
    }

    result, manyErr = backend.Many([]string{"absent", "lapsing"})
    if nil != manyErr {
        t.Fatalf("unexpected many error: %v", manyErr)
    }

    if nil == result || 0 != len(result) {
        t.Fatalf("expected an empty map rather than nil when nothing is found, got %#v", result)
    }
}

func TestInMemoryBackend_DeletingAnAbsentKeyIsANoOp(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    if err := backend.Set("kept", []byte("payload"), 0); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    if err := backend.Delete("absent"); nil != err {
        t.Fatalf("expected deleting an absent key to succeed, got %v", err)
    }

    if err := backend.DeleteMultiple([]string{"absent", "other"}); nil != err {
        t.Fatalf("expected deleting absent keys to succeed, got %v", err)
    }

    _, exists, getErr := backend.Get("kept")
    if nil != getErr || false == exists {
        t.Fatalf("expected the untouched key to survive, got %t, %v", exists, getErr)
    }

    if 1 != backend.lruList.Len() {
        t.Fatalf("expected the recency list to keep exactly the surviving entry, got %d", backend.lruList.Len())
    }
}

func TestInMemoryBackend_TheExactInstantOfExpiryIsAlreadyLapsed(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    if err := backend.Set("lapsing", []byte("payload"), 2*time.Second); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    clockInstance.now = time.Unix(11, 999999999)

    _, exists, getErr := backend.Get("lapsing")
    if nil != getErr || false == exists {
        t.Fatalf("expected the entry to be alive a nanosecond before its expiry, got %t, %v", exists, getErr)
    }

    clockInstance.now = time.Unix(12, 0)

    _, exists, getErr = backend.Get("lapsing")
    if nil != getErr || true == exists {
        t.Fatalf("expected the entry to be lapsed at the exact instant of its expiry, got %t, %v", exists, getErr)
    }
}

func TestInMemoryBackend_DecrementIsRefusedRatherThanWrappingPastTheSmallestInt64(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    if err := backend.Set("counter", []byte(fmt.Sprintf("%d", int64(math.MinInt64)+1)), 0); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    if _, decrementErr := backend.Decrement("counter", 1); nil != decrementErr {
        t.Fatalf("expected the decrement that exactly reaches the bound to succeed, got %v", decrementErr)
    }

    newValue, decrementErr := backend.Decrement("counter", 1)
    if nil == decrementErr {
        t.Fatalf("expected the decrement past the smallest int64 to be refused, got %d", newValue)
    }

    if 0 != newValue {
        t.Fatalf("expected the refused decrement to answer zero, got %d", newValue)
    }

    stored, exists, getErr := backend.Get("counter")
    if nil != getErr || false == exists {
        t.Fatalf("unexpected get error: %t, %v", exists, getErr)
    }

    if fmt.Sprintf("%d", int64(math.MinInt64)) != string(stored) {
        t.Fatalf("expected the refused decrement to leave the counter untouched, got %q", stored)
    }
}

func TestInMemoryBackend_IncrementStartsOverOnceTheCounterHasLapsed(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    if _, err := backend.Increment("window", 5); nil != err {
        t.Fatalf("unexpected increment error: %v", err)
    }

    if err := backend.Set("window", []byte("5"), 2*time.Second); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    clockInstance.now = time.Unix(20, 0)

    newValue, incrementErr := backend.Increment("window", 1)
    if nil != incrementErr {
        t.Fatalf("unexpected increment error: %v", incrementErr)
    }

    if 1 != newValue {
        t.Fatalf("expected the lapsed counter to start over, got %d", newValue)
    }
}

func TestInMemoryBackend_TheTickerDrivesTheSweep(t *testing.T) {
    clockInstance := newCacheTickableTestClock(time.Unix(10, 0))

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    if err := backend.Set("lapsing", []byte("payload"), 2*time.Second); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    if err := backend.Set("kept", []byte("payload"), 0); nil != err {
        t.Fatalf("unexpected set error: %v", err)
    }

    clockInstance.setNow(time.Unix(20, 0))

    /* the tick is delivered on an unbuffered channel, so the send returns only once the loop has received it; the sweep it drives is still in flight, which is what the poll below waits out */
    clockInstance.tick()

    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) {
        backend.mutex.RLock()
        remaining := len(backend.entries)
        backend.mutex.RUnlock()

        if 1 == remaining {
            _, exists, getErr := backend.Get("kept")
            if nil != getErr || false == exists {
                t.Fatalf("expected the sweep to leave the entry without a ttl alone, got %t, %v", exists, getErr)
            }

            return
        }

        time.Sleep(time.Millisecond)
    }

    t.Fatalf("expected the ticker to drive the sweep that reclaims the lapsed entry")
}

/* the sweep-driving clock is separate from cacheTestClock on purpose: once ticks actually fire, the backend goroutine reads the clock while the test advances it, so the time behind this one is guarded — the shared fixture is written by its tests as a plain field and is safe only because no tick ever reaches it. */
func newCacheTickableTestClock(now time.Time) *cacheTickableTestClock {
    return &cacheTickableTestClock{
        now:          now,
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

var _ clockcontract.Clock = (*cacheTickableTestClock)(nil)

func TestInMemoryBackend_EvictionToleratesARecencyListThatLostAgreementWithTheMap(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    t.Run("a list element that is not a key is dropped and the walk goes on", func(t *testing.T) {
        backend := NewInMemoryBackend(10, time.Hour, clockInstance)
        defer backend.Close()

        if err := backend.Set("real", []byte("payload"), 0); nil != err {
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
        backend := NewInMemoryBackend(10, time.Hour, clockInstance)
        defer backend.Close()

        if err := backend.Set("real", []byte("payload"), 0); nil != err {
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
        backend := NewInMemoryBackend(10, time.Hour, clockInstance)
        defer backend.Close()

        if err := backend.Set("real", []byte("payload"), 0); nil != err {
            t.Fatalf("unexpected set error: %v", err)
        }

        /* two intruders in a row: the walk removes the first and its Prev is cleared by the removal, so the walk ends with the second still at the back */
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
        backend := NewInMemoryBackend(10, time.Hour, clockInstance)
        defer backend.Close()

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

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    if false == backend.isExpiredAt(nil, clockInstance.now) {
        t.Fatalf("expected an absent item to count as lapsed")
    }
}

func TestInMemoryBackend_TheExpiringDeleteToleratesAKeyThatIsAlreadyGone(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    if err := backend.Set("kept", []byte("payload"), 0); nil != err {
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

    backend := NewInMemoryBackend(1000, time.Hour, clockInstance)
    defer backend.Close()

    const keyCount = 32
    const rounds = 400

    keys := make([]string, 0, keyCount)
    for keyIndex := 0; keyIndex < keyCount; keyIndex = keyIndex + 1 {
        key := fmt.Sprintf("key.%d", keyIndex)
        keys = append(keys, key)

        if err := backend.Set(key, []byte("payload"), 0); nil != err {
            t.Fatalf("unexpected set error: %v", err)
        }
    }

    var waitGroup sync.WaitGroup
    waitGroup.Add(3)

    go func() {
        defer waitGroup.Done()

        for round := 0; round < rounds; round = round + 1 {
            for _, key := range keys {
                _, _, _ = backend.Get(key)
            }
        }
    }()

    go func() {
        defer waitGroup.Done()

        for round := 0; round < rounds; round = round + 1 {
            _, _ = backend.Many(keys)
        }
    }()

    go func() {
        defer waitGroup.Done()

        for round := 0; round < rounds; round = round + 1 {
            for _, key := range keys {
                _ = backend.Delete(key)
                _ = backend.Set(key, []byte("payload"), 0)
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

/* the key grammar is the contract's, enforced identically by every implementation: a caller must not be able to tell the backends apart by which keys they refuse. */
func TestInMemoryBackend_RefusesTheContractKeyGrammar(t *testing.T) {
    backend := NewInMemoryBackend(
        10,
        time.Hour,
        &cacheTestClock{now: time.Unix(10, 0)},
    )
    defer backend.Close()

    longKey := strings.Repeat("k", 1025)

    for name, key := range map[string]string{
        "spaces":   "user profile:alice",
        "newlines": "user\nprofile",
        "too long": longKey,
    } {
        if setErr := backend.Set(key, []byte("payload"), time.Minute); nil == setErr {
            t.Fatalf("expected the %s key to be refused by Set", name)
        }

        if _, _, getErr := backend.Get(key); nil == getErr {
            t.Fatalf("expected the %s key to be refused by Get", name)
        }

        if _, incrementErr := backend.Increment(key, 1); nil == incrementErr {
            t.Fatalf("expected the %s key to be refused by Increment", name)
        }
    }

    if setMultipleErr := backend.SetMultiple(map[string][]byte{"a b": []byte("x")}, time.Minute); nil == setMultipleErr {
        t.Fatal("expected the spaced key to be refused by SetMultiple")
    }

    if _, manyErr := backend.Many([]string{"good", "with space"}); nil == manyErr {
        t.Fatal("expected the spaced key to be refused by Many")
    }
}

/* the refusal order is part of the shared contract, the redis backend's order: the closed answer wins over the key judgment, and a batch write judges the ttl before its keys, so a call that is wrong in more than one way is refused with the same answer whichever implementation it hit. */
func TestInMemoryBackend_RefusalOrderMatchesTheRedisBackend(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    closed := NewInMemoryBackend(10, time.Hour, clockInstance)
    if closeErr := closed.Close(); nil != closeErr {
        t.Fatalf("Close: %v", closeErr)
    }

    _, _, getErr := closed.Get("")
    if nil == getErr || false == strings.Contains(getErr.Error(), "cache backend is closed") {
        t.Fatalf("expected the closed refusal to win over the key judgment, got %v", getErr)
    }

    open := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer func() {
        _ = open.Close()
    }()

    setErr := open.SetMultiple(map[string][]byte{"bad key": []byte("payload")}, -time.Second)
    if nil == setErr || false == strings.Contains(setErr.Error(), "ttl") {
        t.Fatalf("expected the batch ttl judgment to precede the key judgment, got %v", setErr)
    }
}

func TestInMemoryBackend_ANilPayloadReadsBackEmptyNonNil(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    if setErr := backend.Set("k", nil, 0); nil != setErr {
        t.Fatalf("set failed: %v", setErr)
    }

    payload, exists, getErr := backend.Get("k")
    if nil != getErr || false == exists {
        t.Fatalf("get failed: exists=%v err=%v", exists, getErr)
    }

    if nil == payload || 0 != len(payload) {
        t.Fatalf("expected the stored nil to read back as an empty non-nil slice, the redis identity; got nil=%v len=%d", nil == payload, len(payload))
    }
}

/* the batch refusal names the sorted-first malformed key, never a map-iteration choice: the same wrong batch answers the same refusal on every call — the rule the redis backend's batch reporting follows. */
func TestInMemoryBackend_SetMultipleNamesTheSortedFirstMalformedKey(t *testing.T) {
    backend := NewInMemoryBackend(10, time.Hour, &cacheTestClock{now: time.Unix(10, 0)})
    defer backend.Close()

    items := map[string][]byte{
        "zz bad": []byte("z"),
        "aa bad": []byte("a"),
        "good":   []byte("g"),
    }

    for attempt := 0; attempt < 8; attempt++ {
        setErr := backend.SetMultiple(items, 0)
        if nil == setErr {
            t.Fatal("expected the malformed keys to be refused")
        }

        namedKey := exception.LogContext(setErr)["key"]
        if "aa bad" != namedKey {
            t.Fatalf("expected the sorted-first malformed key named, got %v", namedKey)
        }
    }
}

func TestInMemoryBackend_DecrementRefusesTheClosedBackendBeforeTheDeltaMagnitude(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)

    if closeErr := backend.Close(); nil != closeErr {
        t.Fatalf("Close returned an error: %v", closeErr)
    }

    _, decrementErr := backend.Decrement("counter", math.MinInt64)
    if nil == decrementErr {
        t.Fatalf("expected a refusal from a closed backend")
    }

    if "cache backend is closed" != decrementErr.Error() {
        t.Fatalf("expected the closed refusal the redis sibling answers first, got: %v", decrementErr)
    }
}

func TestInMemoryBackend_DecrementRefusesTheMalformedKeyBeforeTheDeltaMagnitude(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    _, decrementErr := backend.Decrement("bad key", math.MinInt64)
    if nil == decrementErr {
        t.Fatalf("expected a refusal for the malformed key")
    }

    if true == strings.Contains(decrementErr.Error(), "delta overflows") {
        t.Fatalf("expected the key refusal ahead of the magnitude one, got: %v", decrementErr)
    }
}
