package cache

import (
    "errors"
    "testing"
    "time"

    cachecontract "github.com/precision-soft/melody/v2/cache/contract"
    "github.com/precision-soft/melody/v2/exception"
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
    defer backend.Close()

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

func TestNewManager_DoesNotCloseTheBackend(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    manager := NewManager(backend, NewJsonSerializer())

    if closeErr := manager.Close(); nil != closeErr {
        t.Fatalf("close error: %v", closeErr)
    }

    if setErr := backend.Set("a", []byte("1"), 0); nil != setErr {
        t.Fatalf("expected the backend to stay open after the non-owning manager closed, got: %v", setErr)
    }
}

func TestNewManagerOwningBackend_ClosesTheBackend(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)

    manager := NewManagerOwningBackend(backend, NewJsonSerializer())

    if closeErr := manager.Close(); nil != closeErr {
        t.Fatalf("close error: %v", closeErr)
    }

    if setErr := backend.Set("a", []byte("1"), 0); nil == setErr {
        t.Fatalf("expected the backend to be closed by the owning manager")
    }
}

func TestManager_Get_TypesTheDeserializationFailure(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    manager := NewManager(backend, NewJsonSerializer())

    if setErr := backend.Set("corrupt", []byte("{not json"), 0); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    _, exists, getErr := manager.Get("corrupt")
    if nil == getErr {
        t.Fatalf("expected a deserialization error")
    }
    if true == exists {
        t.Fatalf("expected exists false on a corrupt payload")
    }
    if false == IsDeserializationError(getErr) {
        t.Fatalf("expected the error to be typed as a DeserializationError, got: %v", getErr)
    }
}

func TestManager_Many_SkipsCorruptEntriesAndNamesThem(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    manager := NewManager(backend, NewJsonSerializer())

    if setErr := manager.Set("good.one", "1", 0); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }
    if setErr := manager.Set("good.two", "2", 0); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }
    if setErr := backend.Set("corrupt", []byte("{not json"), 0); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    result, manyErr := manager.Many([]string{"good.one", "corrupt", "good.two", "missing"})
    if nil == manyErr {
        t.Fatalf("expected an error naming the corrupt entry")
    }
    if false == IsDeserializationError(manyErr) {
        t.Fatalf("expected the error to be typed as a DeserializationError, got: %v", manyErr)
    }

    if 2 != len(result) {
        t.Fatalf("expected both good values beside the error, got %d", len(result))
    }
    if "1" != result["good.one"].(string) || "2" != result["good.two"].(string) {
        t.Fatalf("unexpected surviving values: %v", result)
    }

    var deserializationErr *DeserializationError
    if false == errors.As(manyErr, &deserializationErr) {
        t.Fatalf("expected errors.As to find the DeserializationError")
    }

    keys, ok := deserializationErr.exceptionErr.Context()["keys"].([]string)
    if false == ok || 1 != len(keys) || "corrupt" != keys[0] {
        t.Fatalf("expected the error context to name exactly the corrupt key, got: %v", deserializationErr.exceptionErr.Context()["keys"])
    }
}

/* the items come as a map, so a refusal that stopped at the first entry the iteration reached named a different key on every call — measured 179/21 over two hundred calls on two refused keys; fifty rounds leave a re-instated "first refusal wins" a chance of about 0.9^50 to hide */
func TestManager_SetMultipleNamesTheRefusedKeysDeterministically(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    manager := NewManager(backend, NewJsonSerializer())

    for round := 0; round < 50; round++ {
        setErr := manager.SetMultiple(map[string]any{"b.bad": make(chan int), "a.bad": make(chan int), "good": "kept"}, 0)
        if nil == setErr {
            t.Fatalf("expected a serialization error from SetMultiple")
        }

        var exceptionErr *exception.Error
        if false == errors.As(setErr, &exceptionErr) {
            t.Fatalf("expected an exception error, got: %v", setErr)
        }

        if "a.bad" != exceptionErr.Context()["key"] {
            t.Fatalf("round %d: expected the first refused key in order, got: %v", round, exceptionErr.Context()["key"])
        }

        keys, isSlice := exceptionErr.Context()["keys"].([]string)
        if false == isSlice || 2 != len(keys) || "a.bad" != keys[0] || "b.bad" != keys[1] {
            t.Fatalf("round %d: expected every refused key in order, got: %v", round, exceptionErr.Context()["keys"])
        }
    }

    if _, exists, _ := backend.Get("good"); true == exists {
        t.Fatalf("expected a refused SetMultiple to write nothing")
    }
}

