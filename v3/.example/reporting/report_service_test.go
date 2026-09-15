package reporting

import (
    "context"
    "fmt"
    "testing"
    "time"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodyclock "github.com/precision-soft/melody/v3/clock"
)

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

func TestCatalogReadingFromTheCacheKeepsTheInstantItWasTakenAt(t *testing.T) {
    takenAt := time.Date(2026, time.September, 6, 10, 0, 0, 0, time.UTC)
    clockInstance := melodyclock.NewFrozenClock(takenAt)
    cacheInstance := &readingCache{values: map[string]any{}}

    reportService := newReportServiceUnderTest(t, clockInstance, cacheInstance)

    if _, refreshErr := reportService.Refresh(context.Background()); nil != refreshErr {
        t.Fatalf("refresh: %v", refreshErr)
    }

    clockInstance.Advance(90 * time.Second)

    reading, readingErr := reportService.Reading(context.Background())
    if nil != readingErr {
        t.Fatalf("reading: %v", readingErr)
    }

    if false == reading.FromCache {
        t.Fatalf("expected the reading to come from the cache")
    }

    if false == reading.RecordedAt.Equal(takenAt) {
        t.Fatalf("expected the instant the reading was taken at (%s), got %s", takenAt.Format(time.RFC3339), reading.RecordedAt.Format(time.RFC3339))
    }
}

func TestCatalogReadingTakesAFreshReadingWhenTheCachedPayloadCarriesNoInstant(t *testing.T) {
    servedAt := time.Date(2026, time.September, 6, 11, 0, 0, 0, time.UTC)
    clockInstance := melodyclock.NewFrozenClock(servedAt)
    cacheInstance := &readingCache{values: map[string]any{catalogReadingCacheKey: "products=1 journal=2"}}

    reportService := newReportServiceUnderTest(t, clockInstance, cacheInstance)

    reading, readingErr := reportService.Reading(context.Background())
    if nil != readingErr {
        t.Fatalf("reading: %v", readingErr)
    }

    if true == reading.FromCache {
        t.Fatalf("expected a fresh reading, got one served from the cache")
    }

    if false == reading.RecordedAt.Equal(servedAt) {
        t.Fatalf("expected the fresh reading to carry the current instant, got %s", reading.RecordedAt.Format(time.RFC3339))
    }
}

func TestArchiveRecordsTheReadingUnderTheInstantItStatesAboutItself(t *testing.T) {
    takenAt := time.Date(2026, time.September, 7, 10, 0, 0, 987654321, time.UTC)
    clockInstance := melodyclock.NewFrozenClock(takenAt)
    cacheInstance := &readingCache{values: map[string]any{}}
    archive := newRecordingReadingRepository()

    reportService, runtimeInstance := newReportServiceWithArchive(t, clockInstance, cacheInstance, archive)

    reading, refreshErr := reportService.Refresh(context.Background())
    if nil != refreshErr {
        t.Fatalf("refresh: %v", refreshErr)
    }

    recorded, archiveErr := reportService.Archive(runtimeInstance, reading)
    if nil != archiveErr {
        t.Fatalf("archive: %v", archiveErr)
    }

    if false == recorded {
        t.Fatal("expected the first archive of a reading to report that it wrote")
    }

    if 1 != len(archive.appended) {
        t.Fatalf("expected exactly one row, got %d", len(archive.appended))
    }

    wanted := time.Date(2026, time.September, 7, 10, 0, 0, 0, time.UTC)
    if false == archive.appended[0].TakenAt.Equal(wanted) {
        t.Fatalf("expected the instant truncated to the second (%s), got %s", wanted, archive.appended[0].TakenAt)
    }

    if archive.appended[0].Payload != reading.Payload {
        t.Fatalf("the archived payload is not the reading's: %q against %q", archive.appended[0].Payload, reading.Payload)
    }
}

func TestArchiveTreatsAReadingAlreadyRecordedAsNotWrittenRatherThanAsAFailure(t *testing.T) {
    takenAt := time.Date(2026, time.September, 7, 10, 0, 0, 100000000, time.UTC)
    clockInstance := melodyclock.NewFrozenClock(takenAt)
    cacheInstance := &readingCache{values: map[string]any{}}
    archive := newRecordingReadingRepository()

    reportService, runtimeInstance := newReportServiceWithArchive(t, clockInstance, cacheInstance, archive)

    first, _ := reportService.Refresh(context.Background())
    if recorded, _ := reportService.Archive(runtimeInstance, first); false == recorded {
        t.Fatal("expected the first archive to write")
    }

    second := &CatalogReading{
        RecordedAt: takenAt.Add(300 * time.Millisecond),
        Headline:   first.Headline,
        Payload:    first.Payload,
    }

    recorded, archiveErr := reportService.Archive(runtimeInstance, second)
    if nil != archiveErr {
        t.Fatalf("expected a reading already recorded to be tolerated, got %v", archiveErr)
    }

    if true == recorded {
        t.Fatal("expected the second archive inside the same second to report that it did not write")
    }

    if 1 != len(archive.appended) {
        t.Fatalf("expected the archive to still hold one row, got %d", len(archive.appended))
    }
}

