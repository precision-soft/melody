package cli

import (
    "fmt"
    "io"
    "os"

    "github.com/precision-soft/melody/v2/.example/migration"
    "github.com/precision-soft/melody/v2/.example/repository"
    melodycache "github.com/precision-soft/melody/v2/cache"
    melodycachecontract "github.com/precision-soft/melody/v2/cache/contract"
    melodyclicontract "github.com/precision-soft/melody/v2/cli/contract"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodyexception "github.com/precision-soft/melody/v2/exception"
    melodyexceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    "github.com/uptrace/bun"
)

const databaseResetFlagForce = "force"

/* DatabaseResetCommand brings this example's database back to the state a fresh volume would be in: the tables the migration set owns are dropped with the bun bookkeeping, the single schema migration is applied again, the nomenclature is reseeded and the cache is cleared. An example has one state, the present one, so a database left in an older shape is answered by this command rather than by code every boot pays for; it belongs to the application because dropping a whole schema is not a door a published module should grow. The database service name and its location are handed in at registration, since the configuration package imports this one. An empty name fails the command with the reason, and the plan prints the location as host:port/schema, so a reset of this volume cannot be mistaken for one of whatever MYSQL_DATABASE points at. */
type DatabaseResetCommand struct {
    databaseServiceName string
    databaseLocation    string
}

func NewDatabaseResetCommand(databaseServiceName string, databaseLocation string) *DatabaseResetCommand {
    return &DatabaseResetCommand{
        databaseServiceName: databaseServiceName,
        databaseLocation:    databaseLocation,
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

    printDatabaseResetPlan(writer, instance.databaseLocation)

    if false == commandContext.Bool(databaseResetFlagForce) {
        fmt.Fprintln(writer, "nothing was touched; pass --force to perform the reset")

        return nil
    }

    /* the runtime's context, not a background one, so an operator's interrupt reaches the drops as it reaches every other command; the first interrupt cuts the drops where they stand, and a second run finishes the volume */
    ctx := runtimeInstance.Context()

    database, resolveErr := melodycontainer.FromResolver[*bun.DB](
        runtimeInstance.Container(),
        instance.databaseServiceName,
    )
    if nil != resolveErr {
        return resolveErr
    }

    /* each step reports itself as it completes, so what the operator reads after a failure is what HAPPENED, not only what was planned */
    if resetErr := migration.Reset(ctx, database); nil != resetErr {
        return databaseResetStepFailure("dropping and recreating the schema", instance.databaseLocation, resetErr)
    }

    fmt.Fprintln(writer, "database reset: the schema was dropped and recreated on "+instance.databaseLocation)

    if seedErr := repository.SeedAll(ctx, database); nil != seedErr {
        return databaseResetStepFailure("reseeding the nomenclature", instance.databaseLocation, seedErr)
    }

    fmt.Fprintln(writer, "database reset: the nomenclature was reseeded")

    return clearCache(runtimeInstance, writer)
}

/* clearCache empties the cache and says so: the entities are cached with no expiry and cleared by the listeners of the write events, and a reset dispatches none, so without it a removed account would keep authenticating from its cached digest. On the in-process fallback it reaches this process alone, which the line says; a failed clear takes the exit code. */
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

/* databaseResetStepFailure names the step that did not complete and the database it did not complete on, in the message itself, since the cli engine echoes the message alone. */
func databaseResetStepFailure(step string, location string, cause error) error {
    return melodyexception.NewError(
        "database reset: "+step+" did not complete on the database at "+location,
        melodyexceptioncontract.Context{"step": step, "location": location},
        cause,
    )
}

/* databaseResetPlanLineList names what the reset reaches and where. It is printed on both paths, so the refusal says what --force would destroy and a run leaves the same lines in its log, and it is a list so a test can read it without capturing a stream. */
func databaseResetPlanLineList(databaseLocation string) []string {
    lineList := []string{"example:db:reset would drop and recreate the tables the migration set owns on " + databaseLocation + ":"}

    for _, table := range migration.SchemaTableNameList() {
        lineList = append(lineList, "  - "+table)
    }

    return append(
        lineList,
        "  - the bun bookkeeping tables, so an older set's rows go with them",
        "and then reseed the nomenclature and clear the cache",
    )
}

func printDatabaseResetPlan(writer io.Writer, databaseLocation string) {
    for _, line := range databaseResetPlanLineList(databaseLocation) {
        fmt.Fprintln(writer, line)
    }
}

var _ melodyclicontract.Command = (*DatabaseResetCommand)(nil)
