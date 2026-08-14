package repository

import (
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
