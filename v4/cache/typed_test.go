package cache

import (
    "context"
    "errors"
    "reflect"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/clock"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type cacheTestUser struct {
    Name string
    Tags []string
    Meta map[string]int
}

type cacheTestCountingBackend struct {
    cachecontract.Backend
    getCalls  atomic.Int64
    manyCalls atomic.Int64
    setCalls  atomic.Int64
    manyErr   error
}

func (instance *cacheTestCountingBackend) Set(runtimeInstance containercontract.Runtime, key string, payload []byte, ttl bound.Duration, options cachecontract.SetOptions) error {
    instance.setCalls.Add(1)

    return instance.Backend.Set(runtimeInstance, key, payload, ttl, options)
}

func (instance *cacheTestCountingBackend) SetMultiple(runtimeInstance containercontract.Runtime, items map[string][]byte, ttl bound.Duration, options cachecontract.SetOptions) error {
    instance.setCalls.Add(1)

    return instance.Backend.SetMultiple(runtimeInstance, items, ttl, options)
}

func (instance *cacheTestCountingBackend) Get(runtimeInstance containercontract.Runtime, key string) ([]byte, bool, error) {
    instance.getCalls.Add(1)

    return instance.Backend.Get(runtimeInstance, key)
}

func (instance *cacheTestCountingBackend) Many(runtimeInstance containercontract.Runtime, keys []string) (map[string][]byte, error) {
    instance.manyCalls.Add(1)
    if nil != instance.manyErr {
        return nil, instance.manyErr
    }

    return instance.Backend.Many(runtimeInstance, keys)
}

func newCacheTestManager(t *testing.T) (*Manager, *cacheTestCountingBackend) {
    t.Helper()

    inner := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(100), CleanupInterval: bound.Of(time.Hour), Clock: clock.NewSystemClock()})
    t.Cleanup(func() { _ = inner.CloseWithContext(context.Background()) })

    backend := &cacheTestCountingBackend{Backend: inner}

    return NewManager(backend, NewJsonSerializer(), ManagerConfig{}), backend
}

