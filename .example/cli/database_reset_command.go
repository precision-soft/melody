package cli

import (
    "fmt"
    "io"
    "os"

    "github.com/precision-soft/melody/.example/migration"
    "github.com/precision-soft/melody/.example/repository"
    melodycache "github.com/precision-soft/melody/cache"
    melodycachecontract "github.com/precision-soft/melody/cache/contract"
    melodyclicontract "github.com/precision-soft/melody/cli/contract"
    melodycontainer "github.com/precision-soft/melody/container"
    melodyexception "github.com/precision-soft/melody/exception"
    melodyexceptioncontract "github.com/precision-soft/melody/exception/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/uptrace/bun"
)

const databaseResetFlagForce = "force"

/* DatabaseResetCommand brings this example's databases back to the state a fresh volume would be in: the tables each migration set owns are dropped, the bun bookkeeping is dropped and recreated with them, the single schema migration of each set is applied again, the nomenclature is reseeded and the cache is cleared.

   It exists because this application has no history. An example is not a project with a past — it has one state, the present one — so it carries no migration that repairs its own history and no changelog that records it. A database left in an older shape is answered HERE, by a command an operator runs deliberately, rather than by code every process pays for at boot.

   It is a command of the APPLICATION rather than of the bunorm/migrate module. Dropping an application's whole schema is not an operator door a published module should grow, least of all on a major that is sealed; an example is not a published module, so this costs no public surface anywhere.

   The database service names and their locations are handed in at registration rather than read from the configuration package, which imports this one. An empty catalog name is how the configuration says there is no connection, and the command then fails with the reason instead of being quietly absent; an empty journal name says only that the journal was not wired, which on this major is a switch of its own. The locations — host:port/schema, as each connection was declared — are what the plan prints: a plan that named tables and never the database they live in could not tell a reset of this example's volume from a reset of whatever MYSQL_DATABASE happens to point at. */
type DatabaseResetCommand struct {
    databaseServiceName        string
    databaseLocation           string
    journalDatabaseServiceName string
    journalDatabaseLocation    string
}

