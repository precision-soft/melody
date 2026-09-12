package reporting

import (
    "context"
    "fmt"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/service"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
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

/* readingCache keeps the values it is given, as they are: the reading is a string either way, and a double that round-tripped through a serializer would answer the same string for a reason that has nothing to do with what these probes ask. */
type readingCache struct {
    values map[string]any
}

func (instance *readingCache) Get(key string) (any, bool, error) {
    value, exists := instance.values[key]

    return value, exists, nil
}

func (instance *readingCache) Set(key string, value any, ttl time.Duration) error {
    instance.values[key] = value

    return nil
}

func (instance *readingCache) Delete(key string) error {
    delete(instance.values, key)

    return nil
}

func (instance *readingCache) Has(key string) (bool, error) {
    _, exists := instance.values[key]

    return exists, nil
}

func (instance *readingCache) Clear() error {
    instance.values = map[string]any{}

    return nil
}

func (instance *readingCache) Many(keys []string) (map[string]any, error) {
    result := map[string]any{}
    for _, key := range keys {
        if value, exists := instance.values[key]; true == exists {
            result[key] = value
        }
    }

    return result, nil
}

func (instance *readingCache) SetMultiple(items map[string]any, ttl time.Duration) error {
    for key, value := range items {
        instance.values[key] = value
    }

    return nil
}

func (instance *readingCache) DeleteMultiple(keys []string) error {
    for _, key := range keys {
        delete(instance.values, key)
    }

    return nil
}

func (instance *readingCache) Increment(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *readingCache) Decrement(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *readingCache) Close() error {
    return nil
}

var _ melodycachecontract.Cache = (*readingCache)(nil)

type emptyProductRepository struct{}

func (instance *emptyProductRepository) All(ctx context.Context) ([]*entity.Product, error) {
    return []*entity.Product{}, nil
}

func (instance *emptyProductRepository) FindById(ctx context.Context, id string) (*entity.Product, bool, error) {
    return nil, false, nil
}

func (instance *emptyProductRepository) Create(ctx context.Context, product *entity.Product) error {
    return nil
}

func (instance *emptyProductRepository) Update(ctx context.Context, product *entity.Product) (bool, error) {
    return false, nil
}

func (instance *emptyProductRepository) DeleteById(ctx context.Context, id string) (bool, error) {
    return false, nil
}

var _ repository.ProductRepository = (*emptyProductRepository)(nil)

func newReportServiceUnderTest(t *testing.T, clockInstance *melodyclock.FrozenClock, cacheInstance melodycachecontract.Cache) *CatalogReportService {
    t.Helper()

    return newReportServiceWithArchive(t, clockInstance, cacheInstance, newRecordingReadingRepository())
}

func newReportServiceWithArchive(
    t *testing.T,
    clockInstance *melodyclock.FrozenClock,
    cacheInstance melodycachecontract.Cache,
    readingRepository repository.CatalogReadingRepository,
) *CatalogReportService {
    t.Helper()

    reportService, buildErr := NewCatalogReportService(
        NewReportFormatter(),
        service.NewProductService(&emptyProductRepository{}, nil, nil, cacheInstance, nil, clockInstance),
        &stubJournalRepository{},
        readingRepository,
        cacheInstance,
        clockInstance,
        "catalog",
        10,
        time.Minute,
    )
    if nil != buildErr {
        t.Fatalf("new report service: %v", buildErr)
    }

    return reportService
}

/* recordingReadingRepository is the archive as a test can inspect it: what it was handed, in order, and a
   refusal it can be told to answer. It keeps the identity rule the two real implementations keep — one
   reading per instant — because a double that accepted what they refuse would let the service's handling
   of that refusal go unproven. */
func newRecordingReadingRepository() *recordingReadingRepository {
    return &recordingReadingRepository{}
}

type recordingReadingRepository struct {
    appended  []*repository.CatalogReadingRecord
    failWith  error
    countFail error
}

func (instance *recordingReadingRepository) Append(ctx context.Context, reading *repository.CatalogReadingRecord) error {
    if nil != instance.failWith {
        return instance.failWith
    }

    for _, existing := range instance.appended {
        if true == existing.TakenAt.Equal(reading.TakenAt) {
            return fmt.Errorf("reading already recorded")
        }
    }

    stored := *reading
    instance.appended = append(instance.appended, &stored)

    return nil
}

func (instance *recordingReadingRepository) Recent(ctx context.Context, limit int) ([]*repository.CatalogReadingRecord, error) {
    if 0 >= limit {
        return []*repository.CatalogReadingRecord{}, nil
    }

    reversed := make([]*repository.CatalogReadingRecord, 0, len(instance.appended))
    for index := len(instance.appended) - 1; 0 <= index; index-- {
        reversed = append(reversed, instance.appended[index])
    }

    if limit < len(reversed) {
        reversed = reversed[:limit]
    }

    return reversed, nil
}

func (instance *recordingReadingRepository) Count(ctx context.Context) (int, error) {
    if nil != instance.countFail {
        return 0, instance.countFail
    }

    return len(instance.appended), nil
}

var _ repository.CatalogReadingRepository = (*recordingReadingRepository)(nil)

/* the stamp is what a caller reads to find out how old the answer is, so a cached reading must carry the instant the reading was TAKEN — stamping the moment of service made a reading a whole refresh interval old say "now". */
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

/* a payload this service cannot read the stamp back from is not served as a reading at all: it would have to be given an instant nobody measured. */
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

/* the archive records what the reading SAYS, and the instant it is keyed on is the one the reading states about itself — truncated to the second, which is the resolution the payload's own recorded_at carries. A key kept finer would disagree with the value it keys. */
func TestArchiveRecordsTheReadingUnderTheInstantItStatesAboutItself(t *testing.T) {
    takenAt := time.Date(2026, time.September, 7, 10, 0, 0, 987654321, time.UTC)
    clockInstance := melodyclock.NewFrozenClock(takenAt)
    cacheInstance := &readingCache{values: map[string]any{}}
    archive := newRecordingReadingRepository()

    reportService := newReportServiceWithArchive(t, clockInstance, cacheInstance, archive)

    reading, refreshErr := reportService.Refresh(context.Background())
    if nil != refreshErr {
        t.Fatalf("refresh: %v", refreshErr)
    }

    recorded, archiveErr := reportService.Archive(context.Background(), reading)
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

/* the truncation is what makes the duplicate reachable at all, so this is the pair that proves it: two archives of readings taken 300ms apart are the SAME reading, and the second is told it did not write rather than failing. */
func TestArchiveTreatsAReadingAlreadyRecordedAsNotWrittenRatherThanAsAFailure(t *testing.T) {
    takenAt := time.Date(2026, time.September, 7, 10, 0, 0, 100000000, time.UTC)
    clockInstance := melodyclock.NewFrozenClock(takenAt)
    cacheInstance := &readingCache{values: map[string]any{}}
    archive := newRecordingReadingRepository()

    reportService := newReportServiceWithArchive(t, clockInstance, cacheInstance, archive)

    first, _ := reportService.Refresh(context.Background())
    if recorded, _ := reportService.Archive(context.Background(), first); false == recorded {
        t.Fatal("expected the first archive to write")
    }

    second := &CatalogReading{
        RecordedAt: takenAt.Add(300 * time.Millisecond),
        Headline:   first.Headline,
        Payload:    first.Payload,
    }

    recorded, archiveErr := reportService.Archive(context.Background(), second)
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

/* every other failure of the archive IS a failure: an archive that could not be reached must not be reported as a reading that was already there. */
func TestArchiveHandsBackAFailureThatIsNotADuplicate(t *testing.T) {
    clockInstance := melodyclock.NewFrozenClock(time.Date(2026, time.September, 7, 10, 0, 0, 0, time.UTC))
    cacheInstance := &readingCache{values: map[string]any{}}
    archive := newRecordingReadingRepository()
    archive.failWith = fmt.Errorf("dial postgres: connection refused")

    reportService := newReportServiceWithArchive(t, clockInstance, cacheInstance, archive)

    reading, _ := reportService.Refresh(context.Background())

    recorded, archiveErr := reportService.Archive(context.Background(), reading)
    if nil == archiveErr {
        t.Fatal("expected an unreachable archive to be reported")
    }

    if true == recorded {
        t.Fatal("expected a failed archive to report that it did not write")
    }
}

/* ArchivedInstantOf is the identity itself, so it is pinned on values rather than only through the door: a fractional instant loses its fraction, a zone becomes UTC, and an instant already on the second is unchanged. */
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

/* the listing is the read half, and it hands back what the archive holds newest first. */
func TestRecentReadingsAnswersTheArchive(t *testing.T) {
    takenAt := time.Date(2026, time.September, 7, 10, 0, 0, 0, time.UTC)
    clockInstance := melodyclock.NewFrozenClock(takenAt)
    cacheInstance := &readingCache{values: map[string]any{}}
    archive := newRecordingReadingRepository()

    reportService := newReportServiceWithArchive(t, clockInstance, cacheInstance, archive)

    for index := 0; 3 > index; index++ {
        _ = archive.Append(context.Background(), &repository.CatalogReadingRecord{
            TakenAt:  takenAt.Add(time.Duration(index) * time.Second),
            Headline: "catalog",
            Payload:  "products=1",
        })
    }

    readingList, readErr := reportService.RecentReadings(context.Background(), 2)
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
