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

func (instance *DatabaseResetCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext melodyclicontract.Context) (runErr error) {
    storage, resolveErr := melodycontainer.FromResolver[*persistence.CatalogStorage](
        runtimeInstance.Container(),
        persistence.ServiceCatalogStorage,
    )
    if nil != resolveErr {
        return resolveErr
    }

    archiveStorage, archiveResolveErr := melodycontainer.FromResolver[*persistence.ArchiveStorage](
        runtimeInstance.Container(),
        persistence.ServiceArchiveStorage,
    )
    if nil != archiveResolveErr {
        return archiveResolveErr
    }

    if false == storage.IsPersistent() && false == archiveStorage.IsPersistent() {
        return exception.NewError(
            "the example has no database configured, so there is nothing to reset",
            nil,
            nil,
        )
    }

    writer := commandContext.Writer()
    if nil == writer {
        writer = os.Stdout
    }

    printDatabaseResetPlan(writer, storage, archiveStorage)

    if false == commandContext.Bool(databaseResetFlagForce) {
        fmt.Fprintln(writer, "nothing was touched; pass --force to perform the reset")

        return nil
    }

    finishCache, cacheErr := prepareDatabaseResetCache(runtimeInstance, writer)
    if nil != cacheErr { return cacheErr }
    defer finishCache(&runErr)

    ctx := runtimeInstance.Context()

    if true == storage.IsPersistent() {
        if resetErr := migration.Reset(ctx, storage.Database()); nil != resetErr {
            return databaseResetStepFailure("dropping and recreating the schema", "catalogue", storage.Location(), resetErr)
        }

        fmt.Fprintln(writer, "catalogue reset: the schema was dropped and recreated on "+storage.Location())

        if trailErr := clearAuditTrail(ctx, storage); nil != trailErr {
            return databaseResetStepFailure("emptying the audit trail", "catalogue", storage.Location(), trailErr)
        }

        fmt.Fprintln(writer, "catalogue reset: the audit trail was emptied")

        if seedErr := repository.SeedAll(ctx, storage); nil != seedErr {
            return databaseResetStepFailure("reseeding the nomenclature", "catalogue", storage.Location(), seedErr)
        }

        fmt.Fprintln(writer, "catalogue reset: the nomenclature was reseeded")
    }

    if true == archiveStorage.IsPersistent() {
        if archiveResetErr := migration.ResetArchive(ctx, archiveStorage.Database()); nil != archiveResetErr {
            return databaseResetStepFailure("dropping and recreating the reading archive", "archive", archiveStorage.Location(), archiveResetErr)
        }

        fmt.Fprintln(writer, "archive reset: the reading archive was dropped and recreated on "+archiveStorage.Location())
    }

    return nil
}

func databaseResetStepFailure(step string, database string, location string, cause error) error {
    return exception.NewError(
        "database reset: "+step+" did not complete on the "+database+" database at "+location,
        exceptioncontract.Context{"step": step, "database": database, "location": location},
        cause,
    )
}

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
            "  and then reseed the nomenclature",
        )
    }

    if true == archiveStorage.IsPersistent() {
        lineList = append(lineList, "on the archive database at "+databaseLocationLabel(archiveStorage.Location())+":")

        for _, table := range migration.ArchiveTableNameList() {
            lineList = append(lineList, "  - "+table)
        }
    }

    return append(lineList, "and then clear the cache")
}

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
