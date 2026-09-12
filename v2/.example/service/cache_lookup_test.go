package service

import (
    "context"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v2/.example/entity"
    melodycachecontract "github.com/precision-soft/melody/v2/cache/contract"
)

/* ttlRecordingCache answers what it was given and remembers under which ttl each write landed, which is
   the whole property under test: the doors cannot be asked "for how long" any other way. It keeps values
   as they are, because these probes are not about serialization. */
type ttlRecordingCache struct {
    mutex  sync.Mutex
    values map[string]any
    writes []cacheWrite
}

type cacheWrite struct {
    key string
    ttl time.Duration
}

func newTtlRecordingCache() *ttlRecordingCache {
    return &ttlRecordingCache{values: map[string]any{}}
}

func (instance *ttlRecordingCache) writesFor(key string) []cacheWrite {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    matched := make([]cacheWrite, 0, len(instance.writes))
    for _, write := range instance.writes {
        if key == write.key {
            matched = append(matched, write)
        }
    }

    return matched
}

func (instance *ttlRecordingCache) Get(key string) (any, bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    value, exists := instance.values[key]

    return value, exists, nil
}

func (instance *ttlRecordingCache) Set(key string, value any, ttl time.Duration) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.values[key] = value
    instance.writes = append(instance.writes, cacheWrite{key: key, ttl: ttl})

    return nil
}

func (instance *ttlRecordingCache) Delete(key string) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    delete(instance.values, key)

    return nil
}

func (instance *ttlRecordingCache) Has(key string) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    _, exists := instance.values[key]

    return exists, nil
}

func (instance *ttlRecordingCache) Clear() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.values = map[string]any{}

    return nil
}

func (instance *ttlRecordingCache) Many(keys []string) (map[string]any, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    result := map[string]any{}
    for _, key := range keys {
        if value, exists := instance.values[key]; true == exists {
            result[key] = value
        }
    }

    return result, nil
}

func (instance *ttlRecordingCache) SetMultiple(items map[string]any, ttl time.Duration) error {
    for key, value := range items {
        if setErr := instance.Set(key, value, ttl); nil != setErr {
            return setErr
        }
    }

    return nil
}

func (instance *ttlRecordingCache) DeleteMultiple(keys []string) error {
    for _, key := range keys {
        if deleteErr := instance.Delete(key); nil != deleteErr {
            return deleteErr
        }
    }

    return nil
}

func (instance *ttlRecordingCache) Increment(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *ttlRecordingCache) Decrement(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *ttlRecordingCache) Close() error {
    return nil
}

var _ melodycachecontract.Cache = (*ttlRecordingCache)(nil)

/* countingUserRepository answers nothing and counts how often it was asked, which is what tells a
   remembered absence from a lookup that reached the directory again. */
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
