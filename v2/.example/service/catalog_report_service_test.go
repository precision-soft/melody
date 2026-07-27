package service

import (
    "context"
    "testing"
    "time"

    melodyclock "github.com/precision-soft/melody/v2/clock"
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

type countingNoteRepository struct {
    count             int
    ensureSchemaCalls int
}

func (instance *countingNoteRepository) EnsureSchema(ctx context.Context) error {
    instance.ensureSchemaCalls = instance.ensureSchemaCalls + 1

    return nil
}

func (instance *countingNoteRepository) Count(ctx context.Context) (int, error) {
    return instance.count, nil
}

/* @info The report is stamped by the clock it was built with, not by the wall, which is what lets a test state the exact instant a reading carries. */
func TestCatalogReportServiceStampsTheReadingWithTheInjectedClock(t *testing.T) {
    frozenTime := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
    frozenClock := melodyclock.NewFrozenClock(frozenTime)

    reportService := NewCatalogReportService(
        frozenClock,
        newRecordingReportBackend(),
        &countingNoteRepository{count: 7},
    )

    report, reportErr := reportService.Report(context.Background())
    if nil != reportErr {
        t.Fatalf("unexpected report error: %v", reportErr)
    }

    if false == report.RecordedAt.Equal(frozenTime) {
        t.Fatalf("expected the reading to carry the frozen instant, got %v", report.RecordedAt)
    }

    if "notes=7 recorded_at=2026-01-01T12:00:00Z" != report.Payload {
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

    reportService := NewCatalogReportService(frozenClock, backend, &countingNoteRepository{count: 3})

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
}

/* @info Both collaborators are optional: an example booted with no redis and no database still answers, with the reading it can actually take. */
func TestCatalogReportServiceWorksWithoutABackendOrARepository(t *testing.T) {
    frozenClock := melodyclock.NewFrozenClock(time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC))

    reportService := NewCatalogReportService(frozenClock, nil, nil)

    report, reportErr := reportService.Report(context.Background())
    if nil != reportErr {
        t.Fatalf("unexpected report error: %v", reportErr)
    }

    if "notes=0 recorded_at=2026-01-01T12:00:00Z" != report.Payload {
        t.Fatalf("unexpected payload: %q", report.Payload)
    }
}
