package application

import (
    "context"
    "errors"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/config"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    middlewarepipeline "github.com/precision-soft/melody/v4/http/middleware/pipeline"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

type warnedBootProbe struct{}

type dependentBootProbe struct{}

type thirdBootProbe struct{}

type bootAmbiguousProbe interface {
    probe()
}

func (instance *thirdBootProbe) probe() {}

type plainClosingBootProbe struct{}

func (instance *plainClosingBootProbe) probe() {}

func (instance *plainClosingBootProbe) Close() error {
    return nil
}

func newHttpBootFailureApplication(t *testing.T) *Application {
    t.Helper()

    applicationInstance := newEnvironmentKeyBootApplication(t, nil)
    applicationInstance.runtimeFlags = NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp})

    return applicationInstance
}

func builtAtBootFailureWarnings(applicationInstance *Application) []internal.BootWarning {
    warnings := make([]internal.BootWarning, 0)
    for _, bootWarning := range applicationInstance.bootWarnings {
        if internal.BootWarningNameServiceBuiltAtBootFailed == bootWarning.Name {
            warnings = append(warnings, bootWarning)
        }
    }

    return warnings
}

func runBuiltAtBootLoop(t *testing.T, applicationInstance *Application) (recovered any) {
    t.Helper()

    openBootRuntime(t, applicationInstance)

    defer func() {
        recovered = recover()
    }()

    applicationInstance.buildServicesBuiltAtBoot(applicationInstance.kernel.BootRuntime())

    return nil
}

func TestBuildServicesBuiltAtBoot_TheDefaultModeWarnsNamingTheServiceAndTheCause(t *testing.T) {
    applicationInstance := newHttpBootFailureApplication(t)
    applicationInstance.RegisterService("app.built.warned", func(resolver containercontract.Resolver) (*warnedBootProbe, error) {
        return nil, errors.New("the database did not answer")
    }, containercontract.RegisterOptions{BuiltAtBoot: true})

    if recovered := runBuiltAtBootLoop(t, applicationInstance); nil != recovered {
        t.Fatalf("expected the boot to go on, got %v", recovered)
    }

    warnings := builtAtBootFailureWarnings(applicationInstance)
    if 1 != len(warnings) {
        t.Fatalf("expected exactly one boot warning, got %v", applicationInstance.bootWarnings)
    }

    if "app.built.warned" != warnings[0].Context["serviceName"] {
        t.Fatalf("expected the warning to name the service, got %v", warnings[0].Context)
    }

    cause, isText := warnings[0].Context["cause"].(string)
    if false == isText || false == strings.Contains(cause, "the database did not answer") || true == strings.HasPrefix(cause, ":") {
        t.Fatalf("expected the provider's reason as the cause, got %q", cause)
    }
}

func TestBuildServicesBuiltAtBoot_AWarnedServiceIsBuiltByTheFirstResolutionUnderItsCallersRuntime(t *testing.T) {
    applicationInstance := newHttpBootFailureApplication(t)

    runs := 0
    kinds := make([]containercontract.Kind, 0)
    applicationInstance.RegisterService("app.built.heals", func(resolver containercontract.Resolver) (*warnedBootProbe, error) {
        runs = runs + 1
        kinds = append(kinds, resolver.Runtime().Kind())
        if 1 == runs {
            return nil, errors.New("not yet")
        }

        return &warnedBootProbe{}, nil
    }, containercontract.RegisterOptions{BuiltAtBoot: true})

    if recovered := runBuiltAtBootLoop(t, applicationInstance); nil != recovered {
        t.Fatalf("expected the boot to go on, got %v", recovered)
    }

    requestRuntime := runtime.NewRequest(t.Context(), applicationInstance.kernel.ServiceContainer().NewScope())
    for index := 0; index < 2; index++ {
        if _, resolveErr := runtime.FromRuntime[*warnedBootProbe](requestRuntime, "app.built.heals"); nil != resolveErr {
            t.Fatalf("expected the first resolution to build the service, got %v", resolveErr)
        }
    }

    if 2 != runs || containercontract.KindBoot != kinds[0] || containercontract.KindRequest != kinds[1] {
        t.Fatalf("expected one run at boot and one under the request, got %d runs with kinds %v", runs, kinds)
    }
}

