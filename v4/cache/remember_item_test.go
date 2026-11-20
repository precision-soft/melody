package cache

import (
    "context"
    "fmt"
    "runtime"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

type rememberItemTestSet struct {
    key string
    ttl bound.Duration
}

/* a manager whose every Set is noted with its ttl, so a pin reads what Remember told the store */
type rememberItemRecordingCache struct {
    *Manager
    mutex       sync.Mutex
    setList     []rememberItemTestSet
    onRoundTrip func()
}

func (instance *rememberItemRecordingCache) Set(ctx context.Context, key string, value any, ttl bound.Duration) error {
    instance.mutex.Lock()
    instance.setList = append(instance.setList, rememberItemTestSet{key: key, ttl: ttl})
    instance.mutex.Unlock()

    return instance.Manager.Set(ctx, key, value, ttl)
}

func (instance *rememberItemRecordingCache) RoundTrip(value any, target any) error {
    instance.mutex.Lock()
    onRoundTrip := instance.onRoundTrip
    instance.mutex.Unlock()

    if nil != onRoundTrip {
        onRoundTrip()
    }

    return instance.Manager.RoundTrip(value, target)
}

func (instance *rememberItemRecordingCache) sets() []rememberItemTestSet {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]rememberItemTestSet(nil), instance.setList...)
}

func newRememberItemRecordingCache(t *testing.T) *rememberItemRecordingCache {
    t.Helper()

    manager, _ := newCacheTestManager(t)

    return &rememberItemRecordingCache{Manager: manager}
}

/* the two engines a choice must reach: the flight's leader and the caller's own computation */
func rememberItemTestPaths() map[string]RememberConfig {
    return map[string]RememberConfig{
        "under the flight":        {Ttl: bound.Of(time.Minute)},
        "with the protection off": {Ttl: bound.Of(time.Minute), StampedeProtectionDisabled: true},
    }
}

func waitForRememberItemWaiters(t *testing.T, key string, waiters int64) {
    t.Helper()

    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) {
        shard := getRememberInFlightShard(rememberFlightKey{key: key})

        joined := int64(0)
        shard.mutex.Lock()
        for flightKey, call := range shard.inFlightByKey {
            if key == flightKey.key {
                joined = call.waitersCount.Load()
            }
        }
        shard.mutex.Unlock()

        if waiters == joined {
            return
        }

        time.Sleep(2 * time.Millisecond)
    }

    t.Fatalf("expected %d waiters in the flight of %s", waiters, key)
}

func TestRemember_TheCallbackChoosesTheTtlOfItsEntry(t *testing.T) {
    for name, config := range rememberItemTestPaths() {
        t.Run(name, func(t *testing.T) {
            cacheInstance := newRememberItemRecordingCache(t)

            value, rememberErr := Remember(context.Background(), cacheInstance, "chosen", config, func(ctx context.Context, item *RememberItem) (string, error) {
                item.SetTtl(bound.Of(5 * time.Second))

                return "value", nil
            })
            if nil != rememberErr || "value" != value {
                t.Fatalf("expected the value, got %v, %v", value, rememberErr)
            }

            sets := cacheInstance.sets()
            if 1 != len(sets) || bound.Of(5*time.Second) != sets[0].ttl {
                t.Fatalf("expected one store under the chosen 5s, got %+v", sets)
            }
        })
    }
}

func TestRemember_AnUntouchedItemStoresUnderTheConfigurationsTtl(t *testing.T) {
    for name, config := range rememberItemTestPaths() {
        t.Run(name, func(t *testing.T) {
            cacheInstance := newRememberItemRecordingCache(t)

            _, rememberErr := Remember(context.Background(), cacheInstance, "untouched", config, func(ctx context.Context, item *RememberItem) (string, error) {
                return "value", nil
            })
            if nil != rememberErr {
                t.Fatalf("remember: %v", rememberErr)
            }

            sets := cacheInstance.sets()
            if 1 != len(sets) || bound.Of(time.Minute) != sets[0].ttl {
                t.Fatalf("expected one store under the configuration's minute, got %+v", sets)
            }
        })
    }
}

