package service

import (
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    "context"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/persistence"
    "github.com/precision-soft/melody/v4/.example/repository"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodyclock "github.com/precision-soft/melody/v4/clock"
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

func (instance *countingUserRepository) Update(ctx context.Context, id string, change repository.UserChange, guard repository.UserGuard) (*entity.User, *entity.User, error) {
    return nil, nil, nil
}

func (instance *countingUserRepository) GrantRole(ctx context.Context, id string, role string) (*entity.User, repository.GrantRoleOutcome, error) {
    return nil, repository.GrantRoleAccountAbsent, nil
}

func (instance *countingUserRepository) DeleteById(ctx context.Context, id string, guard repository.UserGuard) (*entity.User, error) {
    return nil, nil
}

func TestRememberEntityOrAbsenceBoundsAnAbsence(t *testing.T) {
    cacheInstance := newTtlRecordingCache()

    value, rememberErr := rememberEntityOrAbsence(
        melodycache.For(exampleTestRuntime(), cacheInstance),
        "absent-key",
        func(runtimeInstance melodycontainercontract.Runtime) (*string, error) {
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

    if melodybound.Of(absenceCacheTtl) != writes[0].ttl {
        t.Fatalf("expected the absence to be bounded to %s, got %s", absenceCacheTtl, writes[0].ttl)
    }
}

func TestRememberEntityOrAbsenceLeavesAFoundEntityUnbounded(t *testing.T) {
    cacheInstance := newTtlRecordingCache()

    value, rememberErr := rememberEntityOrAbsence(
        melodycache.For(exampleTestRuntime(), cacheInstance),
        "found-key",
        func(runtimeInstance melodycontainercontract.Runtime) (*string, error) {
            return lookupTestEntity(), nil
        },
    )
    if nil != rememberErr {
        t.Fatalf("remember: %v", rememberErr)
    }

    if "entity" != lookupTestText(value) {
        t.Fatalf("expected the computed value, got %v", value)
    }

    writes := cacheInstance.writesFor("found-key")
    if 1 != len(writes) || melodybound.Of(10*time.Minute) != writes[0].ttl {
        t.Fatalf("expected the found value written once, under the ten-minute entity bound, got %+v", writes)
    }
}

func TestRememberEntityOrAbsenceAnswersAFoundHitWithoutWritingItBack(t *testing.T) {
    cacheInstance := newTtlRecordingCache()
    if setErr := cacheInstance.Set(exampleTestRuntime(), "hit-key", lookupTestEntity(), melodybound.Of(entityCacheTtl), melodycachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("seed: %v", setErr)
    }

    loaderCalls := 0

    value, rememberErr := rememberEntityOrAbsence(
        melodycache.For(exampleTestRuntime(), cacheInstance),
        "hit-key",
        func(runtimeInstance melodycontainercontract.Runtime) (*string, error) {
            loaderCalls++

            recomputed := "recomputed"

            return &recomputed, nil
        },
    )
    if nil != rememberErr {
        t.Fatalf("remember: %v", rememberErr)
    }

    if "entity" != lookupTestText(value) {
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
    if setErr := cacheInstance.Set(exampleTestRuntime(), "absent-hit-key", nil, melodybound.Of(absenceCacheTtl), melodycachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("seed: %v", setErr)
    }

    value, rememberErr := rememberEntityOrAbsence(
        melodycache.For(exampleTestRuntime(), cacheInstance),
        "absent-hit-key",
        func(runtimeInstance melodycontainercontract.Runtime) (*string, error) {
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
        _, found, findErr := userService.FindByUsername(exampleTestRuntime(), "zz-absent-name")
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

    if melodybound.Of(time.Minute) != writes[0].ttl {
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
                melodycache.For(exampleTestRuntime(), cacheInstance),
                "coalesced-key",
                func(runtimeInstance melodycontainercontract.Runtime) (*string, error) {
                    loaderRuns.Add(1)
                    <-release

                    return lookupTestEntity(), nil
                },
            )
            if nil != rememberErr || "entity" != lookupTestText(value) {
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
    entityWrites := 0
    for _, write := range writes {
        if melodybound.Of(entityCacheTtl) == write.ttl {
            entityWrites = entityWrites + 1
        }
    }

    if 1 != entityWrites || 1 != len(writes) {
        t.Fatalf("expected the found value to be written once, under the entity bound, got %d such writes among %d", entityWrites, len(writes))
    }
}

func TestRememberEntityOrAbsenceReadsTheStoreAsRememberAloneDoes(t *testing.T) {
    helperCache := newTtlRecordingCache()
    if _, rememberErr := rememberEntityOrAbsence(melodycache.For(exampleTestRuntime(), helperCache), "read-key", func(runtimeInstance melodycontainercontract.Runtime) (*string, error) {
        return lookupTestEntity(), nil
    }); nil != rememberErr {
        t.Fatalf("helper: %v", rememberErr)
    }

    bareCache := newTtlRecordingCache()
    if _, rememberErr := melodycache.Remember(melodycache.For(exampleTestRuntime(), bareCache), "read-key", melodycache.RememberOptions{Ttl: melodybound.Of(absenceCacheTtl)}, func(runtimeInstance melodycontainercontract.Runtime, _ *melodycache.RememberItem) (*string, error) {
        return lookupTestEntity(), nil
    }); nil != rememberErr {
        t.Fatalf("remember: %v", rememberErr)
    }

    if bareCache.readsFor("read-key") != helperCache.readsFor("read-key") {
        t.Fatalf("expected the helper's miss to read the store %d times, as Remember alone does, got %d", bareCache.readsFor("read-key"), helperCache.readsFor("read-key"))
    }
}

func TestTheCatalogueListsAreRememberedUnderTheEntityBound(t *testing.T) {
    storage := persistence.NewCatalogStorage(nil)
    productRepository, _ := repository.NewProductRepository(storage, &persistence.CatalogSchema{})
    categoryRepository, _ := repository.NewCategoryRepository(storage, &persistence.CatalogSchema{})
    currencyRepository, _ := repository.NewCurrencyRepository(storage, &persistence.CatalogSchema{})
    cacheInstance := newTtlRecordingCache()
    ctx := context.Background()

    categoryService := NewCategoryService(categoryRepository, productRepository, cacheInstance, nil)
    currencyService := NewCurrencyService(currencyRepository, productRepository, cacheInstance, nil, melodyclock.NewSystemClock())
    productService := NewProductService(productRepository, categoryService, currencyService, cacheInstance, nil, melodyclock.NewSystemClock(), time.Hour)
    userRepository, _ := repository.NewUserRepository(storage, &persistence.CatalogSchema{})
    userService := NewUserService(userRepository, cacheInstance, nil)

    if _, listErr := productService.List(exampleTestRuntimeUnder(ctx)); nil != listErr {
        t.Fatalf("products: %v", listErr)
    }
    if _, listErr := categoryService.List(exampleTestRuntimeUnder(ctx)); nil != listErr {
        t.Fatalf("categories: %v", listErr)
    }
    if _, listErr := currencyService.List(exampleTestRuntimeUnder(ctx)); nil != listErr {
        t.Fatalf("currencies: %v", listErr)
    }
    if _, listErr := userService.List(exampleTestRuntimeUnder(ctx)); nil != listErr {
        t.Fatalf("users: %v", listErr)
    }

    for _, key := range []string{CacheKeyProductList, CacheKeyCategoryList, CacheKeyCurrencyList, CacheKeyUserList} {
        writes := cacheInstance.writesFor(key)
        if 0 == len(writes) || melodybound.Of(entityCacheTtl) != writes[0].ttl {
            t.Fatalf("expected %s remembered under %s, got %+v", key, entityCacheTtl, writes)
        }
    }
}

func lookupTestEntity() *string {
    name := "entity"

    return &name
}

func lookupTestText(value *string) string {
    if nil == value {
        return ""
    }

    return *value
}
