package encrypt

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "fmt"
    "io"
    "reflect"
    "strings"
    "testing"

    melodycli "github.com/precision-soft/melody/v4/cli"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/runtime"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
    "github.com/uptrace/bun/dialect/mysqldialect"
    "github.com/uptrace/bun/schema"
)

type fakeConnector struct{}

func (instance fakeConnector) Connect(context.Context) (driver.Conn, error) {
    return nil, errors.New("fake connector never connects")
}

func (instance fakeConnector) Driver() driver.Driver {
    return fakeDriver{}
}

type fakeDriver struct{}

func (instance fakeDriver) Open(string) (driver.Conn, error) {
    return nil, errors.New("fake driver never opens")
}

func newMysqlDatabase() *bun.DB {
    return bun.NewDB(sql.OpenDB(fakeConnector{}), mysqldialect.New())
}

type moduleRegistrar struct {
    containercontract.Container
}

func (instance moduleRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.MustRegister(serviceName, provider, options)
}

func newModuleRegistrar() moduleRegistrar {
    return moduleRegistrar{Container: container.NewContainer(container.ContainerConfig{})}
}

func TestModule_RefusesAConfigWithNoCipherAndNoContexts(t *testing.T) {
    assertPanicsWithMessage(t, "encrypt module needs a cipher or at least one context", func() {
        NewModule(ModuleConfig{}).RegisterServices(newModuleRegistrar())
    })

    assertPanicsWithMessage(t, "encrypt module needs a cipher or at least one context", func() {
        NewModule(ModuleConfig{}).RegisterCliCommands(nil)
    })
}

func TestModule_RefusesADatabaseWithoutACipher(t *testing.T) {
    factory := func(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
        return newMysqlDatabase(), nil
    }

    for name, config := range map[string]ModuleConfig{
        "database":                  {Database: newMysqlDatabase()},
        "factory":                   {DatabaseFactory: factory},
        "database beside a context": {Database: newMysqlDatabase(), Contexts: []CommandContextConfig{{Name: "payment", Cipher: NewFakeCipher()}}},
    } {
        t.Run(name, func(t *testing.T) {
            defer func() {
                recoveredErr, isErr := recover().(*exception.Error)
                if false == isErr || "encrypt module database needs a cipher" != recoveredErr.Message() || "ModuleConfig.Cipher" != recoveredErr.Context()["field"] {
                    t.Fatalf("expected the missing cipher named, got %v", recoveredErr)
                }
            }()

            NewModule(config).RegisterCliCommands(nil)
        })
    }
}

/* foreignDialect answers another name over the mysql dialect, since the module requires no second dialect */
type foreignDialect struct {
    schema.Dialect
}

func (instance foreignDialect) Name() dialect.Name {
    return dialect.PG
}

func newForeignDatabase() *bun.DB {
    return bun.NewDB(sql.OpenDB(fakeConnector{}), foreignDialect{Dialect: mysqldialect.New()})
}

func TestModule_RefusesAForeignDialectBeforeInstallingAnything(t *testing.T) {
    previousDefault := NewFakeCipher()
    previousNamed := NewFakeCipher()
    installCipher(t, defaultCipherName, previousDefault)
    installCipher(t, "crm", previousNamed)

    for name, config := range map[string]ModuleConfig{
        "module":  {Cipher: NewFakeCipher(), Database: newForeignDatabase()},
        "context": {Contexts: []CommandContextConfig{{Name: "crm", Cipher: NewFakeCipher()}, {Name: "billing", Cipher: NewFakeCipher(), Database: newForeignDatabase()}}},
    } {
        t.Run(name, func(t *testing.T) {
            assertPanicsWithMessage(t, "encrypt module database requires a mysql dialect", func() {
                NewModule(config).RegisterServices(newModuleRegistrar())
            })

            resolvedDefault, _ := cipherByName(defaultCipherName)
            resolvedNamed, _ := cipherByName("crm")
            if previousDefault != resolvedDefault || previousNamed != resolvedNamed {
                t.Fatalf("expected the refused registration to leave the table as it was")
            }
        })
    }
}

