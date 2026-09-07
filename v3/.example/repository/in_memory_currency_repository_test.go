package repository

import (
    "context"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
)

/* the probes below are about concurrent access, not about quotes, so every currency they create carries the
   same fixed instant: a quote that moved between two of them would be a second variable in a test that has
   one subject. */
var currencyProbeQuoteInstant = time.Date(2026, time.September, 7, 9, 0, 0, 0, time.UTC)

/* every repository is a process-wide singleton and net/http serves each request on its own goroutine, so a listing request and a deleting request overlap; the writer here always removes a non-terminal element, which is what makes DeleteById compact the backing array under the reader */

func TestInMemoryCurrencyRepositoryConcurrentReadAndDelete(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := newInMemoryCurrencyRepository()

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
            createErr := repositoryInstance.Create(ctx, entity.NewCurrency("cur-first", "AAA", "first", 1, currencyProbeQuoteInstant))
            if nil != createErr {
                t.Errorf("create first: %v", createErr)
                return
            }

            createErr = repositoryInstance.Create(ctx, entity.NewCurrency("cur-second", "BBB", "second", 2, currencyProbeQuoteInstant))
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
