package migrate

import (
	"time"

	clicontract "github.com/precision-soft/melody/cli/contract"
	"github.com/precision-soft/melody/cli/output"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
	"github.com/uptrace/bun/migrate"
)

func NewMigrateCommand(migrations *migrate.Migrations, options Options) *MigrateCommand {
	return &MigrateCommand{
		base: baseCommand{migrations: migrations, options: options},
	}
}

type MigrateCommand struct {
	base baseCommand
}

func (instance *MigrateCommand) Name() string {
	return instance.base.options.CommandPrefix + ":migrate"
}

func (instance *MigrateCommand) Description() string {
	return "Apply pending Bun migrations"
}

func (instance *MigrateCommand) Flags() []clicontract.Flag {
	return output.MergeFlags(
		output.StandardFlags(),
		[]clicontract.Flag{
			instance.base.managerFlag(),
		},
	)
}

func (instance *MigrateCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
	startedAt := time.Now()
	option := instance.base.optionFromCommand(commandContext)
	meta := instance.base.meta(instance.Name(), commandContext, option, startedAt)
	envelope := output.NewEnvelope(meta)

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

	group, migrateErr := migrator.Migrate(runtimeInstance.Context())
	if nil != migrateErr {
		instance.base.printErrorLine(commandContext, option, migrateErr)
		return nil
	}

	groupString := "<none>"
	if nil != group {
		groupString = group.String()
	}

	if output.FormatTable == option.Format {
		builder := output.NewTableBuilder()
		builder.AddSummaryLine("MIGRATIONS APPLIED")
		block := builder.AddBlock("DETAILS", []string{"key", "value"})
		block.AddRow("manager", managerName)
		block.AddRow("group", groupString)
		envelope.Table = builder.Build()
	} else {
		envelope.Data = map[string]any{
			"manager": managerName,
			"group":   groupString,
		}
	}

	renderErr := instance.base.render(commandContext, &envelope, option, startedAt)
	if nil != renderErr {
		instance.base.printErrorLine(commandContext, option, renderErr)
		return nil
	}

	return nil
}

var _ clicontract.Command = (*MigrateCommand)(nil)
