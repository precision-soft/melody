package cache

import (
    "testing"
    "time"
)

func TestNewManager_PanicsOnNilBackendOrSerializer(t *testing.T) {
    defer func() {
        _ = recover()
    }()

    func() {
        defer func() {
            recoveredValue := recover()
            if nil == recoveredValue {
                t.Fatalf("expected panic on nil backend")
            }
        }()

        _ = NewManager(nil, NewJsonSerializer())
    }()

    func() {
        defer func() {
            recoveredValue := recover()
            if nil == recoveredValue {
                t.Fatalf("expected panic on nil serializer")
            }
        }()

        clockInstance := &cacheTestClock{now: time.Unix(10, 0)}
        backend := NewInMemoryBackend(10, time.Hour, clockInstance)
        defer backend.Close()

        _ = NewManager(backend, nil)
    }()
}

func TestManager_SetGetManySetMultipleDeleteMultipleClearDeleteHasClose(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(
        10,
        time.Hour,
        clockInstance,
    )
    cacheInstance := NewManager(backend, NewJsonSerializer())

    setErr := cacheInstance.Set("a", "1", 0)
    if nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    value, exists, getErr := cacheInstance.Get("a")
    if nil != getErr {
        t.Fatalf("get error: %v", getErr)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if "1" != value.(string) {
        t.Fatalf("unexpected value")
    }

    hasValue, hasErr := cacheInstance.Has("a")
    if nil != hasErr {
        t.Fatalf("has error: %v", hasErr)
    }
    if true != hasValue {
        t.Fatalf("expected has true")
    }

    many, manyErr := cacheInstance.Many([]string{"a", "b"})
    if nil != manyErr {
        t.Fatalf("many error: %v", manyErr)
    }
    if "1" != many["a"].(string) {
        t.Fatalf("unexpected many[a]")
    }
    if nil != many["b"] {
        t.Fatalf("expected missing key not to exist in result map")
    }

    setMultipleErr := cacheInstance.SetMultiple(
        map[string]any{
            "b": "2",
            "c": "3",
        },
        0,
    )
    if nil != setMultipleErr {
        t.Fatalf("setMultiple error: %v", setMultipleErr)
    }

    many, manyErr = cacheInstance.Many([]string{"a", "b", "c"})
    if nil != manyErr {
        t.Fatalf("many error: %v", manyErr)
    }
    if "2" != many["b"].(string) {
        t.Fatalf("unexpected many[b]")
    }
    if "3" != many["c"].(string) {
        t.Fatalf("unexpected many[c]")
    }

    deleteMultipleErr := cacheInstance.DeleteMultiple([]string{"b", "c"})
    if nil != deleteMultipleErr {
        t.Fatalf("deleteMultiple error: %v", deleteMultipleErr)
    }

    _, exists, getErr = cacheInstance.Get("b")
    if nil != getErr {
        t.Fatalf("get error: %v", getErr)
    }
    if true == exists {
        t.Fatalf("expected b deleted")
    }

    deleteErr := cacheInstance.Delete("a")
    if nil != deleteErr {
        t.Fatalf("delete error: %v", deleteErr)
    }

    _, exists, getErr = cacheInstance.Get("a")
    if nil != getErr {
        t.Fatalf("get error: %v", getErr)
    }
    if true == exists {
        t.Fatalf("expected a deleted")
    }

    setErr = cacheInstance.Set("x", "y", 0)
    if nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    clearErr := cacheInstance.Clear()
    if nil != clearErr {
        t.Fatalf("clear error: %v", clearErr)
    }

    _, exists, getErr = cacheInstance.Get("x")
    if nil != getErr {
        t.Fatalf("get error: %v", getErr)
    }
    if true == exists {
        t.Fatalf("expected cache cleared")
    }

    closeErr := cacheInstance.Close()
    if nil != closeErr {
        t.Fatalf("close error: %v", closeErr)
    }
}
