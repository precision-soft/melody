package migrate

import (
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
	return "Unlock Bun migrations table (use when migration process crashed)"
}

func (instance *UnlockCommand) Flags() []clicontract.Flag {
	return output.MergeFlags(output.StandardFlags(), []clicontract.Flag{instance.base.managerFlag()})
}

func (instance *UnlockCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
	option := instance.base.optionFromCommand(commandContext)
	outputInstance := newCommandOutput(commandContext.Writer, option)

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

	if option.Verbose {
		identity, identityErr := fetchDatabaseIdentity(runtimeInstance.Context(), db)
		if nil != identityErr {
			outputInstance.printError(identityErr)
			return identityErr
		}
		if nil != identity {
			outputInstance.printDatabaseBlock(identity)
			outputInstance.newline()
		}
	}

	unlockErr := migrator.Unlock(runtimeInstance.Context())
	if nil != unlockErr {
		outputInstance.printError(unlockErr)
		return unlockErr
	}

	outputInstance.printSuccess("migrations table unlocked")

	if option.Verbose {
		outputInstance.newline()
		outputInstance.printDetailsBlock(map[string]string{
			"manager": managerName,
			"status":  "unlocked",
		})
	}

	return nil
}

var _ clicontract.Command = (*UnlockCommand)(nil)