func TestRemember_ASkippedStoreAnswersTheValueAndStoresNothing(t *testing.T) {
    for name, config := range rememberItemTestPaths() {
        t.Run(name, func(t *testing.T) {
            cacheInstance := newRememberItemRecordingCache(t)

            calls := 0
            callback := func(ctx context.Context, item *RememberItem) (string, error) {
                calls = calls + 1
                item.SkipStore()

                return "value", nil
            }

            for index := 0; index < 2; index++ {
                value, rememberErr := Remember(context.Background(), cacheInstance, "skipped", config, callback)
                if nil != rememberErr || "value" != value {
                    t.Fatalf("expected the value answered, got %v, %v", value, rememberErr)
                }
            }

            if 0 != len(cacheInstance.sets()) || 2 != calls {
                t.Fatalf("expected no store and a computation per call, got %d stores and %d calls", len(cacheInstance.sets()), calls)
            }
        })
    }
}

func TestRemember_ASkippedStoreAnswersEveryWaiterOfTheFlightItsOwnCopy(t *testing.T) {
    cacheInstance := newRememberItemRecordingCache(t)

    release := make(chan struct{})
    var calls atomic.Int64
    keptMap := map[string]int{"count": 1}
    callback := func(ctx context.Context, item *RememberItem) (map[string]int, error) {
        calls.Add(1)
        <-release
        item.SkipStore()

        return keptMap, nil
    }

    waiters := 8
    answers := make([]map[string]int, waiters)
    errorList := make([]error, waiters)

    var waitGroup sync.WaitGroup
    waitGroup.Add(waiters)
    for index := 0; index < waiters; index++ {
        go func() {
            defer waitGroup.Done()

            answers[index], errorList[index] = Remember(context.Background(), cacheInstance, "skipped.flight", RememberConfig{Ttl: bound.Of(time.Minute)}, callback)
        }()
    }

    waitForRememberItemWaiters(t, "skipped.flight", int64(waiters))
    close(release)
    waitGroup.Wait()

    if 1 != calls.Load() || 0 != len(cacheInstance.sets()) {
        t.Fatalf("expected one computation and no store, got %d and %d", calls.Load(), len(cacheInstance.sets()))
    }

    for index := 0; index < waiters; index++ {
        if nil != errorList[index] || 1 != answers[index]["count"] {
            t.Fatalf("expected every waiter answered the value, got %v, %v", answers[index], errorList[index])
        }
    }

    keptMap["count"] = 99
    answers[0]["count"] = 98
    for index := 1; index < waiters; index++ {
        if 1 != answers[index]["count"] {
            t.Fatalf("expected every waiter a copy of its own, waiter %d reads %d", index, answers[index]["count"])
        }
    }

    if 98 != answers[0]["count"] {
        t.Fatalf("expected the first caller a copy apart from the map the callback kept, it reads %d", answers[0]["count"])
    }
}

func TestRemember_ASkippedAnswerHasTheShapeAStoredOneReadsBackAs(t *testing.T) {
    for name, config := range rememberItemTestPaths() {
        t.Run(name, func(t *testing.T) {
            cacheInstance := newRememberItemRecordingCache(t)

            value, rememberErr := Remember(context.Background(), cacheInstance, "skipped.shape", config, func(ctx context.Context, item *RememberItem) (any, error) {
                item.SkipStore()

                return 7, nil
            })
            if nil != rememberErr {
                t.Fatalf("remember: %v", rememberErr)
            }

            if _, isFloat := value.(float64); false == isFloat {
                t.Fatalf("expected the json shape a hit answers, a float64, got %T", value)
            }
        })
    }
}

func TestRemember_ACallAfterASkippedDetachedFlightComputesAgain(t *testing.T) {
    cacheInstance := newRememberItemRecordingCache(t)

    var calls atomic.Int64
    callback := func(ctx context.Context, item *RememberItem) (int64, error) {
        item.SkipStore()

        return calls.Add(1), nil
    }

    /* the window between a finished flight and its deferred delete is a few instructions wide, and two processors open it most often */
    defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(2))

    iterations := 100000
    for index := 1; index <= iterations; index++ {
        value, rememberErr := Remember(context.Background(), cacheInstance, "skipped.detached", RememberConfig{Ttl: bound.Of(time.Minute), Detached: true}, callback)
        if nil != rememberErr {
            t.Fatalf("remember: %v", rememberErr)
        }

        if int64(index) != value {
            t.Fatalf("expected call %d to compute its own value, it was answered %d of a finished flight", index, value)
        }
    }
}