func TestArchiveHandsBackAFailureThatIsNotADuplicate(t *testing.T) {
    clockInstance := melodyclock.NewFrozenClock(time.Date(2026, time.September, 7, 10, 0, 0, 0, time.UTC))
    cacheInstance := &readingCache{values: map[string]any{}}
    archive := newRecordingReadingRepository()
    archive.failWith = fmt.Errorf("dial postgres: connection refused")

    reportService, runtimeInstance := newReportServiceWithArchive(t, clockInstance, cacheInstance, archive)

    reading, _ := reportService.Refresh(context.Background())

    recorded, archiveErr := reportService.Archive(runtimeInstance, reading)
    if nil == archiveErr {
        t.Fatal("expected an unreachable archive to be reported")
    }

    if true == recorded {
        t.Fatal("expected a failed archive to report that it did not write")
    }
}

func TestArchivedInstantOfTruncatesToTheSecondInUtc(t *testing.T) {
    eastern := time.FixedZone("east", 3*60*60)

    fractional := time.Date(2026, time.September, 7, 13, 0, 0, 999999999, eastern)
    archived := ArchivedInstantOf(fractional)

    if 0 != archived.Nanosecond() {
        t.Fatalf("expected the fraction to be dropped, got %s", archived)
    }

    if time.UTC != archived.Location() {
        t.Fatalf("expected UTC, got %s", archived.Location())
    }

    if false == archived.Equal(time.Date(2026, time.September, 7, 10, 0, 0, 0, time.UTC)) {
        t.Fatalf("expected the same instant in UTC, got %s", archived)
    }
}

func TestRecentReadingsAnswersTheArchive(t *testing.T) {
    takenAt := time.Date(2026, time.September, 7, 10, 0, 0, 0, time.UTC)
    clockInstance := melodyclock.NewFrozenClock(takenAt)
    cacheInstance := &readingCache{values: map[string]any{}}
    archive := newRecordingReadingRepository()

    reportService, runtimeInstance := newReportServiceWithArchive(t, clockInstance, cacheInstance, archive)

    for index := 0; 3 > index; index++ {
        _ = archive.Append(context.Background(), &repository.CatalogReadingRecord{
            TakenAt:  takenAt.Add(time.Duration(index) * time.Second),
            Headline: "catalog",
            Payload:  "products=1",
        })
    }

    readingList, readErr := reportService.RecentReadings(runtimeInstance, 2)
    if nil != readErr {
        t.Fatalf("recent readings: %v", readErr)
    }

    if 2 != len(readingList) {
        t.Fatalf("expected two readings, got %d", len(readingList))
    }

    if false == readingList[0].TakenAt.Equal(takenAt.Add(2*time.Second)) {
        t.Fatalf("expected the newest reading first, got %s", readingList[0].TakenAt)
    }
}

func TestArchiveCarriesTheCountsTheReadingStatesRatherThanASecondObservation(t *testing.T) {
    takenAt := time.Date(2026, time.September, 7, 10, 0, 0, 0, time.UTC)
    clockInstance := melodyclock.NewFrozenClock(takenAt)
    cacheInstance := &readingCache{values: map[string]any{}}
    archive := newRecordingReadingRepository()

    reportService, runtimeInstance := newReportServiceWithArchive(t, clockInstance, cacheInstance, archive)

    reading := &CatalogReading{
        RecordedAt: takenAt,
        Headline:   "catalog",
        Payload:    "products=3 journal=7 recorded_at=" + takenAt.Format(time.RFC3339),
    }

    if recorded, archiveErr := reportService.Archive(runtimeInstance, reading); nil != archiveErr || false == recorded {
        t.Fatalf("expected the reading to be archived, got recorded=%v err=%v", recorded, archiveErr)
    }

    if 3 != archive.appended[0].ProductCount || 7 != archive.appended[0].JournalCount {
        t.Fatalf("expected the row to carry the payload's counts 3/7, got %d/%d", archive.appended[0].ProductCount, archive.appended[0].JournalCount)
    }
}

func TestArchiveRefusesAPayloadThatCarriesNoCounts(t *testing.T) {
    clockInstance := melodyclock.NewFrozenClock(time.Date(2026, time.September, 7, 10, 0, 0, 0, time.UTC))
    archive := newRecordingReadingRepository()

    reportService, runtimeInstance := newReportServiceWithArchive(t, clockInstance, &readingCache{values: map[string]any{}}, archive)

    recorded, archiveErr := reportService.Archive(runtimeInstance, &CatalogReading{RecordedAt: clockInstance.Now(), Headline: "catalog", Payload: "recorded_at=2026-09-07T10:00:00Z"})
    if nil == archiveErr || true == recorded {
        t.Fatalf("expected a payload without counts to be refused, got recorded=%v err=%v", recorded, archiveErr)
    }

    if 0 != len(archive.appended) {
        t.Fatalf("expected nothing appended, got %d rows", len(archive.appended))
    }
}

func TestArchiveResolvesTheRepositoryAtTheCallAndNotAtConstruction(t *testing.T) {
    clockInstance := melodyclock.NewFrozenClock(time.Date(2026, time.September, 7, 10, 0, 0, 0, time.UTC))
    cacheInstance := &readingCache{values: map[string]any{}}

    reportService, _ := newReportServiceWithArchive(t, clockInstance, cacheInstance, nil)

    reading, refreshErr := reportService.Refresh(context.Background())
    if nil != refreshErr {
        t.Fatalf("expected the reading to be taken without an archive on the container, got %v", refreshErr)
    }

    if _, archiveErr := reportService.Archive(newArchiveRuntime(nil), reading); nil == archiveErr {
        t.Fatalf("expected the archive door to fail over a container without the repository")
    }

    if _, readErr := reportService.RecentReadings(newArchiveRuntime(nil), 1); nil == readErr {
        t.Fatalf("expected the history door to fail over a container without the repository")
    }
}
