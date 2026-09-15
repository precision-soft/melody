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

const catalogReadingCacheKey = "catalog.reading"

const (
    catalogReadingProductCountField = "products="
    catalogReadingJournalCountField = "journal="
)

const catalogReadingRecordedAtField = "recorded_at="

func NewReportFormatter() *ReportFormatter {
    return &ReportFormatter{}
}

type ReportFormatter struct {
}

func (instance *ReportFormatter) Format(title string, count int) string {
    return fmt.Sprintf("%s: %d entries", title, count)
}

/* NewCatalogReportService builds the reading service. The archive is resolved lazily by Archive and RecentReadings. */
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

/* CatalogReading carries a catalogue snapshot and its observation time. */
type CatalogReading struct {
    RecordedAt time.Time
    Headline   string
    Payload    string
    FromCache  bool
}

/* Reading returns a cached reading with its original timestamp, or refreshes a missing or malformed entry. The example currently calls Refresh directly from its scheduled command. */
func (instance *CatalogReportService) Reading(ctx context.Context) (*CatalogReading, error) {
    stored, cached, getErr := instance.cache.Get(catalogReadingCacheKey)
    if nil != getErr {
        return nil, getErr
    }

    if true == cached {
        payload, ok := stored.(string)
        if true == ok {

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

/* Refresh replaces the cached reading, using refreshInterval as its TTL. It does not write to the archive. */
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

/* Archive writes the counts from the supplied payload under ArchivedInstantOf. An existing instant returns false, nil; other failures are returned. The scheduled caller holds the archive lock around reading, archiving and export. */
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

func (instance *CatalogReportService) readingRepositoryOf(runtimeInstance melodyruntimecontract.Runtime) (repository.CatalogReadingRepository, error) {
    return melodycontainer.FromResolver[repository.CatalogReadingRepository](
        runtimeInstance.Container(),
        repository.ServiceCatalogReadingRepository,
    )
}

/* ArchivedInstantOf returns the reading instant in UTC truncated to a second, matching the RFC3339 payload. Refreshes within one second share an archive identity. */
func ArchivedInstantOf(recordedAt time.Time) time.Time {
    return recordedAt.UTC().Truncate(time.Second)
}

const readingAlreadyRecordedMessage = "reading already recorded"

/* RecentReadings resolves the archive and lists its newest readings first. */
func (instance *CatalogReportService) RecentReadings(runtimeInstance melodyruntimecontract.Runtime, limit int) ([]*repository.CatalogReadingRecord, error) {
    readingRepository, resolveErr := instance.readingRepositoryOf(runtimeInstance)
    if nil != resolveErr {
        return nil, resolveErr
    }

    return readingRepository.Recent(runtimeInstance.Context(), limit)
}

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

/* Scoped services cannot use the framework-reserved service. prefix. */
const ServiceRequestReportTrail = "service-example-reporting-request-trail"

/* NewRequestReportTrail binds a trail to one request scope; its collaborators may be singletons. */
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

/* RequestReportTrail accumulates changes within one request. Event listeners and flush middleware must resolve the same scoped instance. It is not safe for concurrent use. */
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

/* Record queues a change until Flush. */
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

/* Flush writes one batch and clears it only on success. A failed batch remains for retry by Close; an ambiguous database commit can make that retry duplicate rows. An empty trail performs no write. */
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

/* Close retries any changes left after the middleware flush. */
func (instance *RequestReportTrail) Close() error {
    return instance.Flush(context.Background())
}

/* Entries reports the changes currently queued in this request. */
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
