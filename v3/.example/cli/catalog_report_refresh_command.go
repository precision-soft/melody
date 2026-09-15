package cli

import (
    "errors"
    "fmt"
    "os"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/reporting"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
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

/* Run acquires the archive lock before reading, archiving and exporting. A held lock skips the whole run. Archive writes precede export, so sink failure preserves the row. An unreachable archive still permits reading and export, then returns its failure. */
func (instance *CatalogReportRefreshCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext melodyclicontract.Context) error {
    reportService, resolveErr := melodycontainer.FromResolverByType[*reporting.CatalogReportService](runtimeInstance.Container())
    if nil != resolveErr {
        return resolveErr
    }

    exporter, exporterErr := melodycontainer.FromResolverByType[*reporting.CatalogReportExporter](runtimeInstance.Container())
    if nil != exporterErr {
        return exporterErr
    }

    writer := commandContext.Writer()
    if nil == writer {
        writer = os.Stdout
    }

    archiveLock, archiveWired, lockErr := archiveLockOf(runtimeInstance)
    archiveFailure := lockErr

    if true == archiveWired && nil == archiveFailure {
        acquired, acquireErr := archiveLock.Acquire(runtimeInstance)
        if nil != acquireErr {
            return acquireErr
        }

        if false == acquired {
            fmt.Fprintln(writer, "another process is taking this reading right now; nothing was read, exported or recorded by this one")

            return nil
        }

        defer archiveLock.Release(runtimeInstance)
    }

    reading, refreshErr := reportService.Refresh(runtimeInstance.Context())
    if nil != refreshErr {
        return refreshErr
    }

    archived := false
    if true == archiveWired && nil == archiveFailure {
        recorded, archiveErr := reportService.Archive(runtimeInstance, reading)
        if nil != archiveErr {
            archiveFailure = exception.NewError("catalog report refresh: recording the reading in the archive did not complete", nil, archiveErr)
        }

        archived = recorded
    }

    exported, exportErr := exporter.Export(runtimeInstance, reading)

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

    writeErr := fprintTable(writer, headers, rows)

    if nil != exportErr {
        if true == archived {
            fmt.Fprintln(writer, "the archive holds this reading; the sink did not receive it")
        }

        if nil != archiveFailure {
            fmt.Fprintln(writer, "the sink did not receive this reading either: "+exportErr.Error())
        }

        if nil != writeErr {
            return errors.Join(exportErr, writeErr)
        }
        return exportErr
    }

    if nil != archiveFailure {
        fmt.Fprintln(writer, "the reading was taken and exported; the archive did not record it")

        if nil != writeErr {
            return errors.Join(archiveFailure, writeErr)
        }
        return archiveFailure
    }

    return writeErr
}

var _ melodyclicontract.Command = (*CatalogReportRefreshCommand)(nil)

const archiveLockName = "example.catalog.reading.archive"

const archiveLockTtl = 30 * time.Second

func archiveLockOf(runtimeInstance melodyruntimecontract.Runtime) (melodylockcontract.Lock, bool, error) {
    if false == runtimeInstance.Container().Has(persistence.ServiceArchiveLocker) {
        return nil, false, nil
    }

    locker, lockerErr := melodycontainer.FromResolver[melodylockcontract.Locker](
        runtimeInstance.Container(),
        persistence.ServiceArchiveLocker,
    )
    if nil != lockerErr {

        var ownException *exception.Error
        if true == errors.As(lockerErr, &ownException) {
            return nil, true, lockerErr
        }

        return nil, true, exception.NewError("catalog report refresh: the archive's locker could not be resolved", nil, lockerErr)
    }

    return locker.CreateLock(archiveLockName, archiveLockTtl), true, nil
}
