package cache

import (
    "fmt"
    "reflect"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

func TestRemember_AnswersTheTypeItsCallbackAnswers(t *testing.T) {
    for _, config := range []RememberConfig{
        {Ttl: bound.Of(time.Minute)},
        {Ttl: bound.Of(time.Minute), StampedeProtectionDisabled: true},
    } {
        manager, _ := newCacheTestManager(t)

        callback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (cacheTestUser, error) {
            return cacheTestUser{Name: "a", Tags: []string{"x"}}, nil
        }

        missed, missErr := Remember(For(testRuntime(), manager), "typed", config, callback)
        hit, hitErr := Remember(For(testRuntime(), manager), "typed", config, callback)
        if nil != missErr || nil != hitErr || false == reflect.DeepEqual(missed, hit) || "a" != hit.Name {
            t.Fatalf("expected the miss and the hit to answer one user (protection disabled %v), got %#v / %#v (%v, %v)", config.StampedeProtectionDisabled, missed, hit, missErr, hitErr)
        }

        computedGeneric, computedErr := Remember(For(testRuntime(), manager), "typed.generic", config, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            return 5, nil
        })
        if nil != computedErr || float64(5) != computedGeneric {
            t.Fatalf("expected the computing call of Remember[any] to answer the stored shape (protection disabled %v), got %#v (%v)", config.StampedeProtectionDisabled, computedGeneric, computedErr)
        }

        generic, genericErr := Remember(For(testRuntime(), manager), "typed", config, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            return nil, nil
        })
        if _, isMap := generic.(map[string]any); nil != genericErr || false == isMap {
            t.Fatalf("expected Remember[any] to answer the generic form, got %T (%v)", generic, genericErr)
        }
    }
}

func TestRemember_TwoTypesUnderOneKeyNeverShareAFlight(t *testing.T) {
    manager, _ := newCacheTestManager(t)

    var calls atomic.Int64
    gate := make(chan struct{})

    userAnswer := make(chan any, 1)
    go func() {
        value, err := Remember(For(testRuntime(), manager), "shared", RememberConfig{Ttl: bound.Of(time.Minute)}, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (cacheTestUser, error) {
            calls.Add(1)
            <-gate
            return cacheTestUser{Name: "user"}, nil
        })
        if nil != err {
            userAnswer <- err
            return
        }
        userAnswer <- value
    }()

    listAnswer := make(chan any, 1)
    go func() {
        value, err := Remember(For(testRuntime(), manager), "shared", RememberConfig{Ttl: bound.Of(time.Minute)}, func(runtimeInstance containercontract.Runtime, _ *RememberItem) ([]string, error) {
            calls.Add(1)
            <-gate
            return []string{"list"}, nil
        })
        if nil != err {
            listAnswer <- err
            return
        }
        listAnswer <- value
    }()

    deadline := time.Now().Add(2 * time.Second)
    for 2 != calls.Load() {
        if true == time.Now().After(deadline) {
            close(gate)
            t.Fatalf("expected both callbacks to run, one flight per type, got %d", calls.Load())
        }
        time.Sleep(time.Millisecond)
    }

    close(gate)

    if user, isUser := (<-userAnswer).(cacheTestUser); false == isUser || "user" != user.Name {
        t.Fatalf("expected the user caller answered its user")
    }

    if list, isList := (<-listAnswer).([]string); false == isList || false == reflect.DeepEqual([]string{"list"}, list) {
        t.Fatalf("expected the list caller answered its list")
    }
}

