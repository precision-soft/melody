package kernel

import (
    "testing"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/event"
    "github.com/precision-soft/melody/v3/http"
)

type testEnvironmentSource struct {
    values map[string]string
}

func (instance *testEnvironmentSource) Load() (map[string]string, error) {
    copied := make(map[string]string, len(instance.values))
    for key, value := range instance.values {
        copied[key] = value
    }

    return copied, nil
}

func newTestConfiguration(t *testing.T, environment string) configcontract.Configuration {
    t.Helper()

    environmentInstance, err := config.NewEnvironment(
        &testEnvironmentSource{
            values: map[string]string{
                config.EnvKey: environment,
            },
        },
    )
    if nil != err {
        t.Fatalf("unexpected environment error: %s", err.Error())
    }

    projectDirectory := t.TempDir()

    configuration, err := config.NewConfiguration(environmentInstance, projectDirectory)
    if nil != err {
        t.Fatalf("unexpected configuration error: %s", err.Error())
    }

    return configuration
}

func TestNewKernel_InitialState(t *testing.T) {
    configuration := newTestConfiguration(t, config.EnvDevelopment)
    serviceContainer := container.NewContainer()
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)

    routeRegistry := http.NewRouteRegistry()
    httpRouter := http.NewRouterWithRouteRegistry(routeRegistry)

    kernelInstance := NewKernel(
        configuration,
        serviceContainer,
        httpRouter,
        dispatcher,
        clockInstance,
    )

    if config.EnvDevelopment != kernelInstance.Environment() {
        t.Fatalf("unexpected environment")
    }
    if false == kernelInstance.DebugMode() {
        t.Fatalf("expected debug mode in development")
    }
    if serviceContainer != kernelInstance.ServiceContainer() {
        t.Fatalf("unexpected container instance")
    }
    if dispatcher != kernelInstance.EventDispatcher() {
        t.Fatalf("unexpected dispatcher instance")
    }
    if httpRouter != kernelInstance.HttpRouter() {
        t.Fatalf("unexpected router instance")
    }
    if configuration != kernelInstance.Config() {
        t.Fatalf("unexpected configuration instance")
    }
}

func TestKernel_DebugModeFalseOutsideDevelopment(t *testing.T) {
    routeRegistry := http.NewRouteRegistry()
    httpRouter := http.NewRouterWithRouteRegistry(routeRegistry)
    clockInstance := clock.NewSystemClock()

    kernelInstance := NewKernel(
        newTestConfiguration(t, config.EnvProduction),
        container.NewContainer(),
        httpRouter,
        event.NewEventDispatcher(clockInstance),
        clockInstance,
    )

    if true == kernelInstance.DebugMode() {
        t.Fatalf("expected debug mode to be false outside development")
    }
}

/* the five guards of the constructor are the framework's refusal to be assembled without a collaborator, and every one of them reads a value the composition root hands it: a *config.Configuration a caller left nil, a container a provider answered nil for, are values that are not nil as interfaces. The comparison against nil answered false, a kernel was built over them, and the refusal the operator was meant to read at wiring arrived instead as a bare nil dereference at the first request that asked the kernel for its environment. */
func TestNewKernel_ATypedNilCollaboratorIsRefusedByName(t *testing.T) {
    var absentConfiguration *config.Configuration

    clockInstance := clock.NewSystemClock()
    routeRegistry := http.NewRouteRegistry()

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected the typed-nil configuration to be refused at construction")
        }

        recoveredErr, isErr := recovered.(error)
        if false == isErr || "application configuration is required for new kernel" != recoveredErr.Error() {
            t.Fatalf("expected the refusal to name the missing collaborator, got %v", recovered)
        }
    }()

    NewKernel(
        absentConfiguration,
        container.NewContainer(),
        http.NewRouterWithRouteRegistry(routeRegistry),
        event.NewEventDispatcher(clockInstance),
        clockInstance,
    )
}
