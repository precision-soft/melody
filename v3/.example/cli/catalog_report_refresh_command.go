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

/* Run is what the schedule calls. The reading is cheap enough to take inside a request, but the request that finds a cold cache is the one that pays for it, so the catalogue is read on a timer instead and every request finds a warm answer.

   The service is resolved by type: it is one of the constructors melody:wiring:generate found in the reporting package, so it carries no service name of its own.

   The run is one unit under the archive's lock, taken FIRST: a process that cannot take it skips the run whole — no reading, no export, no row — and says so, because another process is taking this reading right now and the archive will hold it either way. The archive is written BEFORE the export: it is the durable half and depends on nothing the sink does, where an export refused first used to return at once and leave no row for a reading the next tick could not retake. The sink's refusal still takes the exit code, after the row is there; an archive that cannot be reached at all takes it after the reading and the export, which need nothing from postgres. */
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

    /* an archive that cannot be REACHED — its locker resolves by opening the postgres handle — does not take the reading and the export with it: the run goes on without the archive and its failure takes the exit code at the end, after the two halves that do not depend on postgres have been done. The old form read that failure as "no archive wired" and exited zero over a reading nobody recorded. */
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

    if nil != exportErr {
        if true == archived {
            fmt.Fprintln(writer, "the archive holds this reading; the sink did not receive it")
        }

        if nil != archiveFailure {
            /* both halves failed: the console names the archive's refusal — the one that names the database —
               beside the sink's, and the exit carries both, so neither failure hides the other */
            fmt.Fprintln(writer, "the archive did not record this reading: "+archiveFailure.Error())
            fmt.Fprintln(writer, "the sink did not receive it either: "+exportErr.Error())

            return errors.Join(archiveFailure, exportErr)
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

/* archiveOwnRefusalOrWrapped hands back this application's own refusal as it is — the archive's database named
   with where it is, the migration step that did not complete — so the console line, which the cli engine
   renders from the message alone, keeps naming the database; anything else is wrapped with what was being
   done. It is the rule archiveLockOf already keeps for the lock's resolution, applied to the two other doors
   the archive is reached through. */
func archiveOwnRefusalOrWrapped(headline string, cause error) error {
    var ownException *exception.Error
    if true == errors.As(cause, &ownException) {
        return cause
    }

    return exception.NewError(headline, nil, cause)
}

var _ melodyclicontract.Command = (*CatalogReportRefreshCommand)(nil)

/* archiveLockName is the one name the archive's writers contend on. It is a constant rather than a literal because the lock is only exclusion if every writer spells it identically — a second writer with a different spelling takes a different lock and both proceed. */
const archiveLockName = "example.catalog.reading.archive"

/* archiveLockTtl is accepted by CreateLock for interface compatibility and is not honoured as an expiry by the postgres backend: a session advisory lock lives exactly as long as the backend session that took it, so a process that dies mid-refresh releases it when its connection drops. It is written as a real duration all the same, because the locker contract takes one, the in-process locker honours it, and a zero would read as a decision nobody made. */
const archiveLockTtl = 30 * time.Second

/* archiveLockOf hands back the lock the run is taken under.

   The lock is what makes processes that OVERLAP on one schedule record ONE reading between them rather than one each: it is held around the whole run, so the loser takes no reading at all. Two runs that do not overlap — one host's tick a second after another's — are two readings, keyed on the instant each took, and the archive holds both; the identity of a reading is the second it was taken at, and the lock does not change that. It is taken by name from the container rather than through the framework's general locker, because that one is redis when redis is configured and the archive is not on redis: a lock held in the very database being written is exclusion that cannot disagree with the write it guards. Without postgres the locker under this name is the in-process one, over the in-process archive.

   The locker is registered under this name on EVERY wiring — the archive's advisory lock with postgres, the in-process one without — so there is no "not wired" to ask the container about; resolving it OPENS the postgres handle when there is one, so a refusal of that resolution is the archive being unreachable, which is handed back, where it used to read as "no archive" and exit zero over a reading that was never recorded. */
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
