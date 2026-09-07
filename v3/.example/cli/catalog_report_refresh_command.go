package cli

import (
    "strconv"
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

/* Run is what the schedule calls. The reading is cheap enough to take inside a request, but the request that finds a cold cache is the one that pays for it, so the catalogue is read on a timer instead and every request finds a warm answer.

   The service is resolved by type: it is one of the constructors melody:wiring:generate found in the reporting package, so it carries no service name of its own. */
func (instance *CatalogReportRefreshCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext melodyclicontract.Context) error {
    reportService, resolveErr := melodycontainer.FromResolverByType[*reporting.CatalogReportService](runtimeInstance.Container())
    if nil != resolveErr {
        return resolveErr
    }

    reading, refreshErr := reportService.Refresh(runtimeInstance.Context())
    if nil != refreshErr {
        return refreshErr
    }

    /* the export is the second half of a refresh rather than a command of its own: the reading this run took
       is the one a sink wants, and a separate command would either retake it or push whatever the cache
       happened to hold. With no endpoint configured it does nothing, and a sink that refuses takes the
       command's exit code with it — an export nobody received is not a refresh that worked. */
    exporter, exporterErr := melodycontainer.FromResolverByType[*reporting.CatalogReportExporter](runtimeInstance.Container())
    if nil != exporterErr {
        return exporterErr
    }

    exported, exportErr := exporter.Export(runtimeInstance, reading)
    if nil != exportErr {
        return exportErr
    }

    headers := []string{
        "RECORDED_AT",
        "HEADLINE",
        "PAYLOAD",
        "EXPORTED",
    }

    rows := [][]string{
        {
            reading.RecordedAt.UTC().Format(time.RFC3339),
            reading.Headline,
            reading.Payload,
            strconv.FormatBool(exported),
        },
    }

    /* the same table helper product:list prints through, so the commands render alike */
    printTable(headers, rows)

    return nil
}

var _ melodyclicontract.Command = (*CatalogReportRefreshCommand)(nil)
