package migrate

import (
    "time"

    "errors"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun/migrate"
)

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
    outputInstance := newCommandOutput(commandContext.Writer(), option)

    startedAt := time.Now()
    defer func() {
        runErr = outputInstance.finish(instance.Name(), startedAt, runErr)
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
            return finishErr
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
