package reporting

import (
    "fmt"
    "time"

    "github.com/precision-soft/melody/v3/.example/service"
    melodyhttp "github.com/precision-soft/melody/v3/http"
)

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
    catalogTitle string,
    maxItemsPerPage int,
    refreshInterval time.Duration,
) (*CatalogReportService, error) {
    return &CatalogReportService{
        formatter:       formatter,
        productService:  productService,
        catalogTitle:    catalogTitle,
        maxItemsPerPage: maxItemsPerPage,
        refreshInterval: refreshInterval,
    }, nil
}

type CatalogReportService struct {
    formatter       *ReportFormatter
    productService  *service.ProductService
    catalogTitle    string
    maxItemsPerPage int
    refreshInterval time.Duration
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
