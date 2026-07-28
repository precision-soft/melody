package cli

import (
    "time"

    "github.com/precision-soft/melody/.example/service"
    melodyclicontract "github.com/precision-soft/melody/cli/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
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

/* Run is what the schedule calls. The report is cheap enough to compute inside a request, but the request that finds a cold cache is the one that pays for it, so the catalogue is read on a timer instead and every request finds a warm answer.

Without a cache backend the reading is still taken and printed: the command reports the state of the catalogue whether or not there is anywhere to leave the answer. */
func (instance *CatalogReportRefreshCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext *melodyclicontract.CommandContext) error {
    reportService := service.MustGetCatalogReportService(runtimeInstance.Container())

    report, refreshErr := reportService.Refresh(runtimeInstance.Context())
    if nil != refreshErr {
        return refreshErr
    }

    headers := []string{
        "RECORDED_AT",
        "PAYLOAD",
    }

    rows := [][]string{
        {
            report.RecordedAt.UTC().Format(time.RFC3339),
            report.Payload,
        },
    }

    /* the same table helper product:list prints through, so the commands render alike */
    printTable(headers, rows)

    return nil
}

var _ melodyclicontract.Command = (*CatalogReportRefreshCommand)(nil)
