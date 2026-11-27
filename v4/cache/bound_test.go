package cache

import (
    "context"
    "errors"
    "reflect"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

var _ interface {
    Get(key string, target any) (bool, error)
    Set(key string, value any, ttl bound.Duration) error
    Delete(key string) error
    Has(key string) (bool, error)
    Clear() error
    Many(keys []string, target any) error
    SetMultiple(items map[string]any, ttl bound.Duration) error
    DeleteMultiple(keys []string) error
    Increment(key string, delta int64, options cachecontract.CounterOptions) (int64, error)
    Decrement(key string, delta int64, options cachecontract.CounterOptions) (int64, error)
    GetCounter(key string) (int64, bool, error)
    Runtime() containercontract.Runtime
    Cache() cachecontract.Cache
} = Bound{}

func TestBound_ForwardsEveryRuntimeDoorOfTheContract(t *testing.T) {
    contractType := reflect.TypeFor[cachecontract.Cache]()
    runtimeType := reflect.TypeFor[containercontract.Runtime]()
    boundType := reflect.TypeFor[Bound]()

    withoutRuntime := map[string]bool{}
    forwarded := map[string]bool{"Runtime": true, "Cache": true}
    for index := 0; index < contractType.NumMethod(); index = index + 1 {
        door := contractType.Method(index)
        if 0 == door.Type.NumIn() || runtimeType != door.Type.In(0) {
            withoutRuntime[door.Name] = true
            continue
        }

        viewMethod, found := boundType.MethodByName(door.Name)
        if false == found {
            t.Fatalf("the view does not forward %s", door.Name)
        }

        /* the view's method type carries its receiver first */
        if viewMethod.Type.NumIn()-1 != door.Type.NumIn()-1 || viewMethod.Type.NumOut() != door.Type.NumOut() || viewMethod.Type.IsVariadic() != door.Type.IsVariadic() {
            t.Fatalf("%s: the view's signature %s is not the contract's %s less the runtime", door.Name, viewMethod.Type, door.Type)
        }

        for parameterIndex := 1; parameterIndex < door.Type.NumIn(); parameterIndex = parameterIndex + 1 {
            if door.Type.In(parameterIndex) != viewMethod.Type.In(parameterIndex) {
                t.Fatalf("%s: parameter %d is %s on the view, %s on the contract", door.Name, parameterIndex, viewMethod.Type.In(parameterIndex), door.Type.In(parameterIndex))
            }
        }

        for resultIndex := 0; resultIndex < door.Type.NumOut(); resultIndex = resultIndex + 1 {
            if door.Type.Out(resultIndex) != viewMethod.Type.Out(resultIndex) {
                t.Fatalf("%s: result %d is %s on the view, %s on the contract", door.Name, resultIndex, viewMethod.Type.Out(resultIndex), door.Type.Out(resultIndex))
            }
        }

        forwarded[door.Name] = true
    }

    if false == reflect.DeepEqual(map[string]bool{"RoundTrip": true, "CloseWithContext": true}, withoutRuntime) {
        t.Fatalf("expected RoundTrip and CloseWithContext to be the contract's only doors without a runtime, got %v", withoutRuntime)
    }

    /* the pointer's method set holds the value's and any method declared on *Bound */
    pointerType := reflect.TypeFor[*Bound]()
    if pointerType.NumMethod() != boundType.NumMethod() {
        t.Fatalf("expected every method of the view declared on the value, %d on the pointer against %d", pointerType.NumMethod(), boundType.NumMethod())
    }

    for index := 0; index < boundType.NumMethod(); index = index + 1 {
        if false == forwarded[boundType.Method(index).Name] {
            t.Fatalf("the view declares %s, which forwards no door of the contract", boundType.Method(index).Name)
        }
    }
}

var boundRecordedErr = errors.New("the contract's own answer")

type boundRecordedCall struct {
    door            string
    runtimeInstance containercontract.Runtime
    arguments       []any
}

type boundRecordingCache struct {
    callList []boundRecordedCall
}

func (instance *boundRecordingCache) record(door string, runtimeInstance containercontract.Runtime, arguments ...any) {
    instance.callList = append(instance.callList, boundRecordedCall{door: door, runtimeInstance: runtimeInstance, arguments: arguments})
}

func (instance *boundRecordingCache) Get(runtimeInstance containercontract.Runtime, key string, target any) (bool, error) {
    instance.record("Get", runtimeInstance, key, target)
    return true, boundRecordedErr
}

func (instance *boundRecordingCache) Set(runtimeInstance containercontract.Runtime, key string, value any, ttl bound.Duration) error {
    instance.record("Set", runtimeInstance, key, value, ttl)
    return boundRecordedErr
}

func (instance *boundRecordingCache) Delete(runtimeInstance containercontract.Runtime, key string) error {
    instance.record("Delete", runtimeInstance, key)
    return boundRecordedErr
}

func (instance *boundRecordingCache) Has(runtimeInstance containercontract.Runtime, key string) (bool, error) {
    instance.record("Has", runtimeInstance, key)
    return true, boundRecordedErr
}

func (instance *boundRecordingCache) Clear(runtimeInstance containercontract.Runtime) error {
    instance.record("Clear", runtimeInstance)
    return boundRecordedErr
}

func (instance *boundRecordingCache) Many(runtimeInstance containercontract.Runtime, keys []string, target any) error {
    instance.record("Many", runtimeInstance, keys, target)
    return boundRecordedErr
}

func (instance *boundRecordingCache) SetMultiple(runtimeInstance containercontract.Runtime, items map[string]any, ttl bound.Duration) error {
    instance.record("SetMultiple", runtimeInstance, items, ttl)
    return boundRecordedErr
}

func (instance *boundRecordingCache) DeleteMultiple(runtimeInstance containercontract.Runtime, keys []string) error {
    instance.record("DeleteMultiple", runtimeInstance, keys)
    return boundRecordedErr
}

func (instance *boundRecordingCache) Increment(runtimeInstance containercontract.Runtime, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    instance.record("Increment", runtimeInstance, key, delta, options)
    return delta, boundRecordedErr
}

func (instance *boundRecordingCache) Decrement(runtimeInstance containercontract.Runtime, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    instance.record("Decrement", runtimeInstance, key, delta, options)
    return -delta, boundRecordedErr
}

func (instance *boundRecordingCache) GetCounter(runtimeInstance containercontract.Runtime, key string) (int64, bool, error) {
    instance.record("GetCounter", runtimeInstance, key)
    return 19, true, boundRecordedErr
}

func (instance *boundRecordingCache) RoundTrip(value any, target any) error {
    return nil
}

func (instance *boundRecordingCache) CloseWithContext(closeContext context.Context) error {
    return nil
}

func TestBound_EveryMethodReachesItsDoorWithTheBoundRuntimeAndItsArguments(t *testing.T) {
    runtimeInstance := testRuntime()
    recordingCache := &boundRecordingCache{}
    boundCache := For(runtimeInstance, recordingCache)

    var target any
    var targets map[string]any
    keys := []string{"first", "second"}
    items := map[string]any{"first": 1}
    setTtl := bound.Of(3 * time.Minute)
    setMultipleTtl := bound.Of(5 * time.Minute)
    incrementOptions := cachecontract.CounterOptions{Ttl: bound.Of(7 * time.Minute)}
    decrementOptions := cachecontract.CounterOptions{Ttl: bound.Of(11 * time.Minute), RefreshTtl: true}

    found, getErr := boundCache.Get("get.key", &target)
    setErr := boundCache.Set("set.key", "set.value", setTtl)
    deleteErr := boundCache.Delete("delete.key")
    held, hasErr := boundCache.Has("has.key")
    clearErr := boundCache.Clear()
    manyErr := boundCache.Many(keys, &targets)
    setMultipleErr := boundCache.SetMultiple(items, setMultipleTtl)
    deleteMultipleErr := boundCache.DeleteMultiple(keys)
    incremented, incrementErr := boundCache.Increment("increment.key", 13, incrementOptions)
    decremented, decrementErr := boundCache.Decrement("decrement.key", 17, decrementOptions)
    counted, counterExists, getCounterErr := boundCache.GetCounter("counter.key")

    if true != found || true != held || 13 != incremented || -17 != decremented || 19 != counted || true != counterExists {
        t.Fatalf("expected the view to answer what the contract answered, got %v %v %d %d %d %v", found, held, incremented, decremented, counted, counterExists)
    }

    for doorName, doorErr := range map[string]error{"Get": getErr, "Set": setErr, "Delete": deleteErr, "Has": hasErr, "Clear": clearErr, "Many": manyErr, "SetMultiple": setMultipleErr, "DeleteMultiple": deleteMultipleErr, "Increment": incrementErr, "Decrement": decrementErr, "GetCounter": getCounterErr} {
        if boundRecordedErr != doorErr {
            t.Fatalf("%s: expected the view to answer the contract's error, got %v", doorName, doorErr)
        }
    }

    expected := []boundRecordedCall{
        {door: "Get", arguments: []any{"get.key", &target}},
        {door: "Set", arguments: []any{"set.key", "set.value", setTtl}},
        {door: "Delete", arguments: []any{"delete.key"}},
        {door: "Has", arguments: []any{"has.key"}},
        {door: "Clear", arguments: nil},
        {door: "Many", arguments: []any{keys, &targets}},
        {door: "SetMultiple", arguments: []any{items, setMultipleTtl}},
        {door: "DeleteMultiple", arguments: []any{keys}},
        {door: "Increment", arguments: []any{"increment.key", int64(13), incrementOptions}},
        {door: "Decrement", arguments: []any{"decrement.key", int64(17), decrementOptions}},
        {door: "GetCounter", arguments: []any{"counter.key"}},
    }

    if len(expected) != len(recordingCache.callList) {
        t.Fatalf("expected %d calls, got %d", len(expected), len(recordingCache.callList))
    }

    for index, expectedCall := range expected {
        recorded := recordingCache.callList[index]
        if expectedCall.door != recorded.door {
            t.Fatalf("call %d: expected %s, got %s", index, expectedCall.door, recorded.door)
        }

        if runtimeInstance != recorded.runtimeInstance {
            t.Fatalf("%s: expected the bound runtime", recorded.door)
        }

        if false == reflect.DeepEqual(expectedCall.arguments, recorded.arguments) {
            t.Fatalf("%s: expected the arguments %v, got %v", recorded.door, expectedCall.arguments, recorded.arguments)
        }
    }

    if runtimeInstance != boundCache.Runtime() || cachecontract.Cache(recordingCache) != boundCache.Cache() {
        t.Fatalf("expected the halves to answer what the view was bound over")
    }
}

func TestBound_AViewOverNoCacheRefusesEveryDoor(t *testing.T) {
    var typedNilCache *Manager

    for _, boundCache := range []Bound{{}, For(testRuntime(), nil), For(testRuntime(), typedNilCache)} {
        var target any
        var targets map[string]any
        _, getErr := boundCache.Get("k", &target)
        setErr := boundCache.Set("k", "v", bound.Of(time.Minute))
        deleteErr := boundCache.Delete("k")
        _, hasErr := boundCache.Has("k")
        clearErr := boundCache.Clear()
        manyErr := boundCache.Many([]string{"k"}, &targets)
        setMultipleErr := boundCache.SetMultiple(map[string]any{"k": "v"}, bound.Of(time.Minute))
        deleteMultipleErr := boundCache.DeleteMultiple([]string{"k"})
        _, incrementErr := boundCache.Increment("k", 1, cachecontract.CounterOptions{})
        _, decrementErr := boundCache.Decrement("k", 1, cachecontract.CounterOptions{})
        _, _, getCounterErr := boundCache.GetCounter("k")

        for doorName, doorErr := range map[string]error{"Get": getErr, "Set": setErr, "Delete": deleteErr, "Has": hasErr, "Clear": clearErr, "Many": manyErr, "SetMultiple": setMultipleErr, "DeleteMultiple": deleteMultipleErr, "Increment": incrementErr, "Decrement": decrementErr, "GetCounter": getCounterErr} {
            if nil == doorErr || "cache instance is nil" != doorErr.Error() {
                t.Fatalf("%s: expected the absent cache refused, got %v", doorName, doorErr)
            }
        }

        if _, _, getErr := Get[int](boundCache, "k"); nil == getErr || "cache instance is nil" != getErr.Error() {
            t.Fatalf("expected the typed read to refuse the absent cache, got %v", getErr)
        }

        if _, manyErr := Many[int](boundCache, []string{"k"}); nil == manyErr || "cache instance is nil" != manyErr.Error() {
            t.Fatalf("expected the typed batch read to refuse the absent cache, got %v", manyErr)
        }
    }
}

func TestBound_BindingAndCallingAllocateNothing(t *testing.T) {
    runtimeInstance := testRuntime()
    counter := &boundCountingCache{}

    allocations := testing.AllocsPerRun(1000, func() {
        _, _ = For(runtimeInstance, counter).Increment("k", 1, cachecontract.CounterOptions{})
    })
    if 0 != allocations {
        t.Fatalf("expected a bind and a call to allocate nothing, got %v", allocations)
    }

    /* the typed read's target escapes into the contract's call whichever way it is reached, so the view is pinned at adding nothing to the direct call */
    boundAllocationCache = counter
    directAllocations := testing.AllocsPerRun(1000, func() {
        var value int
        _, _ = boundAllocationCache.Get(runtimeInstance, "k", &value)
    })
    typedAllocations := testing.AllocsPerRun(1000, func() {
        _, _, _ = Get[int](For(runtimeInstance, counter), "k")
    })
    if directAllocations != typedAllocations {
        t.Fatalf("expected a bind and a typed read to allocate what the direct call does, %v, got %v", directAllocations, typedAllocations)
    }
}

/* a package variable, so the direct call is made through the contract as the view makes it, never devirtualized */
var boundAllocationCache cachecontract.Cache

/* boundCountingCache answers without allocating, so what an allocation count reads is the view's own */
type boundCountingCache struct {
    boundRecordingCache
    calls int
}

func (instance *boundCountingCache) Get(runtimeInstance containercontract.Runtime, key string, target any) (bool, error) {
    instance.calls = instance.calls + 1
    return false, nil
}

func (instance *boundCountingCache) Increment(runtimeInstance containercontract.Runtime, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    instance.calls = instance.calls + 1
    return delta, nil
}
