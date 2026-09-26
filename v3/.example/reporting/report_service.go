package reporting

import (
    "context"
    "fmt"
    "strconv"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/service"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* catalogReadingCacheKey is where a reading is left for whoever asks next: the scheduled refresh writes it and every request reads it. */
const catalogReadingCacheKey = "catalog.reading"

/* the two counts inside the payload, named once for the writer and the reader */
const (
    catalogReadingProductCountField = "products="
    catalogReadingJournalCountField = "journal="
)

/* catalogReadingRecordedAtField names the stamp inside the payload: the instant travels inside the one cached value, so it cannot lapse apart from the reading. */
const catalogReadingRecordedAtField = "recorded_at="

/* the services in this package are registered by melody:wiring:generate, which renders them into generated/wiring_gen.go for config.Module; they exercise a dependency resolved by type, two scalars bound to configuration parameters and one bound through a directive */

func NewReportFormatter() *ReportFormatter {
    return &ReportFormatter{}
}

type ReportFormatter struct {
}

func (instance *ReportFormatter) Format(title string, count int) string {
    return fmt.Sprintf("%s: %d entries", title, count)
}

/* NewCatalogReportService takes everything a reading needs and not the archive, the second database, which opens lazily: Archive and RecentReadings resolve its repository when called, so a process that never touches the archive never dials it. */
//melody:bind refreshInterval=app.reporting.refresh_interval
func NewCatalogReportService(
    formatter *ReportFormatter,
    productService *service.ProductService,
    journalRepository repository.CatalogJournalRepository,
    cacheInstance melodycachecontract.Cache,
    clockInstance melodyclockcontract.Clock,
    catalogTitle string,
    maxItemsPerPage int,
    refreshInterval time.Duration,
) (*CatalogReportService, error) {
    return &CatalogReportService{
        formatter:         formatter,
        productService:    productService,
        journalRepository: journalRepository,
        cache:             cacheInstance,
        clock:             clockInstance,
        catalogTitle:      catalogTitle,
        maxItemsPerPage:   maxItemsPerPage,
        refreshInterval:   refreshInterval,
    }, nil
}

type CatalogReportService struct {
    formatter         *ReportFormatter
    productService    *service.ProductService
    journalRepository repository.CatalogJournalRepository
    cache             melodycachecontract.Cache
    clock             melodyclockcontract.Clock
    catalogTitle      string
    maxItemsPerPage   int
    refreshInterval   time.Duration
}

/* CatalogReading is one reading of the nomenclature, stamped by the clock the service was built with. */
type CatalogReading struct {
    RecordedAt time.Time
    Headline   string
    Payload    string
    FromCache  bool
}

/* Reading yields the current reading, from the cache when the scheduled refresh left one there; a cached reading keeps the instant it was taken at, so the caller can tell how old the answer is. */
func (instance *CatalogReportService) Reading(ctx context.Context) (*CatalogReading, error) {
    /* one round trip: Get answers presence and value together, so no entry can expire between two reads */
    stored, cached, getErr := instance.cache.Get(catalogReadingCacheKey)
    if nil != getErr {
        return nil, getErr
    }

    if true == cached {
        payload, ok := stored.(string)
        if true == ok {
            /* the stamp comes out of the payload, not off the clock, so RecordedAt is the age of the reading; a payload whose stamp cannot be read back falls through to Refresh */
            recordedAt, readable := recordedAtOf(payload)
            if true == readable {
                return &CatalogReading{
                    RecordedAt: recordedAt,
                    Headline:   instance.Headline(),
                    Payload:    payload,
                    FromCache:  true,
                }, nil
            }
        }
    }

    return instance.Refresh(ctx)
}

/* Refresh takes a new reading and leaves it in the cache whatever was there before; the scheduled command calls it, so a request rarely computes one. */
func (instance *CatalogReportService) Refresh(ctx context.Context) (*CatalogReading, error) {
    recordedAt := instance.clock.Now()

    products, listErr := instance.productService.List()
    if nil != listErr {
        return nil, listErr
    }

    journalCount, countErr := instance.journalRepository.Count(ctx)
    if nil != countErr {
        return nil, countErr
    }

    payload := catalogReadingProductCountField + strconv.Itoa(len(products)) +
        " " + catalogReadingJournalCountField + strconv.Itoa(journalCount) +
        " " + catalogReadingRecordedAtField + recordedAt.UTC().Format(time.RFC3339)

    setErr := instance.cache.Set(catalogReadingCacheKey, payload, instance.refreshInterval)
    if nil != setErr {
        return nil, setErr
    }

    return &CatalogReading{
        RecordedAt: recordedAt,
        Headline:   instance.Headline(),
        Payload:    payload,
        FromCache:  false,
    }, nil
}

/* Archive records a reading in the archive and answers whether this call wrote it. It is not a step of Refresh, because a request with a cold cache takes a reading too, and a read must not write to a second database; the scheduled command calls it under the archive's advisory lock, so overlapping processes record one reading. The row's counts are read back out of the reading's payload. A reading already recorded at that instant is not a failure: the caller is told it did not write, and every other failure is answered. */
func (instance *CatalogReportService) Archive(runtimeInstance melodyruntimecontract.Runtime, reading *CatalogReading) (bool, error) {
    if nil == reading {
        return false, fmt.Errorf("reading is required")
    }

    readingRepository, resolveErr := instance.readingRepositoryOf(runtimeInstance)
    if nil != resolveErr {
        return false, resolveErr
    }

    productCount, productCountFound := payloadCountOf(reading.Payload, catalogReadingProductCountField)
    journalCount, journalCountFound := payloadCountOf(reading.Payload, catalogReadingJournalCountField)
    if false == productCountFound || false == journalCountFound {
        return false, fmt.Errorf("the reading's payload carries no counts to archive: %q", reading.Payload)
    }

    appendErr := readingRepository.Append(runtimeInstance.Context(), &repository.CatalogReadingRecord{
        TakenAt:      ArchivedInstantOf(reading.RecordedAt),
        Headline:     reading.Headline,
        Payload:      reading.Payload,
        ProductCount: productCount,
        JournalCount: journalCount,
    })
    if nil == appendErr {
        return true, nil
    }

    if readingAlreadyRecordedMessage == appendErr.Error() {
        return false, nil
    }

    return false, appendErr
}

/* readingRepositoryOf resolves the archive when it is needed, from the container the runtime carries: a resolution, not a capture, so the container records what this service holds of the archive when it holds it. */
func (instance *CatalogReportService) readingRepositoryOf(runtimeInstance melodyruntimecontract.Runtime) (repository.CatalogReadingRepository, error) {
    return melodycontainer.FromResolver[repository.CatalogReadingRepository](
        runtimeInstance.Container(),
        repository.ServiceCatalogReadingRepository,
    )
}

/* ArchivedInstantOf is the instant a reading is archived under: taken at, in UTC, truncated to the second. The truncation is the archive's identity: the payload writes recorded_at in RFC 3339 without a fraction, so the key agrees with the value it keys, and two refreshes inside one second are the same reading. */
func ArchivedInstantOf(recordedAt time.Time) time.Time {
    return recordedAt.UTC().Truncate(time.Second)
}

/* readingAlreadyRecordedMessage is the sentence both archive implementations answer a duplicate instant with, compared rather than wrapped because one reaches it through a postgres SQLSTATE and the other through a map lookup. */
const readingAlreadyRecordedMessage = "reading already recorded"

/* RecentReadings lists the archive, newest first, resolving it when asked, so the history door is the one request that touches the second database. */
func (instance *CatalogReportService) RecentReadings(runtimeInstance melodyruntimecontract.Runtime, limit int) ([]*repository.CatalogReadingRecord, error) {
    readingRepository, resolveErr := instance.readingRepositoryOf(runtimeInstance)
    if nil != resolveErr {
        return nil, resolveErr
    }

    return readingRepository.Recent(runtimeInstance.Context(), limit)
}

/* payloadFieldOf reads one field back out of the payload Refresh wrote, and says whether it found one. No field carries spaces, so it is read to the end of the value or the next field; a payload of another shape answers false. */
func payloadFieldOf(payload string, field string) (string, bool) {
    fieldIndex := strings.Index(payload, field)
    if 0 > fieldIndex {
        return "", false
    }

    value := payload[fieldIndex+len(field):]
    if separatorIndex := strings.IndexByte(value, ' '); 0 <= separatorIndex {
        value = value[:separatorIndex]
    }

    return value, true
}

/* payloadCountOf reads a count back out of the payload, so the counts an archived row carries are the reading's own. */
func payloadCountOf(payload string, field string) (int, bool) {
    value, found := payloadFieldOf(payload, field)
    if false == found {
        return 0, false
    }

    count, parseErr := strconv.Atoi(value)
    if nil != parseErr || 0 > count {
        return 0, false
    }

    return count, true
}

func recordedAtOf(payload string) (time.Time, bool) {
    value, found := payloadFieldOf(payload, catalogReadingRecordedAtField)
    if false == found {
        return time.Time{}, false
    }

    recordedAt, parseErr := time.Parse(time.RFC3339, value)
    if nil != parseErr {
        return time.Time{}, false
    }

    return recordedAt, true
}

func (instance *CatalogReportService) Headline() string {
    return instance.formatter.Format(instance.catalogTitle, instance.maxItemsPerPage)
}

func (instance *CatalogReportService) RefreshInterval() time.Duration {
    return instance.refreshInterval
}

/* the name stays out of the "service." namespace the framework reserves: a scoped registration there is refused at boot, so a scoped service never shadows a protected singleton. The container-lifetime services keep the "service.example." spelling, which registration admits. */
const ServiceRequestReportTrail = "service-example-reporting-request-trail"

/* the trail belongs to one request: it is built from the request context the kernel installs into every scope, and the directive makes the generator emit it into the scoped registration. A scoped service may take container singletons beside the request context. */
//melody:scoped
//melody:service ServiceRequestReportTrail
func NewRequestReportTrail(
    requestContext *melodyhttp.RequestContext,
    formatter *ReportFormatter,
    journalRepository repository.CatalogJournalRepository,
    clockInstance melodyclockcontract.Clock,
) (*RequestReportTrail, error) {
    return &RequestReportTrail{
        requestContext:    requestContext,
        formatter:         formatter,
        journalRepository: journalRepository,
        clock:             clockInstance,
        entries:           make([]*repository.CatalogJournalEntry, 0, 4),
    }, nil
}

/* RequestReportTrail collects one request's changes to the nomenclature before they are written. The event listeners record into it and the flush middleware writes it once the handler chain returns, both through the scope, so a journal row proves the scope holds one instance. Every entry carries the request that caused it, and a request that changed several records costs one round trip. */
type RequestReportTrail struct {
    requestContext    *melodyhttp.RequestContext
    formatter         *ReportFormatter
    journalRepository repository.CatalogJournalRepository
    clock             melodyclockcontract.Clock
    entries           []*repository.CatalogJournalEntry
}

func (instance *RequestReportTrail) RequestId() string {
    return instance.requestContext.RequestId()
}

/* Record adds one change to what this request will write. Nothing reaches the journal until Flush. */
func (instance *RequestReportTrail) Record(actor string, action string, subject string, subjectId string) {
    instance.entries = append(instance.entries, &repository.CatalogJournalEntry{
        RequestId:  instance.requestContext.RequestId(),
        Actor:      actor,
        Action:     action,
        Subject:    subject,
        SubjectId:  subjectId,
        RecordedAt: instance.clock.Now().UTC(),
    })
}

/* Flush writes what the request accumulated and empties the trail. The trail is emptied only after the write succeeds, so a failed flush leaves the entries for Close to retry; a batch is one statement that fails whole, so only a commit whose acknowledgement was lost can make Close's retry write it twice. An empty trail touches nothing, so a second call after a successful one is a no-op. */
func (instance *RequestReportTrail) Flush(ctx context.Context) error {
    if 0 == len(instance.entries) {
        return nil
    }

    appendErr := instance.journalRepository.AppendBatch(ctx, instance.entries)
    if nil != appendErr {
        return appendErr
    }

    instance.entries = make([]*repository.CatalogJournalEntry, 0, 4)

    return nil
}

/* Close is the scope's last word on the trail: it flushes what the flush middleware did not, as after a panic on the way out of the handler chain, so the record of a change is kept. */
func (instance *RequestReportTrail) Close() error {
    return instance.Flush(context.Background())
}

/* Entries reports what this request has recorded so far, as the report renders it. */
func (instance *RequestReportTrail) Entries() []string {
    copied := make([]string, 0, len(instance.entries))

    for _, entry := range instance.entries {
        copied = append(copied, entry.Action+" "+entry.Subject+" "+entry.SubjectId)
    }

    return copied
}

func (instance *RequestReportTrail) Summary() string {
    return instance.formatter.Format(instance.requestContext.RequestId(), len(instance.entries))
}
