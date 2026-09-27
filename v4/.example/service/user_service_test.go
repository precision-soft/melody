package service

import (
    "context"
    "errors"
    "slices"
    "strings"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v4/.example/cache"
    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/event"
    "github.com/precision-soft/melody/v4/.example/persistence"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/.example/security"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyeventcontract "github.com/precision-soft/melody/v4/event/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v4/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* a name longer than the user table holds is a name this application does not have: it is answered as absent on the anonymous login door rather than composed into a cache key over the backends' 1024-byte ceiling, which the cache would refuse with a 500 */
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

/* the credential comparison is the one decision of the login door: the seeded administrator signs in with the password that produced the stored digest and with nothing else, and an absent username pays a real comparison, since a login that answered "absent" before any bcrypt work would tell an attacker, by its speed, which usernames exist. The floor is the security package's, set between a comparison bcrypt performs and one it skips. */
func TestUserService_AuthenticateAdmitsTheRightPasswordRefusesAWrongOneAndPaysForAnAbsentUser(t *testing.T) {
    backend := melodycache.NewInMemoryBackend(0, time.Minute, melodyclock.NewSystemClock())
    manager := melodycache.NewManagerOwningBackend(backend, examplecache.NewGobSerializer())
    t.Cleanup(func() { _ = manager.Close() })

    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil))
    if nil != repositoryErr {
        t.Fatalf("unexpected repository error: %v", repositoryErr)
    }

    userService := NewUserService(userRepository, manager, nil)

    user, authenticated, authenticateErr := userService.AuthenticateByUsernameAndPassword(context.Background(), "admin", "admin")
    if nil != authenticateErr || false == authenticated || nil == user || "admin" != user.Username {
        t.Fatalf("expected the seeded administrator to sign in with its password, got authenticated=%v user=%v err=%v", authenticated, user, authenticateErr)
    }

    user, authenticated, authenticateErr = userService.AuthenticateByUsernameAndPassword(context.Background(), "admin", "not-the-password")
    if nil != authenticateErr || true == authenticated || nil != user {
        t.Fatalf("expected a wrong password to be refused without an error, got authenticated=%v user=%v err=%v", authenticated, user, authenticateErr)
    }

    startedAt := time.Now()
    user, authenticated, authenticateErr = userService.AuthenticateByUsernameAndPassword(context.Background(), "nobody-of-that-name", "anything")
    absentCost := time.Since(startedAt)
    if nil != authenticateErr || true == authenticated || nil != user {
        t.Fatalf("expected an absent username to be refused without an error, got authenticated=%v user=%v err=%v", authenticated, user, authenticateErr)
    }
    if 5*time.Millisecond > absentCost {
        t.Fatalf("expected an absent username to pay a real comparison, but it was refused in %v", absentCost)
    }
}

/* the login reads the account from the repository, so a password change or a deletion the cache has not dropped yet cannot sign in with the replaced credential */
func TestUserService_AuthenticateReadsTheAccountPastTheCache(t *testing.T) {
    backend := melodycache.NewInMemoryBackend(0, time.Minute, melodyclock.NewSystemClock())
    manager := melodycache.NewManagerOwningBackend(backend, examplecache.NewGobSerializer())
    t.Cleanup(func() { _ = manager.Close() })

    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil))
    if nil != repositoryErr {
        t.Fatalf("unexpected repository error: %v", repositoryErr)
    }

    userService := NewUserService(userRepository, manager, nil)

    cached, found, findErr := userService.FindByUsername("editor")
    if nil != findErr || false == found {
        t.Fatalf("expected the seeded editor to be cached, got found=%v err=%v", found, findErr)
    }

    modified := *cached
    modified.Password = security.MustHashPassword("replacement-password")
    if updated, updateErr := userRepository.Update(context.Background(), &modified); nil != updateErr || false == updated {
        t.Fatalf("expected the password change to land, got updated=%v err=%v", updated, updateErr)
    }

    if _, authenticated, authenticateErr := userService.AuthenticateByUsernameAndPassword(context.Background(), "editor", "editor"); nil != authenticateErr || true == authenticated {
        t.Fatalf("expected the password the cache still holds to be refused, got authenticated=%v err=%v", authenticated, authenticateErr)
    }

    if _, authenticated, authenticateErr := userService.AuthenticateByUsernameAndPassword(context.Background(), "editor", "replacement-password"); nil != authenticateErr || false == authenticated {
        t.Fatalf("expected the current password to sign in, got authenticated=%v err=%v", authenticated, authenticateErr)
    }

    if deleted, deleteErr := userRepository.DeleteById(context.Background(), modified.Id); nil != deleteErr || false == deleted {
        t.Fatalf("expected the deletion to land, got deleted=%v err=%v", deleted, deleteErr)
    }

    if _, authenticated, authenticateErr := userService.AuthenticateByUsernameAndPassword(context.Background(), "editor", "replacement-password"); nil != authenticateErr || true == authenticated {
        t.Fatalf("expected a deleted account to be refused, got authenticated=%v err=%v", authenticated, authenticateErr)
    }
}

