package application

import (
    "os"
    "testing"

    applicationcontract "github.com/precision-soft/melody/application/contract"
    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/config"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    securityconfig "github.com/precision-soft/melody/security/config"
)

type fakeModule struct {
    name string
}

func (instance fakeModule) Name() string {
    return instance.name
}

func (instance fakeModule) Description() string {
    return instance.name
}

type fakeModuleProvider struct {
    fakeModule
    children []applicationcontract.Module
}

func (instance fakeModuleProvider) Modules() []applicationcontract.Module {
    return instance.children
}

type selfReferencingModuleProvider struct {
    fakeModule
}

func (instance selfReferencingModuleProvider) Modules() []applicationcontract.Module {
    return []applicationcontract.Module{instance}
}

func assertModuleNames(t *testing.T, modules []applicationcontract.Module, expected []string) {
    t.Helper()

    if len(expected) != len(modules) {
        t.Fatalf("expected %d modules, got %d", len(expected), len(modules))
    }

    for index := range expected {
        if expected[index] != modules[index].Name() {
            t.Fatalf("expected module %d to be %q, got %q", index, expected[index], modules[index].Name())
        }
    }
}

func TestRegisterModule_ExpandsModuleProvider(t *testing.T) {
    instance := &Application{}

    provider := fakeModuleProvider{
        fakeModule: fakeModule{name: "provider"},
        children:   []applicationcontract.Module{fakeModule{name: "child-a"}, fakeModule{name: "child-b"}},
    }

    instance.RegisterModule(provider)

    assertModuleNames(t, instance.modules, []string{"provider", "child-a", "child-b"})
}

func TestRegisterModule_PlainModuleIsNotExpanded(t *testing.T) {
    instance := &Application{}

    instance.RegisterModule(fakeModule{name: "plain"})

    assertModuleNames(t, instance.modules, []string{"plain"})
}

func TestRegisterModule_ExpandsNestedProviders(t *testing.T) {
    instance := &Application{}

    inner := fakeModuleProvider{
        fakeModule: fakeModule{name: "inner"},
        children:   []applicationcontract.Module{fakeModule{name: "leaf"}},
    }
    outer := fakeModuleProvider{
        fakeModule: fakeModule{name: "outer"},
        children:   []applicationcontract.Module{inner},
    }

    instance.RegisterModule(outer)

    assertModuleNames(t, instance.modules, []string{"outer", "inner", "leaf"})
}

func TestRegisterModule_PanicsOnProviderCycleInsteadOfStackOverflow(t *testing.T) {
    instance := &Application{}

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected a panic on a cyclic module provider, got none")
        }
    }()

    instance.RegisterModule(selfReferencingModuleProvider{fakeModule: fakeModule{name: "cyclic"}})

    t.Fatal("RegisterModule returned without guarding a module provider cycle")
}

func TestRegisterModuleProvider_RegistersChildrenWithoutProvider(t *testing.T) {
    instance := &Application{}

    provider := fakeModuleProvider{
        fakeModule: fakeModule{name: "provider"},
        children:   []applicationcontract.Module{fakeModule{name: "child-a"}, fakeModule{name: "child-b"}},
    }

    instance.RegisterModuleProvider(provider)

    assertModuleNames(t, instance.modules, []string{"child-a", "child-b"})
}

/* @info the module doors close at boot and refuse a nil, because a module registered after the boot phases have run is registered into nothing: its hooks are never called, and the application starts missing whatever the module was supposed to wire */
func TestRegisterModule_RefusesAfterBootAndRefusesANilModule(t *testing.T) {
    bootedApplication := &Application{booted: true}

    testhelper.AssertPanicsWithError(t, func() {
        bootedApplication.RegisterModule(fakeModule{name: "late"})
    }, "may not register modules after boot")

    testhelper.AssertPanicsWithError(t, func() {
        (&Application{}).RegisterModule(nil)
    }, "module instance may not be nil")
}

func TestRegisterModuleProvider_RefusesAfterBootAndRefusesANilProvider(t *testing.T) {
    bootedApplication := &Application{booted: true}

    testhelper.AssertPanicsWithError(t, func() {
        bootedApplication.RegisterModuleProvider(fakeModuleProvider{fakeModule: fakeModule{name: "late"}})
    }, "may not register modules after boot")

    testhelper.AssertPanicsWithError(t, func() {
        (&Application{}).RegisterModuleProvider(nil)
    }, "module provider may not be nil")
}