func TestGet_AnswersTheTypeAsked(t *testing.T) {
    manager, _ := newCacheTestManager(t)

    if setErr := manager.Set(testRuntime(), "k", cacheTestUser{Name: "a", Tags: []string{"x"}}, bound.Of(time.Minute), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    user, exists, getErr := Get[cacheTestUser](For(testRuntime(), manager), "k")
    if nil != getErr || false == exists || false == reflect.DeepEqual(cacheTestUser{Name: "a", Tags: []string{"x"}}, user) {
        t.Fatalf("expected the user read back typed, got %#v, %v, %v", user, exists, getErr)
    }

    generic, exists, getErr := Get[any](For(testRuntime(), manager), "k")
    if _, isMap := generic.(map[string]any); nil != getErr || false == exists || false == isMap {
        t.Fatalf("expected the generic read to answer a map, got %T, %v, %v", generic, exists, getErr)
    }
}

func TestGet_AnswersTheZeroValueBesideAnError(t *testing.T) {
    manager, _ := newCacheTestManager(t)

    if setErr := manager.Set(testRuntime(), "k", "text", bound.Of(time.Minute), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    user, exists, getErr := Get[cacheTestUser](For(testRuntime(), manager), "k")
    if false == IsDeserializationError(getErr) || true == exists || false == reflect.DeepEqual(cacheTestUser{}, user) {
        t.Fatalf("expected the zero user beside a deserialization error, got %#v, %v, %v", user, exists, getErr)
    }
}

type halfFillingCache struct {
    cachecontract.Cache
}

func (instance *halfFillingCache) Get(runtimeInstance containercontract.Runtime, key string, target any) (bool, error) {
    if user, isUser := target.(*cacheTestUser); true == isUser {
        user.Name = "half written"
    }

    return false, errors.New("the store failed after writing")
}

func (instance *halfFillingCache) Many(runtimeInstance containercontract.Runtime, keys []string, target any) error {
    return nil
}

func TestGet_AnswersTheZeroValueBesideAFailureOfACacheThatWroteTheTarget(t *testing.T) {
    user, exists, getErr := Get[cacheTestUser](For(testRuntime(), &halfFillingCache{}), "k")
    if nil == getErr || true == exists || false == reflect.DeepEqual(cacheTestUser{}, user) {
        t.Fatalf("expected the zero user beside the failure, got %#v, %v, %v", user, exists, getErr)
    }
}

func TestMany_AnswersAnEmptyMapWhereTheCacheLeftTheTargetNil(t *testing.T) {
    values, manyErr := Many[cacheTestUser](For(testRuntime(), &halfFillingCache{}), []string{"k"})
    if nil != manyErr || nil == values {
        t.Fatalf("expected an empty non-nil map, got %#v, %v", values, manyErr)
    }
}

type corruptBatchCache struct {
    cachecontract.Cache
}

func (instance *corruptBatchCache) Many(runtimeInstance containercontract.Runtime, keys []string, target any) error {
    return NewDeserializationError(keys, errors.New("corrupt"))
}

func TestMany_AnswersAnEmptyMapBesideADeserializationErrorWhereTheCacheLeftTheTargetNil(t *testing.T) {
    values, manyErr := Many[cacheTestUser](For(testRuntime(), &corruptBatchCache{}), []string{"k"})
    if false == IsDeserializationError(manyErr) || nil == values {
        t.Fatalf("expected an empty non-nil map beside the deserialization error, got %#v, %v", values, manyErr)
    }
}

type cacheTestSummary struct {
    Title  string
    Orders int
}

func TestGet_UnderJsonAnObjectWrittenAsOneStructReadsAsAnotherStruct(t *testing.T) {
    manager, _ := newCacheTestManager(t)

    if setErr := manager.Set(testRuntime(), "k", cacheTestUser{Name: "a"}, bound.Of(time.Minute), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    summary, exists, getErr := Get[cacheTestSummary](For(testRuntime(), manager), "k")
    if nil != getErr || false == exists || (cacheTestSummary{}) != summary {
        t.Fatalf("expected json to read the object as a hit of the zero summary, got %#v, %v, %v", summary, exists, getErr)
    }
}

func TestMany_AnswersAnEmptyMapOnNoHit(t *testing.T) {
    manager, _ := newCacheTestManager(t)

    values, manyErr := Many[cacheTestUser](For(testRuntime(), manager), []string{"absent"})
    if nil != manyErr || nil == values || 0 != len(values) {
        t.Fatalf("expected an empty non-nil map, got %#v, %v", values, manyErr)
    }
}

func TestGetAndMany_RefuseATypedNilCache(t *testing.T) {
    var typedNil *Manager

    if _, _, getErr := Get[string](For(testRuntime(), typedNil), "k"); nil == getErr || "cache instance is nil" != getErr.Error() {
        t.Fatalf("expected Get to refuse a typed-nil cache, got %v", getErr)
    }

    if _, manyErr := Many[string](For(testRuntime(), typedNil), []string{"k"}); nil == manyErr || "cache instance is nil" != manyErr.Error() {
        t.Fatalf("expected Many to refuse a typed-nil cache, got %v", manyErr)
    }
}

func TestGet_ANilStoredValueIsAHitOfTheZeroValue(t *testing.T) {
    manager, _ := newCacheTestManager(t)

    if setErr := manager.Set(testRuntime(), "k", nil, bound.Of(time.Minute), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    user, exists, getErr := Get[*cacheTestUser](For(testRuntime(), manager), "k")
    if nil != getErr || false == exists || nil != user {
        t.Fatalf("expected a hit of the nil pointer, got %#v, %v, %v", user, exists, getErr)
    }
}

func TestManager_Get_OverwritesTheTargetWhole(t *testing.T) {
    manager, _ := newCacheTestManager(t)

    if setErr := manager.Set(testRuntime(), "k", cacheTestUser{Name: "new"}, bound.Of(time.Minute), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    target := cacheTestUser{Name: "old", Tags: []string{"x"}, Meta: map[string]int{"a": 1}}
    exists, getErr := manager.Get(testRuntime(), "k", &target)
    if nil != getErr || false == exists {
        t.Fatalf("expected a hit, got %v, %v", exists, getErr)
    }

    if "new" != target.Name || nil != target.Tags || nil != target.Meta {
        t.Fatalf("expected the target overwritten whole, got %#v", target)
    }
}

type writingRefusingSerializer struct {
    *JsonSerializer
}

func (instance writingRefusingSerializer) Deserialize(payload []byte, target any) error {
    if user, isUser := target.(*cacheTestUser); true == isUser {
        user.Name = "half written"
    }

    return errors.New("refused after writing")
}

func TestManager_Get_LeavesTheTargetOnAMissAFailureAndACorruptPayload(t *testing.T) {
    held := cacheTestUser{Name: "held"}

    t.Run("miss", func(t *testing.T) {
        manager, _ := newCacheTestManager(t)
        target := held
        if exists, getErr := manager.Get(testRuntime(), "absent", &target); nil != getErr || true == exists || "held" != target.Name {
            t.Fatalf("expected the target kept on a miss, got %#v, %v, %v", target, exists, getErr)
        }
    })

    t.Run("failure", func(t *testing.T) {
        manager, _ := newCacheTestManager(t)
        target := held
        if exists, getErr := manager.Get(testRuntime(), "", &target); nil == getErr || true == exists || "held" != target.Name {
            t.Fatalf("expected the target kept on a refused key, got %#v, %v, %v", target, exists, getErr)
        }
    })

    t.Run("corrupt", func(t *testing.T) {
        inner := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clock.NewSystemClock()})
        defer func() { _ = inner.CloseWithContext(context.Background()) }()

        manager := NewManager(inner, writingRefusingSerializer{JsonSerializer: &JsonSerializer{}}, ManagerConfig{})
        if setErr := inner.Set(testRuntime(), "corrupt", []byte("{"), bound.Of(time.Minute), cachecontract.SetOptions{}); nil != setErr {
            t.Fatalf("set error: %v", setErr)
        }

        target := held
        exists, getErr := manager.Get(testRuntime(), "corrupt", &target)
        if false == IsDeserializationError(getErr) || true == exists || "held" != target.Name {
            t.Fatalf("expected the target kept beside a deserialization error, got %#v, %v, %v", target, exists, getErr)
        }

        var deserializationErr *DeserializationError
        if false == errors.As(getErr, &deserializationErr) || false == reflect.DeepEqual([]string{"corrupt"}, deserializationErr.exceptionErr.Context()["keys"]) {
            t.Fatalf("expected the error to name the key, got %v", getErr)
        }
    })
}

func TestManager_Get_RefusesATargetThatIsNoNonNilPointer(t *testing.T) {
    manager, backend := newCacheTestManager(t)

    var typedNil *cacheTestUser
    for name, target := range map[string]any{"nil": nil, "struct value": cacheTestUser{}, "typed nil pointer": typedNil} {
        exists, getErr := manager.Get(testRuntime(), "k", target)
        if nil == getErr || true == exists || true == IsDeserializationError(getErr) {
            t.Fatalf("%s: expected a refusal that is no deserialization error, got %v, %v", name, exists, getErr)
        }
    }

    if 0 != backend.getCalls.Load() {
        t.Fatalf("expected the store untouched, read %d times", backend.getCalls.Load())
    }
}

func TestManager_Get_APayloadOfAnotherTypeIsADeserializationError(t *testing.T) {
    manager, _ := newCacheTestManager(t)

    if setErr := manager.Set(testRuntime(), "k", "text", bound.Of(time.Minute), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    var user cacheTestUser
    if _, getErr := manager.Get(testRuntime(), "k", &user); false == IsDeserializationError(getErr) {
        t.Fatalf("expected a deserialization error, got %v", getErr)
    }
}

func TestManager_Get_ANilRuntimeIsRefusedBeforeTheTarget(t *testing.T) {
    manager, _ := newCacheTestManager(t)

    for _, refusedRuntime := range refusedTestRuntimes() {
        if _, getErr := manager.Get(refusedRuntime, "k", nil); nil == getErr || refusedRuntimeMessage != getErr.Error() {
            t.Fatalf("expected the runtime refusal first, got %v", getErr)
        }
    }
}

func TestManager_Many_ReplacesTheMapWithTheDecodedEntries(t *testing.T) {
    manager, _ := newCacheTestManager(t)

    if setErr := manager.Set(testRuntime(), "a", cacheTestUser{Name: "a"}, bound.Of(time.Minute), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    target := map[string]cacheTestUser{"stale": {Name: "stale"}}
    if manyErr := manager.Many(testRuntime(), []string{"a", "absent"}, &target); nil != manyErr {
        t.Fatalf("many error: %v", manyErr)
    }

    if false == reflect.DeepEqual(map[string]cacheTestUser{"a": {Name: "a"}}, target) {
        t.Fatalf("expected the map replaced by the present entries, got %#v", target)
    }

    var nilTarget map[string]cacheTestUser
    if manyErr := manager.Many(testRuntime(), []string{"a"}, &nilTarget); nil != manyErr || 1 != len(nilTarget) {
        t.Fatalf("expected a nil map behind the pointer admitted, got %#v, %v", nilTarget, manyErr)
    }

    var generic map[string]any
    if manyErr := manager.Many(testRuntime(), []string{"a"}, &generic); nil != manyErr {
        t.Fatalf("many error: %v", manyErr)
    }

    if _, isMap := generic["a"].(map[string]any); false == isMap {
        t.Fatalf("expected the generic batch to answer generic values, got %T", generic["a"])
    }
}

func TestManager_Many_RefusesATargetThatIsNoPointerToAStringKeyedMap(t *testing.T) {
    manager, backend := newCacheTestManager(t)

    for name, target := range map[string]any{
        "nil":          nil,
        "slice":        &[]cacheTestUser{},
        "int keyed":    &map[int]cacheTestUser{},
        "map value":    map[string]cacheTestUser{},
    } {
        if manyErr := manager.Many(testRuntime(), []string{"k"}, target); nil == manyErr || true == IsDeserializationError(manyErr) {
            t.Fatalf("%s: expected a refusal that is no deserialization error, got %v", name, manyErr)
        }
    }

    if 0 != backend.manyCalls.Load() {
        t.Fatalf("expected the store untouched, read %d times", backend.manyCalls.Load())
    }
}

func TestManager_Many_LeavesTheTargetWhenTheBackendFails(t *testing.T) {
    manager, backend := newCacheTestManager(t)
    backend.manyErr = errors.New("the store is down")

    target := map[string]cacheTestUser{"held": {Name: "held"}}
    if manyErr := manager.Many(testRuntime(), []string{"held"}, &target); nil == manyErr {
        t.Fatalf("expected the backend failure answered")
    }

    if false == reflect.DeepEqual(map[string]cacheTestUser{"held": {Name: "held"}}, target) {
        t.Fatalf("expected the target kept, got %#v", target)
    }
}

func TestManager_RoundTrip_AnswersWhatAReadAnswersAndTouchesNoBackend(t *testing.T) {
    manager, backend := newCacheTestManager(t)

    source := cacheTestUser{Name: "a", Meta: map[string]int{"n": 1}}

    var copyOfSource cacheTestUser
    if roundTripErr := manager.RoundTrip(source, &copyOfSource); nil != roundTripErr || false == reflect.DeepEqual(source, copyOfSource) {
        t.Fatalf("expected an equal copy, got %#v, %v", copyOfSource, roundTripErr)
    }

    copyOfSource.Meta["n"] = 2
    if 1 != source.Meta["n"] {
        t.Fatalf("expected the copy apart from the source")
    }

    if 0 != backend.getCalls.Load() || 0 != backend.manyCalls.Load() || 0 != backend.setCalls.Load() {
        t.Fatalf("expected the store untouched")
    }

    refusing := NewManager(backend, refusingSerializer{}, ManagerConfig{})
    if roundTripErr := refusing.RoundTrip(source, &copyOfSource); nil == roundTripErr || "serialization refused" != roundTripErr.Error() {
        t.Fatalf("expected the serializer's refusal answered bare, got %v", roundTripErr)
    }

    if roundTripErr := manager.RoundTrip(source, nil); nil == roundTripErr {
        t.Fatalf("expected a nil target refused")
    }
}
