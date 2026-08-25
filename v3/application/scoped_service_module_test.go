package application

import (
    "context"
    "os"
    "testing"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    "github.com/precision-soft/melody/v3/config"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
)

type scopedRequestProbe struct {
    value string
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

func newScopedServiceApplication(kernelInstance *testKernel) *Application {
    return &Application{
        ctx:                 context.Background(),
        configuration:       nil,
        runtimeFlags:        NewRuntimeFlags(config.ModeHttp),
        kernel:              kernelInstance,
        embeddedPublicFiles: nil,
        modules:             nil,
        cliCommands:         nil,
        httpRouteRegistrars: nil,
        httpMiddlewares:     nil,
    }
}

/* Without the hook a module has no way to declare a request-lifetime service at all, and the only mechanism left is the override the framework installs for its own logger and request context. */
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

/* The boot seal has to cover both lifetimes: a scoped registration accepted after boot would reach the scopes created next while every scope already running keeps the plan it started with, so the same process would answer the same name two different ways depending on when the request arrived. */
func TestApplication_RegisterScopedAfterBootPanics(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
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

/* A name claimed at both lifetimes is a wiring mistake, and it has to join the aggregated boot report rather than end the boot on its own — the report exists so a consolidation that produced several collisions surfaces them all at once. */
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
