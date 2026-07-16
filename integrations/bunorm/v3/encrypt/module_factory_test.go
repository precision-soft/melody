package encrypt

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "testing"

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
)

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

    var runErr error
    cliCommand := &clicontract.CommandContext{
        Name:  command.Name(),
        Flags: command.Flags(),
        Action: func(ctx context.Context, commandContext *clicontract.CommandContext) error {
            runErr = command.Run(runtimeInstance, commandContext)

            return nil
        },
    }

    if executeErr := cliCommand.Run(context.Background(), arguments); nil != executeErr {
        t.Fatalf("cli command execution failed: %v", executeErr)
    }

    return runErr
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
