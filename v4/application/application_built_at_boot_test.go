package application

import (
    "context"
    "errors"
    "os"
    "os/exec"
    "strings"
    "testing"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    middlewarepipeline "github.com/precision-soft/melody/v4/http/middleware/pipeline"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

type builtAtBootRecord struct {
    names []string
    kinds []containercontract.Kind
    live  []bool
}

type firstBuiltAtBootProbe struct{}

type secondBuiltAtBootProbe struct{}

func (instance *builtAtBootRecord) record(serviceName string, resolver containercontract.Resolver) {
    runtimeInstance := resolver.Runtime()
    instance.names = append(instance.names, serviceName)
    instance.kinds = append(instance.kinds, runtimeInstance.Kind())
    instance.live = append(instance.live, nil == runtimeInstance.Context().Err())
}

func TestBoot_BuildsTheServicesRegisteredBuiltAtBootUnderTheBootRuntimeInRegistrationOrder(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, nil)
    record := &builtAtBootRecord{}

    applicationInstance.RegisterService("app.built.second", func(resolver containercontract.Resolver) (*secondBuiltAtBootProbe, error) {
        record.record("app.built.second", resolver)

        return &secondBuiltAtBootProbe{}, nil
    }, containercontract.RegisterOptions{BuiltAtBoot: true, BootFailure: containercontract.BootFailureFailsBoot})
    applicationInstance.RegisterService("app.built.first", func(resolver containercontract.Resolver) (*firstBuiltAtBootProbe, error) {
        record.record("app.built.first", resolver)

        return &firstBuiltAtBootProbe{}, nil
    }, containercontract.RegisterOptions{BuiltAtBoot: true, BootFailure: containercontract.BootFailureFailsBoot})

    applicationInstance.Boot()

    if 2 != len(record.names) || "app.built.second" != record.names[0] || "app.built.first" != record.names[1] {
        t.Fatalf("expected both services built inside Boot in registration order, got %v", record.names)
    }

    for index, kind := range record.kinds {
        if containercontract.KindBoot != kind || false == record.live[index] {
            t.Fatalf("expected %s built under a live boot runtime, got kind %s live %v", record.names[index], kind, record.live[index])
        }
    }
}

func TestRegisterScoped_RefusesBuiltAtBoot(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, nil)

    registerErr := applicationInstance.kernel.ServiceContainer().RegisterScoped("app.scoped.built", func(resolver containercontract.Resolver) (*firstBuiltAtBootProbe, error) {
        return &firstBuiltAtBootProbe{}, nil
    }, containercontract.RegisterOptions{BuiltAtBoot: true})
    if nil == registerErr || false == strings.Contains(registerErr.Error(), "a scoped service is not built at boot") {
        t.Fatalf("expected a scoped registration built at boot to be refused, got %v", registerErr)
    }
}

const builtAtBootFailureProbeMarker = "MELODY_TEST_BUILT_AT_BOOT_FAILURE_PROBE"

func TestBoot_AFailingServiceBuiltAtBootFailsTheBootNamingIt(t *testing.T) {
    if "" != os.Getenv(builtAtBootFailureProbeMarker) {
        applicationInstance := newEnvironmentKeyBootApplication(t, nil)
        applicationInstance.RegisterService("app.built.failing", func(resolver containercontract.Resolver) (*firstBuiltAtBootProbe, error) {
            return nil, errors.New("the provider refused")
        }, containercontract.RegisterOptions{BuiltAtBoot: true, BootFailure: containercontract.BootFailureFailsBoot})

        applicationInstance.Boot()

        os.Exit(93)
    }

    child := exec.Command(os.Args[0], "-test.run=^TestBoot_AFailingServiceBuiltAtBootFailsTheBootNamingIt$")
    child.Env = append(os.Environ(), builtAtBootFailureProbeMarker+"=1")

    output, runErr := child.CombinedOutput()

    exitError, isExitError := runErr.(*exec.ExitError)
    if false == isExitError || 1 != exitError.ExitCode() {
        t.Fatalf("expected the boot to exit 1, got err %v with output: %s", runErr, string(output))
    }

    if false == strings.Contains(string(output), "a service built at boot cannot be built") || false == strings.Contains(string(output), "the provider refused") {
        t.Fatalf("expected the boot refused over the service, got: %s", string(output))
    }
}