func TestModule_ASecondModuleWithACipherInOneApplicationInstallsNothing(t *testing.T) {
    installCipher(t, defaultCipherName, NewFakeCipher())
    first := NewFakeCipher()
    registrar := newModuleRegistrar()

    NewModule(ModuleConfig{Cipher: first}).RegisterServices(registrar)

    assertPanics(t, func() {
        NewModule(ModuleConfig{Cipher: NewFakeCipher(), Contexts: []CommandContextConfig{{Name: "crm", Cipher: NewFakeCipher()}}}).RegisterServices(registrar)
    })

    resolved, resolveErr := cipherByName(defaultCipherName)
    if nil != resolveErr || first != resolved {
        t.Fatalf("expected the first module's cipher kept: %v %v", resolved, resolveErr)
    }

    if _, namedErr := cipherByName("crm"); nil == namedErr {
        t.Fatalf("expected the refused module's context left uninstalled")
    }
}

func TestModule_RefusesATypedNilCipher(t *testing.T) {
    var cipherInstance *fakeCipher

    assertPanicsWithMessage(t, "encrypt module cipher is a typed nil", func() {
        NewModule(ModuleConfig{Cipher: cipherInstance}).RegisterServices(newModuleRegistrar())
    })
}

func TestModule_RefusesARepeatedContextName(t *testing.T) {
    assertPanicsWithMessage(t, "encrypt module context name is repeated", func() {
        NewModule(ModuleConfig{
            Contexts: []CommandContextConfig{
                {Name: "payment", Cipher: NewFakeCipher()},
                {Name: "payment", Cipher: NewFakeCipher()},
            },
        }).RegisterServices(newModuleRegistrar())
    })
}

func TestModule_ACipherWithoutADatabaseInstallsTheCipherAndRegistersNoCommand(t *testing.T) {
    installCipher(t, defaultCipherName, NewFakeCipher())
    cipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))
    module := NewModule(ModuleConfig{Cipher: cipher})

    module.RegisterServices(newModuleRegistrar())

    stored, valueErr := EncryptedString("personal data").Value()
    if nil != valueErr {
        t.Fatalf("expected the column to seal through the installed cipher: %v", valueErr)
    }

    if "personal data" == string(stored.([]byte)) {
        t.Fatalf("expected the value sealed, got the plaintext")
    }

    opened, openErr := cipher.Decrypt(string(stored.([]byte)))
    if nil != openErr || "personal data" != opened {
        t.Fatalf("expected the module's cipher to have sealed the value: %q %v", opened, openErr)
    }

    resolved, resolveErr := cipherByName(defaultCipherName)
    if nil != resolveErr || cipher != resolved {
        t.Fatalf("expected the module's cipher installed as the default: %v %v", resolved, resolveErr)
    }

    if commands := module.RegisterCliCommands(nil); nil != commands {
        t.Fatalf("expected no command without a database, got %d", len(commands))
    }
}

func TestModule_PublishesTheDefaultCipherAsAService(t *testing.T) {
    installCipher(t, defaultCipherName, NewFakeCipher())
    cipher := NewFakeCipher()
    registrar := newModuleRegistrar()

    NewModule(ModuleConfig{Cipher: cipher}).RegisterServices(registrar)

    resolved, resolveErr := container.FromResolver[Cipher](t.Context(), registrar.Container, ServiceCipher)
    if nil != resolveErr || cipher != resolved {
        t.Fatalf("expected the default cipher under ServiceCipher: %v %v", resolved, resolveErr)
    }
}

func TestModule_ASecondRegistrationReplacesTheDefaultCipher(t *testing.T) {
    installCipher(t, defaultCipherName, NewFakeCipher())
    first := NewFakeCipher()
    second := NewFakeCipher()

    NewModule(ModuleConfig{Cipher: first}).RegisterServices(newModuleRegistrar())
    NewModule(ModuleConfig{Cipher: second}).RegisterServices(newModuleRegistrar())

    resolved, resolveErr := cipherByName(defaultCipherName)
    if nil != resolveErr || second != resolved {
        t.Fatalf("expected the second registration's cipher: %v %v", resolved, resolveErr)
    }
}

