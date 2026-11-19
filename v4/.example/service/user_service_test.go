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
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyeventcontract "github.com/precision-soft/melody/v4/event/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v4/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

func TestUserService_FindByUsernameAnswersANameTheTableCannotHoldAsAbsent(t *testing.T) {
    backend := melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{CleanupInterval: melodybound.Of(time.Minute), Clock: melodyclock.NewSystemClock()})
    manager := melodycache.NewManager(backend, examplecache.NewGobSerializer(), melodycache.ManagerConfig{OwnsBackend: true})
    t.Cleanup(func() { _ = manager.CloseWithContext(context.Background()) })

    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil).WithAccountSeed())
    if nil != repositoryErr {
        t.Fatalf("unexpected repository error: %v", repositoryErr)
    }

    userService := NewUserService(userRepository, manager, nil)

    for _, username := range []string{strings.Repeat("a", 1000), strings.Repeat("é", 167), strings.Repeat("😀", 84), strings.Repeat("a", 256)} {
        user, found, findErr := userService.FindByUsername(context.Background(), username)
        if nil != findErr || true == found || nil != user {
            t.Fatalf("expected a %d-byte name to be answered as absent without an error, got found=%v err=%v", len(username), found, findErr)
        }
    }

    if _, found, findErr := userService.FindById(context.Background(), strings.Repeat("😀", 84)); nil != findErr || true == found {
        t.Fatalf("expected an id the key grammar refuses to be answered as absent without an error, got found=%v err=%v", found, findErr)
    }

    user, found, findErr := userService.FindByUsername(context.Background(), "admin")
    if nil != findErr || false == found || nil == user {
        t.Fatalf("expected the seeded administrator to stay reachable, got found=%v err=%v", found, findErr)
    }
}

func TestUserService_AuthenticateAdmitsTheRightPasswordRefusesAWrongOneAndPaysForAnAbsentUser(t *testing.T) {
    backend := melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{CleanupInterval: melodybound.Of(time.Minute), Clock: melodyclock.NewSystemClock()})
    manager := melodycache.NewManager(backend, examplecache.NewGobSerializer(), melodycache.ManagerConfig{OwnsBackend: true})
    t.Cleanup(func() { _ = manager.CloseWithContext(context.Background()) })

    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil).WithAccountSeed())
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

func TestUserService_AuthenticateReadsTheAccountPastTheCache(t *testing.T) {
    backend := melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{CleanupInterval: melodybound.Of(time.Minute), Clock: melodyclock.NewSystemClock()})
    manager := melodycache.NewManager(backend, examplecache.NewGobSerializer(), melodycache.ManagerConfig{OwnsBackend: true})
    t.Cleanup(func() { _ = manager.CloseWithContext(context.Background()) })

    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil).WithAccountSeed())
    if nil != repositoryErr {
        t.Fatalf("unexpected repository error: %v", repositoryErr)
    }

    userService := NewUserService(userRepository, manager, nil)

    cached, found, findErr := userService.FindByUsername(context.Background(), "editor")
    if nil != findErr || false == found {
        t.Fatalf("expected the seeded editor to be cached, got found=%v err=%v", found, findErr)
    }

    replacementHash := security.MustHashPassword("replacement-password")
    if before, _, updateErr := userRepository.Update(context.Background(), cached.Id, repository.UserChange{PasswordHash: &replacementHash}, nil); nil != updateErr || nil == before {
        t.Fatalf("expected the password change to land, got before=%v err=%v", before, updateErr)
    }

    if _, authenticated, authenticateErr := userService.AuthenticateByUsernameAndPassword(context.Background(), "editor", "editor"); nil != authenticateErr || true == authenticated {
        t.Fatalf("expected the password the cache still holds to be refused, got authenticated=%v err=%v", authenticated, authenticateErr)
    }

    if _, authenticated, authenticateErr := userService.AuthenticateByUsernameAndPassword(context.Background(), "editor", "replacement-password"); nil != authenticateErr || false == authenticated {
        t.Fatalf("expected the current password to sign in, got authenticated=%v err=%v", authenticated, authenticateErr)
    }

    if removed, deleteErr := userRepository.DeleteById(context.Background(), cached.Id, nil); nil != deleteErr || nil == removed {
        t.Fatalf("expected the deletion to land, got removed=%v err=%v", removed, deleteErr)
    }

    if _, authenticated, authenticateErr := userService.AuthenticateByUsernameAndPassword(context.Background(), "editor", "replacement-password"); nil != authenticateErr || true == authenticated {
        t.Fatalf("expected a deleted account to be refused, got authenticated=%v err=%v", authenticated, authenticateErr)
    }
}

type failingUsernameRepository struct {
    repository.UserRepository
}

func (instance *failingUsernameRepository) FindByUsername(ctx context.Context, username string) (*entity.User, bool, error) {
    return nil, false, errors.New("the repository was asked")
}