func TestBuildServicesBuiltAtBoot_AServiceStillFailingIsBuiltAgainByEveryResolution(t *testing.T) {
    applicationInstance := newHttpBootFailureApplication(t)

    runs := 0
    applicationInstance.RegisterService("app.built.down", func(resolver containercontract.Resolver) (*warnedBootProbe, error) {
        runs = runs + 1

        return nil, errors.New("still down")
    }, containercontract.RegisterOptions{BuiltAtBoot: true})

    if recovered := runBuiltAtBootLoop(t, applicationInstance); nil != recovered {
        t.Fatalf("expected the boot to go on, got %v", recovered)
    }

    requestRuntime := runtime.NewRequest(t.Context(), applicationInstance.kernel.ServiceContainer().NewScope())
    for index := 0; index < 3; index++ {
        if _, resolveErr := runtime.FromRuntime[*warnedBootProbe](requestRuntime, "app.built.down"); nil == resolveErr || false == strings.Contains(strings.Join(exception.BuildCauseChain(resolveErr, 16), " | "), "still down") {
            t.Fatalf("expected the failure answered, got %v", resolveErr)
        }
    }

    if 4 != runs {
        t.Fatalf("expected the boot run and three more, got %d", runs)
    }
}

func TestBuildServicesBuiltAtBoot_TheLoopGoesOnPastAWarningInRegistrationOrder(t *testing.T) {
    applicationInstance := newHttpBootFailureApplication(t)

    order := make([]string, 0)
    applicationInstance.RegisterService("app.built.first", func(resolver containercontract.Resolver) (*firstBuiltAtBootProbe, error) {
        order = append(order, "first")

        return &firstBuiltAtBootProbe{}, nil
    }, containercontract.RegisterOptions{BuiltAtBoot: true})
    applicationInstance.RegisterService("app.built.middle", func(resolver containercontract.Resolver) (*warnedBootProbe, error) {
        order = append(order, "middle")

        return nil, errors.New("middle down")
    }, containercontract.RegisterOptions{BuiltAtBoot: true})
    applicationInstance.RegisterService("app.built.third", func(resolver containercontract.Resolver) (*thirdBootProbe, error) {
        order = append(order, "third")

        return &thirdBootProbe{}, nil
    }, containercontract.RegisterOptions{BuiltAtBoot: true, BootFailure: containercontract.BootFailureFailsBoot})

    if recovered := runBuiltAtBootLoop(t, applicationInstance); nil != recovered {
        t.Fatalf("expected the boot to go on, got %v", recovered)
    }

    if "first,middle,third" != strings.Join(order, ",") {
        t.Fatalf("expected every service attempted in registration order, got %v", order)
    }
}

func TestBuildServicesBuiltAtBoot_AFailsBootServiceOverAWarnedOneFailsTheBootNamingBoth(t *testing.T) {
    applicationInstance := newHttpBootFailureApplication(t)
    applicationInstance.RegisterService("app.built.warned", func(resolver containercontract.Resolver) (*warnedBootProbe, error) {
        return nil, errors.New("the warned dependency is down")
    }, containercontract.RegisterOptions{BuiltAtBoot: true})
    applicationInstance.RegisterService("app.built.dependent", func(resolver containercontract.Resolver) (*dependentBootProbe, error) {
        if _, resolveErr := runtime.FromRuntime[*warnedBootProbe](resolver.Runtime(), "app.built.warned"); nil != resolveErr {
            return nil, resolveErr
        }

        return &dependentBootProbe{}, nil
    }, containercontract.RegisterOptions{BuiltAtBoot: true, BootFailure: containercontract.BootFailureFailsBoot})

    recovered := runBuiltAtBootLoop(t, applicationInstance)

    refusal, isRefusal := recovered.(*exception.Error)
    if false == isRefusal || "app.built.dependent" != refusal.Context()["serviceName"] || false == strings.Contains(strings.Join(exception.BuildCauseChain(refusal, 16), " | "), "the warned dependency is down") {
        t.Fatalf("expected the dependent's refusal carrying the warned failure, got %v", recovered)
    }

    if 1 != len(builtAtBootFailureWarnings(applicationInstance)) {
        t.Fatalf("expected the warned service's warning recorded before the refusal, got %v", applicationInstance.bootWarnings)
    }
}

