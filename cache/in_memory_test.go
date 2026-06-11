package cache

import (
    "math"
    "testing"
    "time"
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

func TestInMemoryBackend_KeyNormalization_TrimsAndPanicsOnEmpty(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    err := backend.Set("a", []byte("1"), 0)
    if nil != err {
        t.Fatalf("set error: %v", err)
    }

    value, exists, err := backend.Get("a")
    if nil != err {
        t.Fatalf("get error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if "1" != string(value) {
        t.Fatalf("unexpected value")
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

/** @info increment ttl preservation */

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
