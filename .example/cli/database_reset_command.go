package cli

import (
    "errors"
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

/* DatabaseResetCommand brings this example's databases back to the state a fresh volume would be in: the tables each migration set owns are dropped with the bun bookkeeping, the single schema migration of each set is applied again, the nomenclature is reseeded and the cache is cleared. An example has one state, the present one, so a database left in an older shape is answered by this command rather than by code every boot pays for; it belongs to the application because dropping a whole schema is not a door a published module should grow. The database service names and locations are handed in at registration, since the configuration package imports this one. An empty catalog name fails the command with the reason, an empty journal name only leaves the journal alone, and the plan prints each location as host:port/schema, so a reset of this volume cannot be mistaken for one of whatever MYSQL_DATABASE points at. */
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

func (instance *DatabaseResetCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext *melodyclicontract.CommandContext) (runErr error) {
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

    /* the cache is resolved before the first drop, so a cache the reset cannot reach refuses it before anything is touched; and it is cleared on every exit from here on, a failed step included, since a reset that dropped rows and then failed leaves the cache holding entities the database does not hold. A failed clear is joined to the step's failure, which stays first. */
    cacheInstance, cacheErr := resolveCache(runtimeInstance)
    if nil != cacheErr {
        return cacheErr
    }

    cacheCleared := false
    clearTheCache := func() error {
        cacheCleared = true

        return clearResolvedCache(runtimeInstance, cacheInstance, writer)
    }

    defer func() {
        if nil == runErr || true == cacheCleared {
            return
        }

        if deferredClearErr := clearTheCache(); nil != deferredClearErr {
            runErr = errors.Join(runErr, deferredClearErr)
        }
    }()

    /* the runtime's context, not a background one, so an operator's interrupt reaches the drops as it reaches every other command; the first interrupt cuts the drops where they stand, and a second run finishes the volume */
    ctx := runtimeInstance.Context()

    database, resolveErr := melodycontainer.FromResolver[*bun.DB](
        runtimeInstance.Container(),
        instance.databaseServiceName,
    )
    if nil != resolveErr {
        return resolveErr
    }

    /* the catalogue is brought whole, dropped, recreated, reseeded and its cache cleared, BEFORE the journal is touched, so a refusal of the journal database never leaves an empty catalogue behind; each step reports itself as it completes, so the output after a failure says what happened */
    if resetErr := migration.Reset(ctx, database); nil != resetErr {
        return databaseResetStepFailure("dropping and recreating the schema", "catalogue", instance.databaseLocation, resetErr)
    }

    fmt.Fprintln(writer, "catalogue reset: the schema was dropped and recreated on "+instance.databaseLocation)

    if seedErr := repository.SeedAll(ctx, database); nil != seedErr {
        return databaseResetStepFailure("reseeding the nomenclature", "catalogue", instance.databaseLocation, seedErr)
    }

    fmt.Fprintln(writer, "catalogue reset: the nomenclature was reseeded")

    if clearErr := clearTheCache(); nil != clearErr {
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

func resolveCache(runtimeInstance melodyruntimecontract.Runtime) (melodycachecontract.Cache, error) {
    return melodycontainer.FromResolver[melodycachecontract.Cache](
        runtimeInstance.Container(),
        melodycache.ServiceCache,
    )
}

/* clearResolvedCache empties the cache and says so: the entities are cached with no expiry and cleared by the listeners of the write events, and a reset dispatches none, so without it a removed account would keep authenticating from its cached digest. On the in-process fallback it reaches this process alone, which the line says; a failed clear takes the exit code. */
func clearResolvedCache(runtimeInstance melodyruntimecontract.Runtime, cacheInstance melodycachecontract.Cache, writer io.Writer) error {
    if clearErr := cacheInstance.Clear(); nil != clearErr {
        return melodyexception.NewError("database reset: clearing the cache did not complete", nil, clearErr)
    }

    fmt.Fprintln(writer, "cache cleared: "+cacheClearedScope(runtimeInstance))

    return nil
}

/* databaseResetStepFailure names the step that did not complete and the database it did not complete on, in the message itself, since the cli engine echoes the message alone. */
func databaseResetStepFailure(step string, database string, location string, cause error) error {
    return melodyexception.NewError(
        "database reset: "+step+" did not complete on the "+database+" database at "+location,
        melodyexceptioncontract.Context{"step": step, "database": database, "location": location},
        cause,
    )
}

/* databaseResetPlanLineList names what the reset reaches and where. It is printed on both paths, so the refusal says what --force would destroy and a run leaves the same lines in its log, and it is a list so a test can read it without capturing a stream. */
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