func TestBuildServicesBuiltAtBoot_TheRefusalNamesTheService(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, nil)
    applicationInstance.RegisterService("app.built.failing", func(resolver containercontract.Resolver) (*firstBuiltAtBootProbe, error) {
        return nil, errors.New("the provider refused")
    }, containercontract.RegisterOptions{BuiltAtBoot: true, BootFailure: containercontract.BootFailureFailsBoot})

    openBootRuntime(t, applicationInstance)

    var recovered any
    func() {
        defer func() {
            recovered = recover()
        }()

        applicationInstance.buildServicesBuiltAtBoot(applicationInstance.kernel.BootRuntime())
    }()

    refusal, isRefusal := recovered.(*exception.Error)
    if false == isRefusal || "app.built.failing" != refusal.Context()["serviceName"] {
        t.Fatalf("expected the refusal to name the service, got %v", recovered)
    }
}

type factoryCountingModule struct {
    fakeModule
    built *int
}

func (instance *factoryCountingModule) RegisterHttpMiddlewares(kernelInstance kernelcontract.Kernel, registrar applicationcontract.HttpMiddlewareRegistrar) {
    registrar.Use(applicationcontract.MiddlewareRegistration{
        Factories: []middlewarepipeline.MiddlewareFactory{
            func(kernelInstance kernelcontract.Kernel) (httpcontract.Middleware, error) {
                *instance.built++

                return func(next httpcontract.Handler) httpcontract.Handler {
                    return next
                }, nil
            },
        },
    })
}

func TestBoot_AnHttpBootBuildsThePipelineOnceAndACliBootBuildsNone(t *testing.T) {
    for _, testCase := range []struct {
        mode     string
        expected int
    }{
        {config.ModeHttp, 1},
        {config.ModeCli, 0},
    } {
        applicationInstance := newEnvironmentKeyBootApplication(t, map[string]string{config.HttpAddressKey: "127.0.0.1:34526"})
        applicationInstance.runtimeFlags = NewRuntimeFlags(RuntimeFlagsConfig{Mode: testCase.mode})
        applicationInstance.httpMiddlewares = NewHttpMiddleware(newStaticFileServerConfig(testhelper.NewEmbeddedStaticFs(), applicationInstance.configuration), applicationInstance.configuration)

        built := 0
        applicationInstance.RegisterModule(&factoryCountingModule{fakeModule: fakeModule{name: "factory-counting"}, built: &built})

        applicationInstance.Boot()

        if testCase.expected != built {
            t.Fatalf("%s: expected the factory run %d time(s) inside Boot, got %d", testCase.mode, testCase.expected, built)
        }

        if config.ModeHttp == testCase.mode {
            servingContext, cancel := context.WithCancel(context.Background())
            cancel()

            _ = applicationInstance.runHttp(servingContext)

            if 1 != built {
                t.Fatalf("expected the http run to build no second pipeline, the factory ran %d times", built)
            }
        }
    }
}


func TestBoot_TheRegistrationDoorsAreSealedBeforeTheBootBuildsAndTheFactoriesRun(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, nil)
    applicationInstance.registrationsSealed = true

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterService("app.late", func(resolver containercontract.Resolver) (*firstBuiltAtBootProbe, error) {
            return &firstBuiltAtBootProbe{}, nil
        }, containercontract.RegisterOptions{BuiltAtBoot: true, BootFailure: containercontract.BootFailureFailsBoot})
    }, "may not register services after boot")

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterHttpMiddlewareFactories(nil)
    }, "after boot")
}

const builtAtBootLazyProbeMarker = "MELODY_TEST_BUILT_AT_BOOT_LAZY_PROBE"

