package application

import (
    "os"
    "testing"
    "time"

    "github.com/precision-soft/melody/cache"
    cachecontract "github.com/precision-soft/melody/cache/contract"
    "github.com/precision-soft/melody/clock"
    clockcontract "github.com/precision-soft/melody/clock/contract"
    "github.com/precision-soft/melody/config"
    configcontract "github.com/precision-soft/melody/config/contract"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/event"
    eventcontract "github.com/precision-soft/melody/event/contract"
    "github.com/precision-soft/melody/http"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    "github.com/precision-soft/melody/session"
    sessioncontract "github.com/precision-soft/melody/session/contract"
)

type testKernel struct {
    configuration    configcontract.Configuration
    serviceContainer containercontract.Container
    eventDispatcher  eventcontract.EventDispatcher
    httpKernel       httpcontract.Kernel
    httpRouter       httpcontract.Router
    clock            clockcontract.Clock
}

func newTestKernel() *testKernel {
    httpRouter := http.NewRouter()

    return &testKernel{
        configuration:    nil,
        serviceContainer: container.NewContainer(),
        eventDispatcher:  event.NewEventDispatcher(clock.NewSystemClock()),
        httpKernel:       http.NewKernel(httpRouter),
        httpRouter:       httpRouter,
        clock:            clock.NewSystemClock(),
    }
}

func (instance *testKernel) Environment() string {
    return config.EnvDevelopment
}

func (instance *testKernel) DebugMode() bool {
    return true
}

func (instance *testKernel) ServiceContainer() containercontract.Container {
    return instance.serviceContainer
}

func (instance *testKernel) EventDispatcher() eventcontract.EventDispatcher {
    return instance.eventDispatcher
}

func (instance *testKernel) Config() configcontract.Configuration {
    return instance.configuration
}

func (instance *testKernel) HttpKernel() httpcontract.Kernel {
    return instance.httpKernel
}

func (instance *testKernel) HttpRouter() httpcontract.Router {
    return instance.httpRouter
}

func (instance *testKernel) Clock() clockcontract.Clock {
    return instance.clock
}

var _ kernelcontract.Kernel = (*testKernel)(nil)

func TestApplicationRegisterService_RegistersInContainerBeforeBoot(t *testing.T) {
    kernelInstance := newTestKernel()

    applicationInstance := &Application{
        configuration:       nil,
        runtimeFlags:        NewRuntimeFlags(config.ModeHttp),
        kernel:              kernelInstance,
        embeddedPublicFiles: nil,
        modules:             nil,
        cliCommands:         nil,
        httpRouteRegistrars: nil,
        httpMiddlewares:     nil,
    }

    serviceName := "service.test"

    applicationInstance.RegisterService(
        serviceName,
        func(resolver containercontract.Resolver) (*os.File, error) {
            return nil, nil
        },
    )

    if false == kernelInstance.ServiceContainer().Has(serviceName) {
        t.Fatalf("expected service to be registered")
    }
}

func TestApplicationRegisterService_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.Boot()

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterService(
            "service.test",
            func(resolver containercontract.Resolver) (*os.File, error) {
                return nil, nil
            },
        )
    }, "may not register services after boot")
}

type ttlRecordingSessionStorage struct {
    saveCalls int
    savedTtl  time.Duration
}

func (instance *ttlRecordingSessionStorage) Load(sessionId string) (map[string]any, bool, error) {
    return nil, false, nil
}

func (instance *ttlRecordingSessionStorage) Save(sessionId string, data map[string]any, ttl time.Duration) error {
    instance.saveCalls = instance.saveCalls + 1
    instance.savedTtl = ttl

    return nil
}

func (instance *ttlRecordingSessionStorage) Delete(sessionId string) error {
    return nil
}

func (instance *ttlRecordingSessionStorage) Close() error {
    return nil
}

var _ sessioncontract.Storage = (*ttlRecordingSessionStorage)(nil)

