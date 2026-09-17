package cli

import (
    "context"
    "fmt"
    "io"
    "os"

    melodyaudit "github.com/precision-soft/melody/integrations/bunorm/v3/audit"
    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const databaseResetFlagForce = "force"

/* DatabaseResetCommand brings this example's database back to the state a fresh volume would be in: the tables its migration set owns are dropped, the bun bookkeeping is dropped and recreated with them, the single schema migration is applied again, the audit trail is emptied and the nomenclature is reseeded.

   It exists because this application has no history. An example is not a project with a past — it has one state, the present one — so it carries no migration that repairs its own history and no changelog that records it. A database left in an older shape is answered HERE, by a command an operator runs deliberately, rather than by code every process pays for at boot.

   It is a command of the APPLICATION rather than of the bunorm/migrate module. Dropping an application's whole schema is not an operator door a published module should grow, and the two frozen majors carry the same command for the same reason: an example is not a published module, so this costs no public surface anywhere.

   The audit trail is emptied even though the migration set does not own its table, and the distinction is the point: the SCHEMA belongs to the module that opens it — the registry creates it through its own door and it survives a rollback of the set — while the ROWS belong to this application, which wrote them. A trail left standing across a reset names entities that no longer exist, over identifiers this application mints as the highest suffix plus one and therefore recycles: the next user-4 would inherit the history of the last one. The outbox is deliberately left alone: its module publishes no purge door, growing one on a published module is not a patch-size change, and its rows are messages waiting to be delivered rather than a picture of a state this command is restoring. */
type DatabaseResetCommand struct{}

func NewDatabaseResetCommand() *DatabaseResetCommand {
    return &DatabaseResetCommand{}
}

func (instance *DatabaseResetCommand) Name() string {
    return "example:db:reset"
}

func (instance *DatabaseResetCommand) Description() string {
    return "drops this example's schema, applies it again and reseeds it (requires --force)"
}

/* Flags declares --force because the command destroys every row this example holds. Without it the command says what it would do and exits zero, which is what makes a mistyped invocation harmless. */
func (instance *DatabaseResetCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{
        &melodyclicontract.BoolFlag{
            Name:  databaseResetFlagForce,
            Usage: "actually perform the reset; without it the command only prints what it would drop",
        },
    }
}

func (instance *DatabaseResetCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext melodyclicontract.Context) error {
    storage, resolveErr := melodycontainer.FromResolver[*persistence.CatalogStorage](
        runtimeInstance.Container(),
        persistence.ServiceCatalogStorage,
    )
    if nil != resolveErr {
        return resolveErr
    }

    /* the archive is resolved BEFORE the plan is printed, not before it is used: the plan has to name the archive's tables when there is an archive, and a plan that named what it would not touch — or stayed silent about what it would — is the one thing this refusal exists to prevent. */
    archiveStorage, archiveResolveErr := melodycontainer.FromResolver[*persistence.ArchiveStorage](
        runtimeInstance.Container(),
        persistence.ServiceArchiveStorage,
    )
    if nil != archiveResolveErr {
        return archiveResolveErr
    }

    /* the command is registered whether or not a database is configured, so the command surface does not change between environments; without one it fails with the reason instead of being quietly absent. The two databases are two switches, so the refusal is for the environment that wired NEITHER: an archive wired alone is reset alone, the way a catalogue wired alone is. */
    if false == storage.IsPersistent() && false == archiveStorage.IsPersistent() {
        return exception.NewError(
            "the example has no database configured, so there is nothing to reset",
            nil,
            nil,
        )
    }

    /* a nil writer falls back to stdout rather than to the io.Discard the framework's own banner uses: what this command prints is the list of what it is about to destroy, and a plan nobody sees is worse than no plan. */
    writer := commandContext.Writer()
    if nil == writer {
        writer = os.Stdout
    }

    printDatabaseResetPlan(writer, storage, archiveStorage)

    if false == commandContext.Bool(databaseResetFlagForce) {
        fmt.Fprintln(writer, "nothing was touched; pass --force to perform the reset")

        return nil
    }

    /* the runtime's context, not a background one: the drops run under the same signal every other command of this application honours, so an operator's interrupt is not the one thing a reset ignores */
    ctx := runtimeInstance.Context()

    /* the catalogue is brought whole — dropped, recreated, its trail emptied and reseeded — BEFORE the archive is touched: the archive is a second, independent database, and a refusal of it that returned between the catalogue's drop and its reseed left an empty catalogue behind a non-zero exit, with nobody able to log in until a second run. Each step reports itself as it completes, so what the operator reads after a failure is what HAPPENED, not only what was planned. */
    if true == storage.IsPersistent() {
        if resetErr := migration.Reset(ctx, storage.Database()); nil != resetErr {
            return databaseResetStepFailure("dropping and recreating the schema", "catalogue", databaseLocationLabel(storage.Location()), resetErr)
        }

        fmt.Fprintln(writer, "catalogue reset: the schema was dropped and recreated on "+databaseLocationLabel(storage.Location()))

        if trailErr := clearAuditTrail(ctx, storage); nil != trailErr {
            return databaseResetStepFailure("emptying the audit trail", "catalogue", databaseLocationLabel(storage.Location()), trailErr)
        }

        fmt.Fprintln(writer, "catalogue reset: the audit trail was emptied")

        if seedErr := repository.SeedAll(ctx, storage); nil != seedErr {
            return databaseResetStepFailure("reseeding the nomenclature", "catalogue", databaseLocationLabel(storage.Location()), seedErr)
        }

        fmt.Fprintln(writer, "catalogue reset: the nomenclature was reseeded")

        /* the cache is cleared HERE, as the catalogue's last step, not after the archive: the entities it holds
           are the catalogue's, and an archive that refuses between the reseed and a clear placed after it left
           the catalogue reseeded with every stale entry standing — the very account the reset removed still
           authenticating from the cache, the class the clear exists to close */
        if clearErr := clearCache(runtimeInstance, writer); nil != clearErr {
            /* the exit names what the failed clear left undone: the archive after it was not touched, and the
               catalogue before it was */
            if true == archiveStorage.IsPersistent() {
                return exception.NewError("database reset: clearing the cache did not complete; the catalogue was reset and the archive was not touched", nil, clearErr)
            }

            return clearErr
        }
    }

    /* the archive is a set of its own on a database of its own, so it is reset through its own door — and it is SKIPPED rather than refused when this environment wired no archive, the way the catalogue half is skipped when only the archive is wired. An operator who never configured postgres is not told their reset failed over a database they never asked for. */
    if true == archiveStorage.IsPersistent() {
        if archiveResetErr := migration.ResetArchive(ctx, archiveStorage.Database()); nil != archiveResetErr {
            return databaseResetStepFailure("dropping and recreating the reading archive", "archive", databaseLocationLabel(archiveStorage.Location()), archiveResetErr)
        }

        fmt.Fprintln(writer, "archive reset: the reading archive was dropped and recreated on "+databaseLocationLabel(archiveStorage.Location()))
    }

    /* an environment that wired the archive alone has no catalogue to reseed and nothing of its own in the cache;
       the cache is cleared all the same, so a reset leaves the same state whichever halves are wired */
    if false == storage.IsPersistent() {
        if clearErr := clearCache(runtimeInstance, writer); nil != clearErr {
            return clearErr
        }
    }

    return nil
}

/* clearCache empties the cache and says so. On the redis backend the clear is a SCAN of the whole keyspace filtered on this application's prefix, under the backend's one-second command budget — measured at 0 ms over the development keyspace, and declared here because a keyspace shared with much else could take the reset's exit code after both databases were reset. The state a fresh volume holds includes an EMPTY cache: the entities are cached under keys with no expiry and are cleared by name, by the listeners that watch the write events — and a reset writes through no door that dispatches one, so without this an account the reset removed kept authenticating on the login door with its old digest, from a cache nothing could clear afterwards. On the shared cache this reaches the running server; on the in-process fallback it reaches this process alone, which the line says. A clear that fails takes the exit code, and the only door that clears the cache again is this reset — a cache:clear command of its own is filed for the harvest. */
func clearCache(runtimeInstance melodyruntimecontract.Runtime, writer io.Writer) error {
    cacheInstance, cacheErr := melodycontainer.FromResolver[melodycachecontract.Cache](
        runtimeInstance.Container(),
        melodycache.ServiceCache,
    )
    if nil != cacheErr {
        return cacheErr
    }

    if clearErr := cacheInstance.Clear(); nil != clearErr {
        return exception.NewError("database reset: clearing the cache did not complete", nil, clearErr)
    }

    fmt.Fprintln(writer, "cache cleared: "+cacheClearedScope(runtimeInstance))

    return nil
}

/* databaseResetStepFailure names the step that did not complete and the database it did not complete on. The cli engine echoes the error's message alone, so the message carries both: a dial refusal that read "connection refused" over a host name told the operator neither that the catalogue had already been dropped nor which of the two databases had refused. */
func databaseResetStepFailure(step string, database string, location string, cause error) error {
    return exception.NewError(
        "database reset: "+step+" did not complete on the "+database+" database at "+location,
        exceptioncontract.Context{"step": step, "database": database, "location": location},
        cause,
    )
}

/* databaseResetPlanLineList names what the reset reaches, and WHERE. It is a list rather than a series of prints so that what the command SAYS it will destroy is readable by a test without capturing a stream, and it is printed on both paths on purpose: the refusal has to say what the flag would have unleashed, and the run has to leave the same lines in the log of whoever ran it.

   Each half names its database as the connection was declared — host, port and schema — because the tables alone do not: with MYSQL_DATABASE pointed at another major's schema the table list reads the same, the drops are no-ops there and the reseed creates this major's tables inside that database. The location is the one line that separates a reset of this example's volume from a reset of whatever the host happens to point at. */
func databaseResetPlanLineList(storage *persistence.CatalogStorage, archiveStorage *persistence.ArchiveStorage) []string {
    lineList := []string{"example:db:reset would drop and recreate:"}

    if true == storage.IsPersistent() {
        lineList = append(lineList, "on the catalogue database at "+databaseLocationLabel(storage.Location())+":")

        for _, table := range migration.SchemaTableNameList() {
            lineList = append(lineList, "  - "+table)
        }

        lineList = append(
            lineList,
            "  - the bun bookkeeping tables, so an older set's rows go with them",
            fmt.Sprintf("  - the rows of %s and %s, which this application wrote", persistence.AuditTable, melodyaudit.DefaultTransactionTable),
            "  and then reseed the nomenclature and clear the cache",
        )
    }

    /* the archive's tables are read from the archive set's own list, the way the catalogue's are read from the catalogue's: a table added to either schema cannot be left out of the plan by a second copy nobody updated. They are named only when there is an archive, because a plan that promised to drop a table on a database this environment never wired would be a lie in the one direction that matters. */
    if true == archiveStorage.IsPersistent() {
        lineList = append(lineList, "on the archive database at "+databaseLocationLabel(archiveStorage.Location())+":")

        for _, table := range migration.ArchiveTableNameList() {
            lineList = append(lineList, "  - "+table)
        }
    }

    /* the cache holds the catalogue's entities and is cleared as the catalogue's last step; an environment that
       wired the archive alone has no catalogue half, and the plan says the clear on its own line */
    if false == storage.IsPersistent() {
        lineList = append(lineList, "and then clear the cache")
    }

    return lineList
}

/* databaseLocationLabel is what the plan prints for a handle nobody located: the composition root locates both handles, so the fallback is a test's, and it is spelled as an absence rather than left blank so a blank in a plan is never mistaken for a database with no name. */
func databaseLocationLabel(location string) string {
    if "" == location {
        return "an unnamed location"
    }

    return location
}

func printDatabaseResetPlan(writer io.Writer, storage *persistence.CatalogStorage, archiveStorage *persistence.ArchiveStorage) {
    for _, line := range databaseResetPlanLineList(storage, archiveStorage) {
        fmt.Fprintln(writer, line)
    }
}

/* clearAuditTrail empties the two tables the audit registry keeps for this application. It deletes rows rather than dropping tables: the schema is the registry's, opened through its own door, and a reset of the application's state has no business taking it away.

   It asks that door to open the schema first. On this major the composition root is eager, so a repository has already created both tables by the time a command runs and the call is a no-op — but a delete that depends on that ordering would fail on the day it changes, over an application whose schema the reset has just restored, and EnsureSchema is the same door the storage itself calls. */
func clearAuditTrail(ctx context.Context, storage *persistence.CatalogStorage) error {
    if schemaErr := storage.EnsureAuditSchema(ctx); nil != schemaErr {
        return schemaErr
    }

    for _, table := range []string{persistence.AuditTable, melodyaudit.DefaultTransactionTable} {
        if _, execErr := storage.Database().ExecContext(ctx, "DELETE FROM `"+table+"`"); nil != execErr {
            return execErr
        }
    }

    return nil
}

var _ melodyclicontract.Command = (*DatabaseResetCommand)(nil)
