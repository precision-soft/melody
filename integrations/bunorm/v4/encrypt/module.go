package encrypt

import (
    "context"
    "reflect"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
)

/* ServiceCipher is the service the module publishes its default cipher under, for a component that takes the cipher by injection; a compartment's cipher is reached through its CipherReference only. */
const ServiceCipher = "service.encrypt.cipher"

/* ModuleConfig wires the encrypt module. Cipher is the cipher every EncryptedString and EncryptedDeterministicString column seals and opens through, installed when the module registers its services, so every process that loads the module, http, worker and cli alike, reads its columns. Database or DatabaseFactory, at most one, is the database of the melody:encrypt:database bulk command, a factory evaluated at the first command run so a process in another mode never opens it; with neither, no unsuffixed command is registered, and either one needs the Cipher; a prebuilt Database not of the MySQL dialect is refused. Contexts adds one key compartment each. A module with no Cipher and no Contexts is refused at registration, naming the two fields. An application registers the module once, since a module's name is its identity, so every compartment and every binding is declared on one ModuleConfig; a later application in the same process replaces the whole cipher and binding tables of the earlier one, which keeps none of its names or markers. */
type ModuleConfig struct {
    Cipher Cipher

    Database *bun.DB

    /* DatabaseFactory resolves the database for the unsuffixed command at the first command run, after Boot, and a success is memoized. A database it opens is not closed by the container, and a non-MySQL dialect panics at that first run. */
    DatabaseFactory func(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error)

    Contexts []CommandContextConfig

    /* SealBindings declares every bound column of the process, one entry per marker type. */
    SealBindings []SealBindingConfig
}

/* SealBindingConfig declares one bound column. Reference is the marker value its column type is parameterized by. RefuseUnbound, false when left out, makes the column read nothing but a seal bound to it: a seal without a binding and a value without the marker are refused, on the column's Scan and in its equality candidates. Leave it out until every value of the column is a bound seal: deploy the declaration, run melody:encrypt:database --mode encrypt over the column, which seals its plain values and re-seals its unbound seals under the binding, then set it. A declaration the module cannot hold is refused at registration naming ModuleConfig.SealBindings and the index: a nil, typed-nil or pointer-form Reference, a label outside <table>.<column> with each part of [A-Za-z0-9_]{1,64}, a cipher name the module installs no cipher under, a marker with fields, with SealBinding on its pointer receiver or embedding DefaultCipherReference by pointer, a marker declared twice, a label declared twice over one cipher instance under one name or two, and a cipher implemented outside the package. A later registration replaces the whole binding table, so a marker it does not declare is unbound. */
type SealBindingConfig struct {
    Reference     SealBindingReference
    RefuseUnbound bool
}

/* CommandContextConfig is one key compartment: its Cipher is installed under Name for the columns bound through a CipherReference naming it, and its Database or DatabaseFactory, at most one, serves melody:encrypt:database:<Name>; with neither, the compartment registers no command. An empty or repeated Name and a nil Cipher are refused at registration. The name selects the registry entry and is not bound into the ciphertext, so two compartments holding the same key under the same id decrypt each other, and a provider merging every compartment's keys lets either read the other's rows. */
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

/* RegisterServices installs the module's ciphers, the default one and one per context, and its seal bindings for the columns of this process, replacing the whole tables an earlier registration installed, and publishes the default cipher as ServiceCipher. A config outside the rules on ModuleConfig and CommandContextConfig panics naming the field. */
func (instance *Module) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    instance.validateConfig()

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

    ciphers := make(map[string]Cipher, len(instance.config.Contexts)+1)
    for _, contextConfig := range instance.config.Contexts {
        ciphers[contextConfig.Name] = contextConfig.Cipher
    }

    if nil != instance.config.Cipher {
        ciphers[defaultCipherName] = instance.config.Cipher
    }

    bindings := make(map[reflect.Type]sealBinding, len(instance.config.SealBindings))
    for _, bindingConfig := range instance.config.SealBindings {
        marker := reflect.TypeOf(bindingConfig.Reference)
        bindings[marker] = sealBinding{label: reflect.Zero(marker).Interface().(SealBindingReference).SealBinding(), refuseUnbound: bindingConfig.RefuseUnbound}
    }

    replaceRegistries(ciphers, bindings)
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
                instance.sealBindingsOf(defaultCipherName),
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
                instance.sealBindingsOf(contextConfig.Name),
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

    instance.validateSealBindings()
}

