package application

import (
    "testing"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    securityconfig "github.com/precision-soft/melody/v3/security/config"
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

type payloadCarryingModule struct {
    fakeModule
    payload any
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

type bareModuleProvider struct {
    children []applicationcontract.Module
}

func (instance bareModuleProvider) Modules() []applicationcontract.Module {
    return instance.children
}

type mintingModuleProvider struct {
    fakeModule
}

func (instance mintingModuleProvider) Modules() []applicationcontract.Module {
    return []applicationcontract.Module{mintingModuleProvider{fakeModule{name: instance.name + "+"}}}
}

type uncomparableModule struct {
    fakeModule
    tags []string
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

func TestRegisterModule_ASelfReferencingProviderRegistersOnce(t *testing.T) {
    instance := &Application{}

    instance.RegisterModule(selfReferencingModuleProvider{fakeModule: fakeModule{name: "cyclic"}})

    assertModuleNames(t, instance.modules, []string{"cyclic"})
}

func TestRegisterModule_PanicsOnProviderCycleOfDistinctInstances(t *testing.T) {
    instance := &Application{}

    testhelper.AssertPanicsWithError(t, func() {
        instance.RegisterModule(mintingModuleProvider{fakeModule{name: "minting"}})
    }, "module provider expansion exceeded maximum depth")
}

func TestRegisterModuleProvider_RegistersChildrenWithoutProvider(t *testing.T) {
    instance := &Application{}

    provider := bareModuleProvider{
        children: []applicationcontract.Module{fakeModule{name: "child-a"}, fakeModule{name: "child-b"}},
    }

    instance.RegisterModuleProvider(provider)

    assertModuleNames(t, instance.modules, []string{"child-a", "child-b"})
}

func TestRegisterModuleProvider_AProviderThatIsAModuleBootsAsThatModule(t *testing.T) {
    instance := &Application{}

    provider := fakeModuleProvider{
        fakeModule: fakeModule{name: "provider"},
        children:   []applicationcontract.Module{fakeModule{name: "child-a"}, fakeModule{name: "child-b"}},
    }

    instance.RegisterModuleProvider(provider)

    assertModuleNames(t, instance.modules, []string{"provider", "child-a", "child-b"})
}

func TestRegisterModule_TheSameInstanceThroughTwoProvidersBootsOnce(t *testing.T) {
    instance := &Application{}

    sharedInstance := &hookRecordingModule{fakeModule: fakeModule{name: "shared"}}

    instance.RegisterModuleProvider(bareModuleProvider{children: []applicationcontract.Module{sharedInstance}})
    instance.RegisterModuleProvider(bareModuleProvider{children: []applicationcontract.Module{sharedInstance}})

    assertModuleNames(t, instance.modules, []string{"shared"})
}

func TestRegisterModule_TwoDistinctInstancesSharingANameStayTwoModules(t *testing.T) {
    instance := &Application{}

    instance.RegisterModule(&hookRecordingModule{fakeModule: fakeModule{name: "twin"}})
    instance.RegisterModule(&hookRecordingModule{fakeModule: fakeModule{name: "twin"}})

    assertModuleNames(t, instance.modules, []string{"twin", "twin"})
}

func TestRegisterModule_AnUncomparableModuleKeepsThePreDeduplicationBehavior(t *testing.T) {
    instance := &Application{}

    moduleValue := uncomparableModule{fakeModule: fakeModule{name: "uncomparable"}, tags: []string{"tag"}}

    instance.RegisterModule(moduleValue)
    instance.RegisterModule(moduleValue)

    assertModuleNames(t, instance.modules, []string{"uncomparable", "uncomparable"})
}

func TestModuleDoors_RefuseRegistrationDuringTheBootWindow(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        (&Application{booting: true}).RegisterModule(fakeModule{name: "late"})
    }, "may not register a module from inside a module boot hook")

    testhelper.AssertPanicsWithError(t, func() {
        (&Application{booting: true}).RegisterModuleProvider(bareModuleProvider{})
    }, "may not register a module from inside a module boot hook")
}

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

    if true == moduleInstance.servicesRegistered || true == moduleInstance.httpRoutesRegistered {
        t.Fatalf("expected the post-resolve hooks to stay untouched by the pre-resolve phase")
    }
}

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