func TestModule_AContextWithoutADatabaseInstallsItsCipherAndRegistersNoCommand(t *testing.T) {
    defaultCipher := NewFakeCipher()
    installCipher(t, defaultCipherName, defaultCipher)
    installCipher(t, "crm", NewFakeCipher())
    cipher := NewFakeCipher()
    module := NewModule(ModuleConfig{Contexts: []CommandContextConfig{{Name: "crm", Cipher: cipher}}})

    module.RegisterServices(newModuleRegistrar())

    resolved, resolveErr := cipherByName("crm")
    if nil != resolveErr || cipher != resolved {
        t.Fatalf("expected the context's cipher under its name: %v %v", resolved, resolveErr)
    }

    resolvedDefault, defaultErr := cipherByName(defaultCipherName)
    if nil != defaultErr || defaultCipher != resolvedDefault {
        t.Fatalf("expected the default entry left as it was: %v %v", resolvedDefault, defaultErr)
    }

    if commands := module.RegisterCliCommands(nil); nil != commands {
        t.Fatalf("expected no command for a context without a database, got %d", len(commands))
    }
}

func TestModule_ARefusedRegistrationInstallsNothing(t *testing.T) {
    previous := NewFakeCipher()
    installCipher(t, "crm", previous)

    assertPanics(t, func() {
        NewModule(ModuleConfig{
            Contexts: []CommandContextConfig{
                {Name: "crm", Cipher: NewFakeCipher()},
                {Name: "", Cipher: NewFakeCipher()},
            },
        }).RegisterServices(newModuleRegistrar())
    })

    resolved, resolveErr := cipherByName("crm")
    if nil != resolveErr || previous != resolved {
        t.Fatalf("expected the refused registration to leave the entry as it was: %v %v", resolved, resolveErr)
    }
}

func TestModule_RegisterCliCommandsExposesEncryptDatabaseCommand(t *testing.T) {
    cipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))

    commands := NewModule(ModuleConfig{Database: newMysqlDatabase(), Cipher: cipher}).RegisterCliCommands(nil)

    if 1 != len(commands) {
        t.Fatalf("expected exactly one command, got %d", len(commands))
    }

    if "melody:encrypt:database" != commands[0].Name() {
        t.Fatalf("expected the melody:encrypt:database command, got %q", commands[0].Name())
    }
}

func TestModule_RegisterCliCommandsComposesTheDefaultAndContexts(t *testing.T) {
    database := newMysqlDatabase()

    module := NewModule(ModuleConfig{
        Database: database,
        Cipher:   NewFakeCipher(),
        Contexts: []CommandContextConfig{
            {Name: "platform", Database: database, Cipher: NewFakeCipher()},
            {Name: "payment", Database: database, Cipher: NewFakeCipher()},
        },
    })

    commands := module.RegisterCliCommands(nil)

    names := make(map[string]bool, len(commands))
    for _, command := range commands {
        names[command.Name()] = true
    }

    if false == names["melody:encrypt:database"] {
        t.Fatalf("expected the unsuffixed command, got: %v", names)
    }

    if false == names["melody:encrypt:database:platform"] || false == names["melody:encrypt:database:payment"] {
        t.Fatalf("expected the per-context commands, got: %v", names)
    }
}

func TestModule_RegisterCliCommandsPanicsOnInvalidContext(t *testing.T) {
    database := newMysqlDatabase()

    assertPanics(t, func() {
        NewModule(ModuleConfig{
            Contexts: []CommandContextConfig{{Name: "", Database: database, Cipher: NewFakeCipher()}},
        }).RegisterCliCommands(nil)
    })

    assertPanics(t, func() {
        NewModule(ModuleConfig{
            Contexts: []CommandContextConfig{{Name: "payment"}},
        }).RegisterCliCommands(nil)
    })
}

