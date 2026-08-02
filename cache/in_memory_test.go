package cache

import (
    "fmt"
    "math"
    "testing"
    "time"

    clockcontract "github.com/precision-soft/melody/clock/contract"
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

/* @info the empty key is refused on every operation: the in-memory backend accepted it as a real key while the redis reference refuses it, so every caller whose key came up empty silently shared one entry until the deployment switched backends */
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

/* @info a closed backend refuses every operation instead of serving a map whose cleanup goroutine is gone: an entry written after Close would never be reclaimed by anything but a read that happens to name it, and the two answers a caller could get from a closed cache have to be the same on every backend */
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

/* @info a negative ttl is refused instead of storing an immortal entry: a ttl computed from an already-passed deadline meant "as good as expired" and the silent branch it fell on meant the exact opposite; zero keeps its documented meaning of no expiry */
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

/* @info a negative item ceiling is refused at construction: it silently meant "unbounded", so a bound computed wrong out of a configuration disarmed eviction while the operator believed the cache capped */
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

/* @info an existing payload that is empty or blank is refused the way a textual one is, instead of being silently adopted as a zero counter and overwritten — the redis reference answers INCRBY on it with an error, and GetCounter already refused the very same value */
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

func TestInMemoryBackend_Increment_ParsesTrimmedStringAndErrorsOnInvalid(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    _ = backend.Set("n", []byte(" 10 "), 0)

    value, err := backend.Increment("n", 5)
    if nil != err {
        t.Fatalf("increment error: %v", err)
    }
    if int64(15) != value {
        t.Fatalf("expected 15")
    }

    _ = backend.Set("bad", []byte("not-a-number"), 0)

    _, err = backend.Increment("bad", 1)
    if nil == err {
        t.Fatalf("expected error")
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

/* @info the sweep walks a snapshot of the keys in chunks and releases the lock between them; a map larger than one chunk must still be swept whole, and the sweep must terminate */
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

/* @info eviction probes a bounded number of least-recently-used entries for an expired victim before falling back to the least recently used one; an unbounded search made every insert into a full cache pay a whole-list scan under the exclusive lock */
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
