package repository

import (
    "errors"
    "context"
    "sync"
    "testing"

    "github.com/precision-soft/melody/.example/entity"
)

/* Every repository is a process-wide singleton and net/http serves each request on its own goroutine, so a listing request and a deleting request really do overlap. The writer removes a NON-terminal element deliberately: that is the removal which makes DeleteById compact the backing array underneath a reader already walking it, and a terminal one would merely shorten the slice. */
func TestInMemoryUserRepositoryConcurrentReadAndDelete(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := NewInMemoryUserRepository()

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
    repositoryInstance := NewInMemoryUserRepository()

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

/* an identifier that is already taken is refused, as the three sibling repositories refuse it in both implementations: appended as a SECOND row it would be unreachable, since FindById and DeleteById reach the first, and on the bun implementation the primary key would answer the driver's raw duplicate-key text through a 500 instead of this message. */
func TestInMemoryUserRepositoryRefusesAnIdentifierThatIsAlreadyTaken(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := NewInMemoryUserRepository()

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

    /* the assertion that separates a refusal from a message: nothing was stored under the identifier a
       second time, so the reading and the deleting doors still reach exactly one account. */
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

/* the update writes only what the change names over the account the repository holds, and a guard's refusal writes nothing */
func TestInMemoryUserRepositoryUpdateWritesOnlyTheChangeAndHonoursTheGuard(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := NewInMemoryUserRepository()

    if createErr := repositoryInstance.Create(ctx, entity.NewUser("user-7", "alice", "H2", []string{entity.RoleUser, entity.RoleEditor})); nil != createErr {
        t.Fatalf("create: %v", createErr)
    }

    renamed := "alice-renamed"
    before, after, updateErr := repositoryInstance.Update(ctx, "user-7", UserChange{Username: &renamed}, nil)
    if nil != updateErr || nil == before || "alice" != before.Username {
        t.Fatalf("the update answered before=%+v err=%v", before, updateErr)
    }

    if "H2" != after.Password || 2 != len(after.Roles) || renamed != after.Username {
        t.Fatalf("the update wrote %+v; wanted the stored password and roles under the new name", after)
    }

    refusal := errors.New("refused by the guard")
    other := "taken-over"
    if _, _, guardedErr := repositoryInstance.Update(ctx, "user-7", UserChange{Username: &other}, func(current *entity.User) error { return refusal }); false == errors.Is(guardedErr, refusal) {
        t.Fatalf("the guarded update answered %v", guardedErr)
    }

    if _, deleteErr := repositoryInstance.DeleteById(ctx, "user-7", func(current *entity.User) error { return refusal }); false == errors.Is(deleteErr, refusal) {
        t.Fatalf("the guarded delete answered %v", deleteErr)
    }

    stored, found, _ := repositoryInstance.FindById(ctx, "user-7")
    if false == found || renamed != stored.Username {
        t.Fatalf("a refused write changed the account: found=%v %+v", found, stored)
    }

    if absent, _, _ := repositoryInstance.Update(ctx, "user-absent", UserChange{Username: &other}, nil); nil != absent {
        t.Fatalf("an absent account answered %+v", absent)
    }
}
