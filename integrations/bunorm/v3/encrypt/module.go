package encrypt

import (
    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/uptrace/bun"
)

/* ModuleConfig wires the melody:encrypt:database bulk command from a prebuilt *bun.DB or from a DatabaseFactory evaluated at the first command run, so a process in another mode never opens the database. Setting both panics at registration; with Database, DatabaseFactory and Cipher all nil no unsuffixed command is registered, and any other partial combination panics. */
type ModuleConfig struct {
    Database *bun.DB
    Cipher   Cipher

    /* DatabaseFactory resolves the database for the unsuffixed command at the first command run, after Boot, and a success is memoized. A database it opens is not closed by the container, and a non-MySQL dialect panics at that first run. */
    DatabaseFactory func(resolver containercontract.Resolver) (*bun.DB, error)

    /* Contexts adds one bulk command per key compartment — melody:encrypt:database:<name> — for a multi-context binary; it composes with the legacy fields above, which keep the unsuffixed command. */
    Contexts []CommandContextConfig
}

/* CommandContextConfig binds one compartment's database and cipher to a suffixed bulk command. Supply a DatabaseFactory to resolve the compartment's database from the container at the first command run; setting it together with Database is ambiguous and panics at registration. */
type CommandContextConfig struct {
    Name            string
    Database        *bun.DB
    Cipher          Cipher
    DatabaseFactory func(resolver containercontract.Resolver) (*bun.DB, error)
}

func NewModule(config ModuleConfig) *Module {
    return &Module{config: config}
}

type Module struct {
    config ModuleConfig
}

func (instance *Module) Name() string {
    return "bunorm.encrypt"
}

func (instance *Module) Description() string {
    return "registers the melody:encrypt:database command for bulk encrypt, re-encrypt and decrypt"
}

func (instance *Module) RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command {
    commands := make([]clicontract.Command, 0)

    if nil != instance.config.Database || nil != instance.config.DatabaseFactory || nil != instance.config.Cipher {
        if nil == instance.config.Cipher || (nil == instance.config.Database && nil == instance.config.DatabaseFactory) {
            exception.Panic(exception.NewError("encrypt module needs a database and a cipher", nil, nil))
        }

        if nil != instance.config.Database && nil != instance.config.DatabaseFactory {
            exception.Panic(exception.NewError("encrypt module received both a database and a database factory - set exactly one", nil, nil))
        }

        commands = append(
            commands,
            instance.buildCommand(
                instance.config.Database,
                instance.config.DatabaseFactory,
                instance.config.Cipher,
                kernelInstance,
                "",
                "",
            ),
        )
    }

    for _, contextConfig := range instance.config.Contexts {
        if "" == contextConfig.Name {
            exception.Panic(exception.NewError("encrypt command context name is empty", nil, nil))
        }

        if nil == contextConfig.Cipher || (nil == contextConfig.Database && nil == contextConfig.DatabaseFactory) {
            exception.Panic(
                exception.NewError(
                    "encrypt command context needs a database and a cipher",
                    map[string]any{
                        "context": contextConfig.Name,
                    },
                    nil,
                ),
            )
        }

        if nil != contextConfig.Database && nil != contextConfig.DatabaseFactory {
            exception.Panic(
                exception.NewError(
                    "encrypt command context received both a database and a database factory - set exactly one",
                    map[string]any{
                        "context": contextConfig.Name,
                    },
                    nil,
                ),
            )
        }

        commands = append(
            commands,
            instance.buildCommand(
                contextConfig.Database,
                contextConfig.DatabaseFactory,
                contextConfig.Cipher,
                kernelInstance,
                contextConfig.Name,
                "melody:encrypt:database:"+contextConfig.Name,
            ),
        )
    }

    if 0 == len(commands) {
        return nil
    }

    return commands
}

/* buildCommand assembles one bulk command from exactly one of a prebuilt database and a factory; a factory runs at the first command run against the container captured here. */
func (instance *Module) buildCommand(
    database *bun.DB,
    factory func(resolver containercontract.Resolver) (*bun.DB, error),
    cipher Cipher,
    kernelInstance kernelcontract.Kernel,
    contextName string,
    commandName string,
) clicontract.Command {
    if nil == factory {
        if "" == commandName {
            return NewEncryptDatabaseCommand(database, cipher)
        }

        return NewEncryptDatabaseCommandWithName(database, cipher, commandName)
    }

    if nil == kernelInstance {
        exception.Panic(
            exception.NewError(
                "encrypt module database factory needs a kernel to reach the service container",
                map[string]any{
                    "context": contextName,
                },
                nil,
            ),
        )
    }

    resolver := kernelInstance.ServiceContainer()
    if nil == resolver {
        exception.Panic(
            exception.NewError(
                "encrypt module kernel answered no service container",
                map[string]any{
                    "context": contextName,
                },
                nil,
            ),
        )
    }

    databaseResolver := func() (*bun.DB, error) {
        resolved, resolveErr := factory(resolver)
        if nil != resolveErr {
            return nil, exception.NewError(
                "encrypt module database factory failed",
                map[string]any{
                    "context": contextName,
                },
                resolveErr,
            )
        }

        if nil == resolved {
            return nil, exception.NewError(
                "encrypt module database factory returned a nil database",
                map[string]any{
                    "context": contextName,
                },
                nil,
            )
        }

        return resolved, nil
    }

    if "" == commandName {
        return NewEncryptDatabaseCommandFromResolver(databaseResolver, cipher)
    }

    return NewEncryptDatabaseCommandFromResolverWithName(databaseResolver, cipher, commandName)
}

var (
    _ applicationcontract.Module    = (*Module)(nil)
    _ applicationcontract.CliModule = (*Module)(nil)
)
