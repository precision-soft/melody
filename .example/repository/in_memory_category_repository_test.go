package repository

import (
    "context"
    "sync"
    "testing"

    "github.com/precision-soft/melody/.example/entity"
)

/* Every repository is a process-wide singleton and net/http serves each request on its own goroutine, so a listing request and a deleting request really do overlap. The writer removes a NON-terminal element deliberately: that is the removal which makes DeleteById compact the backing array underneath a reader already walking it, and a terminal one would merely shorten the slice. */
func TestInMemoryCategoryRepositoryConcurrentReadAndDelete(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := NewInMemoryCategoryRepository()

    waitGroup := sync.WaitGroup{}
    waitGroup.Add(2)

    go func() {
        defer waitGroup.Done()

        for round := 0; round < concurrentRounds; round++ {
            categories, allErr := repositoryInstance.All(ctx)
            if nil != allErr {
                t.Errorf("all: %v", allErr)
                return
            }

            for _, category := range categories {
                if nil == category {
                    continue
                }

                _ = category.Id
            }
        }
    }()

    go func() {
        defer waitGroup.Done()

        for round := 0; round < concurrentRounds; round++ {
            createErr := repositoryInstance.Create(ctx, entity.NewCategory("cat-first", "first"))
            if nil != createErr {
                t.Errorf("create first: %v", createErr)
                return
            }

            createErr = repositoryInstance.Create(ctx, entity.NewCategory("cat-second", "second"))
            if nil != createErr {
                t.Errorf("create second: %v", createErr)
                return
            }

            _, deleteErr := repositoryInstance.DeleteById(ctx, "cat-first")
            if nil != deleteErr {
                t.Errorf("delete first: %v", deleteErr)
                return
            }

            _, deleteErr = repositoryInstance.DeleteById(ctx, "cat-second")
            if nil != deleteErr {
                t.Errorf("delete second: %v", deleteErr)
                return
            }
        }
    }()

    waitGroup.Wait()

    categories, allErr := repositoryInstance.All(ctx)
    if nil != allErr {
        t.Fatalf("all: %v", allErr)
    }

    if 4 != len(categories) {
        t.Fatalf("expected the seeded categories to survive, got %d", len(categories))
    }
}
