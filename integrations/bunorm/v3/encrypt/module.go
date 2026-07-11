package encrypt

import (
    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/uptrace/bun"
)

type ModuleConfig struct {
    Database *bun.DB
    Cipher   Cipher

    /* Contexts adds one bulk command per key compartment — melody:encrypt:database:<name> — for a multi-context binary; it composes with the legacy fields above, which keep the unsuffixed command. */
    Contexts []CommandContextConfig
}

/* CommandContextConfig binds one compartment's database and cipher to a suffixed bulk command. */
type CommandContextConfig struct {
    Name     string
    Database *bun.DB
    Cipher   Cipher
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

    if nil != instance.config.Database && nil != instance.config.Cipher {
        commands = append(commands, Commands(instance.config.Database, instance.config.Cipher)...)
    }

    for _, contextConfig := range instance.config.Contexts {
        if "" == contextConfig.Name {
            exception.Panic(exception.NewError("encrypt command context name is empty", nil, nil))
        }

        if nil == contextConfig.Database || nil == contextConfig.Cipher {
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
                contextConfig.Database,
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

var (
    _ applicationcontract.Module    = (*Module)(nil)
    _ applicationcontract.CliModule = (*Module)(nil)
)
