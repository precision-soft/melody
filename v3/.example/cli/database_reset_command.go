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
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const databaseResetFlagForce = "force"

/* DatabaseResetCommand rebuilds this example's migration-owned schema and Bun bookkeeping, clears application audit rows, and reseeds the catalogue. It requires --force and belongs to the example application, not the migration module.

   Audit schema belongs to its module, but old audit rows must not survive reuse of example entity identifiers. Pending outbox messages are retained; this command does not purge them. */
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

    /* the command is registered whether or not a database is configured, so the command surface does not change between environments; without one it fails with the reason instead of being quietly absent. */
    if false == storage.IsPersistent() {
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

    /* the archive is resolved BEFORE the plan is printed, not before it is used: the plan has to name the archive's tables when there is an archive, and a plan that named what it would not touch — or stayed silent about what it would — is the one thing this refusal exists to prevent. */
    archiveStorage, archiveResolveErr := melodycontainer.FromResolver[*persistence.ArchiveStorage](
        runtimeInstance.Container(),
        persistence.ServiceArchiveStorage,
    )
    if nil != archiveResolveErr {
        return archiveResolveErr
    }

    printDatabaseResetPlan(writer, archiveStorage.IsPersistent())

    if false == commandContext.Bool(databaseResetFlagForce) {
        fmt.Fprintln(writer, "nothing was touched; pass --force to perform the reset")

        return nil
    }

    ctx := context.Background()

    if resetErr := migration.Reset(ctx, storage.Database()); nil != resetErr {
        return resetErr
    }

    /* the archive is a set of its own on a database of its own, so it is reset through its own door — and it is SKIPPED rather than refused when this environment wired no archive, the way the catalogue half would be if the two switches were the other way round. An operator who never configured postgres is not told their reset failed over a database they never asked for. */
    if true == archiveStorage.IsPersistent() {
        if archiveResetErr := migration.ResetArchive(ctx, archiveStorage.Database()); nil != archiveResetErr {
            return archiveResetErr
        }
    }

    if trailErr := clearAuditTrail(ctx, storage); nil != trailErr {
        return trailErr
    }

    if seedErr := repository.SeedAll(ctx, storage); nil != seedErr {
        return seedErr
    }

    fmt.Fprintln(writer, "database reset: the schema was recreated, the audit trail emptied and the nomenclature reseeded")

    if true == archiveStorage.IsPersistent() {
        fmt.Fprintln(writer, "archive reset: the reading archive was dropped and recreated")
    }

    return nil
}

/* databaseResetPlanLineList names what the reset reaches. It is a list rather than a series of prints so that what the command SAYS it will destroy is readable by a test without capturing a stream, and it is printed on both paths on purpose: the refusal has to say what the flag would have unleashed, and the run has to leave the same lines in the log of whoever ran it. */
func databaseResetPlanLineList(archiveWired bool) []string {
    lineList := []string{"example:db:reset would drop and recreate the tables the migration set owns:"}

    for _, table := range migration.SchemaTableNameList() {
        lineList = append(lineList, "  - "+table)
    }

    /* the archive's tables are read from the archive set's own list, the way the catalogue's are read from the catalogue's: a table added to either schema cannot be left out of the plan by a second copy nobody updated. They are named only when there is an archive, because a plan that promised to drop a table on a database this environment never wired would be a lie in the one direction that matters. */
    if true == archiveWired {
        for _, table := range migration.ArchiveTableNameList() {
            lineList = append(lineList, "  - "+table+" (on the archive database)")
        }
    }

    return append(
        lineList,
        "  - the bun bookkeeping tables, so an older set's rows go with them",
        fmt.Sprintf("  - the rows of %s and %s, which this application wrote", persistence.AuditTable, melodyaudit.DefaultTransactionTable),
        "and then reseed the nomenclature",
    )
}

func printDatabaseResetPlan(writer io.Writer, archiveWired bool) {
    for _, line := range databaseResetPlanLineList(archiveWired) {
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