func newFakeKernel() *fakeKernel {
    return &fakeKernel{serviceContainer: container.NewContainer(container.ContainerConfig{})}
}

type fakeKernel struct {
    serviceContainer containercontract.Container
}

func (instance *fakeKernel) Environment() string {
    return "test"
}

func (instance *fakeKernel) DebugMode() bool {
    return false
}

func (instance *fakeKernel) ServiceContainer() containercontract.Container {
    return instance.serviceContainer
}

func (instance *fakeKernel) EventDispatcher() eventcontract.EventDispatcher {
    return nil
}

func (instance *fakeKernel) Config() configcontract.Configuration {
    return nil
}

func (instance *fakeKernel) HttpRouter() httpcontract.Router {
    return nil
}

func (instance *fakeKernel) Context() context.Context {
    return context.Background()
}

func (instance *fakeKernel) HttpKernel() httpcontract.Kernel {
    return nil
}

func (instance *fakeKernel) Clock() clockcontract.Clock {
    return nil
}

var _ kernelcontract.Kernel = (*fakeKernel)(nil)

/* runEncryptCommand drives one command Run through a parsed cli context with the command's own flag defaults and returns the error the command's Run reported. */
func runEncryptCommand(t *testing.T, command clicontract.Command, arguments []string) error {
    t.Helper()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    capturing := &capturingCommand{Command: command, runtimeInstance: runtimeInstance}

    if executeErr := melodycli.DispatchCommand(
        context.Background(),
        capturing,
        runtimeInstance,
        arguments,
        io.Discard,
    ); nil != executeErr {
        t.Fatalf("cli command execution failed: %v", executeErr)
    }

    return capturing.capturedErr
}

func assertPanicsWithMessage(t *testing.T, expected string, callback func()) {
    t.Helper()

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected a panic containing %q", expected)
        }

        if false == strings.Contains(fmt.Sprintf("%v", recovered), expected) {
            t.Fatalf("expected the panic to mention %q, got %v", expected, recovered)
        }
    }()

    callback()
}

func TestModule_RegisterCliCommandsDoesNotInvokeDatabaseFactory(t *testing.T) {
    database := newMysqlDatabase()

    factoryCalls := 0
    module := NewModule(ModuleConfig{
        Cipher: NewFakeCipher(),
        DatabaseFactory: func(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
            if nil == resolver {
                t.Errorf("expected the captured service container resolver, got nil")
            }

            factoryCalls++

            return database, nil
        },
    })

    commands := module.RegisterCliCommands(newFakeKernel())

    if 0 != factoryCalls {
        t.Fatalf("expected the database factory to stay untouched at RegisterCliCommands, got %d calls", factoryCalls)
    }

    if 1 != len(commands) || "melody:encrypt:database" != commands[0].Name() {
        t.Fatalf("expected the melody:encrypt:database command from the factory, got %+v", commands)
    }

    _ = runEncryptCommand(t, commands[0], []string{commands[0].Name()})
    _ = runEncryptCommand(t, commands[0], []string{commands[0].Name()})

    if 1 != factoryCalls {
        t.Fatalf("expected exactly one factory evaluation across two command runs, got %d", factoryCalls)
    }
}

func TestModule_RegisterCliCommandsBuildsPerContextFromFactory(t *testing.T) {
    database := newMysqlDatabase()

    factoryCalls := 0
    module := NewModule(ModuleConfig{
        Contexts: []CommandContextConfig{
            {
                Name:   "payment",
                Cipher: NewFakeCipher(),
                DatabaseFactory: func(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
                    factoryCalls++

                    return database, nil
                },
            },
        },
    })

    commands := module.RegisterCliCommands(newFakeKernel())

    if 0 != factoryCalls {
        t.Fatalf("expected the per-context database factory to stay untouched at RegisterCliCommands, got %d calls", factoryCalls)
    }

    if 1 != len(commands) || "melody:encrypt:database:payment" != commands[0].Name() {
        t.Fatalf("expected the per-context command from the factory, got %+v", commands)
    }

    _ = runEncryptCommand(t, commands[0], []string{commands[0].Name()})

    if 1 != factoryCalls {
        t.Fatalf("expected the per-context factory to be evaluated at the first command run, got %d calls", factoryCalls)
    }
}

