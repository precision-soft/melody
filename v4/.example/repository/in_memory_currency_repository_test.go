package repository

import (
    "context"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/.example/entity"
)

/* one fixed instant for every currency: the probes are about concurrent access, not quotes */
var currencyProbeQuoteInstant = time.Date(2026, time.September, 7, 9, 0, 0, 0, time.UTC)

/* net/http overlaps a listing and a delete; a non-terminal element makes DeleteById compact the array under the reader */

func TestInMemoryCurrencyRepositoryConcurrentReadAndDelete(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := newInMemoryCurrencyRepository(true)

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

func TestInMemoryCurrencyRepositoryUpdateQuoteWritesOnlyOverARowThatIsNotNewer(t *testing.T) {
    repository := newInMemoryCurrencyRepository(true)
    ctx := context.Background()

    newer := time.Date(2026, time.September, 8, 11, 0, 0, 0, time.UTC)
    older := newer.Add(-time.Hour)

    if written, err := repository.UpdateQuote(ctx, "cur-usd", entity.NewRateQuote(1.2, newer, newer)); nil != err || false == written {
        t.Fatalf("a newer quote was not written: written=%v err=%v", written, err)
    }

    if written, err := repository.UpdateQuote(ctx, "cur-usd", entity.NewRateQuote(1.1, older, older)); nil != err || true == written {
        t.Fatalf("an older quote landed over a newer row: written=%v err=%v", written, err)
    }

    if written, err := repository.UpdateQuote(ctx, "cur-usd", entity.NewRateQuote(1.2, newer, newer)); nil != err || true == written {
        t.Fatalf("the same quote was written a second time: written=%v err=%v", written, err)
    }

    stored, found, _ := repository.FindById(ctx, "cur-usd")
    if false == found || 1.2 != stored.Rate || false == stored.RateAsOf.Equal(newer) {
        t.Fatalf("the row holds %v, wanted 1.2 at %v", stored, newer)
    }

    if written, err := repository.UpdateQuote(ctx, "cur-none", entity.NewRateQuote(1, newer, newer)); nil != err || true == written {
        t.Fatalf("a quote for an absent row was written: written=%v err=%v", written, err)
    }
}

func TestInMemoryCurrencyRepositoryUpdateQuoteJudgesTheReadingOnThisClockAndByItsStamp(t *testing.T) {
    repository := newInMemoryCurrencyRepository(true)
    ctx := context.Background()

    stamped := time.Date(2026, time.September, 8, 9, 4, 0, 0, time.UTC)
    onThisClock := stamped.Add(-4 * time.Minute)

    if written, err := repository.UpdateQuote(ctx, "cur-usd", entity.NewRateQuote(1.2, onThisClock, stamped)); nil != err || false == written {
        t.Fatalf("the reading was not written: written=%v err=%v", written, err)
    }

    if written, err := repository.UpdateQuote(ctx, "cur-usd", entity.NewRateQuote(1.2, onThisClock.Add(-700*time.Millisecond), stamped)); nil != err || true == written {
        t.Fatalf("the same reading measured again was written: written=%v err=%v", written, err)
    }

    if written, err := repository.UpdateQuote(ctx, "cur-usd", entity.NewRateQuote(1.25, onThisClock.Add(-700*time.Millisecond), stamped)); nil != err || false == written {
        t.Fatalf("the provider's re-quote of its reading was not written: written=%v err=%v", written, err)
    }

    if written, err := repository.UpdateQuote(ctx, "cur-usd", entity.NewRateQuote(1.3, onThisClock.Add(-time.Minute), stamped.Add(time.Hour))); nil != err || true == written {
        t.Fatalf("a reading older on this clock was written because its stamp was later: written=%v err=%v", written, err)
    }

    stored, _, _ := repository.FindById(ctx, "cur-usd")
    if 1.25 != stored.Rate || false == stored.ProviderRateAsOf.Equal(stamped) {
        t.Fatalf("the row holds %v, wanted the re-quote 1.25 of the reading stamped %s", stored, stamped)
    }
}

func TestInMemoryCurrencyRepositoryUpdateKeepsAQuoteWrittenAfterTheRenameRead(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := newInMemoryCurrencyRepository(true)
    readAt := time.Date(2026, time.September, 8, 9, 0, 0, 0, time.UTC)

    if createErr := repositoryInstance.Create(ctx, entity.NewCurrency("cur-x", "XXX", "Old", 1.1, readAt)); nil != createErr {
        t.Fatalf("create: %v", createErr)
    }

    read, _, _ := repositoryInstance.FindById(ctx, "cur-x")
    renamed := *read
    renamed.Name = "New"

    later := readAt.Add(time.Hour)
    if written, quoteErr := repositoryInstance.UpdateQuote(ctx, "cur-x", entity.NewRateQuote(1.2, later, later)); nil != quoteErr || false == written {
        t.Fatalf("the refresh's write answered %v, %v", written, quoteErr)
    }

    if updated, updateErr := repositoryInstance.Update(ctx, &renamed); nil != updateErr || false == updated {
        t.Fatalf("the rename answered %v, %v", updated, updateErr)
    }

    final, _, _ := repositoryInstance.FindById(ctx, "cur-x")
    if "New" != final.Name || 1.2 != final.Rate || false == final.RateAsOf.Equal(later) || false == final.ProviderRateAsOf.Equal(later) {
        t.Fatalf("expected the new name over the refresh's quote, got %s at %v, %s", final.Name, final.Rate, final.RateAsOf)
    }
}

func TestInMemoryCurrencyRepositoryNeverMintsADeletedIdentifierAgain(t *testing.T) {
    ctx := context.Background()
    repositoryInstance := newInMemoryCurrencyRepository(true)

    first := entity.NewCurrency("", "CHF", "Swiss Franc", 0.94, currencyProbeQuoteInstant)
    if createErr := repositoryInstance.Create(ctx, first); nil != createErr {
        t.Fatalf("create: %v", createErr)
    }

    if _, deleteErr := repositoryInstance.DeleteById(ctx, first.Id); nil != deleteErr {
        t.Fatalf("delete: %v", deleteErr)
    }

    second := entity.NewCurrency("", "JPY", "Yen", 160, currencyProbeQuoteInstant)
    if createErr := repositoryInstance.Create(ctx, second); nil != createErr {
        t.Fatalf("create: %v", createErr)
    }

    if first.Id == second.Id {
        t.Fatalf("the deleted identifier %q was minted again", first.Id)
    }
}