/* hookRecordingModule implements every module hook the boot phases call, so one registration proves which phase reached which hook */
type hookRecordingModule struct {
    fakeModule
    configurationsRegistered  bool
    parametersRegistered      bool
    servicesRegistered        bool
    eventSubscribersOnKernel  bool
    httpMiddlewaresRegistered bool
    httpRoutesRegistered      bool
    cliCommandsRegistered     bool
    securityRegistered        bool
}

func (instance *hookRecordingModule) RegisterConfigurations(registrar applicationcontract.ConfigRegistrar) {
    instance.configurationsRegistered = true
}

func (instance *hookRecordingModule) RegisterParameters(registrar applicationcontract.ParameterRegistrar) {
    instance.parametersRegistered = true

    registrar.RegisterParameter("module.parameter", "value")
}

func (instance *hookRecordingModule) RegisterServices(
    kernelInstance kernelcontract.Kernel,
    registrar applicationcontract.ServiceRegistrar,
) {
    instance.servicesRegistered = true
}

func (instance *hookRecordingModule) RegisterEventSubscribers(kernelInstance kernelcontract.Kernel) {
    instance.eventSubscribersOnKernel = true
}

func (instance *hookRecordingModule) RegisterHttpMiddlewares(
    kernelInstance kernelcontract.Kernel,
    registrar applicationcontract.HttpMiddlewareRegistrar,
) {
    instance.httpMiddlewaresRegistered = true
}

func (instance *hookRecordingModule) RegisterHttpRoutes(kernelInstance kernelcontract.Kernel) {
    instance.httpRoutesRegistered = true
}

func (instance *hookRecordingModule) RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command {
    instance.cliCommandsRegistered = true

    return []clicontract.Command{&namedTestCommand{name: "module:work"}}
}

func (instance *hookRecordingModule) RegisterSecurity(builder *securityconfig.Builder) {
    instance.securityRegistered = true
}

/* @info the configuration and parameter hooks run BEFORE the configuration resolves, which is the whole reason they are a separate phase: a parameter registered after the resolve would never have its template expanded, and a logging configuration registered after it would never reach the logger the container builds. */
func TestBootModulesPreConfigurationResolve_RunsTheConfigurationAndParameterHooks(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)

    moduleInstance := &hookRecordingModule{fakeModule: fakeModule{name: "recording"}}
    applicationInstance.RegisterModule(moduleInstance)

    applicationInstance.bootModulesPreConfigurationResolve()

    if false == moduleInstance.configurationsRegistered {
        t.Fatalf("expected the configuration hook to run before the resolve")
    }

    if false == moduleInstance.parametersRegistered {
        t.Fatalf("expected the parameter hook to run before the resolve")
    }

    if nil == applicationInstance.configuration.Get("module.parameter") {
        t.Fatalf("expected the parameter the module registered to reach the configuration")
    }

    /* nothing the post-resolve phase owns has run yet */
    if true == moduleInstance.servicesRegistered || true == moduleInstance.httpRoutesRegistered {
        t.Fatalf("expected the post-resolve hooks to stay untouched by the pre-resolve phase")
    }
}

/* @info everything a module wires against resolved configuration runs in the second phase, and each hook is optional: a module implementing all of them must see all of them called, or one silently-missing hook means an unwired subsystem with a green boot. */
func TestBootModulesPostConfigurationResolve_RunsEveryLaterHook(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)
    applicationInstance.httpMiddlewares = NewHttpMiddleware(nil, applicationInstance.configuration)

    moduleInstance := &hookRecordingModule{fakeModule: fakeModule{name: "recording"}}
    applicationInstance.RegisterModule(moduleInstance)

    applicationInstance.bootModulesPostConfigurationResolve()

    if false == moduleInstance.servicesRegistered {
        t.Fatalf("expected the service hook to run")
    }
    if false == moduleInstance.securityRegistered {
        t.Fatalf("expected the security hook to run")
    }
    if false == moduleInstance.eventSubscribersOnKernel {
        t.Fatalf("expected the event subscriber hook to run")
    }
    if false == moduleInstance.httpMiddlewaresRegistered {
        t.Fatalf("expected the http middleware hook to run")
    }
    if false == moduleInstance.httpRoutesRegistered {
        t.Fatalf("expected the http route hook to run")
    }
    if false == moduleInstance.cliCommandsRegistered {
        t.Fatalf("expected the cli command hook to run")
    }

    if 1 != len(applicationInstance.cliCommands) {
        t.Fatalf("expected the command the module returned to be registered, got %d", len(applicationInstance.cliCommands))
    }
}

