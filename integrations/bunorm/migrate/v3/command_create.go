package migrate

import (
    "errors"
    "fmt"
    "regexp"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun/migrate"
)

/* migrationNamePattern is the grammar a migration name is held to before it reaches bun's generator: the same set bun's own nameRE accepts, kept here so the confinement of the file path is this command's, not the pinned dependency's. */
var migrationNamePattern = regexp.MustCompile(`^[0-9a-z_\-]+$`)

func NewCreateGoCommand(migrations *migrate.Migrations, options Options) *CreateCommand {
    return &CreateCommand{base: baseCommand{migrations: migrations, options: options}}
}

type CreateCommand struct {
    base baseCommand
}

func (instance *CreateCommand) Name() string {
    return instance.base.options.CommandPrefix + ":create"
}

func (instance *CreateCommand) Description() string {
    return "Create Go migration file template"
}

func (instance *CreateCommand) Flags() []clicontract.Flag {
    return output.MergeFlags(output.StandardFlags(), []clicontract.Flag{instance.base.managerFlag()})
}

func (instance *CreateCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) (runErr error) {
    option := instance.base.optionFromCommand(commandContext)
    outputInstance := newCommandOutput(commandContext.Writer(), commandContext.Arguments(), option)

    startedAt := time.Now()
    defer func() {
        runErr = outputInstance.finishRun(instance.Name(), startedAt, runErr, recover())
    }()

    migrationName := ""
    arguments := commandContext.Arguments()
    if 0 < len(arguments) {
        migrationName = arguments[0]
    }

    if "" == migrationName {
        err := errors.New("migration name is required (usage: db:create <name>)")
        return err
    }

    /* the name becomes part of a file path under the migrations directory, so it is held to the grammar bun's own generator accepts BEFORE it reaches bun: a path separator or a parent reference in it must never touch the filesystem, and that must not depend on the pinned dependency keeping its regexp — a confinement borrowed from a dependency is one release away from not being there. */
    if false == migrationNamePattern.MatchString(migrationName) {
        return exception.NewError(
            "migration name must match "+migrationNamePattern.String(),
            map[string]any{"name": migrationName},
            nil,
        )
    }

    db, managerName, releaseDatabase, dbErr := instance.base.resolveDatabase(runtimeInstance, commandContext, outputInstance)
    if nil != dbErr {
        return dbErr
    }
    defer releaseDatabase()

    migrator, migratorErr := instance.base.newMigrator(db)
    if nil != migratorErr {
        return migratorErr
    }

    files, createErr := migrator.CreateGoMigration(runtimeInstance.Context(), migrationName)
    if nil != createErr {
        return createErr
    }

    /* bun wrote that file with a single os.WriteFile, which is not atomic and not durable: the same content is written again here into a temporary neighbour, fsynced, renamed over it and the directory fsynced, so a command that reports success has left a whole file behind rather than one a crash could have truncated into a Go source that does not compile. The path and the content are bun's own, taken from what it just returned. */
    if nil != files && "" != files.Path {
        if finishErr := finishFileAtomically(files.Path, []byte(files.Content)); nil != finishErr {
            /* the rename landed and only the directory fsync did not: the file is whole and in place, so the run is a success with a warning naming what could not be guaranteed — reported as a failure, the operator's re-run would create a second migration under a new timestamp beside this one */
            if false == errors.Is(finishErr, errDirectorySyncAfterRename) {
                return finishErr
            }

            outputInstance.printWarning(fmt.Sprintf("migration file %s is in place, but its directory entry could not be fsynced and may not survive a crash: %s", files.Path, finishErr.Error()))
        }
    }

    outputInstance.printSuccess("migration file created")

    fileLines := instance.formatMigrationFiles(files)
    outputInstance.newline()
    outputInstance.printFilesBlock(fileLines)

    if true == outputInstance.wantsDetail() {
        outputInstance.newline()
        outputInstance.printDetailsBlock(map[string]string{
            "manager": managerName,
            "name":    migrationName,
        })
    }

    return nil
}

func (instance *CreateCommand) formatMigrationFiles(file *migrate.MigrationFile) []string {
    lines := make([]string, 0)

    /* bun answers a file on every success today; the guard is for the door's contract, not its current behaviour — a nil here rendered as a nil dereference inside the success path, after the migration had already been created */
    if nil == file {
        return append(lines, "<unknown>")
    }

    if "" != file.Path {
        lines = append(lines, file.Path)
        return lines
    }

    if "" != file.Name {
        lines = append(lines, file.Name)
        return lines
    }

    lines = append(lines, "<unknown>")

    return lines
}

var _ clicontract.Command = (*CreateCommand)(nil)
