package repository

import (
    "context"
    "testing"
    "time"
)

func TestInMemoryCatalogReadingRepositoryRefusesASecondReadingAtTheSameInstant(t *testing.T) {
    repositoryInstance := newInMemoryCatalogReadingRepository()
    takenAt := time.Date(2026, time.September, 7, 10, 0, 0, 0, time.UTC)

    first := &CatalogReadingRecord{TakenAt: takenAt, Headline: "catalog", Payload: "products=1", ProductCount: 1, JournalCount: 0}
    if appendErr := repositoryInstance.Append(context.Background(), first); nil != appendErr {
        t.Fatalf("expected the first reading to be recorded, got %v", appendErr)
    }

    /* the second carries a DIFFERENT payload on the same instant, so a repository that answered by
       overwriting rather than refusing would be caught by the count as well as by the error */
    second := &CatalogReadingRecord{TakenAt: takenAt, Headline: "catalog", Payload: "products=99", ProductCount: 99, JournalCount: 0}
    appendErr := repositoryInstance.Append(context.Background(), second)
    if nil == appendErr {
        t.Fatal("expected a second reading at the same instant to be refused")
    }

    if "reading already recorded" != appendErr.Error() {
        t.Fatalf("expected the refusal the postgres sister answers, got %q", appendErr.Error())
    }

    count, countErr := repositoryInstance.Count(context.Background())
    if nil != countErr {
        t.Fatalf("count: %v", countErr)
    }

    if 1 != count {
        t.Fatalf("expected the refused reading to have left the archive at one row, got %d", count)
    }
}

/* one second apart is the smallest distance the archive can tell apart, because the service truncates the instant to the second before it gets here — so this is the pair that proves the identity is the instant and not the reading. */
func TestInMemoryCatalogReadingRepositoryKeepsReadingsOneSecondApart(t *testing.T) {
    repositoryInstance := newInMemoryCatalogReadingRepository()
    takenAt := time.Date(2026, time.September, 7, 10, 0, 0, 0, time.UTC)

    for index := 0; 3 > index; index++ {
        reading := &CatalogReadingRecord{
            TakenAt:  takenAt.Add(time.Duration(index) * time.Second),
            Headline: "catalog",
            Payload:  "products=1",
        }

        if appendErr := repositoryInstance.Append(context.Background(), reading); nil != appendErr {
            t.Fatalf("expected reading %d to be recorded, got %v", index, appendErr)
        }
    }

    count, _ := repositoryInstance.Count(context.Background())
    if 3 != count {
        t.Fatalf("expected three readings, got %d", count)
    }
}

func TestInMemoryCatalogReadingRepositoryListsNewestFirstAndHonoursTheLimit(t *testing.T) {
    repositoryInstance := newInMemoryCatalogReadingRepository()
    takenAt := time.Date(2026, time.September, 7, 10, 0, 0, 0, time.UTC)

    /* appended OLDEST first, so a repository that simply handed its slice back in insertion order would fail the first assertion */
    for index := 0; 3 > index; index++ {
        _ = repositoryInstance.Append(context.Background(), &CatalogReadingRecord{
            TakenAt:  takenAt.Add(time.Duration(index) * time.Second),
            Headline: "catalog",
            Payload:  "products=1",
        })
    }

    readingList, readErr := repositoryInstance.Recent(context.Background(), 2)
    if nil != readErr {
        t.Fatalf("recent: %v", readErr)
    }

    if 2 != len(readingList) {
        t.Fatalf("expected the limit to be honoured, got %d readings", len(readingList))
    }

    if false == readingList[0].TakenAt.Equal(takenAt.Add(2*time.Second)) {
        t.Fatalf("expected the newest reading first, got %s", readingList[0].TakenAt)
    }

    if false == readingList[1].TakenAt.After(readingList[0].TakenAt.Add(-2*time.Second)) {
        t.Fatalf("expected the second reading to be the next newest, got %s", readingList[1].TakenAt)
    }
}

/* a non-positive limit answers nothing rather than everything, which is the half that matters: the archive grows for the life of a volume, so a caller that asked for nothing must not receive all of it. */
func TestInMemoryCatalogReadingRepositoryAnswersNothingForANonPositiveLimit(t *testing.T) {
    repositoryInstance := newInMemoryCatalogReadingRepository()
    _ = repositoryInstance.Append(context.Background(), &CatalogReadingRecord{
        TakenAt:  time.Date(2026, time.September, 7, 10, 0, 0, 0, time.UTC),
        Headline: "catalog",
        Payload:  "products=1",
    })

    for _, limit := range []int{0, -1} {
        readingList, readErr := repositoryInstance.Recent(context.Background(), limit)
        if nil != readErr {
            t.Fatalf("recent(%d): %v", limit, readErr)
        }

        if 0 != len(readingList) {
            t.Fatalf("expected limit %d to answer no readings, got %d", limit, len(readingList))
        }
    }
}

/* the archive hands back COPIES: a caller that mutates what it read must not be rewriting the archive. */
func TestInMemoryCatalogReadingRepositoryDoesNotHandBackItsOwnRecords(t *testing.T) {
    repositoryInstance := newInMemoryCatalogReadingRepository()
    takenAt := time.Date(2026, time.September, 7, 10, 0, 0, 0, time.UTC)

    _ = repositoryInstance.Append(context.Background(), &CatalogReadingRecord{TakenAt: takenAt, Headline: "catalog", Payload: "products=1"})

    readingList, _ := repositoryInstance.Recent(context.Background(), 1)
    readingList[0].Headline = "rewritten"

    readAgain, _ := repositoryInstance.Recent(context.Background(), 1)
    if "catalog" != readAgain[0].Headline {
        t.Fatalf("the caller's mutation reached the archive: headline is now %q", readAgain[0].Headline)
    }
}

/* the archive keeps a COPY of what it was handed: a caller that mutates the record it appended must not
   be rewriting the archive behind it. It is the write-side sister of the read-side copy above, and both
   are needed — one keeps the caller out of the archive, the other keeps the archive out of the caller. */
func TestInMemoryCatalogReadingRepositoryDoesNotKeepTheCallersRecord(t *testing.T) {
    repositoryInstance := newInMemoryCatalogReadingRepository()
    takenAt := time.Date(2026, time.September, 7, 10, 0, 0, 0, time.UTC)

    handed := &CatalogReadingRecord{TakenAt: takenAt, Headline: "catalog", Payload: "products=1"}
    if appendErr := repositoryInstance.Append(context.Background(), handed); nil != appendErr {
        t.Fatalf("append: %v", appendErr)
    }

    handed.Headline = "rewritten"

    readBack, _ := repositoryInstance.Recent(context.Background(), 1)
    if "catalog" != readBack[0].Headline {
        t.Fatalf("the caller's later mutation reached the archive: headline is now %q", readBack[0].Headline)
    }
}
