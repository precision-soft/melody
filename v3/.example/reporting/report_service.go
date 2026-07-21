package reporting

import (
    "fmt"
    "time"

    "github.com/precision-soft/melody/v3/.example/service"
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