func TestBuildServicesBuiltAtBoot_AWiringRefusalFailsTheBootUnderTheDefaultMode(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        register func(applicationInstance *Application)
    }{
        {
            name: "an unregistered name",
            register: func(applicationInstance *Application) {
                applicationInstance.RegisterService("app.built.wiring", func(resolver containercontract.Resolver) (*warnedBootProbe, error) {
                    _, resolveErr := runtime.FromRuntime[any](resolver.Runtime(), "app.never.registered")

                    return nil, resolveErr
                }, containercontract.RegisterOptions{BuiltAtBoot: true})
            },
        },
        {
            name: "a cycle",
            register: func(applicationInstance *Application) {
                applicationInstance.RegisterService("app.built.wiring", func(resolver containercontract.Resolver) (*warnedBootProbe, error) {
                    _, resolveErr := runtime.FromRuntime[any](resolver.Runtime(), "app.built.wiring")

                    return nil, resolveErr
                }, containercontract.RegisterOptions{BuiltAtBoot: true})
            },
        },
        {
            name: "a scoped service",
            register: func(applicationInstance *Application) {
                applicationInstance.kernel.ServiceContainer().MustRegisterScoped("app.scoped.wiring", func(resolver containercontract.Resolver) (*thirdBootProbe, error) {
                    return &thirdBootProbe{}, nil
                }, containercontract.RegisterOptions{})
                applicationInstance.RegisterService("app.built.wiring", func(resolver containercontract.Resolver) (*warnedBootProbe, error) {
                    _, resolveErr := runtime.FromRuntime[any](resolver.Runtime(), "app.scoped.wiring")

                    return nil, resolveErr
                }, containercontract.RegisterOptions{BuiltAtBoot: true})
            },
        },
        {
            name: "a name of another phase",
            register: func(applicationInstance *Application) {
                applicationInstance.reservePhaseServices()
                applicationInstance.RegisterService("app.built.wiring", func(resolver containercontract.Resolver) (*warnedBootProbe, error) {
                    _, resolveErr := runtime.FromRuntime[any](resolver.Runtime(), ServiceProcessContext)

                    return nil, resolveErr
                }, containercontract.RegisterOptions{BuiltAtBoot: true})
            },
        },
        {
            name: "an ambiguous type",
            register: func(applicationInstance *Application) {
                for _, serviceName := range []string{"app.ambiguous.first", "app.ambiguous.second"} {
                    applicationInstance.RegisterService(serviceName, func(resolver containercontract.Resolver) (bootAmbiguousProbe, error) {
                        return &thirdBootProbe{}, nil
                    }, containercontract.RegisterOptions{TypeRegistrationRelaxed: true})
                }
                applicationInstance.RegisterService("app.built.wiring", func(resolver containercontract.Resolver) (*warnedBootProbe, error) {
                    _, resolveErr := runtime.FromRuntimeByType[bootAmbiguousProbe](resolver.Runtime())

                    return nil, resolveErr
                }, containercontract.RegisterOptions{BuiltAtBoot: true})
            },
        },
        {
            name: "a dependency of another type",
            register: func(applicationInstance *Application) {
                applicationInstance.RegisterService("app.other.type", func(resolver containercontract.Resolver) (*thirdBootProbe, error) {
                    return &thirdBootProbe{}, nil
                }, containercontract.RegisterOptions{})
                applicationInstance.RegisterService("app.built.wiring", func(resolver containercontract.Resolver) (*warnedBootProbe, error) {
                    _, resolveErr := runtime.FromRuntime[*firstBuiltAtBootProbe](resolver.Runtime(), "app.other.type")

                    return nil, resolveErr
                }, containercontract.RegisterOptions{BuiltAtBoot: true})
            },
        },
        {
            name: "a provider answering nothing",
            register: func(applicationInstance *Application) {
                applicationInstance.RegisterService("app.built.wiring", func(resolver containercontract.Resolver) (*warnedBootProbe, error) {
                    return nil, nil
                }, containercontract.RegisterOptions{BuiltAtBoot: true})
            },
        },
        {
            name: "a value that closes without the teardown's context",
            register: func(applicationInstance *Application) {
                applicationInstance.RegisterService("app.built.wiring", func(resolver containercontract.Resolver) (bootAmbiguousProbe, error) {
                    return &plainClosingBootProbe{}, nil
                }, containercontract.RegisterOptions{BuiltAtBoot: true})
            },
        },
    } {
        applicationInstance := newHttpBootFailureApplication(t)
        testCase.register(applicationInstance)

        recovered := runBuiltAtBootLoop(t, applicationInstance)

        refusal, isRefusal := recovered.(*exception.Error)
        if false == isRefusal || "a service built at boot cannot be built" != refusal.Message() || "warns" != refusal.Context()["bootFailure"] {
            t.Fatalf("%s: expected the boot to fail naming the mode, got %v", testCase.name, recovered)
        }

        if 0 != len(builtAtBootFailureWarnings(applicationInstance)) {
            t.Fatalf("%s: expected no warning for a wiring refusal, got %v", testCase.name, applicationInstance.bootWarnings)
        }
    }
}

