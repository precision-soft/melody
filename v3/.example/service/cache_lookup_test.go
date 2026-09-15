package service

import (
    "context"
    "sync"
    "sync/atomic"
    "testing"
    "time"
)

func TestRememberEntityOrAbsenceBoundsAnAbsence(t *testing.T) {
    cacheInstance := newTtlRecordingCache()

    value, rememberErr := rememberEntityOrAbsence(
        cacheInstance,
        "absent-key",
        func(ctx context.Context) (any, error) {
            return nil, nil
        },
    )
    if nil != rememberErr {
        t.Fatalf("remember: %v", rememberErr)
    }

    if nil != value {
        t.Fatalf("expected the absence to be answered as nil, got %v", value)
    }

    writes := cacheInstance.writesFor("absent-key")
    if 1 != len(writes) {
        t.Fatalf("expected the absence to be written once, got %d writes", len(writes))
    }

    if absenceCacheTtl != writes[0].ttl {
        t.Fatalf("expected the absence to be bounded to %s, got %s", absenceCacheTtl, writes[0].ttl)
    }
}

func TestRememberEntityOrAbsenceLeavesAFoundEntityUnbounded(t *testing.T) {
    cacheInstance := newTtlRecordingCache()

    value, rememberErr := rememberEntityOrAbsence(
        cacheInstance,
        "found-key",
        func(ctx context.Context) (any, error) {
            return "entity", nil
        },
    )
    if nil != rememberErr {
        t.Fatalf("remember: %v", rememberErr)
    }

    if "entity" != value {
        t.Fatalf("expected the computed value, got %v", value)
    }

    writes := cacheInstance.writesFor("found-key")
    if 0 == len(writes) {
        t.Fatalf("expected the value to be written")
    }

    lastWrite := writes[len(writes)-1]
    if entityCacheTtl != lastWrite.ttl {
        t.Fatalf("expected the found value to end unbounded, got %s", lastWrite.ttl)
    }
}

func TestRememberEntityOrAbsenceAnswersAFoundHitWithoutWritingItBack(t *testing.T) {
    cacheInstance := newTtlRecordingCache()
    if setErr := cacheInstance.Set("hit-key", "entity", entityCacheTtl); nil != setErr {
        t.Fatalf("seed: %v", setErr)
    }

    loaderCalls := 0

    value, rememberErr := rememberEntityOrAbsence(
        cacheInstance,
        "hit-key",
        func(ctx context.Context) (any, error) {
            loaderCalls++

            return "recomputed", nil
        },
    )
    if nil != rememberErr {
        t.Fatalf("remember: %v", rememberErr)
    }

    if "entity" != value {
        t.Fatalf("expected the stored value, got %v", value)
    }

    if 0 != loaderCalls {
        t.Fatalf("expected the hit to be answered without the loader, got %d calls", loaderCalls)
    }

    if 1 != len(cacheInstance.writesFor("hit-key")) {
        t.Fatalf("expected no write beyond the seed, got %d", len(cacheInstance.writesFor("hit-key")))
    }
}

func TestRememberEntityOrAbsenceLeavesARememberedAbsenceUntouched(t *testing.T) {
    cacheInstance := newTtlRecordingCache()
    if setErr := cacheInstance.Set("absent-hit-key", nil, absenceCacheTtl); nil != setErr {
        t.Fatalf("seed: %v", setErr)
    }

    value, rememberErr := rememberEntityOrAbsence(
        cacheInstance,
        "absent-hit-key",
        func(ctx context.Context) (any, error) {
            t.Fatalf("the loader ran over a remembered absence")

            return nil, nil
        },
    )
    if nil != rememberErr {
        t.Fatalf("remember: %v", rememberErr)
    }

    if nil != value {
        t.Fatalf("expected the remembered absence, got %v", value)
    }

    if 1 != len(cacheInstance.writesFor("absent-hit-key")) {
        t.Fatalf("expected the window to be left alone, got %d writes", len(cacheInstance.writesFor("absent-hit-key")))
    }
}

func TestFindByUsernameBoundsTheAbsenceItRemembers(t *testing.T) {
    cacheInstance := newTtlRecordingCache()
    userRepository := &countingUserRepository{}

    userService := NewUserService(userRepository, cacheInstance, nil)

    for attempt := 0; 2 > attempt; attempt++ {
        _, found, findErr := userService.FindByUsername("zz-absent-name")
        if nil != findErr {
            t.Fatalf("find by username: %v", findErr)
        }

        if true == found {
            t.Fatalf("expected the name to be absent")
        }
    }

    if 1 != userRepository.lookups {
        t.Fatalf("expected the absence to be remembered after the first lookup, got %d lookups", userRepository.lookups)
    }

    writes := cacheInstance.writesFor(CacheKeyUserByUsername("zz-absent-name"))
    if 1 != len(writes) {
        t.Fatalf("expected one write for the absence, got %d", len(writes))
    }

    if time.Minute != writes[0].ttl {
        t.Fatalf("expected the door to bound the absence to a minute, got %s", writes[0].ttl)
    }
}

func TestRememberEntityOrAbsenceWritesAFoundValueOnceForEveryWaiterOfOneLoad(t *testing.T) {
    cacheInstance := newTtlRecordingCache()

    readers := 20
    release := make(chan struct{})
    loaderRuns := atomic.Int64{}

    started := sync.WaitGroup{}
    finished := sync.WaitGroup{}
    for index := 0; index < readers; index++ {
        started.Add(1)
        finished.Add(1)

        go func() {
            defer finished.Done()

            started.Done()

            value, rememberErr := rememberEntityOrAbsence(
                cacheInstance,
                "coalesced-key",
                func(ctx context.Context) (any, error) {
                    loaderRuns.Add(1)
                    <-release

                    return "entity", nil
                },
            )
            if nil != rememberErr || "entity" != value {
                t.Errorf("expected every reader to receive the entity, got %v %v", value, rememberErr)
            }
        }()
    }

    started.Wait()

    deadline := time.Now().Add(2 * time.Second)
    for 1 > loaderRuns.Load() && time.Now().Before(deadline) {
        time.Sleep(time.Millisecond)
    }
    time.Sleep(20 * time.Millisecond)
    close(release)

    finished.Wait()

    if 1 != loaderRuns.Load() {
        t.Fatalf("expected one loader run for the coalesced readers, got %d — the probe did not coalesce and cannot separate the writers", loaderRuns.Load())
    }

    writes := cacheInstance.writesFor("coalesced-key")
    unbounded := 0
    for _, write := range writes {
        if entityCacheTtl == write.ttl {
            unbounded = unbounded + 1
        }
    }

    if 1 != unbounded {
        t.Fatalf("expected the found value to be written unbounded exactly once, by the reader that loaded it, got %d unbounded writes among %d", unbounded, len(writes))
    }
}
