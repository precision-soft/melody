package migrate

import (
    "errors"
    "fmt"
    "regexp"

    "github.com/precision-soft/melody/integrations/bunorm/v3"
    "github.com/precision-soft/melody/v3/exception"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun/migrate"
)

/* migrationNamePattern is the grammar a migration name is held to before it reaches bun's generator, the set bun's own nameRE accepts, kept here so the confinement of the file path is this command's, not the pinned dependency's. */
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

func (instance *CreateCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    return instance.base.run(instance.Name(), runtimeInstance, commandContext, instance.runCreate)
}

func (instance *CreateCommand) runCreate(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
    outputInstance *commandOutput,
) (runErr error) {
    /* the result of this command is the file it writes, not the report: a report the writer lost is recorded in the journal rather than failing a run whose file is already in place — the re-run an exit of one invites creates a second migration beside the first */
    outputInstance.reportLostWritesTo(instance.base.journal(runtimeInstance))

    migrationName := ""
    arguments := commandContext.Arguments()
    if 0 < len(arguments) {
        migrationName = arguments[0]
    }

    if "" == migrationName {
        err := errors.New("migration name is required (usage: " + instance.Name() + " <name>)")
        return err
    }

    /* the name becomes part of a file path, so a path separator or a parent reference is refused here, before bun, rather than relying on the pinned dependency's regexp */
    if false == migrationNamePattern.MatchString(migrationName) {
        return exception.NewError(
            "migration name must match "+migrationNamePattern.String(),
            map[string]any{"name": migrationName},
            nil,
        )
    }

    /* no database is opened: the file is written from the migrations collection alone and the manager name only labels the detail line. A name given explicitly is still checked against the registry without opening, so a misspelt --manager is refused here. */
    managerName := instance.base.managerLabel(commandContext)

    if defaultManagerLabel != managerName {
        registry, registryErr := instance.base.resolveRegistry(runtimeInstance.Scope())
        if nil != registryErr {
            return registryErr
        }

        if nil == registry || false == registry.HasProviderDefinition(managerName) {
            return exception.NewError(
                "manager is not registered in the manager registry",
                map[string]any{"manager": managerName},
                bunorm.ErrProviderDefinitionNotFound,
            )
        }
    }

    migrator, migratorErr := instance.base.newFileMigrator()
    if nil != migratorErr {
        return migratorErr
    }

    files, createErr := migrator.CreateGoMigration(runtimeInstance.Context(), migrationName)
    if nil != createErr {
        return createErr
    }

    /* bun wrote the file with a single os.WriteFile, so the same content is rewritten atomically and durably before the command reports success */
    if nil != files && "" != files.Path {
        if finishErr := finishFileAtomically(files.Path, []byte(files.Content)); nil != finishErr {
            /* the rename landed and only the directory fsync did not: the file is whole and in place, so the run is a success with a warning naming what could not be guaranteed — reported as a failure, the operator's re-run would create a second migration under a new timestamp beside this one */
            if false == errors.Is(finishErr, errDirectorySyncAfterRename) {
                return finishErr
            }

            outputInstance.printWarning(fmt.Sprintf("migration file %s is in place, but its directory entry could not be fsynced and may not survive a crash: %s", files.Path, finishErr.Error()))
        }
    }

    if nil != files {
        outputInstance.resultWrittenTo(files.Path)
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

    /* bun answers a file on every success; the guard keeps a nil from being dereferenced after the migration is created */
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