func TestBuildServicesBuiltAtBoot_ACliBootBuildsOnlyTheServicesThatFailTheBoot(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, nil)

    built := make([]string, 0)
    applicationInstance.RegisterService("app.built.warns", func(resolver containercontract.Resolver) (*warnedBootProbe, error) {
        built = append(built, "warns")

        return nil, errors.New("down")
    }, containercontract.RegisterOptions{BuiltAtBoot: true})
    applicationInstance.RegisterService("app.built.fails", func(resolver containercontract.Resolver) (*firstBuiltAtBootProbe, error) {
        built = append(built, "fails")

        return &firstBuiltAtBootProbe{}, nil
    }, containercontract.RegisterOptions{BuiltAtBoot: true, BootFailure: containercontract.BootFailureFailsBoot})

    if recovered := runBuiltAtBootLoop(t, applicationInstance); nil != recovered {
        t.Fatalf("expected the cli boot to go on, got %v", recovered)
    }

    if "fails" != strings.Join(built, ",") || 0 != len(builtAtBootFailureWarnings(applicationInstance)) {
        t.Fatalf("expected only the fails-boot service built and no warning, got %v and %v", built, applicationInstance.bootWarnings)
    }
}

func TestBuildServicesBuiltAtBoot_APanickingProviderIsJudgedByItsMode(t *testing.T) {
    for _, testCase := range []struct {
        mode         containercontract.BootFailureMode
        failsTheBoot bool
    }{
        {containercontract.BootFailureWarns, false},
        {containercontract.BootFailureFailsBoot, true},
    } {
        applicationInstance := newHttpBootFailureApplication(t)
        applicationInstance.RegisterService("app.built.panics", func(resolver containercontract.Resolver) (*warnedBootProbe, error) {
            panic("the provider panicked")
        }, containercontract.RegisterOptions{BuiltAtBoot: true, BootFailure: testCase.mode})

        recovered := runBuiltAtBootLoop(t, applicationInstance)

        if testCase.failsTheBoot != (nil != recovered) {
            t.Fatalf("%s: expected the boot to fail %v, got %v", testCase.mode, testCase.failsTheBoot, recovered)
        }

        if false == testCase.failsTheBoot && 1 != len(builtAtBootFailureWarnings(applicationInstance)) {
            t.Fatalf("%s: expected one warning, got %v", testCase.mode, applicationInstance.bootWarnings)
        }
    }
}

func TestBuildServicesBuiltAtBoot_ABootWhoseContextEndedFailsWhateverTheMode(t *testing.T) {
    applicationInstance := newHttpBootFailureApplication(t)

    bootContext, cancel := context.WithCancel(t.Context())
    applicationInstance.RegisterService("app.built.stopping", func(resolver containercontract.Resolver) (*warnedBootProbe, error) {
        cancel()

        return nil, errors.New("the dial was interrupted")
    }, containercontract.RegisterOptions{BuiltAtBoot: true})

    bootRuntime, finish := runtime.NewBoot(bootContext, applicationInstance.kernel.ServiceContainer())
    defer func() {
        _ = finish()
    }()

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.buildServicesBuiltAtBoot(bootRuntime)
    }, "a service built at boot cannot be built")
}

const builtAtBootFactoryProbeMarker = "MELODY_TEST_BUILT_AT_BOOT_FACTORY_PROBE"

type warnedServiceFactoryModule struct {
    fakeModule
}