func TestRemember_ATtlTheCallbackChoseBelowTheMinimumIsRefusedNamingTheItem(t *testing.T) {
    for _, chosen := range []bound.Duration{bound.Of(time.Duration(0)), bound.Of(-time.Second), bound.Of(time.Second - time.Nanosecond)} {
        for name, config := range rememberItemTestPaths() {
            t.Run(fmt.Sprintf("%v %s", chosen, name), func(t *testing.T) {
                cacheInstance := newRememberItemRecordingCache(t)

                value, rememberErr := Remember(context.Background(), cacheInstance, "refused", config, func(ctx context.Context, item *RememberItem) (string, error) {
                    item.SetTtl(chosen)

                    return "value", nil
                })
                if nil == rememberErr || "RememberItem.Ttl" != rememberErrContext(rememberErr)["field"] {
                    t.Fatalf("expected the refusal naming RememberItem.Ttl, got %v (%v)", rememberErr, rememberErrContext(rememberErr))
                }

                if "" != value || 0 != len(cacheInstance.sets()) {
                    t.Fatalf("expected no value and no store, got %q and %+v", value, cacheInstance.sets())
                }
            })
        }
    }

    t.Run("every waiter of the flight", func(t *testing.T) {
        cacheInstance := newRememberItemRecordingCache(t)

        release := make(chan struct{})
        callback := func(ctx context.Context, item *RememberItem) (string, error) {
            <-release
            item.SetTtl(bound.Of(time.Millisecond))

            return "value", nil
        }

        waiters := 4
        errorList := make([]error, waiters)

        var waitGroup sync.WaitGroup
        waitGroup.Add(waiters)
        for index := 0; index < waiters; index++ {
            go func() {
                defer waitGroup.Done()

                _, errorList[index] = Remember(context.Background(), cacheInstance, "refused.flight", RememberConfig{Ttl: bound.Of(time.Minute)}, callback)
            }()
        }

        waitForRememberItemWaiters(t, "refused.flight", int64(waiters))
        close(release)
        waitGroup.Wait()

        for index := 0; index < waiters; index++ {
            if nil == errorList[index] || "RememberItem.Ttl" != rememberErrContext(errorList[index])["field"] {
                t.Fatalf("expected waiter %d refused naming the item, got %v", index, errorList[index])
            }
        }
    })
}

func TestRemember_AnUnlimitedTtlChosenByTheCallbackKeepsTheValue(t *testing.T) {
    cacheInstance := newRememberItemRecordingCache(t)

    _, rememberErr := Remember(context.Background(), cacheInstance, "unlimited", RememberConfig{Ttl: bound.Of(time.Minute)}, func(ctx context.Context, item *RememberItem) (string, error) {
        item.SetTtl(bound.UnlimitedDuration())

        return "value", nil
    })
    if nil != rememberErr {
        t.Fatalf("remember: %v", rememberErr)
    }

    sets := cacheInstance.sets()
    if 1 != len(sets) || bound.UnlimitedDuration() != sets[0].ttl {
        t.Fatalf("expected one store with no expiry, got %+v", sets)
    }
}

func TestRemember_AnAbsentTtlTakesTheChoiceBack(t *testing.T) {
    cacheInstance := newRememberItemRecordingCache(t)

    _, rememberErr := Remember(context.Background(), cacheInstance, "taken.back", RememberConfig{Ttl: bound.Of(time.Minute)}, func(ctx context.Context, item *RememberItem) (string, error) {
        item.SetTtl(bound.Of(5 * time.Second))
        item.SetTtl(bound.Duration{})

        return "value", nil
    })
    if nil != rememberErr {
        t.Fatalf("remember: %v", rememberErr)
    }

    sets := cacheInstance.sets()
    if 1 != len(sets) || bound.Of(time.Minute) != sets[0].ttl {
        t.Fatalf("expected the configuration's minute again, got %+v", sets)
    }
}

func TestRemember_ACallbackThatFailsStoresNothingWhateverItsItemSays(t *testing.T) {
    callbackErr := fmt.Errorf("the loader failed")

    type failingCallback struct {
        callback func(ctx context.Context, item *RememberItem) (string, error)
        message  string
    }

    callbackList := map[string]failingCallback{
        "a chosen ttl and an error": {func(ctx context.Context, item *RememberItem) (string, error) {
            item.SetTtl(bound.Of(5 * time.Second))

            return "value", callbackErr
        }, "the loader failed"},
        "a skipped store and an error": {func(ctx context.Context, item *RememberItem) (string, error) {
            item.SkipStore()

            return "value", callbackErr
        }, "the loader failed"},
        "a chosen ttl and a panic": {func(ctx context.Context, item *RememberItem) (string, error) {
            item.SetTtl(bound.Of(5 * time.Second))

            panic("the loader exploded")
        }, "cache remember callback panicked"},
    }

    for callbackName, failing := range callbackList {
        for name, config := range rememberItemTestPaths() {
            t.Run(callbackName+" "+name, func(t *testing.T) {
                cacheInstance := newRememberItemRecordingCache(t)

                _, rememberErr := Remember(context.Background(), cacheInstance, "failed", config, failing.callback)
                if nil == rememberErr || failing.message != rememberErr.Error() {
                    t.Fatalf("expected %q, got %v", failing.message, rememberErr)
                }

                if 0 != len(cacheInstance.sets()) {
                    t.Fatalf("expected no store, got %+v", cacheInstance.sets())
                }
            })
        }
    }
}