/* Serializer is a PUBLIC contract, so the refusal SetMultiple reads to build its per-key reasons is the
   application's error. Reading its text bare turned a failure this door ANSWERS into a panic nothing on
   the request path contains — the sibling Set, which hands the same error to NewError without reading it,
   answered an error for the same input on every major. The text is read the way the repository reads every
   foreign error's text, under a recover, and the marker takes the place of the reason. */
func TestManager_SetMultipleAnswersARefusalWhenTheSerializersErrorTextPanics(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    manager := NewManager(backend, &panickingTextSerializer{})

    var recoveredValue any
    var setErr error

    func() {
        defer func() {
            recoveredValue = recover()
        }()

        setErr = manager.SetMultiple(map[string]any{"a.bad": "first", "b.bad": "second"}, 0)
    }()

    if nil != recoveredValue {
        t.Fatalf("expected a refusal; the door panicked with the serializer's own error text: %v", recoveredValue)
    }

    if nil == setErr {
        t.Fatalf("expected a serialization error from SetMultiple")
    }

    var exceptionErr *exception.Error
    if false == errors.As(setErr, &exceptionErr) {
        t.Fatalf("expected an exception error, got: %v", setErr)
    }

    causeByKey, isMap := exceptionErr.Context()["causeByKey"].(map[string]string)
    if false == isMap || 2 != len(causeByKey) {
        t.Fatalf("expected a reason for each refused key, got: %v", exceptionErr.Context()["causeByKey"])
    }

    for _, refusedKey := range []string{"a.bad", "b.bad"} {
        if "serialization error message panicked: the serializer's error text panicked" != causeByKey[refusedKey] {
            t.Fatalf("expected %s to carry the panic in place of its reason, got: %q", refusedKey, causeByKey[refusedKey])
        }
    }
}

/* an error of the application's whose Error() panics — typically on the very field that made the
   serialization fail — is the value this whole class is about; it is not producible through any shipped
   serializer, so the double is what makes the guard observable */
type panickingTextError struct{}

func (instance *panickingTextError) Error() string {
    panic("the serializer's error text panicked")
}

type panickingTextSerializer struct {
    cachecontract.Serializer
}

func (instance *panickingTextSerializer) Serialize(value any) ([]byte, error) {
    return nil, &panickingTextError{}
}

func TestManager_Set_NamesTheKeyOnSerializationFailure(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    manager := NewManager(backend, NewJsonSerializer())

    setErr := manager.Set("unserializable", make(chan int), 0)
    if nil == setErr {
        t.Fatalf("expected a serialization error")
    }

    var exceptionErr *exception.Error
    if false == errors.As(setErr, &exceptionErr) {
        t.Fatalf("expected an exception error, got: %v", setErr)
    }
    if "unserializable" != exceptionErr.Context()["key"] {
        t.Fatalf("expected the error context to name the key, got: %v", exceptionErr.Context())
    }

    setMultipleErr := manager.SetMultiple(map[string]any{"multi.bad": make(chan int)}, 0)
    if nil == setMultipleErr {
        t.Fatalf("expected a serialization error from SetMultiple")
    }
    if false == errors.As(setMultipleErr, &exceptionErr) {
        t.Fatalf("expected an exception error, got: %v", setMultipleErr)
    }
    if "multi.bad" != exceptionErr.Context()["key"] {
        t.Fatalf("expected the SetMultiple error context to name the key, got: %v", exceptionErr.Context())
    }
}

type cacheTestTypedNilErrorBackend struct {
    inner *InMemoryBackend
}

func (instance *cacheTestTypedNilErrorBackend) typedNilError() error {
    var typedNil *exception.Error

    return typedNil
}

func (instance *cacheTestTypedNilErrorBackend) Get(key string) ([]byte, bool, error) {
    payload, exists, _ := instance.inner.Get(key)
    return payload, exists, instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) Set(key string, payload []byte, ttl time.Duration) error {
    _ = instance.inner.Set(key, payload, ttl)
    return instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) Delete(key string) error {
    _ = instance.inner.Delete(key)
    return instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) Has(key string) (bool, error) {
    hasValue, _ := instance.inner.Has(key)
    return hasValue, instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) Clear() error {
    return instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) Many(keys []string) (map[string][]byte, error) {
    result, _ := instance.inner.Many(keys)
    return result, instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) SetMultiple(items map[string][]byte, ttl time.Duration) error {
    return instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) DeleteMultiple(keys []string) error {
    return instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) Increment(key string, delta int64) (int64, error) {
    newValue, _ := instance.inner.Increment(key, delta)
    return newValue, instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) Decrement(key string, delta int64) (int64, error) {
    newValue, _ := instance.inner.Decrement(key, delta)
    return newValue, instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) Close() error {
    return instance.typedNilError()
}

