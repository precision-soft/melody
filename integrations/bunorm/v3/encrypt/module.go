package encrypt

import (
    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/uptrace/bun"
)

/* ModuleConfig wires the melody:encrypt:database bulk command. Hand in a prebuilt *bun.DB, or — for an app whose database is resolved from a registry per context and is not available before Boot — a DatabaseFactory: it is evaluated at RegisterCliCommands time, when the container is available, rather than eagerly in main.go, so a multi-database binary can use the built-in command against a registry-resolved database. The factory wins when both are set. */
type ModuleConfig struct {
    Database *bun.DB
    Cipher   Cipher

    /* DatabaseFactory resolves the database for the unsuffixed command at RegisterCliCommands time; use it instead of Database when the database comes from a resolver-backed registry. */
    DatabaseFactory func(resolver containercontract.Resolver) (*bun.DB, error)

    /* Contexts adds one bulk command per key compartment — melody:encrypt:database:<name> — for a multi-context binary; it composes with the legacy fields above, which keep the unsuffixed command. */
    Contexts []CommandContextConfig
}

/* CommandContextConfig binds one compartment's database and cipher to a suffixed bulk command. Supply a DatabaseFactory instead of Database when the compartment's database is resolved from a registry. */
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

    database := instance.resolveDatabase(instance.config.Database, instance.config.DatabaseFactory, kernelInstance, "")
    if nil != database && nil != instance.config.Cipher {
        commands = append(commands, Commands(database, instance.config.Cipher)...)
    }

    for _, contextConfig := range instance.config.Contexts {
        if "" == contextConfig.Name {
            exception.Panic(exception.NewError("encrypt command context name is empty", nil, nil))
        }

        contextDatabase := instance.resolveDatabase(
            contextConfig.Database,
            contextConfig.DatabaseFactory,
            kernelInstance,
            contextConfig.Name,
        )

        if nil == contextDatabase || nil == contextConfig.Cipher {
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

        commands = append(
            commands,
            NewEncryptDatabaseCommandWithName(
                contextDatabase,
                contextConfig.Cipher,
                "melody:encrypt:database:"+contextConfig.Name,
            ),
        )
    }

    if 0 == len(commands) {
        return nil
    }

    return commands
}

/* resolveDatabase prefers a prebuilt database and otherwise runs the factory against the container resolver at this RegisterCliCommands phase, so a registry-resolved database is opened when the container is available rather than in main.go before Boot. The container is read from the kernel only when a factory is actually present, so a module configured with prebuilt databases still works with a nil kernel. */
func (instance *Module) resolveDatabase(
    database *bun.DB,
    factory func(resolver containercontract.Resolver) (*bun.DB, error),
    kernelInstance kernelcontract.Kernel,
    contextName string,
) *bun.DB {
    if nil != database {
        return database
    }

    if nil == factory {
        return nil
    }

    var resolver containercontract.Resolver
    if nil != kernelInstance {
        resolver = kernelInstance.ServiceContainer()
    }

    resolved, resolveErr := factory(resolver)
    if nil != resolveErr {
        exception.Panic(
            exception.NewError(
                "encrypt module database factory failed",
                map[string]any{
                    "context": contextName,
                },
                resolveErr,
            ),
        )
    }

    return resolved
}

var (
    _ applicationcontract.Module    = (*Module)(nil)
    _ applicationcontract.CliModule = (*Module)(nil)
)