func TestRemember_ASkippedStoreWinsOverAChosenTtl(t *testing.T) {
    callbackList := map[string]func(item *RememberItem){
        "skip then ttl":           func(item *RememberItem) { item.SkipStore(); item.SetTtl(bound.Of(5 * time.Second)) },
        "ttl then skip":           func(item *RememberItem) { item.SetTtl(bound.Of(5 * time.Second)); item.SkipStore() },
        "skip with a refused ttl": func(item *RememberItem) { item.SkipStore(); item.SetTtl(bound.Of(time.Millisecond)) },
        "skip then an absent ttl": func(item *RememberItem) { item.SkipStore(); item.SetTtl(bound.Duration{}) },
    }

    for name, tell := range callbackList {
        t.Run(name, func(t *testing.T) {
            cacheInstance := newRememberItemRecordingCache(t)

            value, rememberErr := Remember(context.Background(), cacheInstance, "skip.wins", RememberConfig{Ttl: bound.Of(time.Minute)}, func(ctx context.Context, item *RememberItem) (string, error) {
                tell(item)

                return "value", nil
            })
            if nil != rememberErr || "value" != value || 0 != len(cacheInstance.sets()) {
                t.Fatalf("expected the value answered and nothing stored, got %v, %v, %+v", value, rememberErr, cacheInstance.sets())
            }
        })
    }
}

func TestRemember_AnItemToldSomethingAfterTheCallbackReturnedChangesNoStore(t *testing.T) {
    for name, config := range rememberItemTestPaths() {
        t.Run("told before the store "+name, func(t *testing.T) {
            cacheInstance := newRememberItemRecordingCache(t)

            /* the round trip runs after the item is read and before the Set, so what it tells the kept item lands between the two */
            var keptItem *RememberItem
            cacheInstance.onRoundTrip = func() {
                keptItem.SkipStore()
                keptItem.SetTtl(bound.Of(5 * time.Second))
            }

            _, rememberErr := Remember(context.Background(), cacheInstance, "kept.before.store", config, func(ctx context.Context, item *RememberItem) (string, error) {
                item.SetTtl(bound.Of(2 * time.Second))
                keptItem = item

                return "value", nil
            })
            if nil != rememberErr {
                t.Fatalf("remember: %v", rememberErr)
            }

            sets := cacheInstance.sets()
            if 1 != len(sets) || bound.Of(2*time.Second) != sets[0].ttl {
                t.Fatalf("expected the store told before the return, got %+v", sets)
            }
        })
    }

    cacheInstance := newRememberItemRecordingCache(t)

    keptItem := make(chan *RememberItem, 1)
    stored := make(chan struct{})

    var told sync.WaitGroup
    told.Add(1)
    go func() {
        defer told.Done()

        item := <-keptItem
        <-stored
        item.SkipStore()
        item.SetTtl(bound.Of(5 * time.Second))
    }()

    _, rememberErr := Remember(context.Background(), cacheInstance, "kept", RememberConfig{Ttl: bound.Of(time.Minute)}, func(ctx context.Context, item *RememberItem) (string, error) {
        item.SetTtl(bound.Of(2 * time.Second))
        keptItem <- item

        return "value", nil
    })
    close(stored)
    told.Wait()

    if nil != rememberErr {
        t.Fatalf("remember: %v", rememberErr)
    }

    sets := cacheInstance.sets()
    if 1 != len(sets) || bound.Of(2*time.Second) != sets[0].ttl {
        t.Fatalf("expected the store told before the return, got %+v", sets)
    }

    /* the item is read once: a goroutine telling it something while the callback returns races nothing */
    raceItem := make(chan *RememberItem, 1)
    var raced sync.WaitGroup
    raced.Add(1)
    go func() {
        defer raced.Done()

        item := <-raceItem
        for index := 0; index < 100; index++ {
            item.SetTtl(bound.Of(time.Duration(index+1) * time.Second))
        }
    }()

    _, raceErr := Remember(context.Background(), cacheInstance, "kept.race", RememberConfig{Ttl: bound.Of(time.Minute)}, func(ctx context.Context, item *RememberItem) (string, error) {
        raceItem <- item

        return "value", nil
    })
    raced.Wait()
    if nil != raceErr {
        t.Fatalf("remember under a racing teller: %v", raceErr)
    }
}