func TestModule_RegisterCliCommandsPanicsOnDatabaseAndFactoryBothSet(t *testing.T) {
    assertPanicsWithMessage(t, "encrypt module received both a database and a database factory - set exactly one", func() {
        NewModule(ModuleConfig{
            Database: newMysqlDatabase(),
            Cipher:   NewFakeCipher(),
            DatabaseFactory: func(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
                return newMysqlDatabase(), nil
            },
        }).RegisterCliCommands(newFakeKernel())
    })
}

func TestModule_RegisterCliCommandsPanicsOnContextDatabaseAndFactoryBothSet(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected a panic for a context with both a database and a database factory")
        }

        exceptionErr, ok := recovered.(*exception.Error)
        if false == ok {
            t.Fatalf("expected an *exception.Error panic, got %T", recovered)
        }

        if "encrypt module context received both a database and a database factory - set exactly one" != exceptionErr.Message() {
            t.Fatalf("unexpected panic message %q", exceptionErr.Message())
        }

        if "payment" != exceptionErr.Context()["context"] {
            t.Fatalf("expected the context name on the panic, got %v", exceptionErr.Context())
        }
    }()

    NewModule(ModuleConfig{
        Contexts: []CommandContextConfig{
            {
                Name:     "payment",
                Database: newMysqlDatabase(),
                Cipher:   NewFakeCipher(),
                DatabaseFactory: func(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
                    return newMysqlDatabase(), nil
                },
            },
        },
    }).RegisterCliCommands(newFakeKernel())
}

func TestModule_CommandRunSurfacesDatabaseFactoryError(t *testing.T) {
    module := NewModule(ModuleConfig{
        Cipher: NewFakeCipher(),
        DatabaseFactory: func(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
            return nil, errFactory
        },
    })

    commands := module.RegisterCliCommands(newFakeKernel())

    if 1 != len(commands) {
        t.Fatalf("expected the command to register despite the failing factory, got %d commands", len(commands))
    }

    runErr := runEncryptCommand(t, commands[0], []string{commands[0].Name()})

    if nil == runErr {
        t.Fatalf("expected the factory failure to surface from the command run")
    }

    if false == strings.Contains(runErr.Error(), "encrypt module database factory failed") {
        t.Fatalf("expected the descriptive factory error, got %v", runErr)
    }

    if false == errors.Is(runErr, errFactory) {
        t.Fatalf("expected the factory cause to stay wrapped, got %v", runErr)
    }
}

func TestModule_CommandRunSurfacesContextNameOnFactoryError(t *testing.T) {
    module := NewModule(ModuleConfig{
        Contexts: []CommandContextConfig{
            {
                Name:   "payment",
                Cipher: NewFakeCipher(),
                DatabaseFactory: func(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
                    return nil, errFactory
                },
            },
        },
    })

    commands := module.RegisterCliCommands(newFakeKernel())

    if 1 != len(commands) {
        t.Fatalf("expected the per-context command to register despite the failing factory, got %d commands", len(commands))
    }

    runErr := runEncryptCommand(t, commands[0], []string{commands[0].Name()})

    var exceptionErr *exception.Error
    if false == errors.As(runErr, &exceptionErr) {
        t.Fatalf("expected an exception error from the factory failure, got %v", runErr)
    }

    if "encrypt module database factory failed" != exceptionErr.Message() {
        t.Fatalf("expected the descriptive factory error, got %q", exceptionErr.Message())
    }

    if "payment" != exceptionErr.Context()["context"] {
        t.Fatalf("expected the context name on the factory error, got %v", exceptionErr.Context())
    }
}

