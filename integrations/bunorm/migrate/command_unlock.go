package migrate

import (
	"time"

	clicontract "github.com/precision-soft/melody/cli/contract"
	"github.com/precision-soft/melody/cli/output"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
	"github.com/uptrace/bun/migrate"
)

func NewUnlockCommand(migrations *migrate.Migrations, options Options) *UnlockCommand {
	return &UnlockCommand{base: baseCommand{migrations: migrations, options: options}}
}

type UnlockCommand struct {
	base baseCommand
}

func (instance *UnlockCommand) Name() string {
	return instance.base.options.CommandPrefix + ":unlock"
}

func (instance *UnlockCommand) Description() string {
	return "Unlock Bun migrations table"
}

func (instance *UnlockCommand) Flags() []clicontract.Flag {
	return output.MergeFlags(output.StandardFlags(), []clicontract.Flag{instance.base.managerFlag()})
}

func (instance *UnlockCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
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

	unlockErr := migrator.Unlock(runtimeInstance.Context())
	if nil != unlockErr {
		instance.base.printErrorLine(commandContext, option, unlockErr)
		return nil
	}

	if output.FormatTable == option.Format {
		builder := output.NewTableBuilder()
		builder.AddSummaryLine("MIGRATIONS UNLOCKED")
		block := builder.AddBlock("DETAILS", []string{"key", "value"})
		block.AddRow("manager", managerName)
		envelope.Table = builder.Build()
	} else {
		envelope.Data = map[string]any{
			"manager": managerName,
			"status":  "unlocked",
		}
	}

	renderErr := instance.base.render(commandContext, &envelope, option, startedAt)
	if nil != renderErr {
		instance.base.printErrorLine(commandContext, option, renderErr)
		return nil
	}

	return nil
}

var _ clicontract.Command = (*UnlockCommand)(nil)
