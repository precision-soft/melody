package cli

import (
    "time"

    "github.com/precision-soft/melody/v3/.example/reporting"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type CatalogReportRefreshCommand struct{}

func NewCatalogReportRefreshCommand() *CatalogReportRefreshCommand {
    return &CatalogReportRefreshCommand{}
}

func (instance *CatalogReportRefreshCommand) Name() string {
    return "catalog:report:refresh"
}

func (instance *CatalogReportRefreshCommand) Description() string {
    return "takes a fresh reading of the catalogue and leaves it in the cache"
}

func (instance *CatalogReportRefreshCommand) Flags() []melodyclicontract.Flag {
    return nil
}

/* Run is what the schedule calls. The reading is cheap enough to take inside a request, but the request that
finds a cold cache is the one that pays for it, so the catalogue is read on a timer instead and every request
finds a warm answer.

The service is resolved by type: it is one of the constructors melody:wiring:generate found in the reporting
package, so it carries no service name of its own. */
func (instance *CatalogReportRefreshCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext melodyclicontract.Context) error {
    reportService, resolveErr := melodycontainer.FromResolverByType[*reporting.CatalogReportService](runtimeInstance.Container())
    if nil != resolveErr {
        return resolveErr
    }

    reading, refreshErr := reportService.Refresh(runtimeInstance.Context())
    if nil != refreshErr {
        return refreshErr
    }

    headers := []string{
        "RECORDED_AT",
        "HEADLINE",
        "PAYLOAD",
    }

    rows := [][]string{
        {
            reading.RecordedAt.UTC().Format(time.RFC3339),
            reading.Headline,
            reading.Payload,
        },
    }

    /* the same table helper product:list prints through, so the commands render alike */
    printTable(headers, rows)

    return nil
}

var _ melodyclicontract.Command = (*CatalogReportRefreshCommand)(nil)