func TestModule_RegisterCliCommandsPanicsOnFactoryWithoutKernel(t *testing.T) {
    factory := func(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
        return newMysqlDatabase(), nil
    }

    assertPanicsWithMessage(t, "kernel", func() {
        NewModule(ModuleConfig{
            Cipher:          NewFakeCipher(),
            DatabaseFactory: factory,
        }).RegisterCliCommands(nil)
    })

    assertPanicsWithMessage(t, "kernel", func() {
        NewModule(ModuleConfig{
            Contexts: []CommandContextConfig{
                {Name: "payment", Cipher: NewFakeCipher(), DatabaseFactory: factory},
            },
        }).RegisterCliCommands(nil)
    })
}

func TestModule_RegisterCliCommandsPanicsOnFactoryWithoutCipher(t *testing.T) {
    assertPanics(t, func() {
        NewModule(ModuleConfig{
            DatabaseFactory: func(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
                return newMysqlDatabase(), nil
            },
        }).RegisterCliCommands(newFakeKernel())
    })
}

var errFactory = &factoryError{}

type factoryError struct{}

func (instance *factoryError) Error() string {
    return "factory failed"
}

/* a kernel answering no service container is refused at registration, rather than captured into the closure to fail at the first command run, far from the registration that produced it */
func TestModule_RefusesAKernelWithoutAServiceContainer(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected the missing service container to be refused at registration")
        }

        if false == strings.Contains(fmt.Sprintf("%v", recovered), "no service container") {
            t.Fatalf("expected the panic to name the missing container, got %v", recovered)
        }
    }()

    module := NewModule(ModuleConfig{
        Cipher: NewFakeCipher(),
        DatabaseFactory: func(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
            return newMysqlDatabase(), nil
        },
    })

    module.RegisterCliCommands(&fakeKernel{})
}

type factoryContextKey struct{}

func TestModule_TheFactoryResolvesUnderTheRunsContext(t *testing.T) {
    observed := make(chan any, 1)

    module := NewModule(ModuleConfig{
        Cipher: NewFakeCipher(),
        DatabaseFactory: func(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
            observed <- ctx.Value(factoryContextKey{})

            return newMysqlDatabase(), nil
        },
    })

    commands := module.RegisterCliCommands(newFakeKernel())

    command, isCommand := commands[0].(*EncryptDatabaseCommand)
    if false == isCommand {
        t.Fatalf("expected the encrypt database command, got %T", commands[0])
    }

    runContext := context.WithValue(t.Context(), factoryContextKey{}, "run")

    if _, resolveErr := command.resolveMigrator(runContext); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if "run" != <-observed {
        t.Fatalf("expected the factory to receive the run's context")
    }
}

/* labelBinding carries its label as a field, the shape a marker may not have, since its column reads the zero value */
type labelBinding struct {
    label string
}

func (instance labelBinding) CipherName() string {
    return "test:crm"
}

func (instance labelBinding) SealBinding() string {
    return instance.label
}

type pointerBinding struct{}

func (instance *pointerBinding) CipherName() string {
    return "test:crm"
}

func (instance *pointerBinding) SealBinding() string {
    return "customer.pointer"
}

type otherCompartmentBinding struct{}

func (instance otherCompartmentBinding) CipherName() string {
    return "test:other"
}

func (instance otherCompartmentBinding) SealBinding() string {
    return "customer.other"
}

type emptyNameBinding struct{}

func (instance emptyNameBinding) CipherName() string {
    return ""
}

func (instance emptyNameBinding) SealBinding() string {
    return "customer.empty"
}

type badLabelBinding struct{}

func (instance badLabelBinding) CipherName() string {
    return "test:crm"
}

func (instance badLabelBinding) SealBinding() string {
    return "users"
}

type pointerDefaultBinding struct {
    *DefaultCipherRef
}

func (instance pointerDefaultBinding) SealBinding() string {
    return "users.pointer_default"
}

type secondCompartmentIbanBinding struct{}

func (instance secondCompartmentIbanBinding) CipherName() string {
    return "test:crm2"
}