func TestUserService_AuthenticateAnswersANameTheTableCannotHoldBeforeTheRepository(t *testing.T) {
    userService := NewUserService(&failingUsernameRepository{}, nil, nil)

    user, authenticated, authenticateErr := userService.AuthenticateByUsernameAndPassword(context.Background(), strings.Repeat("a", 256), "anything")
    if nil != authenticateErr || true == authenticated || nil != user {
        t.Fatalf("expected the name to be answered as absent without asking the repository, got authenticated=%v err=%v", authenticated, authenticateErr)
    }
}

func TestUserService_GrantRoleTellsTheListeners(t *testing.T) {
    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil).WithAccountSeed())
    if nil != repositoryErr {
        t.Fatalf("unexpected repository error: %v", repositoryErr)
    }

    clockInstance := melodyclock.NewSystemClock()
    dispatcher := newRecordingDispatcher(clockInstance, event.UserUpdatedEventName)

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
        melodycontainer.RegisterOptions{},
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

func TestUserService_GrantRoleHealsTheCacheWhenTheRoleIsAlreadyHeld(t *testing.T) {
    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil).WithAccountSeed())
    if nil != repositoryErr {
        t.Fatalf("unexpected repository error: %v", repositoryErr)
    }

    clockInstance := melodyclock.NewSystemClock()
    dispatcher := newRecordingDispatcher(clockInstance, event.UserUpdatedEventName)
    refusing := &refusingOnceDispatcher{EventDispatcher: dispatcher.dispatcher}

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
        melodycontainer.RegisterOptions{},
    )
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    cacheInstance := newTtlRecordingCache()
    userService := NewUserService(userRepository, cacheInstance, refusing)

    cached, found, _ := userService.FindByUsername(context.Background(), "user")
    if false == found || 1 != len(cached.Roles) {
        t.Fatalf("expected the seeded account with one role, got %v", cached)
    }

    granted, outcome, grantErr := userService.GrantRole(runtimeInstance, cached.Id, entity.RoleEditor)
    if nil == grantErr || repository.GrantRoleGranted != outcome || nil == granted {
        t.Fatalf("expected the committed grant handed back beside its failed dispatch, got %v, %d, %v", granted, outcome, grantErr)
    }

    if dropped, _, _ := userService.FindByUsername(context.Background(), "user"); 2 != len(dropped.Roles) {
        t.Fatalf("expected the committed grant to drop the account's entries despite the failed dispatch, got %v", dropped.Roles)
    }

    if _, outcome, grantErr := userService.GrantRole(runtimeInstance, cached.Id, entity.RoleEditor); nil != grantErr || repository.GrantRoleAlreadyHeld != outcome {
        t.Fatalf("expected the re-run to find the role held, got %d, %v", outcome, grantErr)
    }

    healed, _, _ := userService.FindByUsername(context.Background(), "user")
    if 2 != len(healed.Roles) || entity.RoleEditor != healed.Roles[1] {
        t.Fatalf("expected the already-held answer to drop the stale entries so the directory is read again, got %v", healed.Roles)
    }
}

func TestUserService_GrantRoleAlreadyHeldDropsTheListEntryToo(t *testing.T) {
    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil).WithAccountSeed())
    if nil != repositoryErr {
        t.Fatalf("unexpected repository error: %v", repositoryErr)
    }

    clockInstance := melodyclock.NewSystemClock()
    dispatcher := newRecordingDispatcher(clockInstance, event.UserUpdatedEventName)

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
        melodycontainer.RegisterOptions{},
    )
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    cacheInstance := newTtlRecordingCache()
    userService := NewUserService(userRepository, cacheInstance, dispatcher.dispatcher)

    cached, found, _ := userService.FindByUsername(context.Background(), "editor")
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

func TestUserService_UpdateStoresACopyOfTheCallersRoleList(t *testing.T) {
    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil).WithAccountSeed())
    if nil != repositoryErr {
        t.Fatalf("unexpected repository error: %v", repositoryErr)
    }

    dispatcher := newRecordingDispatcher(melodyclock.NewSystemClock(), event.UserUpdatedEventName)

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
        melodycontainer.RegisterOptions{},
    )
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    userService := NewUserService(userRepository, newTtlRecordingCache(), dispatcher.dispatcher)

    target, found, findErr := userRepository.FindByUsername(context.Background(), "editor")
    if nil != findErr || false == found {
        t.Fatalf("expected the seeded editor, got found=%v err=%v", found, findErr)
    }

    roles := []string{"ROLE_USER", "ROLE_EDITOR"}
    if _, updated, updateErr := userService.Update(runtimeInstance, target.Id, repository.UserChange{Roles: roles}, nil); nil != updateErr || false == updated {
        t.Fatalf("expected the update to land, got updated=%v err=%v", updated, updateErr)
    }

    roles[1] = "ROLE_ADMIN"

    stored, found, findErr := userRepository.FindById(context.Background(), target.Id)
    if nil != findErr || false == found || "ROLE_EDITOR" != stored.Roles[1] {
        t.Fatalf("the caller's slice reached the stored account: %v", stored.Roles)
    }
}

