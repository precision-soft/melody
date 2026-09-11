package service

import (
    "context"
    "testing"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/security"
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
