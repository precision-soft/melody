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
takes a container singleton beside the request context on purpose — a scoped service may read both levels. */
//melody:scoped
//melody:service ServiceRequestReportTrail
func NewRequestReportTrail(
    requestContext *melodyhttp.RequestContext,
    formatter *ReportFormatter,
) (*RequestReportTrail, error) {
    return &RequestReportTrail{
        requestContext: requestContext,
        formatter:      formatter,
        entries:        make([]string, 0, 4),
    }, nil
}

/* RequestReportTrail records what one request did. Two handlers in the same request share one trail; two
requests never do. */
type RequestReportTrail struct {
    requestContext *melodyhttp.RequestContext
    formatter      *ReportFormatter
    entries        []string
}

func (instance *RequestReportTrail) RequestId() string {
    return instance.requestContext.RequestId()
}

func (instance *RequestReportTrail) Record(entry string) {
    instance.entries = append(instance.entries, entry)
}

func (instance *RequestReportTrail) Entries() []string {
    copied := make([]string, len(instance.entries))
    copy(copied, instance.entries)

    return copied
}

func (instance *RequestReportTrail) Summary() string {
    return instance.formatter.Format(instance.requestContext.RequestId(), len(instance.entries))
}
