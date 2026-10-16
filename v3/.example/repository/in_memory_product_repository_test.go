package repository

import (
    "context"
    "errors"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
)

/* every repository is a process-wide singleton and net/http serves each request on its own goroutine, so a listing request and a deleting request overlap; the writer here always removes a non-terminal element, which is what makes DeleteById compact the backing array under the reader */

func TestInMemoryProductRepositoryConcurrentReadAndDelete(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := newInMemoryProductRepository()

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

/* All() must hand back a copy: mutating the returned slice may not reach the repository */

func TestInMemoryProductRepositoryAllReturnsCopy(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := newInMemoryProductRepository()

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

/* the two implementations share one validation, so a write the in-memory catalogue refuses is refused by the database-backed one with the same words */

func TestInMemoryProductRepositoryCreateRefusesAnIncompleteProduct(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := newInMemoryProductRepository()

    now := time.Now()
    createErr := repositoryInstance.Create(ctx, entity.NewProduct("prod-blank", "", "black", "cat-1", 1, "cur-eur", 1, now, now))
    if nil == createErr {
        t.Fatalf("expected a product with no name to be refused")
    }

    if "name is required" != createErr.Error() {
        t.Fatalf("expected the shared validation message, got %q", createErr.Error())
    }
}

func TestInMemoryProductRepositoryCreateAnswersATakenIdentifierWithTheSentinel(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := newInMemoryProductRepository()

    now := time.Now()
    if createErr := repositoryInstance.Create(ctx, entity.NewProduct("prod-taken", "Probe", "black", "cat-1", 1, "cur-eur", 1, now, now)); nil != createErr {
        t.Fatalf("unexpected create error: %v", createErr)
    }

    createErr := repositoryInstance.Create(ctx, entity.NewProduct("prod-taken", "Second", "black", "cat-1", 1, "cur-eur", 1, now, now))
    if false == errors.Is(createErr, ErrIdAlreadyExists) {
        t.Fatalf("expected the taken identifier's refusal, got %v", createErr)
    }
}

/* deleting the newest SEEDED product hands its identifier to nobody: the repository starts from the seed's floor */
func TestInMemoryProductRepositoryCreate_NeverMintsADeletedSeededIdentifier(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := newInMemoryProductRepository()

    if _, deleteErr := repositoryInstance.DeleteById(ctx, "prod-5"); nil != deleteErr {
        t.Fatalf("delete: %v", deleteErr)
    }

    product := entity.NewProduct("", "probe", "probe", "cat-1", 1, "cur-eur", 1, time.Now(), time.Now())
    if createErr := repositoryInstance.Create(ctx, product); nil != createErr {
        t.Fatalf("create: %v", createErr)
    }

    if "prod-6" != product.Id {
        t.Fatalf("expected prod-6 after the seeded prod-5 was deleted, got %q", product.Id)
    }
}

/* a stored identifier at the ceiling would raise the floor to it, and every later mint would collide with it */
func TestInMemoryProductRepositoryCreate_RefusesASuppliedIdentifierAtTheCeiling(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := newInMemoryProductRepository()

    now := time.Now()
    createErr := repositoryInstance.Create(ctx, entity.NewProduct("prod-9223372036854775806", "Probe", "black", "cat-1", 1, "cur-eur", 1, now, now))
    if false == errors.Is(createErr, ErrIdentifierAtCeiling) {
        t.Fatalf("expected the ceiling refused, got %v", createErr)
    }

    minted := entity.NewProduct("", "Probe", "black", "cat-1", 1, "cur-eur", 1, now, now)
    if mintErr := repositoryInstance.Create(ctx, minted); nil != mintErr {
        t.Fatalf("expected the next mint to land, got %v", mintErr)
    }

    if "prod-9223372036854775806" == minted.Id || "prod-9223372036854775807" == minted.Id {
        t.Fatalf("expected the refused identifier not to have raised the floor, minted %q", minted.Id)
    }
}
