package migrate

import (
	"errors"
	"fmt"
	"time"

	clicontract "github.com/precision-soft/melody/cli/contract"
	"github.com/precision-soft/melody/cli/output"
	"github.com/precision-soft/melody/container"
	containercontract "github.com/precision-soft/melody/container/contract"
	"github.com/precision-soft/melody/integrations/bunorm"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

type baseCommand struct {
	migrations *migrate.Migrations
	options    Options
}

func (instance *baseCommand) managerFlag() clicontract.Flag {
	return &clicontract.StringFlag{
		Name:  instance.options.ManagerFlagName,
		Usage: "manager name (defaults to registry default)",
		Value: "",
	}
}

func (instance *baseCommand) optionFromCommand(commandContext *clicontract.CommandContext) output.Option {
	return output.NormalizeOption(
		output.ParseOptionFromCommand(commandContext),
	)
}

func (instance *baseCommand) meta(
	commandName string,
	commandContext *clicontract.CommandContext,
	option output.Option,
	startedAt time.Time,
) output.Meta {
	return output.NewMeta(
		commandName,
		commandContext.Args().Slice(),
		option,
		startedAt,
		time.Duration(0),
		output.Version{},
	)
}

func (instance *baseCommand) resolveRegistry(resolver containercontract.Resolver) (*bunorm.ManagerRegistry, error) {
	if "" == instance.options.ManagerRegistryServiceId {
		return nil, errors.New("manager registry service id is required")
	}

	return container.FromResolver[*bunorm.ManagerRegistry](resolver, instance.options.ManagerRegistryServiceId)
}

func (instance *baseCommand) resolveDatabase(
	runtimeInstance runtimecontract.Runtime,
	commandContext *clicontract.CommandContext,
) (*bun.DB, string, error) {
	registry, registryErr := instance.resolveRegistry(runtimeInstance.Scope())
	if nil != registryErr {
		return nil, "", registryErr
	}

	if nil == registry {
		return nil, "", errors.New("manager registry service is nil")
	}

	managerName := commandContext.String(instance.options.ManagerFlagName)
	if "" == managerName {
		defaultManager, defaultManagerErr := registry.DefaultManager()
		if nil != defaultManagerErr {
			return nil, "", defaultManagerErr
		}

		return defaultManager.Database(), "<default>", nil
	}

	manager := registry.MustManager(managerName)

	return manager.Database(), managerName, nil
}

func (instance *baseCommand) newMigrator(db *bun.DB) (*migrate.Migrator, error) {
	if nil == db {
		return nil, errors.New("bun database is nil")
	}

	if nil == instance.migrations {
		return nil, errors.New("migrations collection is nil")
	}

	return migrate.NewMigrator(db, instance.migrations), nil
}

func (instance *baseCommand) render(
	commandContext *clicontract.CommandContext,
	envelope *output.Envelope,
	option output.Option,
	startedAt time.Time,
) error {
	envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

	return output.Render(commandContext.Writer, *envelope, option)
}

func (instance *baseCommand) printErrorLine(
	commandContext *clicontract.CommandContext,
	option output.Option,
	err error,
) {
	if nil == err {
		return
	}

	if false == option.NoColor {
		_, _ = fmt.Fprintf(commandContext.Writer, "\n\x1b[31mERROR: %s\x1b[0m\n\n", err.Error())
		return
	}

	_, _ = fmt.Fprintf(commandContext.Writer, "\nERROR: %s\n\n", err.Error())
}