func TestRemember_AnEntryOfAnotherTypeIsAMissThatHeals(t *testing.T) {
    manager, _ := newCacheTestManager(t)

    if setErr := manager.Set(testRuntime(), "k", "text", bound.Of(time.Minute), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    calls := 0
    user, rememberErr := Remember(For(testRuntime(), manager), "k", RememberConfig{Ttl: bound.Of(time.Minute)}, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (cacheTestUser, error) {
        calls = calls + 1
        return cacheTestUser{Name: "healed"}, nil
    })
    if nil != rememberErr || 1 != calls || "healed" != user.Name {
        t.Fatalf("expected one recomputation over the other type, got %#v after %d calls (%v)", user, calls, rememberErr)
    }

    if stored, exists, getErr := Get[cacheTestUser](For(testRuntime(), manager), "k"); nil != getErr || false == exists || "healed" != stored.Name {
        t.Fatalf("expected the key healed, got %#v, %v, %v", stored, exists, getErr)
    }
}

type cacheTestStringer interface {
    String() string
}

type cacheTestNamed struct {
    Name string
}

func (instance cacheTestNamed) String() string {
    return instance.Name
}

func TestRemember_AValueTheCacheCannotReadBackIsRefusedOnce(t *testing.T) {
    for _, config := range []RememberConfig{
        {Ttl: bound.Of(time.Minute)},
        {Ttl: bound.Of(time.Minute), StampedeProtectionDisabled: true},
    } {
        manager, _ := newCacheTestManager(t)

        calls := 0
        _, rememberErr := Remember(For(testRuntime(), manager), "stringer", config, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (cacheTestStringer, error) {
            calls = calls + 1
            return cacheTestNamed{Name: "a"}, nil
        })
        if nil == rememberErr || false == strings.Contains(rememberErr.Error(), "cache value serialization failed") {
            t.Fatalf("expected the refusal named (protection disabled %v), got %v", config.StampedeProtectionDisabled, rememberErr)
        }

        if false == strings.Contains(fmt.Sprintf("%v", rememberErrContext(rememberErr)["key"]), "stringer") {
            t.Fatalf("expected the refusal to name the key, got %v", rememberErrContext(rememberErr))
        }

        if hasValue, _ := manager.Has(testRuntime(), "stringer"); true == hasValue || 1 != calls {
            t.Fatalf("expected nothing stored after one call (protection disabled %v), stored %v after %d calls", config.StampedeProtectionDisabled, hasValue, calls)
        }
    }
}

func TestRemember_ANilValueOfAPointerTypeIsRememberedAsAHit(t *testing.T) {
    manager, _ := newCacheTestManager(t)

    calls := 0
    callback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (*cacheTestUser, error) {
        calls = calls + 1
        return nil, nil
    }

    for index := 0; index < 2; index++ {
        user, rememberErr := Remember(For(testRuntime(), manager), "absent", RememberConfig{Ttl: bound.Of(time.Minute)}, callback)
        if nil != rememberErr || nil != user {
            t.Fatalf("expected the nil remembered, got %#v (%v)", user, rememberErr)
        }
    }

    if 1 != calls {
        t.Fatalf("expected the absence to be a hit, the callback ran %d times", calls)
    }

    t.Run("a nil of an interface type", func(t *testing.T) {
        stringer, rememberErr := Remember(For(testRuntime(), manager), "absent.stringer", RememberConfig{Ttl: bound.Of(time.Minute)}, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (cacheTestStringer, error) {
            return nil, nil
        })
        if nil != rememberErr || nil != stringer {
            t.Fatalf("expected the nil interface answered, got %#v (%v)", stringer, rememberErr)
        }
    })
}

func rememberErrContext(err error) map[string]any {
    typedErr, isTyped := err.(*exception.Error)
    if false == isTyped {
        return nil
    }

    return typedErr.Context()
}

func TestRememberedValue_AFlightOfAnotherTypeIsRefusedNotAsserted(t *testing.T) {
    value, unboxErr := unboxRememberedValue[int](rememberedValue[string]{value: "text"}, nil, "k")
    if 0 != value || nil == unboxErr || "cache remember flight answered another type" != unboxErr.Error() {
        t.Fatalf("expected the named refusal, got %v, %v", value, unboxErr)
    }

    operations := newRememberOperations(nil, "k", func(runtimeInstance containercontract.Runtime, _ *RememberItem) (int, error) { return 0, nil })
    if _, roundTripErr := operations.roundTrip(rememberedValue[string]{value: "text"}); nil == roundTripErr {
        t.Fatalf("expected the round trip of another box refused")
    }

    if storeErr := operations.store(testRuntime(), rememberedValue[string]{value: "text"}, bound.Of(time.Minute)); nil == storeErr {
        t.Fatalf("expected the store of another box refused")
    }
}

func TestRememberFlightKey_TellsTypesOfOneNameApart(t *testing.T) {
    manager, _ := newCacheTestManager(t)

    firstType := func() reflect.Type {
        type sameName struct{ A int }
        return reflect.TypeFor[sameName]()
    }()
    secondType := func() reflect.Type {
        type sameName struct{ A int }
        return reflect.TypeFor[sameName]()
    }()

    if firstType.String() != secondType.String() {
        t.Fatalf("expected the two types to share a name, got %s and %s", firstType, secondType)
    }

    firstKey, _ := rememberFlightKeyOf(manager, "k", firstType, true)
    secondKey, _ := rememberFlightKeyOf(manager, "k", secondType, true)
    if firstKey == secondKey {
        t.Fatalf("expected two types of one name to lead two flights")
    }
}
