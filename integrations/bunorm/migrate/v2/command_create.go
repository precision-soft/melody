package migrate

import (
    "errors"

    clicontract "github.com/precision-soft/melody/v2/cli/contract"
    "github.com/precision-soft/melody/v2/cli/output"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
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

func (instance *CreateCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
    option := instance.base.optionFromCommand(commandContext)
    outputInstance := newCommandOutput(commandContext.Writer, option)

    migrationName := commandContext.Args().First()
    if "" == migrationName {
        err := errors.New("migration name is required (usage: db:create <name>)")
        outputInstance.printError(err)
        return err
    }

    db, managerName, dbErr := instance.base.resolveDatabase(runtimeInstance, commandContext)
    if nil != dbErr {
        outputInstance.printError(dbErr)
        return dbErr
    }

    migrator, migratorErr := instance.base.newMigrator(db)
    if nil != migratorErr {
        outputInstance.printError(migratorErr)
        return migratorErr
    }

    files, createErr := migrator.CreateGoMigration(runtimeInstance.Context(), migrationName)
    if nil != createErr {
        outputInstance.printError(createErr)
        return createErr
    }

    outputInstance.printSuccess("migration file created")

    fileLines := instance.formatMigrationFiles(files)
    outputInstance.newline()
    outputInstance.printFilesBlock(fileLines)

    if option.Verbose {
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