func (instance secondCompartmentIbanBinding) SealBinding() string {
    return "customer.iban"
}

type sameLabelBinding struct{}

func (instance sameLabelBinding) CipherName() string {
    return "test:crm"
}

func (instance sameLabelBinding) SealBinding() string {
    return "customer.iban"
}

func withDefaultCipher(config ModuleConfig) ModuleConfig {
    config.Cipher = NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))

    return config
}

func newBindingModuleConfig(bindings ...SealBindingConfig) ModuleConfig {
    return ModuleConfig{
        Contexts:     []CommandContextConfig{{Name: "test:crm", Cipher: NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "crm-v1", Keys: map[string][]byte{"crm-v1": newKey(11)}}))}},
        SealBindings: bindings,
    }
}

/* a declaration the module cannot hold is refused at registration naming the field and the index */
func TestModule_RefusesASealBindingItCannotHold(t *testing.T) {
    valid := SealBindingConfig{Ref: crmIbanBinding{}}

    cases := map[string]ModuleConfig{
        "a nil Ref":                         newBindingModuleConfig(valid, SealBindingConfig{}),
        "a typed nil Ref":                   newBindingModuleConfig(valid, SealBindingConfig{Ref: (*pointerBinding)(nil)}),
        "a pointer marker":                  newBindingModuleConfig(valid, SealBindingConfig{Ref: &pointerBinding{}}),
        "an unknown cipher name":            newBindingModuleConfig(valid, SealBindingConfig{Ref: otherCompartmentBinding{}}),
        "an empty name without the default": withDefaultCipher(newBindingModuleConfig(valid, SealBindingConfig{Ref: emptyNameBinding{}})),
        "the default with no Cipher":        newBindingModuleConfig(valid, SealBindingConfig{Ref: defaultSecretBinding{}}),
        "the marker twice":                  newBindingModuleConfig(valid, SealBindingConfig{Ref: crmIbanBinding{}, RefuseUnbound: true}),
        "a marker with fields":              newBindingModuleConfig(valid, SealBindingConfig{Ref: labelBinding{label: "customer.fields"}}),
        "DefaultCipherRef by pointer":       withDefaultCipher(newBindingModuleConfig(valid, SealBindingConfig{Ref: pointerDefaultBinding{}})),
        "a label outside the grammar":       newBindingModuleConfig(valid, SealBindingConfig{Ref: badLabelBinding{}}),
        "the label twice in a compartment":  newBindingModuleConfig(valid, SealBindingConfig{Ref: sameLabelBinding{}}),
    }

    oneCipherTwoNames := newBindingModuleConfig(valid, SealBindingConfig{Ref: secondCompartmentIbanBinding{}})
    oneCipherTwoNames.Contexts = append(oneCipherTwoNames.Contexts, CommandContextConfig{Name: "test:crm2", Cipher: oneCipherTwoNames.Contexts[0].Cipher})
    cases["the label twice over one cipher under two names"] = oneCipherTwoNames

    wrapped := newBindingModuleConfig(valid)
    wrapped.Contexts[0].Cipher = wrappedCipher{Cipher: wrapped.Contexts[0].Cipher}
    cases["a wrapped cipher"] = ModuleConfig{Contexts: wrapped.Contexts, SealBindings: []SealBindingConfig{valid}}

    reasons := map[string]string{
        "a nil Ref":                                       "has no Ref",
        "a typed nil Ref":                                 "is a pointer",
        "a pointer marker":                                "is a pointer",
        "an unknown cipher name":                          "does not install",
        "an empty name without the default":               "without embedding DefaultCipherRef",
        "the default with no Cipher":                      "does not install",
        "the marker twice":                                "declares a marker twice",
        "the label twice in a compartment":                "declares a label twice",
        "a wrapped cipher":                                "needs the package cipher",
        "a marker with fields":                            "holds fields",
        "DefaultCipherRef by pointer":                     "embeds DefaultCipherRef by pointer",
        "a label outside the grammar":                     "label must match",
        "the label twice over one cipher under two names": "declares a label twice",
    }

    for name, config := range cases {
        t.Run(name, func(t *testing.T) {
            reason := reasons[name]

            assertPanicsWithMessage(t, reason, func() {
                NewModule(config).RegisterServices(newModuleRegistrar())
            })

            expectedIndex := 1
            if "a wrapped cipher" == name {
                expectedIndex = 0
            }

            refusalContext := recoverRefusalContext(t, func() {
                NewModule(config).RegisterServices(newModuleRegistrar())
            })

            if "ModuleConfig.SealBindings" != refusalContext["field"] || expectedIndex != refusalContext["index"] {
                t.Fatalf("expected the refusal to name ModuleConfig.SealBindings at %d, got %v", expectedIndex, refusalContext)
            }
        })
    }
}