func TestManager_ReadsTypedNilBackendErrorsAsSuccess(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    inner := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer inner.Close()

    manager := NewManager(&cacheTestTypedNilErrorBackend{inner: inner}, NewJsonSerializer())

    if setErr := manager.Set("a", "1", 0); nil != setErr {
        t.Fatalf("expected the typed-nil Set error to read as success, got: %v", setErr)
    }

    value, exists, getErr := manager.Get("a")
    if nil != getErr {
        t.Fatalf("expected the typed-nil Get error to read as success, got: %v", getErr)
    }
    if false == exists || "1" != value.(string) {
        t.Fatalf("expected the stored value back, got exists=%v value=%v", exists, value)
    }
}

func TestManager_GetCounterReadsWhatIncrementWrote(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}
    manager := NewManagerOwningBackend(NewInMemoryBackend(0, time.Hour, clockInstance), NewJsonSerializer())
    defer manager.Close()

    if _, incrementErr := manager.Increment("visits", 5); nil != incrementErr {
        t.Fatalf("unexpected increment error: %v", incrementErr)
    }

    value, exists, getCounterErr := manager.GetCounter("visits")
    if nil != getCounterErr {
        t.Fatalf("unexpected error: %v", getCounterErr)
    }

    if false == exists {
        t.Fatalf("expected the counter to exist")
    }

    if 5 != value {
        t.Fatalf("expected the counter to read back as an int64, got %d", value)
    }
}

func TestManager_GetCounterReportsAnAbsentKey(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}
    manager := NewManagerOwningBackend(NewInMemoryBackend(0, time.Hour, clockInstance), NewJsonSerializer())
    defer manager.Close()

    if _, exists, getCounterErr := manager.GetCounter("missing"); nil != getCounterErr || true == exists {
        t.Fatalf("expected an absent counter to report exists=false with no error, got exists=%v err=%v", exists, getCounterErr)
    }
}

func TestManager_GetCounterRejectsANonCounterPayload(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}
    manager := NewManagerOwningBackend(NewInMemoryBackend(0, time.Hour, clockInstance), NewJsonSerializer())
    defer manager.Close()

    if setErr := manager.Set("greeting", "hello", time.Hour); nil != setErr {
        t.Fatalf("unexpected set error: %v", setErr)
    }

    if _, _, getCounterErr := manager.GetCounter("greeting"); nil == getCounterErr {
        t.Fatalf("expected a serialized value to be refused by GetCounter")
    }
}

func TestManager_DecrementCountsDownAndLeavesACounterGetCounterCanRead(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    manager := NewManager(backend, NewJsonSerializer())

    if _, incrementErr := manager.Increment("remaining", 10); nil != incrementErr {
        t.Fatalf("unexpected increment error: %v", incrementErr)
    }

    newValue, decrementErr := manager.Decrement("remaining", 3)
    if nil != decrementErr {
        t.Fatalf("unexpected decrement error: %v", decrementErr)
    }

    if 7 != newValue {
        t.Fatalf("expected the decrement to count down to 7, got %d", newValue)
    }

    storedValue, exists, getCounterErr := manager.GetCounter("remaining")
    if nil != getCounterErr || false == exists {
        t.Fatalf("unexpected get counter result: %t, %v", exists, getCounterErr)
    }

    if 7 != storedValue {
        t.Fatalf("expected the stored counter to be 7, got %d", storedValue)
    }

    /* a counter written by Decrement crosses zero into the negatives rather than clamping: a remaining-quota counter that stopped at zero would report the same value for "exactly used up" and "overdrawn by a thousand" */
    newValue, decrementErr = manager.Decrement("remaining", 10)
    if nil != decrementErr {
        t.Fatalf("unexpected decrement error: %v", decrementErr)
    }

    if -3 != newValue {
        t.Fatalf("expected the counter to cross zero, got %d", newValue)
    }
}

func TestManager_DecrementOnAnAbsentKeyStartsFromZero(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    manager := NewManager(backend, NewJsonSerializer())

    newValue, decrementErr := manager.Decrement("never.written", 2)
    if nil != decrementErr {
        t.Fatalf("unexpected decrement error: %v", decrementErr)
    }

    if -2 != newValue {
        t.Fatalf("expected an absent counter to start from zero, got %d", newValue)
    }
}