func newSessionTtlTestApplication(t *testing.T, sessionTtl string) *Application {
    t.Helper()

    environment, environmentErr := config.NewEnvironment(
        &mapEnvironmentSource{
            values: map[string]string{
                config.HttpSessionTtlKey: sessionTtl,
            },
        },
    )
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, t.TempDir())
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    return &Application{
        configuration:        configuration,
        runtimeFlags:         NewRuntimeFlags(config.ModeHttp),
        kernel:               newTestKernel(),
        moduleConfigurations: make(map[string]any),
    }
}

func TestApplicationRegisterHttpSession_HandsTheConfiguredTtlToTheManager(t *testing.T) {
    applicationInstance := newSessionTtlTestApplication(t, "30m")

    storage := &ttlRecordingSessionStorage{}

    applicationInstance.RegisterService(
        session.ServiceSessionStorage,
        func(resolver containercontract.Resolver) (sessioncontract.Storage, error) {
            return storage, nil
        },
    )

    applicationInstance.registerHttpSession()

    manager := session.SessionMustFromContainer(applicationInstance.kernel.ServiceContainer())

    sessionInstance := manager.NewSession()
    sessionInstance.Set("key", "value")

    saveErr := manager.SaveSession(sessionInstance)
    if nil != saveErr {
        t.Fatalf("unexpected save session error: %v", saveErr)
    }

    if 1 != storage.saveCalls {
        t.Fatalf("expected the manager to store the session once, got %d", storage.saveCalls)
    }

    if 30*time.Minute != storage.savedTtl {
        t.Fatalf("expected the configured session ttl to reach the storage, got %v", storage.savedTtl)
    }
}

func TestApplicationRegisterHttpSession_KeepsAnUnconfiguredTtlUnbounded(t *testing.T) {
    applicationInstance := newSessionTtlTestApplication(t, "0")

    storage := &ttlRecordingSessionStorage{}

    applicationInstance.RegisterService(
        session.ServiceSessionStorage,
        func(resolver containercontract.Resolver) (sessioncontract.Storage, error) {
            return storage, nil
        },
    )

    applicationInstance.registerHttpSession()

    manager := session.SessionMustFromContainer(applicationInstance.kernel.ServiceContainer())

    sessionInstance := manager.NewSession()
    sessionInstance.Set("key", "value")

    saveErr := manager.SaveSession(sessionInstance)
    if nil != saveErr {
        t.Fatalf("unexpected save session error: %v", saveErr)
    }

    if 0 != storage.savedTtl {
        t.Fatalf("expected an unconfigured session ttl to stay unbounded, got %v", storage.savedTtl)
    }
}

/* @info The framework's fallback cache backend is unbounded in both dimensions, so the application has to be told; the flag is what carries that to the http path, and it must be set exactly when melody supplied the backend itself. */
func TestApplicationRegisterCache_MarksTheFallbackBackendAsUnbounded(t *testing.T) {
    applicationInstance := newSessionTtlTestApplication(t, "30m")

    applicationInstance.registerCache()

    if false == applicationInstance.unboundedDefaultCacheBackend {
        t.Fatalf("expected the fallback in-memory cache backend to be marked unbounded")
    }
}

/* @info An application that brought its own backend chose its own bounds, and melody has nothing to warn it about. */
func TestApplicationRegisterCache_LeavesAnApplicationSuppliedBackendUnmarked(t *testing.T) {
    applicationInstance := newSessionTtlTestApplication(t, "30m")

    applicationInstance.RegisterService(
        cache.ServiceCacheBackend,
        func(resolver containercontract.Resolver) (cachecontract.Backend, error) {
            return cache.NewInMemoryBackend(128, time.Hour, clock.NewSystemClock()), nil
        },
    )

    applicationInstance.registerCache()

    if true == applicationInstance.unboundedDefaultCacheBackend {
        t.Fatalf("expected an application-supplied cache backend to leave the warning unarmed")
    }
}
