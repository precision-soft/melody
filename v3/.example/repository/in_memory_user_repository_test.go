package repository

import (
    "context"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
)

/* every repository is a process-wide singleton and net/http serves each request on its own goroutine, so a listing request and a deleting request overlap; the writer here always removes a non-terminal element, which is what makes DeleteById compact the backing array under the reader */

func TestInMemoryUserRepositoryConcurrentReadAndDelete(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := newInMemoryUserRepository()

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

            _, deleteErr := repositoryInstance.DeleteById(ctx, "user-first")
            if nil != deleteErr {
                t.Errorf("delete first: %v", deleteErr)
                return
            }

            _, deleteErr = repositoryInstance.DeleteById(ctx, "user-second")
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

/* usernames are matched without regard to case in both implementations, so the comparison is normalised in the example rather than left to a database collation */

func TestInMemoryUserRepositoryFindByUsernameIgnoresCase(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := newInMemoryUserRepository()

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

/* the three sibling repositories — products, categories and currencies — refuse an identifier that is
   already taken, in both of their implementations; users refused it in neither. Measured, an occupied id
   was appended as a SECOND row: FindById answered the first, DeleteById removed the first, and the account
   behind the second could be reached by no door that goes through the id. On the bun implementation the
   same create surfaced the driver's raw duplicate-key text through a 500 instead of this message — which
   is also what the identifier ceiling's own written rationale promises the caller is told. */
func TestInMemoryUserRepositoryRefusesAnIdentifierThatIsAlreadyTaken(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := newInMemoryUserRepository()

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