type scopedRequestProbe struct {
    value string
}

type scopedServiceFakeModule struct {
    fakeModule
    registered bool
}

func (instance *scopedServiceFakeModule) RegisterScopedServices(
    kernelInstance kernelcontract.Kernel,
    registrar applicationcontract.ScopedServiceRegistrar,
) {
    instance.registered = true

    registrar.RegisterScopedService(
        "app.request.probe",
        func(resolver containercontract.Resolver) (*scopedRequestProbe, error) {
            return &scopedRequestProbe{value: "request"}, nil
        },
    )
}

func newScopedServiceApplication(kernelInstance *testKernel) *Application {
    return &Application{
        runtimeFlags: NewRuntimeFlags(config.ModeHttp),
        kernel:       kernelInstance,
    }
}

/* @info Without the hook a module has no way to declare a request-lifetime service at all, and the only mechanism left is the override the framework installs for its own logger and request context. */
func TestApplication_RegisterScopedServicesHookRunsForScopedServiceModules(t *testing.T) {
    kernelInstance := newTestKernel()
    applicationInstance := newScopedServiceApplication(kernelInstance)

    moduleInstance := &scopedServiceFakeModule{fakeModule: fakeModule{name: "scoped"}}
    applicationInstance.RegisterModule(moduleInstance)

    applicationInstance.bootModulesPostConfigurationResolve()

    if false == moduleInstance.registered {
        t.Fatalf("expected the scoped registration hook to run")
    }

    scopeInstance := kernelInstance.ServiceContainer().NewScope()

    value, getErr := scopeInstance.Get("app.request.probe")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if "request" != value.(*scopedRequestProbe).value {
        t.Fatalf("unexpected scoped service value: %#v", value)
    }

    if true == kernelInstance.ServiceContainer().Has("app.request.probe") {
        t.Fatalf("expected the container to stay blind to a scoped registration")
    }
}

/* @info The boot seal has to cover both lifetimes: a scoped registration accepted after boot would reach the scopes created next while every scope already running keeps the plan it started with, so the same process would answer the same name two different ways depending on when the request arrived. */
func TestApplication_RegisterScopedAfterBootPanics(t *testing.T) {
    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.Boot()

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterScopedService(
            "app.request.probe",
            func(resolver containercontract.Resolver) (*os.File, error) {
                return nil, nil
            },
        )
    }, "may not register scoped services after boot")
}

/* @info A name claimed at both lifetimes is a wiring mistake, and it has to join the aggregated boot report rather than end the boot on its own — the report exists so a consolidation that produced several collisions surfaces them all at once. */
func TestApplication_AScopedNameCollidingWithAContainerServiceIsReportedAtBoot(t *testing.T) {
    kernelInstance := newTestKernel()
    applicationInstance := newScopedServiceApplication(kernelInstance)

    applicationInstance.RegisterService(
        "app.thing",
        func(resolver containercontract.Resolver) (*scopedRequestProbe, error) {
            return &scopedRequestProbe{value: "container"}, nil
        },
        container.WithoutTypeRegistration(),
    )

    applicationInstance.RegisterScopedService(
        "app.thing",
        func(resolver containercontract.Resolver) (*scopedRequestProbe, error) {
            return &scopedRequestProbe{value: "scoped"}, nil
        },
        container.WithoutTypeRegistration(),
    )

    if 1 != len(applicationInstance.bootCollisions) {
        t.Fatalf("expected exactly one recorded collision, got %d", len(applicationInstance.bootCollisions))
    }

    if bootCollisionKindScopedService != applicationInstance.bootCollisions[0].kind {
        t.Fatalf("expected the collision to name the scoped lifetime, got %q", applicationInstance.bootCollisions[0].kind)
    }

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.panicOnBootCollisions()
    }, "duplicate registrations detected at boot")
}
