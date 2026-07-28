package service

import (
    "context"
    "testing"
    "time"

    "github.com/precision-soft/melody/.example/entity"
    melodyclock "github.com/precision-soft/melody/clock"
)

type recordingReportBackend struct {
    entries  map[string][]byte
    setCalls int
}

func newRecordingReportBackend() *recordingReportBackend {
    return &recordingReportBackend{entries: make(map[string][]byte)}
}

func (instance *recordingReportBackend) Get(key string) ([]byte, bool, error) {
    payload, found := instance.entries[key]

    return payload, found, nil
}

func (instance *recordingReportBackend) Set(key string, payload []byte, ttl time.Duration) error {
    instance.setCalls = instance.setCalls + 1
    instance.entries[key] = payload

    return nil
}

type countingProductRepository struct {
    count     int
    allCalls  int
    lastError error
}

func (instance *countingProductRepository) All(ctx context.Context) ([]*entity.Product, error) {
    instance.allCalls = instance.allCalls + 1

    if nil != instance.lastError {
        return nil, instance.lastError
    }

    products := make([]*entity.Product, 0, instance.count)
    for index := 0; index < instance.count; index++ {
        products = append(products, &entity.Product{})
    }

    return products, nil
}

type countingJournalRepository struct {
    count int
}

func (instance *countingJournalRepository) Count(ctx context.Context) (int, error) {
    return instance.count, nil
}

/* @info The report is stamped by the clock it was built with, not by the wall, which is what lets a test state the exact instant a reading carries. */
func TestCatalogReportServiceStampsTheReadingWithTheInjectedClock(t *testing.T) {
    frozenTime := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
    frozenClock := melodyclock.NewFrozenClock(frozenTime)

    reportService := NewCatalogReportService(
        frozenClock,
        newRecordingReportBackend(),
        &countingProductRepository{count: 5},
        &countingJournalRepository{count: 7},
    )

    report, reportErr := reportService.Report(context.Background())
    if nil != reportErr {
        t.Fatalf("unexpected report error: %v", reportErr)
    }

    if false == report.RecordedAt.Equal(frozenTime) {
        t.Fatalf("expected the reading to carry the frozen instant, got %v", report.RecordedAt)
    }

    if "products=5 journal=7 recorded_at=2026-01-01T12:00:00Z" != report.Payload {
        t.Fatalf("unexpected payload: %q", report.Payload)
    }

    if true == report.FromCache {
        t.Fatalf("expected the first reading to be built rather than served from the cache")
    }
}

/* @info A warm cache is what the backend is there for: the second reading must not go back to the repository, and it must say it came from the cache so the caller can tell how old the answer is. */
func TestCatalogReportServiceServesTheSecondReadingFromTheCache(t *testing.T) {
    frozenClock := melodyclock.NewFrozenClock(time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC))
    backend := newRecordingReportBackend()
    productRepository := &countingProductRepository{count: 2}

    reportService := NewCatalogReportService(frozenClock, backend, productRepository, &countingJournalRepository{count: 3})

    firstReport, reportErr := reportService.Report(context.Background())
    if nil != reportErr {
        t.Fatalf("unexpected report error: %v", reportErr)
    }

    frozenClock.Advance(30 * time.Second)

    secondReport, reportErr := reportService.Report(context.Background())
    if nil != reportErr {
        t.Fatalf("unexpected second report error: %v", reportErr)
    }

    if false == secondReport.FromCache {
        t.Fatalf("expected the second reading to come from the cache")
    }

    if firstReport.Payload != secondReport.Payload {
        t.Fatalf("expected the cached payload to be the one that was stored, got %q", secondReport.Payload)
    }

    if 1 != backend.setCalls {
        t.Fatalf("expected the report to be written once, got %d writes", backend.setCalls)
    }

    if 1 != productRepository.allCalls {
        t.Fatalf("expected the catalogue to be counted once, got %d readings", productRepository.allCalls)
    }
}

/* @info Refresh is what the schedule calls, so it must ignore a warm cache and leave a new reading behind — otherwise the scheduled run would keep re-storing the answer it just read. */
func TestCatalogReportServiceRefreshIgnoresTheWarmCache(t *testing.T) {
    frozenClock := melodyclock.NewFrozenClock(time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC))
    backend := newRecordingReportBackend()
    productRepository := &countingProductRepository{count: 1}

    reportService := NewCatalogReportService(frozenClock, backend, productRepository, &countingJournalRepository{count: 0})

    _, reportErr := reportService.Report(context.Background())
    if nil != reportErr {
        t.Fatalf("unexpected report error: %v", reportErr)
    }

    frozenClock.Advance(time.Minute)

    refreshed, refreshErr := reportService.Refresh(context.Background())
    if nil != refreshErr {
        t.Fatalf("unexpected refresh error: %v", refreshErr)
    }

    if true == refreshed.FromCache {
        t.Fatalf("expected the refreshed reading to be built rather than served from the cache")
    }

    if "products=1 journal=0 recorded_at=2026-01-01T12:01:00Z" != refreshed.Payload {
        t.Fatalf("expected the refreshed reading to carry the advanced instant, got %q", refreshed.Payload)
    }

    if 2 != backend.setCalls {
        t.Fatalf("expected the refreshed reading to be written back, got %d writes", backend.setCalls)
    }

    if 2 != productRepository.allCalls {
        t.Fatalf("expected the catalogue to be counted again, got %d readings", productRepository.allCalls)
    }
}

/* @info Both optional collaborators may be absent: an example booted with no redis and no database still answers, with the reading it can actually take. */
func TestCatalogReportServiceWorksWithoutABackendOrAJournal(t *testing.T) {
    frozenClock := melodyclock.NewFrozenClock(time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC))

    reportService := NewCatalogReportService(frozenClock, nil, &countingProductRepository{count: 4}, nil)

    report, reportErr := reportService.Report(context.Background())
    if nil != reportErr {
        t.Fatalf("unexpected report error: %v", reportErr)
    }

    if "products=4 journal=0 recorded_at=2026-01-01T12:00:00Z" != report.Payload {
        t.Fatalf("unexpected payload: %q", report.Payload)
    }
}
