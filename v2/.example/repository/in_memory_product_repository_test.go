package repository

import (
    "context"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v2/.example/entity"
)

/* concurrentRounds is shared by the four in-memory suites in this package. */
const concurrentRounds = 500

/* Every repository is a process-wide singleton and net/http serves each request on its own goroutine, so a listing request and a deleting request really do overlap. The writer removes a NON-terminal element deliberately: that is the removal which makes DeleteById compact the backing array underneath a reader already walking it, and a terminal one would merely shorten the slice. */
func TestInMemoryProductRepositoryConcurrentReadAndDelete(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := NewInMemoryProductRepository()

    waitGroup := sync.WaitGroup{}
    waitGroup.Add(2)

    go func() {
        defer waitGroup.Done()

        for round := 0; round < concurrentRounds; round++ {
            products, allErr := repositoryInstance.All(ctx)
            if nil != allErr {
                t.Errorf("all: %v", allErr)
                return
            }

            for _, product := range products {
                if nil == product {
                    continue
                }

                _ = product.Id
            }
        }
    }()

    go func() {
        defer waitGroup.Done()

        now := time.Now()

        for round := 0; round < concurrentRounds; round++ {
            first := entity.NewProduct("prod-first", "first", "black", "cat-1", 1, "cur-eur", 1, now, now)
            second := entity.NewProduct("prod-second", "second", "black", "cat-1", 1, "cur-eur", 1, now, now)

            createErr := repositoryInstance.Create(ctx, first)
            if nil != createErr {
                t.Errorf("create first: %v", createErr)
                return
            }

            createErr = repositoryInstance.Create(ctx, second)
            if nil != createErr {
                t.Errorf("create second: %v", createErr)
                return
            }

            _, deleteErr := repositoryInstance.DeleteById(ctx, "prod-first")
            if nil != deleteErr {
                t.Errorf("delete first: %v", deleteErr)
                return
            }

            _, deleteErr = repositoryInstance.DeleteById(ctx, "prod-second")
            if nil != deleteErr {
                t.Errorf("delete second: %v", deleteErr)
                return
            }
        }
    }()

    waitGroup.Wait()

    products, allErr := repositoryInstance.All(ctx)
    if nil != allErr {
        t.Fatalf("all: %v", allErr)
    }

    if 5 != len(products) {
        t.Fatalf("expected the seeded products to survive, got %d", len(products))
    }
}

func TestInMemoryProductRepositoryAllReturnsCopy(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := NewInMemoryProductRepository()

    products, allErr := repositoryInstance.All(ctx)
    if nil != allErr {
        t.Fatalf("all: %v", allErr)
    }

    if 0 == len(products) {
        t.Fatalf("expected seeded products")
    }

    products[0] = nil

    reread, rereadErr := repositoryInstance.All(ctx)
    if nil != rereadErr {
        t.Fatalf("all: %v", rereadErr)
    }

    if nil == reread[0] {
        t.Fatalf("expected the repository slice to be unaffected by a caller write")
    }
}

/* The two implementations share one validation, so a write the in-memory catalogue refuses is refused by the database-backed one with the same words. */
func TestInMemoryProductRepositoryCreateRefusesAnIncompleteProduct(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := NewInMemoryProductRepository()

    now := time.Now()
    createErr := repositoryInstance.Create(ctx, entity.NewProduct("prod-blank", "", "black", "cat-1", 1, "cur-eur", 1, now, now))
    if nil == createErr {
        t.Fatalf("expected a product with no name to be refused")
    }

    if "name is required" != createErr.Error() {
        t.Fatalf("expected the shared validation message, got %q", createErr.Error())
    }
}
