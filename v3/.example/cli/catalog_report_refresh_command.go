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

/* Run is what the schedule calls, so every request finds a warm reading rather than paying for it on a cold cache. The run is one unit under the archive's lock, taken first: a process that cannot take it skips the whole run and says so. The archive is written before the export, because it is the durable half and depends on nothing the sink does; the sink's refusal takes the exit code after the row is there, and an unreachable archive takes it after the reading and the export. */
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

    /* an archive that cannot be reached (its locker resolves by opening the postgres handle) does not take the reading and the export with it: the run goes on without the archive, and its failure takes the exit code at the end */
    archiveLock, lockErr := archiveLockOf(runtimeInstance)
    archiveFailure := lockErr

    if nil == archiveFailure {
        acquired, acquireErr := archiveLock.Acquire(runtimeInstance)
        if nil != acquireErr {
            /* an ERROR taking the lock — the connection lost between the open and the advisory lock, a pool that
               refused — is the archive being unreachable, the same case as a refusal of the open above: the
               reading and the export go on without the archive, and the failure takes the exit code at the end */
            archiveFailure = archiveOwnRefusalOrWrapped("catalog report refresh: the archive's lock could not be taken", acquireErr)
        } else if false == acquired {
            fmt.Fprintln(writer, "another process is taking this reading right now; nothing was read, exported or recorded by this one")

            return nil
        } else {
            defer archiveLock.Release(runtimeInstance)
        }
    }

    reading, refreshErr := reportService.Refresh(runtimeInstance.Context())
    if nil != refreshErr {
        /* a reading that fails after the archive has refused carries both, so the second failure does not hide the first, which is the one that names the database */
        if nil != archiveFailure {
            return exception.NewError(
                "catalog report refresh: "+refreshErr.Error()+"; "+archiveFailure.Error(),
                nil,
                errors.Join(refreshErr, archiveFailure),
            )
        }

        return refreshErr
    }

    archived := false
    if nil == archiveFailure {
        recorded, archiveErr := reportService.Archive(runtimeInstance, reading)
        if nil != archiveErr {
            archiveFailure = archiveOwnRefusalOrWrapped("catalog report refresh: recording the reading in the archive did not complete", archiveErr)
        }

        archived = recorded
    }

    /* the export is the second half of a refresh rather than a command of its own: the reading this run took
       is the one a sink wants, and a separate command would either retake it or push whatever the cache
       happened to hold. With no endpoint configured it does nothing, and a sink that refuses takes the
       command's exit code with it — an export nobody received is not a refresh that worked. */
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

    /* the same table helper product:list prints through, rendered into the command's own writer so the
       section that drives this command can read what it printed; printed BEFORE the sink's refusal is
       returned, so the operator reads that the archive holds the reading the sink did not receive */
    fprintTable(writer, headers, rows)

    /* ARCHIVED false with no failure is the archive already holding a reading taken at this instant, two runs inside one second, which exits zero; the console says so, since the table alone cannot tell it from an archive that did not record the reading */
    if nil == archiveFailure && false == archived {
        fmt.Fprintln(writer, "the archive already holds a reading taken at this instant; nothing new was recorded")
    }

    if nil != exportErr {
        if true == archived {
            fmt.Fprintln(writer, "the archive holds this reading; the sink did not receive it")
        }

        if nil != archiveFailure {
            /* both halves failed: the console names the archive's refusal — the one that names the database —
               beside the sink's, and the exit carries both, so neither failure hides the other */
            fmt.Fprintln(writer, "the archive did not record this reading: "+archiveFailure.Error())
            fmt.Fprintln(writer, "the sink did not receive it either: "+exportErr.Error())

            /* one message on one line, since the cli engine escapes a newline in a failure's message, with the join as the cause, so errors.Is reaches either half */
            return exception.NewError(
                "catalog report refresh: "+archiveFailure.Error()+"; "+exportErr.Error(),
                nil,
                errors.Join(archiveFailure, exportErr),
            )
        }

        return exportErr
    }

    if nil != archiveFailure {
        if true == exported {
            fmt.Fprintln(writer, "the reading was taken and exported; the archive did not record it")
        } else {
            fmt.Fprintln(writer, "the reading was taken; no sink is configured, and the archive did not record it")
        }

        return archiveFailure
    }

    return nil
}

/* archiveOwnRefusalOrWrapped wraps a refusal of the archive with what was being done and carries the refusal's own words in the message: the cli engine renders a failure from its message alone, so the operator reads the step and whatever the refusal named, the database, the migration step or the advisory lock. The refusal stays the cause, so errors.Is still reaches it. */
func archiveOwnRefusalOrWrapped(headline string, cause error) error {
    return exception.NewError(headline+": "+cause.Error(), nil, cause)
}

var _ melodyclicontract.Command = (*CatalogReportRefreshCommand)(nil)

/* archiveLockName is the one name the archive's writers contend on. It is a constant rather than a literal because the lock is only exclusion if every writer spells it identically — a second writer with a different spelling takes a different lock and both proceed. */
const archiveLockName = "example.catalog.reading.archive"

/* archiveLockTtl is accepted by CreateLock for interface compatibility and is not honoured as an expiry by the postgres backend: a session advisory lock lives exactly as long as the backend session that took it, so a process that dies mid-refresh releases it when its connection drops. It is written as a real duration all the same, because the locker contract takes one, the in-process locker honours it, and a zero would read as a decision nobody made. */
const archiveLockTtl = 30 * time.Second

/* archiveLockOf hands back the lock the run is taken under, so processes that overlap on one schedule record one reading between them; runs that do not overlap are two readings, keyed on the second each took. It is taken by name rather than through the general locker, which is redis when configured, because a lock held in the database being written cannot disagree with the write it guards; without postgres the locker under this name is the in-process one. It is registered on every wiring, so resolving it opens the postgres handle when there is one, and a refusal of that resolution is the archive being unreachable and is handed back. */
func archiveLockOf(runtimeInstance melodyruntimecontract.Runtime) (melodylockcontract.Lock, error) {
    locker, lockerErr := melodycontainer.FromResolver[melodylockcontract.Locker](
        runtimeInstance.Container(),
        persistence.ServiceArchiveLocker,
    )
    if nil != lockerErr {
        /* a refusal of this application's own — the archive's database named with where it is — is handed back as it is, so the console line names the database; anything else is wrapped with what was being resolved */
        return nil, archiveOwnRefusalOrWrapped("catalog report refresh: the archive's locker could not be resolved", lockerErr)
    }

    return locker.CreateLock(archiveLockName, archiveLockTtl), nil
}