/* a refused declaration installs nothing: no cipher and no binding */
func TestModule_InstallsNothingWhenASealBindingIsRefused(t *testing.T) {
    previous := NewFakeCipher()
    installCipher(t, "test:crm", previous)

    recoverRefusalContext(t, func() {
        NewModule(newBindingModuleConfig(SealBindingConfig{Ref: crmIbanBinding{}}, SealBindingConfig{Ref: badLabelBinding{}})).RegisterServices(newModuleRegistrar())
    })

    if resolved, _ := cipherByName("test:crm"); previous != resolved {
        t.Fatalf("expected the cipher table left as it was")
    }

    if _, declared := sealBindingOf(reflect.TypeOf(crmIbanBinding{})); true == declared {
        t.Fatalf("expected no binding installed")
    }
}

/* a later registration replaces a binding, so the switch follows the last one */
func TestModule_ALaterRegistrationReplacesABinding(t *testing.T) {
    installCipher(t, "test:crm", NewFakeCipher())
    installSealBinding(t, crmIbanBinding{}, false)

    NewModule(newBindingModuleConfig(SealBindingConfig{Ref: crmIbanBinding{}, RefuseUnbound: true})).RegisterServices(newModuleRegistrar())

    if binding, declared := sealBindingOf(reflect.TypeOf(crmIbanBinding{})); false == declared || false == binding.refuseUnbound || "customer.iban" != binding.label {
        t.Fatalf("expected the later registration's binding, got %+v (declared %v)", binding, declared)
    }
}

/* the label grammar is <table>.<column>, each part [A-Za-z0-9_]{1,64}, one spelling per column */
func TestSealBindingLabelPattern_AdmitsOneSpellingPerColumn(t *testing.T) {
    for _, label := range []string{"users.email", "melody_example_v4_two_factor.secret", strings.Repeat("a", 64) + "." + strings.Repeat("b", 64)} {
        if false == sealBindingLabelPattern.MatchString(label) {
            t.Fatalf("expected %q admitted", label)
        }
    }

    for _, label := range []string{"", "users", ".email", "users.", "users.email.x", "us ers.email", "users.e-mail", strings.Repeat("a", 65) + ".email", "users." + strings.Repeat("b", 65)} {
        if true == sealBindingLabelPattern.MatchString(label) {
            t.Fatalf("expected %q refused", label)
        }
    }
}

/* one label under two compartments of their own ciphers is two bindings, not a collision */
func TestModule_AdmitsOneLabelUnderTwoCiphers(t *testing.T) {
    config := newBindingModuleConfig(SealBindingConfig{Ref: crmIbanBinding{}}, SealBindingConfig{Ref: secondCompartmentIbanBinding{}})
    config.Contexts = append(config.Contexts, CommandContextConfig{Name: "test:crm2", Cipher: NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "crm2-v1", Keys: map[string][]byte{"crm2-v1": newKey(13)}}))})

    installCipher(t, "test:crm", NewFakeCipher())
    installCipher(t, "test:crm2", NewFakeCipher())
    installSealBinding(t, crmIbanBinding{}, false)
    installSealBinding(t, secondCompartmentIbanBinding{}, false)

    NewModule(config).RegisterServices(newModuleRegistrar())
}