type paddingUserRepository struct {
    repository.UserRepository
}

func (instance *paddingUserRepository) FindById(ctx context.Context, id string) (*entity.User, bool, error) {
    return instance.UserRepository.FindById(ctx, strings.TrimRight(id, " "))
}

func TestFindByIdAnswersARowFoundForAnotherSpellingAsAbsentAndCachesNothing(t *testing.T) {
    inMemory, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil).WithAccountSeed())
    if nil != repositoryErr {
        t.Fatalf("unexpected repository error: %v", repositoryErr)
    }

    editor, found, findErr := inMemory.FindByUsername(context.Background(), "editor")
    if nil != findErr || false == found {
        t.Fatalf("expected the seeded editor, got found=%v err=%v", found, findErr)
    }

    cacheInstance := newTtlRecordingCache()
    userService := NewUserService(&paddingUserRepository{UserRepository: inMemory}, cacheInstance, nil)

    if _, padded, paddedErr := userService.FindById(context.Background(), editor.Id + " "); nil != paddedErr || true == padded {
        t.Fatalf("expected the padded identifier answered absent, got found=%v err=%v", padded, paddedErr)
    }

    for _, write := range cacheInstance.writesFor(CacheKeyUserById(editor.Id + " ")) {
        if melodybound.Of(entityCacheTtl) == write.ttl {
            t.Fatalf("the account was cached under the padded identifier: %+v", write)
        }
    }

    if cached, exists, _ := melodycache.Get[*entity.UserProfile](context.Background(), cacheInstance, CacheKeyUserById(editor.Id + " ")); true == exists && nil != cached {
        t.Fatalf("the padded identifier holds an account in the cache: %+v", cached)
    }

    if _, canonical, canonicalErr := userService.FindById(context.Background(), editor.Id); nil != canonicalErr || false == canonical {
        t.Fatalf("expected the canonical identifier found, got found=%v err=%v", canonical, canonicalErr)
    }

    if 0 == len(cacheInstance.writesFor(CacheKeyUserById(editor.Id))) {
        t.Fatalf("the canonical identifier was not cached")
    }
}

type slowUserRepository struct {
    repository.UserRepository
}

func (instance *slowUserRepository) All(ctx context.Context) ([]*entity.User, error) {
    return nil, waitForTheCallerContext(ctx)
}

func (instance *slowUserRepository) FindById(ctx context.Context, id string) (*entity.User, bool, error) {
    return nil, false, waitForTheCallerContext(ctx)
}

func (instance *slowUserRepository) FindByUsername(ctx context.Context, username string) (*entity.User, bool, error) {
    return nil, false, waitForTheCallerContext(ctx)
}

func TestUserService_TheReadDoorsEndOnTheCallersContext(t *testing.T) {
    userService := NewUserService(&slowUserRepository{}, newTtlRecordingCache(), nil)

    assertACanceledContextEndsTheRead(t, "List", func(ctx context.Context) error {
        _, listErr := userService.List(ctx)

        return listErr
    })
    assertACanceledContextEndsTheRead(t, "FindById", func(ctx context.Context) error {
        _, _, findErr := userService.FindById(ctx, "user-1")

        return findErr
    })
    assertACanceledContextEndsTheRead(t, "FindByUsername", func(ctx context.Context) error {
        _, _, findErr := userService.FindByUsername(ctx, "admin")

        return findErr
    })
}

func TestUserService_CreateDropsTheKeysWhenAListenerFails(t *testing.T) {
    underTest := newUserServiceUnderTest(t, refusingEveryDispatch, nil)
    keyList := []string{CacheKeyUserById("user-9"), CacheKeyUserByUsername("operator"), CacheKeyUserList}
    underTest.prime(t, keyList...)

    _, createErr := underTest.service.Create(underTest.runtime, "user-9", "operator", "digest", []string{entity.RoleUser})
    if nil == createErr || false == strings.Contains(createErr.Error(), "refused") {
        t.Fatalf("expected the listener's failure answered, got %v", createErr)
    }

    underTest.assertDropped(t, keyList...)
}

