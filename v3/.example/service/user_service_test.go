package service

import (
    "context"
    "testing"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/security"
    "strings"
    "time"
    examplecache "github.com/precision-soft/melody/v3/.example/cache"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodyclock "github.com/precision-soft/melody/v3/clock"
)


func TestAuthenticationDoesNotReuseCachedCredentials(t *testing.T) {
    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil))
    if nil != repositoryErr {
        t.Fatal(repositoryErr)
    }
    userService := NewUserService(userRepository, newTtlRecordingCache(), nil)
    cached, found, err := userService.FindByUsername("editor")
    if nil != err || false == found {
        t.Fatalf("prime cached editor: %v", err)
    }
    modified := *cached
    modified.Password = security.MustHashPassword("replacement-password")
    if updated, err := userRepository.Update(context.Background(), &modified); nil != err || false == updated {
        t.Fatalf("change password: %v", err)
    }
    if _, authenticated, err := userService.AuthenticateByUsernameAndPassword("editor", "editor"); nil != err || true == authenticated {
        t.Fatalf("old cached credentials were used: %v", err)
    }
    if _, authenticated, err := userService.AuthenticateByUsernameAndPassword("editor", "replacement-password"); nil != err || false == authenticated {
        t.Fatalf("current credentials were refused: %v", err)
    }
    if deleted, err := userRepository.DeleteById(context.Background(), modified.Id); nil != err || false == deleted {
        t.Fatalf("delete user: %v", err)
    }
    if _, authenticated, err := userService.AuthenticateByUsernameAndPassword("editor", "replacement-password"); nil != err || true == authenticated {
        t.Fatalf("deleted cached account authenticated: %v", err)
    }
}


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
