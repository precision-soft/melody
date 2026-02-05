package migrate

import (
	"time"

	clicontract "github.com/precision-soft/melody/cli/contract"
	"github.com/precision-soft/melody/cli/output"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
	"github.com/uptrace/bun/migrate"
)

func NewRollbackCommand(migrations *migrate.Migrations, options Options) *RollbackCommand {
	return &RollbackCommand{base: baseCommand{migrations: migrations, options: options}}
}

type RollbackCommand struct {
	base baseCommand
}

func (instance *RollbackCommand) Name() string {
	return instance.base.options.CommandPrefix + ":rollback"
}

func (instance *RollbackCommand) Description() string {
	return "Rollback last Bun migration group"
}

func (instance *RollbackCommand) Flags() []clicontract.Flag {
	return output.MergeFlags(
		output.StandardFlags(),
		[]clicontract.Flag{instance.base.managerFlag()},
	)
}

func (instance *RollbackCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
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

	group, rollbackErr := migrator.Rollback(runtimeInstance.Context())
	if nil != rollbackErr {
		instance.base.printErrorLine(commandContext, option, rollbackErr)
		return nil
	}

	groupString := "<none>"
	if nil != group {
		groupString = group.String()
	}

	if output.FormatTable == option.Format {
		builder := output.NewTableBuilder()
		builder.AddSummaryLine("MIGRATIONS ROLLED BACK")
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

var _ clicontract.Command = (*RollbackCommand)(nil)