func (instance *warnedServiceFactoryModule) RegisterHttpMiddlewares(kernelInstance kernelcontract.Kernel, registrar applicationcontract.HttpMiddlewareRegistrar) {
    registrar.Use(applicationcontract.MiddlewareRegistration{
        Factories: []middlewarepipeline.MiddlewareFactory{
            func(kernelInstance kernelcontract.Kernel) (httpcontract.Middleware, error) {
                if _, resolveErr := runtime.FromRuntime[*warnedBootProbe](kernelInstance.BootRuntime(), "app.built.factory"); nil != resolveErr {
                    return nil, resolveErr
                }

                return func(next httpcontract.Handler) httpcontract.Handler {
                    return next
                }, nil
            },
        },
    })
}

func TestBoot_AMiddlewareFactoryOverAWarnedServiceFailsTheBootAfterTheWarningIsWritten(t *testing.T) {
    if projectDirectory := os.Getenv(builtAtBootFactoryProbeMarker); "" != projectDirectory {
        applicationInstance := newEnvironmentKeyBootApplicationIn(t, map[string]string{config.HttpAddressKey: "127.0.0.1:34527"}, projectDirectory)
        applicationInstance.runtimeFlags = NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp})
        applicationInstance.httpMiddlewares = NewHttpMiddleware(newStaticFileServerConfig(testhelper.NewEmbeddedStaticFs(), applicationInstance.configuration), applicationInstance.configuration)
        applicationInstance.RegisterService("app.built.factory", func(resolver containercontract.Resolver) (*warnedBootProbe, error) {
            return nil, errors.New("the factory's service is down")
        }, containercontract.RegisterOptions{BuiltAtBoot: true})
        applicationInstance.RegisterModule(&warnedServiceFactoryModule{fakeModule: fakeModule{name: "warned-factory"}})

        applicationInstance.Boot()

        os.Exit(93)
    }

    childProjectDirectory := t.TempDir()
    child := exec.Command(os.Args[0], "-test.run=^TestBoot_AMiddlewareFactoryOverAWarnedServiceFailsTheBootAfterTheWarningIsWritten$")
    child.Env = append(os.Environ(), builtAtBootFactoryProbeMarker+"="+childProjectDirectory)

    output, runErr := child.CombinedOutput()

    exitError, isExitError := runErr.(*exec.ExitError)
    if false == isExitError || 1 != exitError.ExitCode() || false == strings.Contains(string(output), "failed to build middleware pipeline") {
        t.Fatalf("expected the boot to exit 1 through the factory, got err %v with output: %s", runErr, string(output))
    }

    journal, readErr := os.ReadFile(filepath.Join(childProjectDirectory, "var", "log", "dev.log"))
    if nil != readErr {
        t.Fatalf("expected the child's journal: %v", readErr)
    }

    warningAt := strings.Index(string(journal), internal.BootWarningNameServiceBuiltAtBootFailed)
    refusalAt := strings.Index(string(journal), "failed to build middleware pipeline")
    if 0 > warningAt || 0 > refusalAt || warningAt > refusalAt {
        t.Fatalf("expected the warning written before the factory's refusal, got: %s", string(journal))
    }
}

func TestBootWarningRegistry_TheBuiltAtBootCodeIsKnownAndNotACliCode(t *testing.T) {
    registry := internal.NewBootWarningRegistry()

    if false == registry.Known(internal.BootWarningNameServiceBuiltAtBootFailed) {
        t.Fatalf("expected the code known")
    }

    for _, name := range registry.CliNames() {
        if internal.BootWarningNameServiceBuiltAtBootFailed == name {
            t.Fatalf("expected the code not written by a cli process, which builds no warn-mode service at boot")
        }
    }
}

func TestBuildServicesBuiltAtBoot_TheWarningsCauseNumbersEveryLinkAfterTheFirst(t *testing.T) {
    applicationInstance := newHttpBootFailureApplication(t)
    applicationInstance.RegisterService("app.built.chained", func(resolver containercontract.Resolver) (*warnedBootProbe, error) {
        return nil, exception.NewError("the catalogue could not be opened", nil, errors.New("connection refused"))
    }, containercontract.RegisterOptions{BuiltAtBoot: true})

    if recovered := runBuiltAtBootLoop(t, applicationInstance); nil != recovered {
        t.Fatalf("expected the boot to go on, got %v", recovered)
    }

    cause, _ := builtAtBootFailureWarnings(applicationInstance)[0].Context["cause"].(string)
    if "the catalogue could not be opened: cause 1: connection refused" != cause {
        t.Fatalf("expected the first link unnumbered and the next ones numbered from 1, got %q", cause)
    }
}