/* failingUsernameRepository fails every username read, so a login that reached the repository is told apart from one refused before it */
type failingUsernameRepository struct {
    repository.UserRepository
}

func (instance *failingUsernameRepository) FindByUsername(ctx context.Context, username string) (*entity.User, bool, error) {
    return nil, false, errors.New("the repository was asked")
}

/* a name the user table cannot hold is answered as absent before any backend is asked, on the login door as on FindByUsername */
func TestUserService_AuthenticateAnswersANameTheTableCannotHoldBeforeTheRepository(t *testing.T) {
    userService := NewUserService(&failingUsernameRepository{}, nil, nil)

    user, authenticated, authenticateErr := userService.AuthenticateByUsernameAndPassword(context.Background(), strings.Repeat("a", 256), "anything")
    if nil != authenticateErr || true == authenticated || nil != user {
        t.Fatalf("expected the name to be answered as absent without asking the repository, got authenticated=%v err=%v", authenticated, authenticateErr)
    }
}

/* a grant is a write of the account, and the listeners that drop the account's cache entries are subscribed to the updated event the service dispatches, so a grant that dispatched nothing would leave the earlier roles served from the cache for the life of the entry */
func TestUserService_GrantRoleTellsTheListeners(t *testing.T) {
    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil))
    if nil != repositoryErr {
        t.Fatalf("unexpected repository error: %v", repositoryErr)
    }

    clockInstance := melodyclock.NewSystemClock()
    dispatcher := newRecordingDispatcher(clockInstance, event.UserUpdatedEventName)

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    userService := NewUserService(userRepository, newTtlRecordingCache(), dispatcher.dispatcher)

    account, found, findErr := userRepository.FindByUsername(context.Background(), "user")
    if nil != findErr || false == found {
        t.Fatalf("expected the seeded account, got found=%v err=%v", found, findErr)
    }

    granted, outcome, grantErr := userService.GrantRole(runtimeInstance, account.Id, entity.RoleEditor)
    if nil != grantErr || repository.GrantRoleGranted != outcome || nil == granted {
        t.Fatalf("the grant answered %v, %d, %v", granted, outcome, grantErr)
    }

    if 1 != len(dispatcher.names()) || event.UserUpdatedEventName != dispatcher.names()[0] {
        t.Fatalf("the grant dispatched %v, wanted the one updated event", dispatcher.names())
    }

    if _, outcome, _ := userService.GrantRole(runtimeInstance, account.Id, entity.RoleEditor); repository.GrantRoleAlreadyHeld != outcome || 1 != len(dispatcher.names()) {
        t.Fatalf("a grant of a held role answered %d and dispatched %v, wanted already-held and no second event", outcome, dispatcher.names())
    }
}

/* refusingOnceDispatcher refuses the first dispatch and lets every later one through — a listener whose
   backend was gone for one tick */
type refusingOnceDispatcher struct {
    melodyeventcontract.EventDispatcher
    refused bool
}

func (instance *refusingOnceDispatcher) DispatchName(runtimeInstance melodyruntimecontract.Runtime, eventName string, payload any) (melodyeventcontract.Event, error) {
    if false == instance.refused {
        instance.refused = true

        return nil, errors.New("redis: connection refused")
    }

    return instance.EventDispatcher.DispatchName(runtimeInstance, eventName, payload)
}

