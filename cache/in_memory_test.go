package cache

import (
    "fmt"
    "math"
    "sync"
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

/* @info a non-positive cleanup interval is a configuration that resolved to nothing, and it becomes the one-minute default rather than a ticker built on zero — time.NewTicker panics on a non-positive duration, so without this the whole backend would die inside the goroutine started by its own constructor, where no caller can recover it. */
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

/* @info the clock is refused at construction, through the interface, because every read and every expiry decision dereferences it: a nil clock would panic inside Get on the request path, and a typed-nil one would pass a plain comparison to do the same. */
func TestNewInMemoryBackend_RefusesANilClock(t *testing.T) {
    assertInMemoryPanic(t, "clock is nil", func() {
        _ = NewInMemoryBackend(10, time.Hour, nil)
    })

    assertInMemoryPanic(t, "clock is nil", func() {
        var typedNilClock *cacheTestClock
        _ = NewInMemoryBackend(10, time.Hour, typedNilClock)
    })
}

/* @info a negative bound is refused rather than silently disarming eviction: zero already means "unbounded" on purpose, so a bound computed wrong — a subtraction that went below zero — would read as the deliberate unbounded setting and the cache would grow without limit. */
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

/* @info Has answers the three shapes apart: a key that was never written, one whose ttl has lapsed, and a live one. Only the live answer was pinned, so a Has that reported true for a lapsed entry — the shape that makes a "set only if absent" caller skip the write forever — would have kept the suite green. */
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

/* @info Many skips what it cannot serve and says so by omission rather than by an error or a nil entry, and a request in which nothing at all is found still answers an empty map — the early return that avoids taking the exclusive lock for zero work. A caller ranging over the result must see the absent keys missing, not present with a nil payload. */
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

/* @info deleting a key that is not there is a no-op, not a failure and not a list corruption: DeleteMultiple over a partly-evicted set reaches this on every key the eviction already took, and a branch that fell through would remove a list element belonging to somebody else. */
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

/* @info the instant of expiry belongs to the lapsed side. The boundary is the one value a ttl actually describes — an entry written with a two-second ttl at t is gone at t+2, not alive for one more read — and the two comparisons that decide it, After and Equal, answer differently only here. */
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

/* @info the counter arithmetic is bounded on both sides. Only the overflow half was pinned, so a decrement walking past the smallest int64 — the shape a "remaining quota" counter reaches when the quota was never replenished — would have wrapped around to a large positive number and read as an enormous remaining allowance. */
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

/* @info an increment on a key that lapsed between writes starts from zero rather than from the lapsed value: getEntryLocked reclaims it on the way past, which is what keeps a rate-limit counter with a window ttl from carrying the previous window's count into the new one. */
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

/* @info the periodic sweep is driven by the ticker the constructor started, not only by the direct call every other test makes. The tick branch of the loop had never been entered by anything: a loop that selected only on the stop channel would leave every lapsed entry in the map until a reader happened to name it, which is precisely the leak the sweep exists to prevent — and Close would still work, so the suite would stay green. */
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

/* @info the eviction walk carries four defences against a recency list that no longer agrees with the entry map. None is reachable through the public API — every mutation of the two happens under the same exclusive lock, so the invariant holds — and they are pinned here white-box, by corrupting the pair by hand, because the alternative is deleting guards whose whole job is to keep a corruption from becoming an eviction of somebody else's entry or a nil dereference on the write path. Each is entered on its own: a list element that is not a key at all, a key the map has already lost, the same two reached after the bounded probe gave up, and a list that the walk emptied on its way. */
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

/* @info an entry whose item is absent counts as lapsed rather than as live. Every caller checks nil == entry.item before asking, so the branch is defence rather than a path — but the answer it gives is the safe one: reading a live verdict off an entry with no item would send the reader on to dereference it. */
func TestInMemoryBackend_AnEntryWithoutAnItemCountsAsLapsed(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    if false == backend.isExpiredAt(nil, clockInstance.now) {
        t.Fatalf("expected an absent item to count as lapsed")
    }
}

/* @info the expiring delete tolerates a key that is no longer there. The sweep collects the keys under the lock, releases it between chunks and then deletes — a key the caller deleted meanwhile arrives here absent, which is the ordinary outcome of the chunking that keeps the sweep from stalling every concurrent read, not an error. */
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

/* @info Get and Many read under the shared lock and then take the exclusive one to touch the recency list, so between the two sections another caller can delete or replace the entry they just served. Both re-check what they find there rather than trusting the first look, and the re-check is what keeps a concurrent delete from touching a list element that is no longer in the map. The branches are reachable only through that interleaving, so this drives it under load; it is the -race lane's test, not a coverage claim. */
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
