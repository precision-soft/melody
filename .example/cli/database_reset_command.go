package cli

import (
    "context"
    "fmt"
    "io"
    "os"

    "github.com/precision-soft/melody/.example/migration"
    "github.com/precision-soft/melody/.example/repository"
    melodyclicontract "github.com/precision-soft/melody/cli/contract"
    melodycontainer "github.com/precision-soft/melody/container"
    melodyexception "github.com/precision-soft/melody/exception"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/uptrace/bun"
)

const databaseResetFlagForce = "force"

/* DatabaseResetCommand brings this example's databases back to the state a fresh volume would be in: the tables each migration set owns are dropped, the bun bookkeeping is dropped and recreated with them, the single schema migration of each set is applied again, and the nomenclature is reseeded.

   It exists because this application has no history. An example is not a project with a past — it has one state, the present one — so it carries no migration that repairs its own history and no changelog that records it. A database left in an older shape is answered HERE, by a command an operator runs deliberately, rather than by code every process pays for at boot.

   It is a command of the APPLICATION rather than of the bunorm/migrate module. Dropping an application's whole schema is not an operator door a published module should grow, least of all on a major that is sealed; an example is not a published module, so this costs no public surface anywhere.

   The database service names are handed in at registration rather than read from the configuration package, which imports this one. An empty catalog name is how the configuration says there is no connection, and the command then fails with the reason instead of being quietly absent; an empty journal name says only that the journal was not wired, which on this major is a switch of its own. */
type DatabaseResetCommand struct {
    databaseServiceName        string
    journalDatabaseServiceName string
}

func NewDatabaseResetCommand(databaseServiceName string, journalDatabaseServiceName string) *DatabaseResetCommand {
    return &DatabaseResetCommand{
        databaseServiceName:        databaseServiceName,
        journalDatabaseServiceName: journalDatabaseServiceName,
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

    printDatabaseResetPlan(writer, instance.journalDatabaseServiceName)

    if false == commandContext.Bool(databaseResetFlagForce) {
        fmt.Fprintln(writer, "nothing was touched; pass --force to perform the reset")

        return nil
    }

    ctx := context.Background()

    database, resolveErr := melodycontainer.FromResolver[*bun.DB](
        runtimeInstance.Container(),
        instance.databaseServiceName,
    )
    if nil != resolveErr {
        return resolveErr
    }

    if resetErr := migration.Reset(ctx, database); nil != resetErr {
        return resetErr
    }

    if seedErr := repository.SeedAll(ctx, database); nil != seedErr {
        return seedErr
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
            return journalResetErr
        }
    }

    fmt.Fprintln(writer, "database reset: the schema was recreated and the nomenclature reseeded")

    return nil
}

/* databaseResetPlanLineList names what the reset reaches, and it is printed on both paths on purpose: the refusal has to say what the flag would have unleashed, and the run has to leave the same lines in the log of whoever ran it. It is a list rather than a series of prints so that what the command SAYS it will destroy is readable by a test without capturing a stream. */
func databaseResetPlanLineList(journalDatabaseServiceName string) []string {
    lineList := []string{"example:db:reset would drop and recreate the tables the migration sets own:"}

    for _, table := range migration.SchemaTableNameList() {
        lineList = append(lineList, "  - "+table)
    }

    lineList = append(lineList, "  - the bun bookkeeping tables of each database, so an older set's rows go with them")

    if "" == journalDatabaseServiceName {
        lineList = append(lineList, "the journal database is not wired, so its set is left alone")
    }

    return append(lineList, "and then reseed the nomenclature")
}

func printDatabaseResetPlan(writer io.Writer, journalDatabaseServiceName string) {
    for _, line := range databaseResetPlanLineList(journalDatabaseServiceName) {
        fmt.Fprintln(writer, line)
    }
}

var _ melodyclicontract.Command = (*DatabaseResetCommand)(nil)