type lazyHoldingProbe struct {
    missing *container.LazyService[*firstBuiltAtBootProbe]
}

func TestBoot_ALazyHandleABootBuiltServiceCreatesIsVerified(t *testing.T) {
    if "" != os.Getenv(builtAtBootLazyProbeMarker) {
        applicationInstance := newEnvironmentKeyBootApplication(t, nil)
        applicationInstance.RegisterService("app.built.lazy", func(resolver containercontract.Resolver) (*lazyHoldingProbe, error) {
            return &lazyHoldingProbe{missing: container.Lazy[*firstBuiltAtBootProbe](resolver, "app.never.registered")}, nil
        }, containercontract.RegisterOptions{BuiltAtBoot: true, BootFailure: containercontract.BootFailureFailsBoot})

        applicationInstance.Boot()

        os.Exit(93)
    }

    child := exec.Command(os.Args[0], "-test.run=^TestBoot_ALazyHandleABootBuiltServiceCreatesIsVerified$")
    child.Env = append(os.Environ(), builtAtBootLazyProbeMarker+"=1")

    output, runErr := child.CombinedOutput()

    exitError, isExitError := runErr.(*exec.ExitError)
    if false == isExitError || 1 != exitError.ExitCode() || false == strings.Contains(string(output), "a container.Lazy handle names no container service") {
        t.Fatalf("expected the boot refused over the lazy handle, got err %v with output: %s", runErr, string(output))
    }
}

func TestBuildServicesBuiltAtBoot_AServiceAScopedOneReplacesIsRefused(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, nil)
    applicationInstance.RegisterService("app.built.replaced", func(resolver containercontract.Resolver) (*firstBuiltAtBootProbe, error) {
        return &firstBuiltAtBootProbe{}, nil
    }, containercontract.RegisterOptions{BuiltAtBoot: true, BootFailure: containercontract.BootFailureFailsBoot})
    applicationInstance.kernel.ServiceContainer().MustRegisterScoped("app.built.replaced", func(resolver containercontract.Resolver) (*firstBuiltAtBootProbe, error) {
        return &firstBuiltAtBootProbe{}, nil
    }, containercontract.RegisterOptions{ReplacesContainerService: true})

    openBootRuntime(t, applicationInstance)

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.buildServicesBuiltAtBoot(applicationInstance.kernel.BootRuntime())
    }, "a service built at boot cannot be built")
}

const sealedDoorProbeMarker = "MELODY_TEST_SEALED_DOOR_PROBE"

func TestBoot_AServiceBuiltAtBootThatRegistersAnotherFailsTheBoot(t *testing.T) {
    if "" != os.Getenv(sealedDoorProbeMarker) {
        applicationInstance := newEnvironmentKeyBootApplication(t, nil)
        applicationInstance.RegisterService("app.built.registering", func(resolver containercontract.Resolver) (*firstBuiltAtBootProbe, error) {
            applicationInstance.RegisterService("app.built.late", func(resolver containercontract.Resolver) (*secondBuiltAtBootProbe, error) {
                return &secondBuiltAtBootProbe{}, nil
            }, containercontract.RegisterOptions{BuiltAtBoot: true, BootFailure: containercontract.BootFailureFailsBoot})

            return &firstBuiltAtBootProbe{}, nil
        }, containercontract.RegisterOptions{BuiltAtBoot: true, BootFailure: containercontract.BootFailureFailsBoot})

        applicationInstance.Boot()

        os.Exit(93)
    }

    child := exec.Command(os.Args[0], "-test.run=^TestBoot_AServiceBuiltAtBootThatRegistersAnotherFailsTheBoot$")
    child.Env = append(os.Environ(), sealedDoorProbeMarker+"=1")

    output, runErr := child.CombinedOutput()

    exitError, isExitError := runErr.(*exec.ExitError)
    if false == isExitError || 1 != exitError.ExitCode() || false == strings.Contains(string(output), "may not register services after boot") {
        t.Fatalf("expected the boot refused over the late registration, got err %v with output: %s", runErr, string(output))
    }
}