func TestRememberItem_ANilItemIgnoresWhatItIsToldAndTheZeroValueSaysNothing(t *testing.T) {
    var nilItem *RememberItem
    nilItem.SetTtl(bound.Of(time.Minute))
    nilItem.SkipStore()
    if false == nilItem.Ttl().IsAbsent() || true == nilItem.IsStoreSkipped() {
        t.Fatalf("expected a nil item to answer the zero value, got %v, %v", nilItem.Ttl(), nilItem.IsStoreSkipped())
    }

    var zeroItem RememberItem
    if false == zeroItem.Ttl().IsAbsent() || true == zeroItem.IsStoreSkipped() {
        t.Fatalf("expected the zero item to say nothing, got %v, %v", zeroItem.Ttl(), zeroItem.IsStoreSkipped())
    }

    zeroItem.SetTtl(bound.Of(5 * time.Second))
    zeroItem.SkipStore()
    if bound.Of(5*time.Second) != zeroItem.Ttl() || false == zeroItem.IsStoreSkipped() {
        t.Fatalf("expected the item to read back what it was told, got %v, %v", zeroItem.Ttl(), zeroItem.IsStoreSkipped())
    }
}

func TestRemember_EveryComputationGetsAnItemOfItsOwn(t *testing.T) {
    for name, config := range rememberItemTestPaths() {
        t.Run(name, func(t *testing.T) {
            cacheInstance := newRememberItemRecordingCache(t)

            calls := 0
            callback := func(ctx context.Context, item *RememberItem) (string, error) {
                calls = calls + 1
                if 1 == calls {
                    item.SkipStore()
                }

                return "value", nil
            }

            for index := 0; index < 2; index++ {
                if _, rememberErr := Remember(context.Background(), cacheInstance, "own.item", config, callback); nil != rememberErr {
                    t.Fatalf("remember: %v", rememberErr)
                }
            }

            sets := cacheInstance.sets()
            if 2 != calls || 1 != len(sets) || bound.Of(time.Minute) != sets[0].ttl {
                t.Fatalf("expected the second computation stored under the configuration's ttl, got %d calls and %+v", calls, sets)
            }
        })
    }
}

func TestRemember_ASkippedValueTheCacheCannotReadBackIsStillRefused(t *testing.T) {
    for name, config := range rememberItemTestPaths() {
        t.Run(name, func(t *testing.T) {
            cacheInstance := newRememberItemRecordingCache(t)

            _, rememberErr := Remember(context.Background(), cacheInstance, "skipped.stringer", config, func(ctx context.Context, item *RememberItem) (cacheTestStringer, error) {
                item.SkipStore()

                return cacheTestNamed{Name: "a"}, nil
            })
            if nil == rememberErr || false == strings.Contains(rememberErr.Error(), "cache value serialization failed") {
                t.Fatalf("expected the round trip refusal, got %v", rememberErr)
            }
        })
    }
}

func TestRemember_AnAbsenceIsRememberedUnderTheTtlItsCallbackChose(t *testing.T) {
    cacheInstance := newRememberItemRecordingCache(t)

    calls := 0
    callback := func(ctx context.Context, item *RememberItem) (*cacheTestUser, error) {
        calls = calls + 1
        item.SetTtl(bound.Of(time.Second))

        return nil, nil
    }

    for index := 0; index < 2; index++ {
        user, rememberErr := Remember(context.Background(), cacheInstance, "absence.chosen", RememberConfig{Ttl: bound.Of(time.Minute)}, callback)
        if nil != rememberErr || nil != user {
            t.Fatalf("expected the absence answered, got %#v, %v", user, rememberErr)
        }
    }

    sets := cacheInstance.sets()
    if 1 != calls || 1 != len(sets) || bound.Of(time.Second) != sets[0].ttl {
        t.Fatalf("expected one store of the absence under 1s and a hit after it, got %d calls and %+v", calls, sets)
    }
}