/* a grant that committed and whose dispatch then failed leaves the account's cache entries standing, with no expiry, and a re-run that finds the role held dispatches nothing: the already-held answer drops the entries, the heal the unchanged quote has, and the failed dispatch hands back the account it did not announce */
func TestUserService_GrantRoleHealsTheCacheWhenTheRoleIsAlreadyHeld(t *testing.T) {
    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil))
    if nil != repositoryErr {
        t.Fatalf("unexpected repository error: %v", repositoryErr)
    }

    clockInstance := melodyclock.NewSystemClock()
    dispatcher := newRecordingDispatcher(clockInstance, event.UserUpdatedEventName)
    refusing := &refusingOnceDispatcher{EventDispatcher: dispatcher.dispatcher}

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    cacheInstance := newTtlRecordingCache()
    userService := NewUserService(userRepository, cacheInstance, refusing)

    /* the account is served from the cache, roles as seeded */
    cached, found, _ := userService.FindByUsername("user")
    if false == found || 1 != len(cached.Roles) {
        t.Fatalf("expected the seeded account with one role, got %v", cached)
    }

    granted, outcome, grantErr := userService.GrantRole(runtimeInstance, cached.Id, entity.RoleEditor)
    if nil == grantErr || repository.GrantRoleGranted != outcome || nil == granted {
        t.Fatalf("expected the committed grant handed back beside its failed dispatch, got %v, %d, %v", granted, outcome, grantErr)
    }

    if stillCached, _, _ := userService.FindByUsername("user"); 1 != len(stillCached.Roles) {
        t.Fatalf("expected the failed dispatch to leave the cache as it was, got %v", stillCached.Roles)
    }

    if _, outcome, grantErr := userService.GrantRole(runtimeInstance, cached.Id, entity.RoleEditor); nil != grantErr || repository.GrantRoleAlreadyHeld != outcome {
        t.Fatalf("expected the re-run to find the role held, got %d, %v", outcome, grantErr)
    }

    healed, _, _ := userService.FindByUsername("user")
    if 2 != len(healed.Roles) || entity.RoleEditor != healed.Roles[1] {
        t.Fatalf("expected the already-held answer to drop the stale entries so the directory is read again, got %v", healed.Roles)
    }
}

/* the already-held answer drops the three entries an account is served from, by name: the heal above reads the account by id again and would be satisfied by that one key alone, while the list, served to every reader of the directory, would keep the earlier roles for the life of the cache */
func TestUserService_GrantRoleAlreadyHeldDropsTheListEntryToo(t *testing.T) {
    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil))
    if nil != repositoryErr {
        t.Fatalf("unexpected repository error: %v", repositoryErr)
    }

    clockInstance := melodyclock.NewSystemClock()
    dispatcher := newRecordingDispatcher(clockInstance, event.UserUpdatedEventName)

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    cacheInstance := newTtlRecordingCache()
    userService := NewUserService(userRepository, cacheInstance, dispatcher.dispatcher)

    cached, found, _ := userService.FindByUsername("editor")
    if false == found || false == slices.Contains(cached.Roles, entity.RoleEditor) {
        t.Fatalf("expected the seeded editor holding its role, got %v", cached)
    }

    dropsBefore := len(cacheInstance.deletedKeyList())
    if _, outcome, grantErr := userService.GrantRole(runtimeInstance, cached.Id, entity.RoleEditor); nil != grantErr || repository.GrantRoleAlreadyHeld != outcome {
        t.Fatalf("expected the role found held, got %d, %v", outcome, grantErr)
    }

    dropped := cacheInstance.deletedKeyList()[dropsBefore:]
    for _, wanted := range []string{CacheKeyUserById(cached.Id), CacheKeyUserByUsername("editor"), CacheKeyUserList} {
        if false == slices.Contains(dropped, wanted) {
            t.Fatalf("expected the already-held answer to drop %q, it dropped %v", wanted, dropped)
        }
    }
}

/* the stored account owns its role list: a caller that goes on writing into the slice it handed Update must not change the account behind the repository */
func TestUserService_UpdateStoresACopyOfTheCallersRoleList(t *testing.T) {
    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil))
    if nil != repositoryErr {
        t.Fatalf("unexpected repository error: %v", repositoryErr)
    }

    dispatcher := newRecordingDispatcher(melodyclock.NewSystemClock(), event.UserUpdatedEventName)

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    userService := NewUserService(userRepository, newTtlRecordingCache(), dispatcher.dispatcher)

    target, found, findErr := userRepository.FindByUsername(context.Background(), "editor")
    if nil != findErr || false == found {
        t.Fatalf("expected the seeded editor, got found=%v err=%v", found, findErr)
    }

    roles := []string{"ROLE_USER", "ROLE_EDITOR"}
    if _, updated, updateErr := userService.Update(runtimeInstance, target.Id, target.Username, target.Password, roles); nil != updateErr || false == updated {
        t.Fatalf("expected the update to land, got updated=%v err=%v", updated, updateErr)
    }

    roles[1] = "ROLE_ADMIN"

    stored, found, findErr := userRepository.FindById(context.Background(), target.Id)
    if nil != findErr || false == found || "ROLE_EDITOR" != stored.Roles[1] {
        t.Fatalf("the caller's slice reached the stored account: %v", stored.Roles)
    }
}
