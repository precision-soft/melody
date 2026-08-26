package reporting

import (
    "context"
    "fmt"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/repository"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyhttp "github.com/precision-soft/melody/v3/http"
)

/* stubJournalRepository records what it was asked to write and can be told to refuse, which is the only way to observe what the trail does with a batch that did not land. */
type stubJournalRepository struct {
    batches   [][]*repository.CatalogJournalEntry
    appendErr error
}

func (instance *stubJournalRepository) Append(ctx context.Context, entry *repository.CatalogJournalEntry) (*repository.CatalogJournalEntry, error) {
    return entry, instance.appendErr
}

func (instance *stubJournalRepository) AppendBatch(ctx context.Context, entryList []*repository.CatalogJournalEntry) error {
    if nil != instance.appendErr {
        return instance.appendErr
    }

    copied := make([]*repository.CatalogJournalEntry, len(entryList))
    copy(copied, entryList)
    instance.batches = append(instance.batches, copied)

    return nil
}

func (instance *stubJournalRepository) Latest(ctx context.Context, limit int) ([]*repository.CatalogJournalEntry, error) {
    return nil, nil
}

func (instance *stubJournalRepository) Count(ctx context.Context) (int, error) {
    return 0, nil
}

var _ repository.CatalogJournalRepository = (*stubJournalRepository)(nil)

func newTestTrail(journalRepository repository.CatalogJournalRepository, requestId string) *RequestReportTrail {
    trail, buildErr := NewRequestReportTrail(
        melodyhttp.NewRequestContext(requestId, time.Unix(0, 0)),
        NewReportFormatter(),
        journalRepository,
        melodyclock.NewFrozenClock(time.Unix(1700000000, 0).UTC()),
    )
    if nil != buildErr {
        panic(buildErr)
    }

    return trail
}

/* nothing may reach the journal before the flush: the whole reason the trail exists is that the write happens once, at a point the request can still be failed at */

func TestRequestReportTrailWritesNothingBeforeFlush(t *testing.T) {
    journalRepository := &stubJournalRepository{}
    trail := newTestTrail(journalRepository, "request-1")

    trail.Record("editor", repository.CatalogJournalActionCreated, "product", "prod-1")
    trail.Record("editor", repository.CatalogJournalActionUpdated, "product", "prod-2")

    if 0 != len(journalRepository.batches) {
        t.Fatalf("expected nothing written before the flush, got %d batch(es)", len(journalRepository.batches))
    }

    if 2 != len(trail.Entries()) {
        t.Fatalf("expected the trail to hold the two recorded changes, got %v", trail.Entries())
    }
}

/* one request's changes go out as ONE batch, and every entry carries the request that caused it — a per-entry write would cost a round trip per change and an entry without the request id could not be traced back to it */

func TestRequestReportTrailFlushesOneBatchStampedWithTheRequest(t *testing.T) {
    journalRepository := &stubJournalRepository{}
    trail := newTestTrail(journalRepository, "request-7")

    trail.Record("editor", repository.CatalogJournalActionCreated, "product", "prod-1")
    trail.Record("editor", repository.CatalogJournalActionDeleted, "product", "prod-2")

    if flushErr := trail.Flush(context.Background()); nil != flushErr {
        t.Fatalf("flush: %v", flushErr)
    }

    if 1 != len(journalRepository.batches) {
        t.Fatalf("expected exactly one batch, got %d", len(journalRepository.batches))
    }

    batch := journalRepository.batches[0]
    if 2 != len(batch) {
        t.Fatalf("expected both changes in the one batch, got %d", len(batch))
    }

    for index, entry := range batch {
        if "request-7" != entry.RequestId {
            t.Fatalf("entry %d carries request id %q, wanted the trail's own %q", index, entry.RequestId, "request-7")
        }

        if true == entry.RecordedAt.IsZero() {
            t.Fatalf("entry %d was not stamped by the injected clock", index)
        }
    }
}

/* a second flush must not write the same changes again: the flush middleware and the scope's Close both call it on the ordinary path, and a trail that re-wrote what it already wrote would double every journal entry in the application */

func TestRequestReportTrailFlushIsIdempotent(t *testing.T) {
    journalRepository := &stubJournalRepository{}
    trail := newTestTrail(journalRepository, "request-2")

    trail.Record("editor", repository.CatalogJournalActionCreated, "product", "prod-1")

    if flushErr := trail.Flush(context.Background()); nil != flushErr {
        t.Fatalf("first flush: %v", flushErr)
    }
    if flushErr := trail.Flush(context.Background()); nil != flushErr {
        t.Fatalf("second flush: %v", flushErr)
    }
    if closeErr := trail.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if 1 != len(journalRepository.batches) {
        t.Fatalf("expected the changes to be written once, got %d batches", len(journalRepository.batches))
    }
}

/* a flush nobody made must not be reported as one either: a read-only request resolves the trail too, and it may not pay a query for having changed nothing */

func TestRequestReportTrailFlushOfAnEmptyTrailTouchesNothing(t *testing.T) {
    journalRepository := &stubJournalRepository{}
    trail := newTestTrail(journalRepository, "request-3")

    if flushErr := trail.Flush(context.Background()); nil != flushErr {
        t.Fatalf("flush: %v", flushErr)
    }

    if 0 != len(journalRepository.batches) {
        t.Fatalf("expected an empty trail to write nothing, got %d batch(es)", len(journalRepository.batches))
    }
}

/* a failed flush keeps the entries staged so Close can try again. Dropping them would lose the record of a change that DID happen, and re-trying cannot duplicate anything because the batch is written in one statement and fails as a whole */

func TestRequestReportTrailKeepsEntriesStagedWhenTheFlushFails(t *testing.T) {
    journalRepository := &stubJournalRepository{appendErr: fmt.Errorf("database is gone")}
    trail := newTestTrail(journalRepository, "request-4")

    trail.Record("editor", repository.CatalogJournalActionCreated, "product", "prod-1")

    if flushErr := trail.Flush(context.Background()); nil == flushErr {
        t.Fatalf("expected the flush to report the failure")
    }

    if 1 != len(trail.Entries()) {
        t.Fatalf("expected the change to stay staged after a failed flush, got %v", trail.Entries())
    }

    journalRepository.appendErr = nil

    if closeErr := trail.Close(); nil != closeErr {
        t.Fatalf("close after the failure recovered: %v", closeErr)
    }

    if 1 != len(journalRepository.batches) {
        t.Fatalf("expected Close to write what the failed flush left staged, got %d batch(es)", len(journalRepository.batches))
    }
}

/* the trail reads BOTH container levels: the request context of its own scope and the formatter singleton. A summary that named no request would mean the scoped registration handed it the wrong one */

func TestRequestReportTrailSummaryNamesItsOwnRequest(t *testing.T) {
    trail := newTestTrail(&stubJournalRepository{}, "request-5")

    trail.Record("editor", repository.CatalogJournalActionCreated, "product", "prod-1")

    if "request-5" != trail.RequestId() {
        t.Fatalf("the trail reports request id %q, wanted %q", trail.RequestId(), "request-5")
    }

    if "request-5: 1 entries" != trail.Summary() {
        t.Fatalf("the trail summarised as %q, wanted the formatter's rendering of its own request", trail.Summary())
    }
}