func (instance *Module) validateSealBindings() {
    config := instance.config

    ciphersByName := make(map[string]Cipher, len(config.Contexts)+1)
    for _, contextConfig := range config.Contexts {
        ciphersByName[contextConfig.Name] = contextConfig.Cipher
    }

    if nil != config.Cipher {
        ciphersByName[defaultCipherName] = config.Cipher
    }

    seenMarkers := make(map[reflect.Type]bool, len(config.SealBindings))
    seenLabels := make(map[sealBindingLabelKey]bool, len(config.SealBindings))

    for position, bindingConfig := range config.SealBindings {
        refuse := func(message string, extra map[string]any) {
            refusalContext := map[string]any{"field": "ModuleConfig.SealBindings", "index": position}
            for key, value := range extra {
                refusalContext[key] = value
            }

            exception.Panic(exception.NewError(message, refusalContext, nil))
        }

        if nil == bindingConfig.Reference {
            refuse("encrypt module seal binding has no Reference", nil)
        }

        marker := reflect.TypeOf(bindingConfig.Reference)
        if reflect.Pointer == marker.Kind() {
            refuse("encrypt module seal binding Reference is a pointer; a marker must be a zero-size value type", map[string]any{"cipherReference": marker.String()})
        }

        if defect := markerShapeDefect(marker); "" != defect {
            refuse(defect, map[string]any{"cipherReference": marker.String()})
        }

        label := bindingConfig.Reference.SealBinding()
        if false == sealBindingLabelPattern.MatchString(label) {
            refuse("encrypt module seal binding label must match "+sealBindingLabelPattern.String(), map[string]any{"cipherReference": marker.String(), "sealBinding": label})
        }

        name := bindingConfig.Reference.CipherName()
        if _, selectsDefault := bindingConfig.Reference.(defaultCipherSelector); defaultCipherName == name && false == selectsDefault {
            refuse("encrypt module seal binding names the empty compartment without embedding DefaultCipherReference", map[string]any{"cipherReference": marker.String()})
        }

        cipherInstance, installed := ciphersByName[name]
        if false == installed {
            refuse("encrypt module seal binding names a cipher the module does not install", map[string]any{"cipherReference": marker.String(), "cipherName": name})
        }

        if true == seenMarkers[marker] {
            refuse("encrypt module seal binding declares a marker twice", map[string]any{"cipherReference": marker.String()})
        }

        seenMarkers[marker] = true

        if _, carriesBinding := cipherInstance.(bindingSealer); false == carriesBinding {
            refuse("encrypt module seal binding needs the package cipher; a cipher implemented outside the package, a wrapper included, cannot bind a seal", map[string]any{"cipherReference": marker.String(), "cipherName": name})
        }

        /* two markers of one label over one cipher, under one name or two, would open each other's seals, the separation the binding is for lost without a word; the cipher is the package's or the fake, a comparable pointer */
        labelKey := sealBindingLabelKey{cipher: cipherInstance, label: label}
        if true == seenLabels[labelKey] {
            refuse("encrypt module seal binding declares a label twice under one cipher", map[string]any{"cipherReference": marker.String(), "sealBinding": label, "cipherName": name})
        }

        seenLabels[labelKey] = true
    }
}

type sealBindingLabelKey struct {
    cipher Cipher
    label  string
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
    sealBindings []SealBindingConfig,
) clicontract.Command {
    if nil == factory {
        return NewEncryptDatabaseCommand(cipher, EncryptDatabaseCommandConfig{Database: database, CommandName: commandName, SealBindings: sealBindings})
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

    return NewEncryptDatabaseCommand(cipher, EncryptDatabaseCommandConfig{DatabaseResolver: databaseResolver, CommandName: commandName, SealBindings: sealBindings})
}

/* sealBindingsOf answers the declarations of one compartment, the ones a command over its cipher reads */
func (instance *Module) sealBindingsOf(cipherName string) []SealBindingConfig {
    var sealBindings []SealBindingConfig

    for _, bindingConfig := range instance.config.SealBindings {
        if cipherName == bindingConfig.Reference.CipherName() {
            sealBindings = append(sealBindings, bindingConfig)
        }
    }

    return sealBindings
}

var (
    _ applicationcontract.Module        = (*Module)(nil)
    _ applicationcontract.ServiceModule = (*Module)(nil)
    _ applicationcontract.CliModule     = (*Module)(nil)
)