func TestUserService_UpdateDropsTheOldAndTheNewNameWhenAListenerFails(t *testing.T) {
    underTest := newUserServiceUnderTest(t, refusingEveryDispatch, nil)
    keyList := []string{CacheKeyUserById("user-1"), CacheKeyUserByUsername("user"), CacheKeyUserByUsername("renamed"), CacheKeyUserList}
    underTest.prime(t, keyList...)

    renamed := "renamed"
    _, updated, updateErr := underTest.service.Update(underTest.runtime, "user-1", repository.UserChange{Username: &renamed}, nil)
    if nil == updateErr || false == updated {
        t.Fatalf("expected the committed rename answered with the listener's failure, got %t, %v", updated, updateErr)
    }

    underTest.assertDropped(t, keyList...)
}

func TestUserService_DeleteDropsTheKeysWhenAListenerFails(t *testing.T) {
    underTest := newUserServiceUnderTest(t, refusingEveryDispatch, nil)
    keyList := []string{CacheKeyUserById("user-1"), CacheKeyUserByUsername("user"), CacheKeyUserList}
    underTest.prime(t, keyList...)

    deleted, deleteErr := underTest.service.DeleteById(underTest.runtime, "user-1", nil)
    if nil == deleteErr || false == deleted {
        t.Fatalf("expected the committed delete answered with the listener's failure, got %t, %v", deleted, deleteErr)
    }

    underTest.assertDropped(t, keyList...)
}

func TestUserService_GrantRoleDropsTheKeysWhenAListenerFails(t *testing.T) {
    underTest := newUserServiceUnderTest(t, refusingEveryDispatch, nil)
    keyList := []string{CacheKeyUserById("user-1"), CacheKeyUserByUsername("user"), CacheKeyUserList}
    underTest.prime(t, keyList...)

    _, outcome, grantErr := underTest.service.GrantRole(underTest.runtime, "user-1", entity.RoleEditor)
    if nil == grantErr || repository.GrantRoleGranted != outcome {
        t.Fatalf("expected the committed grant answered with the listener's failure, got %d, %v", outcome, grantErr)
    }

    underTest.assertDropped(t, keyList...)
}

func TestUserService_DropAttemptsEveryKeyWhenOneDeleteFails(t *testing.T) {
    failing := &deleteFailingCache{failingKeyList: []string{CacheKeyUserById("user-1")}}
    underTest := newUserServiceUnderTest(t, nil, func(cacheInstance melodycachecontract.Cache) melodycachecontract.Cache {
        failing.Cache = cacheInstance

        return failing
    })

    dropErr := underTest.service.dropCachedAccount(context.Background(), "user-1", "user", "renamed")
    if nil == dropErr {
        t.Fatal("expected the failed delete answered")
    }

    expected := []string{CacheKeyUserById("user-1"), CacheKeyUserList, CacheKeyUserByUsername("user"), CacheKeyUserByUsername("renamed")}
    if attempted := failing.attempted(); false == slices.Equal(expected, attempted) {
        t.Fatalf("expected every key attempted %v, got %v", expected, attempted)
    }
}

func TestUserService_CreateAnswersTheDropFailureWhenTheCacheIsDown(t *testing.T) {
    underTest := newUserServiceUnderTest(t, nil, func(cacheInstance melodycachecontract.Cache) melodycachecontract.Cache {
        return &deleteFailingCache{Cache: cacheInstance}
    })

    _, createErr := underTest.service.Create(underTest.runtime, "user-9", "operator", "digest", []string{entity.RoleUser})
    if nil == createErr || false == strings.Contains(createErr.Error(), "cache down") {
        t.Fatalf("expected the failed drop answered, got %v", createErr)
    }

    if _, stored, _ := underTest.service.FindByUsernameInDirectory(context.Background(), "operator"); false == stored {
        t.Fatal("expected the committed row stored")
    }
}

func TestUserService_CachedEntriesCarryNoPasswordDigest(t *testing.T) {
    underTest := newUserServiceUnderTest(t, nil, nil)

    stored, found, findErr := underTest.userRepository.FindByUsername(context.Background(), "admin")
    if nil != findErr || false == found || "" == stored.Password {
        t.Fatalf("expected the seeded admin with a digest, got %v, %v", stored, findErr)
    }

    if _, listErr := underTest.service.List(context.Background()); nil != listErr {
        t.Fatalf("list: %v", listErr)
    }

    profile, found, findErr := underTest.service.FindById(context.Background(), stored.Id)
    if nil != findErr || false == found || "admin" != profile.Username {
        t.Fatalf("expected the admin by id, got %v, %v", profile, findErr)
    }

    if _, found, findErr := underTest.service.FindByUsername(context.Background(), "admin"); nil != findErr || false == found {
        t.Fatalf("expected the admin by name, got %v", findErr)
    }

    payloadList := underTest.serializer.payloads()
    if 3 > len(payloadList) {
        t.Fatalf("expected the three doors to write their entries, got %d payloads", len(payloadList))
    }

    for _, payload := range payloadList {
        if true == strings.Contains(string(payload), stored.Password) {
            t.Fatalf("expected no cached payload to carry the password digest")
        }
    }
}
