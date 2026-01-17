package kernel

import (
	"testing"

	"github.com/precision-soft/melody/clock"
	"github.com/precision-soft/melody/config"
	configcontract "github.com/precision-soft/melody/config/contract"
	"github.com/precision-soft/melody/container"
	"github.com/precision-soft/melody/event"
	"github.com/precision-soft/melody/http"
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
