package cli

import (
    "strconv"
    "time"

    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/reporting"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodylockcontract "github.com/precision-soft/melody/v3/lock/contract"
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

    archived, archiveErr := archiveReading(runtimeInstance, reportService, reading)
    if nil != archiveErr {
        return archiveErr
    }

    headers := []string{
        "RECORDED_AT",
        "HEADLINE",
        "PAYLOAD",
        "EXPORTED",
        "ARCHIVED",
    }

    rows := [][]string{
        {
            reading.RecordedAt.UTC().Format(time.RFC3339),
            reading.Headline,
            reading.Payload,
            strconv.FormatBool(exported),
            strconv.FormatBool(archived),
        },
    }

    /* the same table helper product:list prints through, so the commands render alike */
    printTable(headers, rows)

    return nil
}

var _ melodyclicontract.Command = (*CatalogReportRefreshCommand)(nil)

/* archiveLockName is the one name the archive's writers contend on. It is a constant rather than a literal because the lock is only exclusion if every writer spells it identically — a second writer with a different spelling takes a different lock and both proceed. */
const archiveLockName = "example.catalog.reading.archive"

/* archiveLockTtl is accepted by CreateLock for interface compatibility and is not honoured as an expiry by this backend: a postgres session advisory lock lives exactly as long as the backend session that took it, so a process that dies mid-refresh releases it when its connection drops. It is written as a real duration all the same, because the locker contract takes one and a zero would read as a decision nobody made. */
const archiveLockTtl = 30 * time.Second

/* archiveReading records the reading this run took, under the archive's own advisory lock, and answers whether this process is the one that wrote it.

   The lock is what makes a schedule running on several processes record ONE reading rather than one each. It is taken by name from the container rather than through the framework's general locker, because that one is redis when redis is configured and the archive is not on redis: a lock held in the very database being written is exclusion that cannot disagree with the write it guards.

   A lock this process could not take is not a failure. It means another process is taking the same reading right now, so the archive will hold it either way; this run reports that it did not write. That is the same answer, and for the same reason, as an instant already recorded.

   With no archive wired there is no locker service and nothing to record, so the whole step is skipped: an environment without postgres refreshes and exports exactly as it did before. */
func archiveReading(
    runtimeInstance melodyruntimecontract.Runtime,
    reportService *reporting.CatalogReportService,
    reading *reporting.CatalogReading,
) (bool, error) {
    locker, lockerErr := melodycontainer.FromResolver[melodylockcontract.Locker](
        runtimeInstance.Container(),
        persistence.ServiceArchiveLocker,
    )
    if nil != lockerErr {
        return false, nil
    }

    archiveLock := locker.CreateLock(archiveLockName, archiveLockTtl)

    acquired, acquireErr := archiveLock.Acquire(runtimeInstance)
    if nil != acquireErr {
        return false, acquireErr
    }

    if false == acquired {
        return false, nil
    }

    defer archiveLock.Release(runtimeInstance)

    return reportService.Archive(runtimeInstance.Context(), reading)
}
