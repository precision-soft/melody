package migrate

import (
	"errors"
	"time"

	clicontract "github.com/precision-soft/melody/cli/contract"
	"github.com/precision-soft/melody/cli/output"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
	"github.com/uptrace/bun/migrate"
)

func NewCreateTxSqlCommand(migrations *migrate.Migrations, options Options) *CreateTxSqlCommand {
	return &CreateTxSqlCommand{base: baseCommand{migrations: migrations, options: options}}
}

type CreateTxSqlCommand struct {
	base baseCommand
}

func (instance *CreateTxSqlCommand) Name() string {
	return instance.base.options.CommandPrefix + ":create-tx-sql"
}

func (instance *CreateTxSqlCommand) Description() string {
	return "Create transactional up/down SQL migration files"
}

func (instance *CreateTxSqlCommand) Flags() []clicontract.Flag {
	return output.MergeFlags(output.StandardFlags(), []clicontract.Flag{instance.base.managerFlag()})
}

func (instance *CreateTxSqlCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
	startedAt := time.Now()
	option := instance.base.optionFromCommand(commandContext)
	meta := instance.base.meta(instance.Name(), commandContext, option, startedAt)
	envelope := output.NewEnvelope(meta)

	migrationName := commandContext.Args().First()
	if "" == migrationName {
		return errors.New("migration name is required")
	}

	db, managerName, dbErr := instance.base.resolveDatabase(runtimeInstance, commandContext)
	if nil != dbErr {
		instance.base.printErrorLine(commandContext, option, dbErr)
		return nil
	}

	migrator, migratorErr := instance.base.newMigrator(db)
	if nil != migratorErr {
		instance.base.printErrorLine(commandContext, option, migratorErr)
		return nil
	}

	files, createErr := migrator.CreateTxSQLMigrations(runtimeInstance.Context(), migrationName)
	if nil != createErr {
		instance.base.printErrorLine(commandContext, option, createErr)
		return nil
	}

	fileLines := instance.formatMigrationFiles(files)

	if output.FormatTable == option.Format {
		builder := output.NewTableBuilder()
		builder.AddSummaryLine("MIGRATION FILES CREATED")
		block := builder.AddBlock("DETAILS", []string{"key", "value"})
		block.AddRow("manager", managerName)
		block.AddRow("name", migrationName)

		filesBlock := builder.AddBlock("FILES", []string{"file"})
		for _, line := range fileLines {
			filesBlock.AddRow(line)
		}

		envelope.Table = builder.Build()
	} else {
		envelope.Data = map[string]any{
			"manager": managerName,
			"name":    migrationName,
			"files":   fileLines,
		}
	}

	renderErr := instance.base.render(commandContext, &envelope, option, startedAt)
	if nil != renderErr {
		instance.base.printErrorLine(commandContext, option, renderErr)
		return nil
	}

	return nil
}

func (instance *CreateTxSqlCommand) formatMigrationFiles(files []*migrate.MigrationFile) []string {
	lines := make([]string, 0, len(files))

	for _, file := range files {
		if "" != file.Path {
			lines = append(lines, file.Path)
			continue
		}

		if "" != file.Name {
			lines = append(lines, file.Name)
			continue
		}

		lines = append(lines, "<unknown>")
	}

	return lines
}

var _ clicontract.Command = (*CreateTxSqlCommand)(nil)
