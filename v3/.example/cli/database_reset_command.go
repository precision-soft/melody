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
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const databaseResetFlagForce = "force"

/* DatabaseResetCommand brings this example's databases back to the state a fresh volume would be in: the tables its migration sets own are dropped, the bun bookkeeping is recreated, the schema is applied again, the audit trail is emptied and the nomenclature reseeded. An example has one state and no migration that repairs its history, so an older volume is answered here, by a command an operator runs deliberately; it is the application's command because dropping a whole schema is no door a published module should grow. The audit rows are emptied because identifiers are minted as the highest suffix plus one and recycle, so a trail left standing would give the next user-4 the last one's history; the outbox is left alone, its rows being messages still to deliver. */
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

    /* the catalogue is brought whole (dropped, recreated, its trail emptied and reseeded) before the archive is touched, so a refusal of the independent archive cannot leave an empty catalogue behind a non-zero exit. Each step reports itself as it completes, so after a failure the operator reads what happened, not only what was planned. */
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

        /* the cache is cleared here, as the catalogue's last step, because the entities it holds are the catalogue's: a clear placed after the archive would leave every stale entry standing when the archive refuses, the removed account still authenticating from the cache */
        if clearErr := clearCache(runtimeInstance, writer, "database reset"); nil != clearErr {
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
        if clearErr := clearCache(runtimeInstance, writer, "database reset"); nil != clearErr {
            return clearErr
        }
    }

    return nil
}

/* databaseResetStepFailure names the step that did not complete and the database it did not complete on, in the message, because the cli engine echoes the message alone and a bare "connection refused" says neither that the catalogue is already dropped nor which database refused. */
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

/* clearAuditTrail empties the two tables the audit registry keeps for this application, deleting rows rather than dropping tables because the schema is the registry's. It asks the registry's door to open the schema first, so the delete does not depend on a repository having created the tables before the command runs. */
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
