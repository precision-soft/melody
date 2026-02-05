package migrate

import (
	"fmt"
	"time"

	clicontract "github.com/precision-soft/melody/cli/contract"
	"github.com/precision-soft/melody/cli/output"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
	"github.com/uptrace/bun/migrate"
)

func NewStatusCommand(migrations *migrate.Migrations, options Options) *StatusCommand {
	return &StatusCommand{base: baseCommand{migrations: migrations, options: options}}
}

type StatusCommand struct {
	base baseCommand
}

func (instance *StatusCommand) Name() string {
	return instance.base.options.CommandPrefix + ":status"
}

func (instance *StatusCommand) Description() string {
	return "Display Bun migrations status"
}

func (instance *StatusCommand) Flags() []clicontract.Flag {
	return output.MergeFlags(
		output.StandardFlags(),
		[]clicontract.Flag{instance.base.managerFlag()},
	)
}

func (instance *StatusCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
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

	items, statusErr := migrator.MigrationsWithStatus(runtimeInstance.Context())
	if nil != statusErr {
		instance.base.printErrorLine(commandContext, option, statusErr)
		return nil
	}

	applied := items.Applied()
	unapplied := items.Unapplied()

	if output.FormatTable == option.Format {
		builder := output.NewTableBuilder()
		builder.AddSummaryLine("MIGRATION STATUS")
		builder.AddSummaryLine("manager: " + managerName)
		builder.AddSummaryLine("applied: " + fmt.Sprintf("%d", len(applied)))
		builder.AddSummaryLine("pending: " + fmt.Sprintf("%d", len(unapplied)))

		appliedBlock := builder.AddBlock("APPLIED", []string{"migration"})
		for _, migration := range applied {
			appliedBlock.AddRow(migration.String())
		}

		unappliedBlock := builder.AddBlock("PENDING", []string{"migration"})
		for _, migration := range unapplied {
			unappliedBlock.AddRow(migration.String())
		}

		envelope.Table = builder.Build()
	} else {
		appliedStrings := make([]string, 0, len(applied))
		for _, migration := range applied {
			appliedStrings = append(appliedStrings, migration.String())
		}
		pendingStrings := make([]string, 0, len(unapplied))
		for _, migration := range unapplied {
			pendingStrings = append(pendingStrings, migration.String())
		}

		envelope.Data = map[string]any{
			"manager": managerName,
			"applied": appliedStrings,
			"pending": pendingStrings,
		}
	}

	renderErr := instance.base.render(commandContext, &envelope, option, startedAt)
	if nil != renderErr {
		instance.base.printErrorLine(commandContext, option, renderErr)
		return nil
	}

	return nil
}

var _ clicontract.Command = (*StatusCommand)(nil)