type orderRecordingModule struct {
    fakeModule
    log *[]string
}

func (instance orderRecordingModule) RegisterEventSubscribers(kernelInstance kernelcontract.Kernel) {
    *instance.log = append(*instance.log, instance.name+":events")
}

func (instance orderRecordingModule) RegisterHttpMiddlewares(
    kernelInstance kernelcontract.Kernel,
    registrar applicationcontract.HttpMiddlewareRegistrar,
) {
    *instance.log = append(*instance.log, instance.name+":middlewares")
}

func (instance orderRecordingModule) RegisterHttpRoutes(kernelInstance kernelcontract.Kernel) {
    *instance.log = append(*instance.log, instance.name+":routes")
}

func (instance orderRecordingModule) RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command {
    *instance.log = append(*instance.log, instance.name+":cli")

    return nil
}

func TestBootModulesPostConfigurationResolve_RunsEachHookAcrossEveryModuleBeforeTheNextHook(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)

    log := make([]string, 0, 8)
    applicationInstance.RegisterModule(orderRecordingModule{fakeModule: fakeModule{name: "a"}, log: &log})
    applicationInstance.RegisterModule(orderRecordingModule{fakeModule: fakeModule{name: "b"}, log: &log})

    applicationInstance.bootModulesPostConfigurationResolve()

    expected := []string{"a:events", "b:events", "a:middlewares", "b:middlewares", "a:routes", "b:routes", "a:cli", "b:cli"}

    if len(expected) != len(log) {
        t.Fatalf("expected %d hook calls, got %d: %v", len(expected), len(log), log)
    }

    for index := range expected {
        if expected[index] != log[index] {
            t.Fatalf("expected hook call %d to be %q, got %q in %v", index, expected[index], log[index], log)
        }
    }
}

type typedNilProbeModule struct{}

func (instance *typedNilProbeModule) Name() string {
    return "typed-nil-probe"
}

func (instance *typedNilProbeModule) Description() string {
    return "typed nil probe"
}

func (instance *typedNilProbeModule) Modules() []applicationcontract.Module {
    return nil
}

func TestApplicationRegisterModule_RefusesATypedNilModule(t *testing.T) {
    applicationInstance := &Application{}

    testhelper.AssertPanicsWithError(
        t,
        func() {
            applicationInstance.RegisterModule((*typedNilProbeModule)(nil))
        },
        "module instance may not be nil",
    )
}

func TestApplicationRegisterModuleProvider_RefusesATypedNilProvider(t *testing.T) {
    applicationInstance := &Application{}

    testhelper.AssertPanicsWithError(
        t,
        func() {
            applicationInstance.RegisterModuleProvider((*typedNilProbeModule)(nil))
        },
        "module provider may not be nil",
    )
}

type scopedServiceFakeModule struct {
    fakeModule
    registered bool
}

func (instance *scopedServiceFakeModule) RegisterScopedServices(registrar applicationcontract.ScopedServiceRegistrar) {
    instance.registered = true

    container.MustRegisterScoped(
        registrar,
        "app.request.probe",
        func(resolver containercontract.Resolver) (*scopedRequestProbe, error) {
            return &scopedRequestProbe{value: "request"}, nil
        },
    )
}
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

func TestRegisterModule_AValueModuleCarryingAnUnhashableFieldIsRegisteredInsteadOfPanicking(t *testing.T) {
    instance := &Application{}

    instance.RegisterModule(payloadCarryingModule{
        fakeModule: fakeModule{name: "carrier"},
        payload:    map[string]any{"unhashable": 1},
    })

    assertModuleNames(t, instance.modules, []string{"carrier"})
}

func TestRegisterModule_AValueModuleCarryingAHashableFieldKeepsItsIdentity(t *testing.T) {
    instance := &Application{}

    moduleInstance := payloadCarryingModule{
        fakeModule: fakeModule{name: "carrier"},
        payload:    "hashable",
    }

    instance.RegisterModule(moduleInstance)
    instance.RegisterModule(moduleInstance)

    assertModuleNames(t, instance.modules, []string{"carrier"})
}