func TestManager_ABackendFailureTravelsOutOfEveryRead(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    manager := NewManager(backend, NewJsonSerializer())

    if setErr := manager.Set("key", "value", 0); nil != setErr {
        t.Fatalf("unexpected set error: %v", setErr)
    }

    if closeErr := backend.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    value, exists, getErr := manager.Get("key")
    if nil == getErr {
        t.Fatalf("expected Get to report the closed backend, got %v, %t", value, exists)
    }
    if nil != value || true == exists {
        t.Fatalf("expected Get to answer nothing alongside the failure, got %v, %t", value, exists)
    }
    if "cache backend is closed" != getErr.Error() {
        t.Fatalf("unexpected Get error: %v", getErr)
    }

    manyResult, manyErr := manager.Many([]string{"key"})
    if nil == manyErr {
        t.Fatalf("expected Many to report the closed backend, got %v", manyResult)
    }
    if nil != manyResult {
        t.Fatalf("expected Many to answer nothing alongside the failure, got %v", manyResult)
    }
    if "cache backend is closed" != manyErr.Error() {
        t.Fatalf("unexpected Many error: %v", manyErr)
    }

    counterValue, counterExists, counterErr := manager.GetCounter("key")
    if nil == counterErr {
        t.Fatalf("expected GetCounter to report the closed backend, got %d, %t", counterValue, counterExists)
    }
    if 0 != counterValue || true == counterExists {
        t.Fatalf("expected GetCounter to answer nothing alongside the failure, got %d, %t", counterValue, counterExists)
    }
    if "cache backend is closed" != counterErr.Error() {
        t.Fatalf("unexpected GetCounter error: %v", counterErr)
    }
}

func TestManager_ManyDeserializesARepeatedKeyOnlyOnce(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    defer backend.Close()

    countingSerializer := &cacheTestCountingSerializer{inner: NewJsonSerializer()}
    manager := NewManager(backend, countingSerializer)

    if setErr := manager.Set("key", "value", 0); nil != setErr {
        t.Fatalf("unexpected set error: %v", setErr)
    }

    result, manyErr := manager.Many([]string{"key", "key", "key"})
    if nil != manyErr {
        t.Fatalf("unexpected many error: %v", manyErr)
    }

    if 1 != len(result) {
        t.Fatalf("expected the repeated key to appear once, got %#v", result)
    }

    if "value" != result["key"] {
        t.Fatalf("unexpected value: %v", result["key"])
    }

    if 1 != countingSerializer.deserializeCallCount {
        t.Fatalf("expected exactly one deserialization for the repeated key, got %d", countingSerializer.deserializeCallCount)
    }
}

type cacheTestCountingSerializer struct {
    inner                cachecontract.Serializer
    deserializeCallCount int
}

func (instance *cacheTestCountingSerializer) Serialize(value any) ([]byte, error) {
    return instance.inner.Serialize(value)
}

func (instance *cacheTestCountingSerializer) Deserialize(payload []byte) (any, error) {
    instance.deserializeCallCount = instance.deserializeCallCount + 1

    return instance.inner.Deserialize(payload)
}

/* the reasons are as many as the refused keys, and one of them can be the error's: a batch refused for several reasons reported one cause under a list of keys, leaving the operator to say which key it belonged to. The empty key is the one the sorted list names first whenever the batch carries it, which is a key the backend contract calls malformed — so "key" alone is not the whole answer. */
func TestManager_SetMultipleCarriesTheReasonOfEveryRefusedKey(t *testing.T) {
    backend := NewInMemoryBackend(10, time.Hour, &cacheTestClock{now: time.Unix(10, 0)})
    defer backend.Close()

    manager := NewManager(backend, NewJsonSerializer())

    setErr := manager.SetMultiple(map[string]any{
        "":     make(chan int),
        "mmm":  func() {},
        "fine": "value",
    }, time.Minute)

    if nil == setErr {
        t.Fatalf("expected the unserializable entries to be refused")
    }

    context := exception.LogContext(setErr)

    causeByKey, hasCauses := context["causeByKey"].(map[string]string)
    if false == hasCauses || 2 != len(causeByKey) {
        t.Fatalf("expected a reason per refused key, got %v", context["causeByKey"])
    }

    if "" == causeByKey[""] || "" == causeByKey["mmm"] {
        t.Fatalf("expected both refused keys to carry their own reason, got %v", causeByKey)
    }

    if causeByKey[""] == causeByKey["mmm"] {
        t.Fatalf("expected the two reasons to be the ones their own keys raised, got %v", causeByKey)
    }
}
