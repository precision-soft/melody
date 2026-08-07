package application

import (
    "errors"
    "os"
    "path/filepath"
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
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/precision-soft/melody/security"
    securityconfig "github.com/precision-soft/melody/security/config"
    securitycontract "github.com/precision-soft/melody/security/contract"
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

/* anonymousProbeTokenSource is the smallest token source a firewall can be compiled with */
type anonymousProbeTokenSource struct{}

func (instance *anonymousProbeTokenSource) Name() string {
    return "anonymous"
}

func (instance *anonymousProbeTokenSource) Resolve(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
) (securitycontract.Token, error) {
    return security.NewAnonymousToken(), nil
}

var _ securitycontract.TokenSource = (*anonymousProbeTokenSource)(nil)

func newSecurityWiringApplication(t *testing.T, mode string) *Application {
    t.Helper()

    applicationInstance := newCollisionTestApplication(t)
    applicationInstance.runtimeFlags = NewRuntimeFlags(mode)

    builder := securityconfig.NewBuilder()
    builder.AddStatelessFirewall(
        "api",
        security.NewPathPrefixMatcher("/api"),
        []securitycontract.Rule{},
        &anonymousProbeTokenSource{},
        securityconfig.NewFirewallOverrideConfiguration(),
    )

    applicationInstance.securityConfiguration = builder.BuildAndCompile()

    return applicationInstance
}

/* registeredListenerCount reads how many listeners the kernel dispatcher carries, through the introspection the debug commands use */
func registeredListenerCount(t *testing.T, applicationInstance *Application) int {
    t.Helper()

    inspector, isInspector := applicationInstance.kernel.EventDispatcher().(eventcontract.EventDispatcherInspector)
    if false == isInspector {
        t.Fatalf("expected the kernel dispatcher to be inspectable")
    }

    total := 0
    for _, registeredEvent := range inspector.RegisteredEvents() {
        total = total + len(registeredEvent.Listeners)
    }

    return total
}

/* a compiled security configuration only becomes enforcement when this runs: the firewall manager reaches the container and the two kernel listeners reach the dispatcher. Security in this framework is a pair of listeners rather than middleware, so a boot that skipped them would serve every protected route wide open with a configuration that looks correct everywhere it is printed. */
func TestRegisterSecurity_WiresTheFirewallManagerAndTheKernelListeners(t *testing.T) {
    applicationInstance := newSecurityWiringApplication(t, config.ModeHttp)

    if nil == applicationInstance.securityConfiguration {
        t.Fatalf("the probe configuration did not compile, so the assertions below would be vacuous")
    }

    listenersBefore := registeredListenerCount(t, applicationInstance)

    if wiringErr := applicationInstance.registerSecurity(); nil != wiringErr {
        t.Fatalf("unexpected wiring error: %v", wiringErr)
    }

    if false == applicationInstance.kernel.ServiceContainer().Has(security.ServiceFirewallManager) {
        t.Fatalf("expected the firewall manager to be registered")
    }

    listenersAfter := registeredListenerCount(t, applicationInstance)
    if listenersBefore >= listenersAfter {
        t.Fatalf("expected the security listeners to reach the dispatcher, got %d listeners over %d", listenersAfter, listenersBefore)
    }
}

/* a console process with a compiled configuration resolves the firewall manager — configured means resolvable, whatever the mode — but wires no listeners: they are the enforcement, they listen for requests, and a console process has no request to guard. A process without a compiled configuration wires nothing at all — that is an application that declared no security, not one whose security failed to compile, which the compile step refuses on its own. */
func TestRegisterSecurity_AConsoleProcessResolvesTheManagerAndWiresNoListeners(t *testing.T) {
    cliApplication := newSecurityWiringApplication(t, config.ModeCli)

    listenersBefore := registeredListenerCount(t, cliApplication)

    if wiringErr := cliApplication.registerSecurity(); nil != wiringErr {
        t.Fatalf("unexpected wiring error: %v", wiringErr)
    }

    if false == cliApplication.kernel.ServiceContainer().Has(security.ServiceFirewallManager) {
        t.Fatalf("expected a console process with security configured to resolve the firewall manager")
    }

    listenersAfter := registeredListenerCount(t, cliApplication)
    if listenersBefore != listenersAfter {
        t.Fatalf("expected a console process to wire no security listeners, got %d listeners over %d", listenersAfter, listenersBefore)
    }

    unconfiguredApplication := newCollisionTestApplication(t)
    unconfiguredApplication.runtimeFlags = NewRuntimeFlags(config.ModeHttp)

    if wiringErr := unconfiguredApplication.registerSecurity(); nil != wiringErr {
        t.Fatalf("unexpected wiring error: %v", wiringErr)
    }

    if true == unconfiguredApplication.kernel.ServiceContainer().Has(security.ServiceFirewallManager) {
        t.Fatalf("expected an application that declared no security to wire no firewall manager")
    }
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

/* The framework's fallback cache backend is unbounded in both dimensions, so the application has to be told; the flag is what carries that to the http path, and it must be set exactly when melody supplied the backend itself. */
func TestApplicationRegisterCache_MarksTheFallbackBackendAsUnbounded(t *testing.T) {
    applicationInstance := newSessionTtlTestApplication(t, "30m")

    applicationInstance.registerCache()

    if false == applicationInstance.unboundedDefaultCacheBackend {
        t.Fatalf("expected the fallback in-memory cache backend to be marked unbounded")
    }
}

/* An application that brought its own backend chose its own bounds, and melody has nothing to warn it about. */
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

/* every one of the three cache services is only supplied when the application did not: a framework registration that overwrote an application's own serializer would silently change the format of everything already in the cache, and one that overwrote the cache itself would hand every consumer a different instance than the one the wiring built */
func TestApplicationRegisterCache_LeavesEveryApplicationSuppliedServiceAlone(t *testing.T) {
    applicationInstance := newSessionTtlTestApplication(t, "30m")

    suppliedSerializer := cache.NewJsonSerializer()
    suppliedBackend := cache.NewInMemoryBackend(128, time.Hour, clock.NewSystemClock())
    suppliedCache := cache.NewManager(suppliedBackend, suppliedSerializer)

    applicationInstance.RegisterService(
        cache.ServiceCacheSerializer,
        func(resolver containercontract.Resolver) (cachecontract.Serializer, error) {
            return suppliedSerializer, nil
        },
    )
    applicationInstance.RegisterService(
        cache.ServiceCacheBackend,
        func(resolver containercontract.Resolver) (cachecontract.Backend, error) {
            return suppliedBackend, nil
        },
    )
    applicationInstance.RegisterService(
        cache.ServiceCache,
        func(resolver containercontract.Resolver) (cachecontract.Cache, error) {
            return suppliedCache, nil
        },
    )

    applicationInstance.registerCache()

    if 0 != len(applicationInstance.bootCollisions) {
        t.Fatalf("expected the framework to register nothing over the application's own services, got %+v", applicationInstance.bootCollisions)
    }

    resolvedCache := container.MustFromResolver[cachecontract.Cache](applicationInstance.kernel.ServiceContainer(), cache.ServiceCache)
    if suppliedCache != resolvedCache {
        t.Fatalf("expected the application's own cache to be the one consumers resolve")
    }

    if true == applicationInstance.unboundedDefaultCacheBackend {
        t.Fatalf("expected an application-supplied backend to leave the warning unarmed")
    }
}

func TestNewContainerLogger_LeavesNoDescriptorBehindWhenTheModuleConfigurationIsInvalid(t *testing.T) {
    logPath := filepath.Join(t.TempDir(), "application.log")

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = newContainerLogger(
                logPath,
                loggingcontract.LevelDebug,
                map[string]any{
                    loggingcontract.LoggingConfigurationName: "not a logging configuration",
                },
            )
        },
        "invalid logging configuration",
    )

    _, statErr := os.Stat(logPath)
    if false == os.IsNotExist(statErr) {
        t.Fatalf("expected the log file never to be created when the module configuration is refused, stat answered %v", statErr)
    }
}