func NewDatabaseResetCommand(
    databaseServiceName string,
    databaseLocation string,
    journalDatabaseServiceName string,
    journalDatabaseLocation string,
) *DatabaseResetCommand {
    return &DatabaseResetCommand{
        databaseServiceName:        databaseServiceName,
        databaseLocation:           databaseLocation,
        journalDatabaseServiceName: journalDatabaseServiceName,
        journalDatabaseLocation:    journalDatabaseLocation,
    }
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

func (instance *DatabaseResetCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext *melodyclicontract.CommandContext) error {
    if "" == instance.databaseServiceName {
        return melodyexception.NewError(
            "the example has no database configured, so there is nothing to reset",
            nil,
            nil,
        )
    }

    /* a nil writer falls back to stdout rather than to the io.Discard the framework's own banner uses: what this command prints is the list of what it is about to destroy, and a plan nobody sees is worse than no plan. */
    writer := commandContext.Writer
    if nil == writer {
        writer = os.Stdout
    }

    printDatabaseResetPlan(writer, instance.databaseLocation, instance.journalDatabaseServiceName, instance.journalDatabaseLocation)

    if false == commandContext.Bool(databaseResetFlagForce) {
        fmt.Fprintln(writer, "nothing was touched; pass --force to perform the reset")

        return nil
    }

    /* the runtime's context, not a background one: the drops run under the same signal every other command of this application honours, so an operator's interrupt is not the one thing a reset ignores — and the price is declared: the first interrupt cuts the drops where they stand, and a second run brings the volume the rest of the way */
    ctx := runtimeInstance.Context()

    database, resolveErr := melodycontainer.FromResolver[*bun.DB](
        runtimeInstance.Container(),
        instance.databaseServiceName,
    )
    if nil != resolveErr {
        return resolveErr
    }

    /* the catalogue is brought whole — dropped, recreated and reseeded — and its cache cleared BEFORE the journal is touched: the journal is a second, independent database, and a refusal of it that returned between the catalogue's drop and its reseed would leave an empty catalogue behind a non-zero exit, with nobody able to log in until a second run. Each step reports itself as it completes, so what the operator reads after a failure is what HAPPENED, not only what was planned. */
    if resetErr := migration.Reset(ctx, database); nil != resetErr {
        return databaseResetStepFailure("dropping and recreating the schema", "catalogue", instance.databaseLocation, resetErr)
    }

    fmt.Fprintln(writer, "catalogue reset: the schema was dropped and recreated on "+instance.databaseLocation)

    if seedErr := repository.SeedAll(ctx, database); nil != seedErr {
        return databaseResetStepFailure("reseeding the nomenclature", "catalogue", instance.databaseLocation, seedErr)
    }

    fmt.Fprintln(writer, "catalogue reset: the nomenclature was reseeded")

    if clearErr := clearCache(runtimeInstance, writer); nil != clearErr {
        return clearErr
    }

    /* the journal is a database of its own, with a set of its own, and it is append-only: a reset of it is the schema alone, because an empty journal IS its opening state. */
    if "" != instance.journalDatabaseServiceName {
        journalDatabase, journalResolveErr := melodycontainer.FromResolver[*bun.DB](
            runtimeInstance.Container(),
            instance.journalDatabaseServiceName,
        )
        if nil != journalResolveErr {
            return journalResolveErr
        }

        if journalResetErr := migration.ResetJournal(ctx, journalDatabase); nil != journalResetErr {
            return databaseResetStepFailure("dropping and recreating the journal", "journal", instance.journalDatabaseLocation, journalResetErr)
        }

        fmt.Fprintln(writer, "journal reset: the journal table was dropped and recreated on "+instance.journalDatabaseLocation)
    }

    return nil
}

/* clearCache empties the cache and says so. The state a fresh volume holds includes an EMPTY cache: the entities are cached under keys with no expiry and are cleared by name, by the listeners that watch the write events — and a reset writes through no door that dispatches one, so without this an account the reset removed kept authenticating on the login door with its old digest, from a cache nothing could clear afterwards. On the shared cache this reaches the running server; on the in-process fallback it reaches this process alone, which the line says. A clear that fails takes the exit code, and the only door that clears the cache again is this reset. */
func clearCache(runtimeInstance melodyruntimecontract.Runtime, writer io.Writer) error {
    cacheInstance, cacheErr := melodycontainer.FromResolver[melodycachecontract.Cache](
        runtimeInstance.Container(),
        melodycache.ServiceCache,
    )
    if nil != cacheErr {
        return cacheErr
    }

    if clearErr := cacheInstance.Clear(); nil != clearErr {
        return melodyexception.NewError("database reset: clearing the cache did not complete", nil, clearErr)
    }

    fmt.Fprintln(writer, "cache cleared: "+cacheClearedScope(runtimeInstance))

    return nil
}

/* databaseResetStepFailure names the step that did not complete and the database it did not complete on. The cli engine echoes the error's message alone, so the message carries both: a dial refusal that read "connection refused" over a host name told the operator neither that the catalogue had already been dropped nor which of the two databases had refused. */
func databaseResetStepFailure(step string, database string, location string, cause error) error {
    return melodyexception.NewError(
        "database reset: "+step+" did not complete on the "+database+" database at "+location,
        melodyexceptioncontract.Context{"step": step, "database": database, "location": location},
        cause,
    )
}

/* databaseResetPlanLineList names what the reset reaches and WHERE, and it is printed on both paths on purpose: the refusal has to say what the flag would have unleashed, and the run has to leave the same lines in the log of whoever ran it. It is a list rather than a series of prints so that what the command SAYS it will destroy is readable by a test without capturing a stream. */
func databaseResetPlanLineList(databaseLocation string, journalDatabaseServiceName string, journalDatabaseLocation string) []string {
    lineList := []string{"example:db:reset would drop and recreate the tables the migration sets own on " + databaseLocation + ":"}

    for _, table := range migration.SchemaTableNameList() {
        lineList = append(lineList, "  - "+table)
    }

    lineList = append(lineList, "  - the bun bookkeeping tables of each database, so an older set's rows go with them")

    if "" == journalDatabaseServiceName {
        lineList = append(lineList, "the journal database is not wired, so its set is left alone")
    } else {
        lineList = append(lineList, "  - the journal table on "+journalDatabaseLocation+", an empty journal being its opening state")
    }

    return append(lineList, "and then reseed the nomenclature and clear the cache")
}

func printDatabaseResetPlan(writer io.Writer, databaseLocation string, journalDatabaseServiceName string, journalDatabaseLocation string) {
    for _, line := range databaseResetPlanLineList(databaseLocation, journalDatabaseServiceName, journalDatabaseLocation) {
        fmt.Fprintln(writer, line)
    }
}

var _ melodyclicontract.Command = (*DatabaseResetCommand)(nil)
