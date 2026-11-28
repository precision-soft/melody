package repository

import (
    "context"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v4/.example/entity"
)

/* net/http overlaps a listing and a delete; a non-terminal element makes DeleteById compact the array under the reader */

func TestInMemoryUserRepositoryConcurrentReadAndDelete(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := newInMemoryUserRepository(true)

    waitGroup := sync.WaitGroup{}
    waitGroup.Add(2)

    go func() {
        defer waitGroup.Done()

        for round := 0; round < concurrentRounds; round++ {
            users, allErr := repositoryInstance.All(ctx)
            if nil != allErr {
                t.Errorf("all: %v", allErr)
                return
            }

            for _, user := range users {
                if nil == user {
                    continue
                }

                _ = user.Id
            }
        }
    }()

    go func() {
        defer waitGroup.Done()

        for round := 0; round < concurrentRounds; round++ {
            createErr := repositoryInstance.Create(ctx, entity.NewUser("user-first", "first", "hash", []string{entity.RoleUser}))
            if nil != createErr {
                t.Errorf("create first: %v", createErr)
                return
            }

            createErr = repositoryInstance.Create(ctx, entity.NewUser("user-second", "second", "hash", []string{entity.RoleUser}))
            if nil != createErr {
                t.Errorf("create second: %v", createErr)
                return
            }

            _, deleteErr := repositoryInstance.DeleteById(ctx, "user-first", nil)
            if nil != deleteErr {
                t.Errorf("delete first: %v", deleteErr)
                return
            }

            _, deleteErr = repositoryInstance.DeleteById(ctx, "user-second", nil)
            if nil != deleteErr {
                t.Errorf("delete second: %v", deleteErr)
                return
            }
        }
    }()

    waitGroup.Wait()

    users, allErr := repositoryInstance.All(ctx)
    if nil != allErr {
        t.Fatalf("all: %v", allErr)
    }

    if 3 != len(users) {
        t.Fatalf("expected the seeded users to survive, got %d", len(users))
    }
}

func TestInMemoryUserRepositoryFindByUsernameIgnoresCase(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := newInMemoryUserRepository(true)

    user, found, findErr := repositoryInstance.FindByUsername(ctx, "  ADMIN ")
    if nil != findErr {
        t.Fatalf("find by username: %v", findErr)
    }

    if false == found {
        t.Fatalf("expected the seeded admin to be found whatever the case")
    }

    if "admin" != user.Username {
        t.Fatalf("expected the seeded admin, got %q", user.Username)
    }
}

func TestInMemoryUserRepositoryRefusesAnIdentifierThatIsAlreadyTaken(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := newInMemoryUserRepository(true)

    first := entity.NewUser("user-90", "zz-first", "digest", []string{entity.RoleUser})
    if createErr := repositoryInstance.Create(ctx, first); nil != createErr {
        t.Fatalf("expected a free identifier to be accepted, got %v", createErr)
    }

    createErr := repositoryInstance.Create(ctx, entity.NewUser("user-90", "zz-second", "digest", []string{entity.RoleUser}))
    if nil == createErr {
        t.Fatalf("expected the occupied identifier to be refused")
    }

    if "id already exists" != createErr.Error() {
        t.Fatalf("expected the refusal the sibling repositories answer, got %q", createErr.Error())
    }

    all, allErr := repositoryInstance.All(ctx)
    if nil != allErr {
        t.Fatalf("all: %v", allErr)
    }

    carrying := 0
    for _, user := range all {
        if "user-90" == user.Id {
            carrying++
        }
    }

    if 1 != carrying {
        t.Fatalf("expected one row to carry the identifier, found %d", carrying)
    }

    found, exists, findErr := repositoryInstance.FindById(ctx, "user-90")
    if nil != findErr {
        t.Fatalf("find by id: %v", findErr)
    }

    if false == exists || "zz-first" != found.Username {
        t.Fatalf("expected the identifier to still name the account that took it, got exists=%t user=%v", exists, found)
    }
}

func TestInMemoryUserRepositoryGrantRoleAppendsOnceOntoACopy(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := newInMemoryUserRepository(true)

    before, _, _ := repositoryInstance.FindByUsername(ctx, "user")

    granted, outcome, grantErr := repositoryInstance.GrantRole(ctx, before.Id, entity.RoleEditor)
    if nil != grantErr || GrantRoleGranted != outcome {
        t.Fatalf("the grant answered %d, %v; wanted GrantRoleGranted", outcome, grantErr)
    }

    if 1 != len(before.Roles) {
        t.Fatalf("the value read before the grant now holds %v: the roles were appended in place", before.Roles)
    }

    if nil == granted || 2 != len(granted.Roles) || entity.RoleEditor != granted.Roles[1] {
        t.Fatalf("the grant handed back %v, wanted the widened account", granted)
    }
    granted.Roles[0] = "mutated"
    if stored, _, _ := repositoryInstance.FindById(ctx, before.Id); "mutated" == stored.Roles[0] {
        t.Fatal("the account handed back is the stored value")
    }

    _, outcome, grantErr = repositoryInstance.GrantRole(ctx, before.Id, entity.RoleEditor)
    if nil != grantErr || GrantRoleAlreadyHeld != outcome {
        t.Fatalf("a second grant answered %d, %v; wanted GrantRoleAlreadyHeld", outcome, grantErr)
    }

    after, _, _ := repositoryInstance.FindById(ctx, before.Id)
    if 2 != len(after.Roles) || entity.RoleEditor != after.Roles[1] {
        t.Fatalf("two grants of one role stored %v, wanted it once", after.Roles)
    }

    _, outcome, grantErr = repositoryInstance.GrantRole(ctx, "user-none", entity.RoleEditor)
    if nil != grantErr || GrantRoleAccountAbsent != outcome {
        t.Fatalf("a grant on a missing account answered %d, %v; wanted GrantRoleAccountAbsent", outcome, grantErr)
    }
}

func TestInMemoryUserRepositoryCreate_NeverMintsADeletedSeededIdentifier(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := newInMemoryUserRepository(true)

    if _, deleteErr := repositoryInstance.DeleteById(ctx, "user-3", nil); nil != deleteErr {
        t.Fatalf("delete: %v", deleteErr)
    }

    user := entity.NewUser("", "probe", "hash", []string{entity.RoleUser})
    if createErr := repositoryInstance.Create(ctx, user); nil != createErr {
        t.Fatalf("create: %v", createErr)
    }

    if "user-4" != user.Id {
        t.Fatalf("expected user-4 after the seeded user-3 was deleted, got %q", user.Id)
    }
}
