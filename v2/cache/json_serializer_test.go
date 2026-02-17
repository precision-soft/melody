package cache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRemember_ReturnsCachedValueWithoutCallingCallback(t *testing.T) {
	clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

	backend := NewInMemoryBackend(
		10,
		time.Hour,
		clockInstance,
	)
	defer backend.Close()

	cacheInstance := NewManager(backend, NewJsonSerializer())

	setErr := cacheInstance.Set("k", "v", 0)
	if nil != setErr {
		t.Fatalf("set error: %v", setErr)
	}

	called := false
	value, rememberErr := Remember(
		cacheInstance,
		"k",
		time.Second,
		func(ctx context.Context) (any, error) {
			called = true
			return "new", nil
		},
		nil,
	)
	if nil != rememberErr {
		t.Fatalf("remember error: %v", rememberErr)
	}
	if true == called {
		t.Fatalf("expected callback not to be called on cache hit")
	}
	if "v" != value.(string) {
		t.Fatalf("expected cached value")
	}
}

func TestRemember_CallsCallbackAndStoresValue(t *testing.T) {
	clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

	backend := NewInMemoryBackend(
		10,
		time.Hour,
		clockInstance,
	)
	defer backend.Close()

	cacheInstance := NewManager(backend, NewJsonSerializer())

	called := false
	value, rememberErr := Remember(
		cacheInstance,
		"k",
		time.Second,
		func(ctx context.Context) (any, error) {
			called = true
			return "computed", nil
		},
		nil,
	)
	if nil != rememberErr {
		t.Fatalf("remember error: %v", rememberErr)
	}
	if false == called {
		t.Fatalf("expected callback to be called on cache miss")
	}
	if "computed" != value.(string) {
		t.Fatalf("unexpected value")
	}

	storedValue, exists, getErr := cacheInstance.Get("k")
	if nil != getErr {
		t.Fatalf("get error: %v", getErr)
	}
	if false == exists {
		t.Fatalf("expected value to be stored")
	}
	if "computed" != storedValue.(string) {
		t.Fatalf("expected stored value")
	}
}

func TestRemember_PropagatesCallbackErrorAndDoesNotStore(t *testing.T) {
	clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

	backend := NewInMemoryBackend(
		10,
		time.Hour,
		clockInstance,
	)
	defer backend.Close()

	cacheInstance := NewManager(backend, NewJsonSerializer())

	expectedErr := errors.New("callback error")

	_, rememberErr := Remember(
		cacheInstance,
		"k",
		time.Second,
		func(ctx context.Context) (any, error) {
			return nil, expectedErr
		},
		nil,
	)
	if nil == rememberErr {
		t.Fatalf("expected error")
	}
	if expectedErr.Error() != rememberErr.Error() {
		t.Fatalf("expected callback error to propagate")
	}

	_, exists, getErr := cacheInstance.Get("k")
	if nil != getErr {
		t.Fatalf("get error: %v", getErr)
	}
	if true == exists {
		t.Fatalf("expected value not to be stored")
	}
}

func TestRemember_ZeroTtlActsAsForever(t *testing.T) {
	clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

	backend := NewInMemoryBackend(
		10,
		time.Hour,
		clockInstance,
	)
	defer backend.Close()

	cacheInstance := NewManager(backend, NewJsonSerializer())

	_, rememberErr := Remember(
		cacheInstance,
		"k",
		0,
		func(ctx context.Context) (any, error) {
			return "v", nil
		},
		nil,
	)
	if nil != rememberErr {
		t.Fatalf("remember error: %v", rememberErr)
	}

	clockInstance.now = time.Unix(10+3600, 0)

	value, exists, getErr := cacheInstance.Get("k")
	if nil != getErr {
		t.Fatalf("get error: %v", getErr)
	}
	if false == exists {
		t.Fatalf("expected value to still exist with ttl=0")
	}
	if "v" != value.(string) {
		t.Fatalf("unexpected value")
	}
}

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

func TestJsonSerializer_RoundTrip(t *testing.T) {
	serializer := NewJsonSerializer()

	payload, serializeErr := serializer.Serialize(
		map[string]any{
			"a": "b",
			"n": float64(1),
		},
	)
	if nil != serializeErr {
		t.Fatalf("serialize error: %v", serializeErr)
	}

	value, deserializeErr := serializer.Deserialize(payload)
	if nil != deserializeErr {
		t.Fatalf("deserialize error: %v", deserializeErr)
	}

	decoded := value.(map[string]any)

	if "b" != decoded["a"].(string) {
		t.Fatalf("unexpected decoded value")
	}
	if float64(1) != decoded["n"].(float64) {
		t.Fatalf("unexpected decoded number")
	}
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
