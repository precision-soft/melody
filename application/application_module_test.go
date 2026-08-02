package application

import (
    "os"
    "testing"

    applicationcontract "github.com/precision-soft/melody/application/contract"
    "github.com/precision-soft/melody/config"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
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
