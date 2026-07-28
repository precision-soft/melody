package reporting

import (
    "context"
    "fmt"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/service"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
)

/* catalogReadingCacheKey is where a reading is left for whoever asks next. The scheduled refresh writes it and
every request reads it, which is the whole point: the request that finds a cold cache is the one that pays for
the reading. */
const catalogReadingCacheKey = "catalog.reading"

/* the services in this package are never registered by hand: melody:wiring:generate scans the package and renders their registrations into generated/wiring_gen.go, which is what config.Module registers. They are here to exercise the generated wiring against a real container — a dependency resolved by type, two scalars bound to configuration parameters and one bound through a directive. */

func NewReportFormatter() *ReportFormatter {
    return &ReportFormatter{}
}

type ReportFormatter struct {
}

func (instance *ReportFormatter) Format(title string, count int) string {
    return fmt.Sprintf("%s: %d entries", title, count)
}

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

/* Reading yields the current reading, from the cache when the scheduled refresh left one there. A cached
reading keeps the instant it was taken at, which is the point of stamping it: the caller can tell how old the
answer is. */
func (instance *CatalogReportService) Reading(ctx context.Context) (*CatalogReading, error) {
    cached, existsErr := instance.cache.Has(catalogReadingCacheKey)
    if nil != existsErr {
        return nil, existsErr
    }

    if true == cached {
        stored, _, getErr := instance.cache.Get(catalogReadingCacheKey)
        if nil != getErr {
            return nil, getErr
        }

        payload, ok := stored.(string)
        if true == ok {
            return &CatalogReading{
                RecordedAt: instance.clock.Now(),
                Headline:   instance.Headline(),
                Payload:    payload,
                FromCache:  true,
            }, nil
        }
    }

    return instance.Refresh(ctx)
}

/* Refresh takes a new reading and leaves it in the cache whatever was there before, which is what the
scheduled command calls: a reading nobody asked for in the last window is the one that would otherwise be
computed inside a request. */
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

    payload := "products=" + strconv.Itoa(len(products)) +
        " journal=" + strconv.Itoa(journalCount) +
        " recorded_at=" + recordedAt.UTC().Format(time.RFC3339)

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

func (instance *CatalogReportService) Headline() string {
    return instance.formatter.Format(instance.catalogTitle, instance.maxItemsPerPage)
}

func (instance *CatalogReportService) RefreshInterval() time.Duration {
    return instance.refreshInterval
}

const ServiceRequestReportTrail = "service.example.reporting.request_trail"

/* the trail belongs to one request: it is built from the request context the kernel installs into every
scope, and a service that holds one request's identity must not be a process singleton. The directive is what
says so, and the generator emits it into the scoped registration function rather than the container one. It
takes container singletons beside the request context on purpose — a scoped service may read both levels. */
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

/* RequestReportTrail is where one request's changes to the nomenclature are collected before they are
written.

It is what makes the scope-owned registration mean something rather than demonstrate itself. The event
listeners record into it while the request is being served, and the flush middleware writes what it holds once
the handler chain has returned; both reach it through the scope, and if those two resolutions ever yielded
different objects the middleware would flush an empty trail and nothing would be journalled at all. The
journal row existing is therefore the proof that a scope holds one instance of what it owns — a claim no
single response can make about itself.

Collecting first also buys the journal something real: every entry carries the request that caused it, and a
request that changed several records costs one round trip instead of one per change. */
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

/* Flush writes what the request accumulated and empties the trail.

The trail is emptied only once the write has succeeded, so a failed flush leaves the entries staged for Close
to try again rather than dropping the record of a change that did happen. That is safe to retry because a
batch is written in one statement and fails as a whole: a failure means no row was written, so the second
attempt cannot duplicate the first.

Calling Flush on an empty trail touches nothing, so a request that read rather than wrote pays no query — and
a second call after a successful one is a no-op rather than a duplicate. */
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

/* Close is the scope's own last word on the trail. The flush middleware is what normally empties it, before
the response is written and while the caller can still be told the write failed; this runs when that never
happened — a panic on the way out of the handler chain — and is the difference between losing the record of a
change and keeping it. */
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
