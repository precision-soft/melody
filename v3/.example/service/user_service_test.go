package service

import (
    "strings"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v3/.example/cache"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodyclock "github.com/precision-soft/melody/v3/clock"
)

/* a name longer than the user table holds is a name this application does not have: answered as absent on the anonymous login door, where a cache key over the backends' 1024-byte ceiling — 1000 ASCII bytes, or 167 two-byte runes once the escape has tripled them — was refused by the cache and surfaced as a 500 */
func TestUserService_FindByUsernameAnswersANameTheTableCannotHoldAsAbsent(t *testing.T) {
    backend := melodycache.NewInMemoryBackend(0, time.Minute, melodyclock.NewSystemClock())
    manager := melodycache.NewManagerOwningBackend(backend, examplecache.NewGobSerializer())
    t.Cleanup(func() { _ = manager.Close() })

    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil))
    if nil != repositoryErr {
        t.Fatalf("unexpected repository error: %v", repositoryErr)
    }

    userService := NewUserService(userRepository, manager, nil)

    for _, username := range []string{strings.Repeat("a", 1000), strings.Repeat("é", 167), strings.Repeat("😀", 84), strings.Repeat("a", 256)} {
        user, found, findErr := userService.FindByUsername(username)
        if nil != findErr || true == found || nil != user {
            t.Fatalf("expected a %d-byte name to be answered as absent without an error, got found=%v err=%v", len(username), found, findErr)
        }
    }

    if _, found, findErr := userService.FindById(strings.Repeat("😀", 84)); nil != findErr || true == found {
        t.Fatalf("expected an id the key grammar refuses to be answered as absent without an error, got found=%v err=%v", found, findErr)
    }

    user, found, findErr := userService.FindByUsername("admin")
    if nil != findErr || false == found || nil == user {
        t.Fatalf("expected the seeded administrator to stay reachable, got found=%v err=%v", found, findErr)
    }
}
