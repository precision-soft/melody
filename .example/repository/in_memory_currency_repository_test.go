package repository

import (
    "context"
    "sync"
    "testing"

    "github.com/precision-soft/melody/.example/entity"
)

/* Every repository is a process-wide singleton and net/http serves each request on its own goroutine, so a listing request and a deleting request really do overlap. The writer removes a NON-terminal element deliberately: that is the removal which makes DeleteById compact the backing array underneath a reader already walking it, and a terminal one would merely shorten the slice. */
func TestInMemoryCurrencyRepositoryConcurrentReadAndDelete(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := NewInMemoryCurrencyRepository()

    waitGroup := sync.WaitGroup{}
    waitGroup.Add(2)

    go func() {
        defer waitGroup.Done()

        for round := 0; round < concurrentRounds; round++ {
            currencies, allErr := repositoryInstance.All(ctx)
            if nil != allErr {
                t.Errorf("all: %v", allErr)
                return
            }

            for _, currency := range currencies {
                if nil == currency {
                    continue
                }

                _ = currency.Id
            }
        }
    }()

    go func() {
        defer waitGroup.Done()

        for round := 0; round < concurrentRounds; round++ {
            createErr := repositoryInstance.Create(ctx, entity.NewCurrency("cur-first", "AAA", "first"))
            if nil != createErr {
                t.Errorf("create first: %v", createErr)
                return
            }

            createErr = repositoryInstance.Create(ctx, entity.NewCurrency("cur-second", "BBB", "second"))
            if nil != createErr {
                t.Errorf("create second: %v", createErr)
                return
            }

            _, deleteErr := repositoryInstance.DeleteById(ctx, "cur-first")
            if nil != deleteErr {
                t.Errorf("delete first: %v", deleteErr)
                return
            }

            _, deleteErr = repositoryInstance.DeleteById(ctx, "cur-second")
            if nil != deleteErr {
                t.Errorf("delete second: %v", deleteErr)
                return
            }
        }
    }()

    waitGroup.Wait()

    currencies, allErr := repositoryInstance.All(ctx)
    if nil != allErr {
        t.Fatalf("all: %v", allErr)
    }

    if 3 != len(currencies) {
        t.Fatalf("expected the seeded currencies to survive, got %d", len(currencies))
    }
}
