package migrate

import (
	"time"

	clicontract "github.com/precision-soft/melody/cli/contract"
	"github.com/precision-soft/melody/cli/output"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
	"github.com/uptrace/bun/migrate"
)

func NewInitCommand(migrations *migrate.Migrations, options Options) *InitCommand {
	return &InitCommand{
		base: baseCommand{
			migrations: migrations,
			options:    options,
		},
	}
}

type InitCommand struct {
	base baseCommand
}

func (instance *InitCommand) Name() string {
	return instance.base.options.CommandPrefix + ":init"
}

func (instance *InitCommand) Description() string {
	return "Initialize Bun migrations tables"
}

func (instance *InitCommand) Flags() []clicontract.Flag {
	return output.MergeFlags(
		output.StandardFlags(),
		[]clicontract.Flag{
			instance.base.managerFlag(),
		},
	)
}

func (instance *InitCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
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

	initErr := migrator.Init(runtimeInstance.Context())
	if nil != initErr {
		instance.base.printErrorLine(commandContext, option, initErr)
		return nil
	}

	if output.FormatTable == option.Format {
		builder := output.NewTableBuilder()
		builder.AddSummaryLine("MIGRATIONS INITIALIZED")
		block := builder.AddBlock("DETAILS", []string{"key", "value"})
		block.AddRow("manager", managerName)
		envelope.Table = builder.Build()
	} else {
		envelope.Data = map[string]any{
			"manager": managerName,
			"status":  "initialized",
		}
	}

	renderErr := instance.base.render(commandContext, &envelope, option, startedAt)
	if nil != renderErr {
		instance.base.printErrorLine(commandContext, option, renderErr)
		return nil
	}

	return nil
}

var _ clicontract.Command = (*InitCommand)(nil)
