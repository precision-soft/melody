package service

import (
    "context"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/repository"
)

type countingUserRepository struct {
    mutex   sync.Mutex
    lookups int
}

func (instance *countingUserRepository) All(ctx context.Context) ([]*entity.User, error) {
    return nil, nil
}

func (instance *countingUserRepository) FindById(ctx context.Context, id string) (*entity.User, bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.lookups++

    return nil, false, nil
}

func (instance *countingUserRepository) FindByUsername(ctx context.Context, username string) (*entity.User, bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.lookups++

    return nil, false, nil
}

func (instance *countingUserRepository) Create(ctx context.Context, user *entity.User) error {
    return nil
}

func (instance *countingUserRepository) Update(ctx context.Context, user *entity.User) (bool, error) {
    return false, nil
}

func (instance *countingUserRepository) GrantRole(ctx context.Context, id string, role string) (*entity.User, repository.GrantRoleOutcome, error) {
    return nil, repository.GrantRoleAccountAbsent, nil
}

func (instance *countingUserRepository) DeleteById(ctx context.Context, id string) (bool, error) {
    return false, nil
}

/* an absence is remembered under a key its caller spelled, so it has to lapse: the unauthenticated login
   door writes one of these for every name anyone invents. */
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

/* an entity that was found keeps the unbounded entry the event listeners clear by name: an expiry here
   would send every request after it back to the database for a value nothing had invalidated. */
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

/* the probe is on a hit that FOUND something, because that is the only hit the early return changes:
   a remembered absence is answered the same way with or without it — Remember reads the store itself and
   writes nothing on a hit, and a nil computed value is returned before the second write — while a found
   entity would be re-stored by every reader, one cache write per request served from memory. Measured:
   with the early return disarmed, this is the assertion that moves. */
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

/* the absence keeps the expiry it was given: nothing on the read path writes it back, so a name someone
   keeps trying still lapses on its own schedule. */
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

/* the door is what has to bound the absence, not the helper alone: this is the one reachable without
   credentials, and a lookup that stopped going through the helper would leave it permanent again. */
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

    /* the window is asserted as the VALUE, not as the constant the code reads: comparing against
       absenceCacheTtl moves both sides of the assertion together, so a window shortened to nothing —
       which is what an unbounded entry is spelled as — would read as correct. */
    if time.Minute != writes[0].ttl {
        t.Fatalf("expected the door to bound the absence to a minute, got %s", writes[0].ttl)
    }
}

/* a loader that parks until it is released puts twenty readers of one key in flight together, so Remember coalesces them onto one loader; what is then counted is who writes the found value unbounded: the reader that loaded it once, and no waiter — a waiter's write landed after the leader's and re-installed, unbounded, whatever a listener had cleared in between, so the absence of every waiter's write is the whole repair. */
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

    /* the readers are started, and the one whose loader is running is parked at the gate; the others wait on it inside Remember. The gate opens once the leader's loader has been entered, which the run counter shows. */
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
