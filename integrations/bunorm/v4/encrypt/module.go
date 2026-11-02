package encrypt

import (
    "context"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
)

/* ServiceCipher is the service the module publishes its default cipher under, for a component that takes the cipher by injection; a compartment's cipher is reached through its CipherRef only. */
const ServiceCipher = "service.encrypt.cipher"

/* ModuleConfig wires the encrypt module. Cipher is the cipher every EncryptedString and EncryptedDeterministicString column seals and opens through, installed when the module registers its services, so every process that loads the module, http, worker and cli alike, reads its columns. Database or DatabaseFactory, at most one, is the database of the melody:encrypt:database bulk command, a factory evaluated at the first command run so a process in another mode never opens it; with neither, no unsuffixed command is registered, and either one needs the Cipher; a prebuilt Database not of the MySQL dialect is refused. Contexts adds one key compartment each. A module with no Cipher and no Contexts is refused at registration, naming the two fields. An application registers at most one module with a Cipher, since the container refuses ServiceCipher twice; a later application in the same process replaces the earlier one's ciphers. */
type ModuleConfig struct {
    Cipher Cipher

    Database *bun.DB

    /* DatabaseFactory resolves the database for the unsuffixed command at the first command run, after Boot, and a success is memoized. A database it opens is not closed by the container, and a non-MySQL dialect panics at that first run. */
    DatabaseFactory func(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error)

    Contexts []CommandContextConfig
}

/* CommandContextConfig is one key compartment: its Cipher is installed under Name for the columns bound through a CipherRef naming it, and its Database or DatabaseFactory, at most one, serves melody:encrypt:database:<Name>; with neither, the compartment registers no command. An empty or repeated Name and a nil Cipher are refused at registration. The name selects the registry entry and is not bound into the ciphertext, so two compartments holding the same key under the same id decrypt each other, and a provider merging every compartment's keys lets either read the other's rows. */
type CommandContextConfig struct {
    Name            string
    Database        *bun.DB
    Cipher          Cipher
    DatabaseFactory func(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error)
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
    return "installs the ciphers the encrypted columns read and registers the melody:encrypt:database command for bulk encrypt, re-encrypt and decrypt"
}

/* RegisterServices installs the module's ciphers, the default one and one per context, for the columns of this process, a cipher installed under the same name by an earlier registration replaced, and publishes the default cipher as ServiceCipher. A config outside the rules on ModuleConfig and CommandContextConfig panics naming the field. */
func (instance *Module) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    instance.validateConfig()

    /* registered first, so a second module with a Cipher, which the container refuses, installs nothing */
    if nil != instance.config.Cipher {
        cipher := instance.config.Cipher
        registrar.RegisterService(
            ServiceCipher,
            func(resolver containercontract.Resolver) (Cipher, error) {
                return cipher, nil
            },
            containercontract.RegisterOptions{},
        )
    }

    for _, contextConfig := range instance.config.Contexts {
        storeCipher(contextConfig.Name, contextConfig.Cipher)
    }

    if nil != instance.config.Cipher {
        storeCipher(defaultCipherName, instance.config.Cipher)
    }
}

func (instance *Module) RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command {
    instance.validateConfig()

    commands := make([]clicontract.Command, 0)

    if nil != instance.config.Database || nil != instance.config.DatabaseFactory {
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
        if nil == contextConfig.Database && nil == contextConfig.DatabaseFactory {
            continue
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

func (instance *Module) validateConfig() {
    config := instance.config

    if (nil != config.Database || nil != config.DatabaseFactory) && nil == config.Cipher {
        exception.Panic(exception.NewError(
            "encrypt module database needs a cipher",
            map[string]any{"field": "ModuleConfig.Cipher"},
            nil,
        ))
    }

    if nil == config.Cipher && 0 == len(config.Contexts) {
        exception.Panic(exception.NewError(
            "encrypt module needs a cipher or at least one context",
            map[string]any{"fields": []string{"ModuleConfig.Cipher", "ModuleConfig.Contexts"}},
            nil,
        ))
    }

    if nil != config.Cipher && true == isNilCipher(config.Cipher) {
        exception.Panic(exception.NewError("encrypt module cipher is a typed nil", map[string]any{"field": "ModuleConfig.Cipher"}, nil))
    }

    if nil != config.Database && nil != config.DatabaseFactory {
        exception.Panic(exception.NewError(
            "encrypt module received both a database and a database factory - set exactly one",
            map[string]any{"fields": []string{"ModuleConfig.Database", "ModuleConfig.DatabaseFactory"}},
            nil,
        ))
    }

    refuseForeignDialect(config.Database, map[string]any{"field": "ModuleConfig.Database"})

    seenNames := make(map[string]bool, len(config.Contexts))
    for position, contextConfig := range config.Contexts {
        if "" == contextConfig.Name {
            exception.Panic(exception.NewError(
                "encrypt module context name is empty",
                map[string]any{"field": "CommandContextConfig.Name", "index": position},
                nil,
            ))
        }

        if true == seenNames[contextConfig.Name] {
            exception.Panic(exception.NewError(
                "encrypt module context name is repeated",
                map[string]any{"field": "CommandContextConfig.Name", "index": position, "context": contextConfig.Name},
                nil,
            ))
        }

        seenNames[contextConfig.Name] = true

        if true == isNilCipher(contextConfig.Cipher) {
            exception.Panic(exception.NewError(
                "encrypt module context needs a cipher",
                map[string]any{"field": "CommandContextConfig.Cipher", "index": position, "context": contextConfig.Name},
                nil,
            ))
        }

        if nil != contextConfig.Database && nil != contextConfig.DatabaseFactory {
            exception.Panic(exception.NewError(
                "encrypt module context received both a database and a database factory - set exactly one",
                map[string]any{"fields": []string{"CommandContextConfig.Database", "CommandContextConfig.DatabaseFactory"}, "index": position, "context": contextConfig.Name},
                nil,
            ))
        }

        refuseForeignDialect(contextConfig.Database, map[string]any{"field": "CommandContextConfig.Database", "index": position, "context": contextConfig.Name})
    }
}

func refuseForeignDialect(database *bun.DB, refusalContext map[string]any) {
    if nil == database || dialect.MySQL == database.Dialect().Name() {
        return
    }

    refusalContext["dialect"] = database.Dialect().Name().String()
    exception.Panic(exception.NewError("encrypt module database requires a mysql dialect", refusalContext, nil))
}

/* buildCommand assembles one bulk command from exactly one of a prebuilt database and a factory; a factory runs at the first command run against the container captured here. */
func (instance *Module) buildCommand(
    database *bun.DB,
    factory func(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error),
    cipher Cipher,
    kernelInstance kernelcontract.Kernel,
    contextName string,
    commandName string,
) clicontract.Command {
    if nil == factory {
        return NewEncryptDatabaseCommand(cipher, EncryptDatabaseCommandConfig{Database: database, CommandName: commandName})
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

    databaseResolver := func(ctx context.Context) (*bun.DB, error) {
        resolved, resolveErr := factory(ctx, resolver)
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

    return NewEncryptDatabaseCommand(cipher, EncryptDatabaseCommandConfig{DatabaseResolver: databaseResolver, CommandName: commandName})
}

var (
    _ applicationcontract.Module        = (*Module)(nil)
    _ applicationcontract.ServiceModule = (*Module)(nil)
    _ applicationcontract.CliModule     = (*Module)(nil)
)
