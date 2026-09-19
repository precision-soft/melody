package encrypt

import (
    "context"
    "io"
    "database/sql"
    "database/sql/driver"
    "errors"
    "fmt"
    "strings"
    "testing"

    melodycli "github.com/precision-soft/melody/v3/cli"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/runtime"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
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

func TestModule_RegisterCliCommandsReturnsNilWithoutDependencies(t *testing.T) {
    if commands := NewModule(ModuleConfig{}).RegisterCliCommands(nil); nil != commands {
        t.Fatalf("expected no commands for the fully-unset legacy shape, got %d", len(commands))
    }
}

func TestModule_RegisterCliCommandsPanicsOnPartialTopLevelConfig(t *testing.T) {
    cipher := NewCipher(NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)}))

    assertPanics(t, func() {
        NewModule(ModuleConfig{Database: newMysqlDatabase()}).RegisterCliCommands(nil)
    })

    assertPanics(t, func() {
        NewModule(ModuleConfig{Cipher: cipher}).RegisterCliCommands(nil)
    })
}

func TestModule_RegisterCliCommandsExposesEncryptDatabaseCommand(t *testing.T) {
    cipher := NewCipher(NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)}))

    commands := NewModule(ModuleConfig{Database: newMysqlDatabase(), Cipher: cipher}).RegisterCliCommands(nil)

    if 1 != len(commands) {
        t.Fatalf("expected exactly one command, got %d", len(commands))
    }

    if "melody:encrypt:database" != commands[0].Name() {
        t.Fatalf("expected the melody:encrypt:database command, got %q", commands[0].Name())
    }
}

func TestModule_RegisterCliCommandsComposesLegacyAndContexts(t *testing.T) {
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
        t.Fatalf("expected the legacy unsuffixed command, got: %v", names)
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
    return &fakeKernel{serviceContainer: container.NewContainer()}
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

    serviceContainer := container.NewContainer()
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
        DatabaseFactory: func(resolver containercontract.Resolver) (*bun.DB, error) {
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
                DatabaseFactory: func(resolver containercontract.Resolver) (*bun.DB, error) {
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
            DatabaseFactory: func(resolver containercontract.Resolver) (*bun.DB, error) {
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

        if "encrypt command context received both a database and a database factory - set exactly one" != exceptionErr.Message() {
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
                DatabaseFactory: func(resolver containercontract.Resolver) (*bun.DB, error) {
                    return newMysqlDatabase(), nil
                },
            },
        },
    }).RegisterCliCommands(newFakeKernel())
}

func TestModule_CommandRunSurfacesDatabaseFactoryError(t *testing.T) {
    module := NewModule(ModuleConfig{
        Cipher: NewFakeCipher(),
        DatabaseFactory: func(resolver containercontract.Resolver) (*bun.DB, error) {
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
                DatabaseFactory: func(resolver containercontract.Resolver) (*bun.DB, error) {
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
    factory := func(resolver containercontract.Resolver) (*bun.DB, error) {
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
            DatabaseFactory: func(resolver containercontract.Resolver) (*bun.DB, error) {
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

/* a kernel answering no service container used to be captured silently into the closure and explode at the first command run, far from the registration that produced it */
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
        DatabaseFactory: func(resolver containercontract.Resolver) (*bun.DB, error) {
            return newMysqlDatabase(), nil
        },
    })

    module.RegisterCliCommands(&fakeKernel{})
}