func TestNewContainerLogger_OpensTheLogFileWhenEverythingItNeedsIsSound(t *testing.T) {
    logPath := filepath.Join(t.TempDir(), "application.log")

    logger := newContainerLogger(logPath, loggingcontract.LevelDebug, nil)
    if nil == logger {
        t.Fatalf("expected a logger")
    }

    if _, statErr := os.Stat(logPath); nil != statErr {
        t.Fatalf("expected the log file to be created, got %v", statErr)
    }
}

func TestBootContainer_TheApplicationsOwnLoggerIsSubstitutedNotCollided(t *testing.T) {
    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    ownLogger := logging.NewNopLogger()

    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return ownLogger, nil
        },
    )

    kernelInstance := applicationInstance.Boot()

    resolved, resolveErr := logging.LoggerFromContainer(kernelInstance.ServiceContainer())
    if nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if ownLogger != resolved {
        t.Fatalf("expected the application's own logger to be served, got %T", resolved)
    }
}

func TestBootContainer_AFailingLoggerProviderFailsTheBoot(t *testing.T) {
    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return nil, errors.New("logger backend unavailable")
        },
    )

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.bootContainer()
    }, "the configured logger cannot be built")
}

func TestNewContainerLogger_CreatesTheLogDirectory(t *testing.T) {
    logPath := filepath.Join(t.TempDir(), "nested", "deep", "app.log")

    logger := newContainerLogger(logPath, loggingcontract.LevelDebug, nil)

    logger.Emergency("directory created", nil)

    fileInfo, statErr := os.Stat(logPath)
    if nil != statErr {
        t.Fatalf("expected the log file inside the created directory, got %v", statErr)
    }

    if 0 == fileInfo.Size() {
        t.Fatalf("expected the record inside the created file")
    }
}
