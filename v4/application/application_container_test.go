package application

import (
    "context"
    "errors"
    "io"
    "os"
    "path/filepath"
    "slices"
    "strconv"
    "strings"
    "sync/atomic"
    "syscall"
    "testing"

    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"

    "github.com/precision-soft/melody/v4/cache"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/messagebus"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/precision-soft/melody/v4/security"
    securityconfig "github.com/precision-soft/melody/v4/security/config"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/serializer"
    serializercontract "github.com/precision-soft/melody/v4/serializer/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
    "github.com/precision-soft/melody/v4/validation"
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
        httpKernel:       http.NewKernel(httpRouter, http.KernelConfig{}),
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
        ctx:                 context.Background(),
        configuration:       nil,
        runtimeFlags:        NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp}),
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
        container.RegisterOptions{},
    )

    if false == kernelInstance.ServiceContainer().Has(serviceName) {
        t.Fatalf("expected service to be registered")
    }
}

func TestApplicationRegisterService_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvFiles: testhelper.NewEmbeddedEnvFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    applicationInstance.Boot()

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterService(
            "service.test",
            func(resolver containercontract.Resolver) (*os.File, error) {
                return nil, nil
            },
            container.RegisterOptions{},
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
    applicationInstance.runtimeFlags = NewRuntimeFlags(RuntimeFlagsConfig{Mode: mode})

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
    unconfiguredApplication.runtimeFlags = NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp})

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
        ctx:                  context.Background(),
        configuration:        configuration,
        runtimeFlags:         NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp}),
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
        container.RegisterOptions{},
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

/* the configured window and the kernel's clock must both travel into the manager: on a frozen clock a deleted session is still refused a write-back 30 minutes later under a one-hour window and accepted 61 minutes later, where the five-minute default would accept the first and the wall clock would refuse the second. */
func TestApplicationRegisterHttpSession_HandsTheConfiguredTombstoneRetentionAndTheKernelClockToTheManager(t *testing.T) {
    environment, environmentErr := config.NewEnvironment(
        &mapEnvironmentSource{
            values: map[string]string{
                config.HttpSessionTombstoneRetentionKey: "1h",
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

    frozenClock := clock.NewFrozenClock(time.Date(2026, time.October, 3, 12, 0, 0, 0, time.UTC))

    kernelInstance := newTestKernel()
    kernelInstance.clock = frozenClock

    applicationInstance := &Application{
        ctx:                  context.Background(),
        configuration:        configuration,
        runtimeFlags:         NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp}),
        kernel:               kernelInstance,
        moduleConfigurations: make(map[string]any),
    }

    applicationInstance.registerHttpSession()

    manager := session.SessionMustFromContainer(applicationInstance.kernel.ServiceContainer())

    sessionInstance := manager.NewSession()
    sessionInstance.Set("identity", "alice")
    if saveErr := manager.SaveSession(sessionInstance); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    inFlightView := manager.Session(sessionInstance.Id())
    if nil == inFlightView {
        t.Fatalf("expected the session to load")
    }

    if deleteErr := manager.DeleteSession(sessionInstance.Id()); nil != deleteErr {
        t.Fatalf("unexpected delete error: %v", deleteErr)
    }

    frozenClock.Advance(30 * time.Minute)

    inFlightView.Set("lastSeen", "half an hour later")

    if saveErr := manager.SaveSession(inFlightView); false == errors.Is(saveErr, session.ErrSessionDeleted) {
        t.Fatalf("expected the write-back inside the configured one-hour window to be refused, got %v", saveErr)
    }

    frozenClock.Advance(31 * time.Minute)

    inFlightView.Set("lastSeen", "an hour and a minute later")

    if saveErr := manager.SaveSession(inFlightView); nil != saveErr {
        t.Fatalf("expected the configured window to have lapsed on the kernel's clock, got %v", saveErr)
    }
}

func TestApplicationRegisterHttpSession_HandsAnUnlimitedTtlToTheStorageAsNoExpiry(t *testing.T) {
    applicationInstance := newSessionTtlTestApplication(t, "unlimited")

    storage := &ttlRecordingSessionStorage{}

    applicationInstance.RegisterService(
        session.ServiceSessionStorage,
        func(resolver containercontract.Resolver) (sessioncontract.Storage, error) {
            return storage, nil
        },
        container.RegisterOptions{},
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
        t.Fatalf("expected an unlimited session ttl to store without expiry, got %v", storage.savedTtl)
    }
}

func TestApplicationRegisterCache_TheFallbackBackendIsBoundedAtTheDefaultCeiling(t *testing.T) {
    applicationInstance := newSessionTtlTestApplication(t, "30m")

    applicationInstance.RegisterService(
        clock.ServiceClock,
        func(resolver containercontract.Resolver) (clockcontract.Clock, error) {
            return clock.NewSystemClock(), nil
        },
        container.RegisterOptions{},
    )

    applicationInstance.registerCache()

    backend := container.MustFromResolver[cachecontract.Backend](applicationInstance.kernel.ServiceContainer(), cache.ServiceCacheBackend)
    defer func() { _ = backend.Close() }()

    keys := make([]string, 0, 10_001)
    for index := 0; index <= 10_000; index++ {
        key := "key-" + strconv.Itoa(index)
        keys = append(keys, key)

        if setErr := backend.Set(key, []byte("1"), bound.UnlimitedDuration()); nil != setErr {
            t.Fatalf("set error: %v", setErr)
        }
    }

    stored := 0
    for _, key := range keys {
        if found, _ := backend.Has(key); true == found {
            stored++
        }
    }

    if 10_000 != stored {
        t.Fatalf("expected the fallback backend to hold 10,000 entries, got %d", stored)
    }
}

/* every one of the three cache services is only supplied when the application did not: a framework registration that overwrote an application's own serializer would silently change the format of everything already in the cache, and one that overwrote the cache itself would hand every consumer a different instance than the one the wiring built */
func TestApplicationRegisterCache_LeavesEveryApplicationSuppliedServiceAlone(t *testing.T) {
    applicationInstance := newSessionTtlTestApplication(t, "30m")

    suppliedSerializer := cache.NewJsonSerializer()
    suppliedBackend := cache.NewInMemoryBackend(cache.InMemoryBackendConfig{MaxItems: bound.Of(128), CleanupInterval: bound.Of(time.Hour), Clock: clock.NewSystemClock()})
    suppliedCache := cache.NewManager(suppliedBackend, suppliedSerializer, cache.ManagerConfig{})

    applicationInstance.RegisterService(
        cache.ServiceCacheSerializer,
        func(resolver containercontract.Resolver) (cachecontract.Serializer, error) {
            return suppliedSerializer, nil
        },
        container.RegisterOptions{},
    )
    applicationInstance.RegisterService(
        cache.ServiceCacheBackend,
        func(resolver containercontract.Resolver) (cachecontract.Backend, error) {
            return suppliedBackend, nil
        },
        container.RegisterOptions{},
    )
    applicationInstance.RegisterService(
        cache.ServiceCache,
        func(resolver containercontract.Resolver) (cachecontract.Cache, error) {
            return suppliedCache, nil
        },
        container.RegisterOptions{},
    )

    applicationInstance.registerCache()

    if 0 != len(applicationInstance.bootCollisions) {
        t.Fatalf("expected the framework to register nothing over the application's own services, got %+v", applicationInstance.bootCollisions)
    }

    resolvedCache := container.MustFromResolver[cachecontract.Cache](applicationInstance.kernel.ServiceContainer(), cache.ServiceCache)
    if suppliedCache != resolvedCache {
        t.Fatalf("expected the application's own cache to be the one consumers resolve")
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
                clock.NewSystemClock(),
                false,
                os.Stdout,
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

    logger := newContainerLogger(logPath, loggingcontract.LevelDebug, nil, clock.NewSystemClock(), false, os.Stdout)
    if nil == logger {
        t.Fatalf("expected a logger")
    }

    if _, statErr := os.Stat(logPath); nil != statErr {
        t.Fatalf("expected the log file to be created, got %v", statErr)
    }
}

func TestBootContainer_TheApplicationsOwnLoggerIsSubstitutedNotCollided(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvFiles: testhelper.NewEmbeddedEnvFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    ownLogger := logging.NewNopLogger()

    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return ownLogger, nil
        },
        container.RegisterOptions{},
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
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvFiles: testhelper.NewEmbeddedEnvFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return nil, errors.New("logger backend unavailable")
        },
        container.RegisterOptions{},
    )

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.bootContainer()
    }, "the configured logger cannot be built")
}

func TestNewContainerLogger_CreatesTheLogDirectory(t *testing.T) {
    logPath := filepath.Join(t.TempDir(), "nested", "deep", "app.log")

    logger := newContainerLogger(logPath, loggingcontract.LevelDebug, nil, clock.NewSystemClock(), false, os.Stdout)

    logger.Emergency("directory created", nil)

    fileInfo, statErr := os.Stat(logPath)
    if nil != statErr {
        t.Fatalf("expected the log file inside the created directory, got %v", statErr)
    }

    if 0 == fileInfo.Size() {
        t.Fatalf("expected the record inside the created file")
    }
}

/* the configured window must travel from the environment key through the configuration into the manager: only a lapsed 300ms window explains a deleted session accepting a write-back 400ms later, where the five-minute default would still refuse it. */
func TestBootContainer_TheSerializerManagerIsSubstitutedNotCollided(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvFiles: testhelper.NewEmbeddedEnvFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    ownManager, managerErr := serializer.NewSerializerManager(
        map[string]serializercontract.Serializer{
            "application/json": serializer.NewJsonSerializer(),
            "application/xml":  serializer.NewJsonSerializer(),
        },
    )
    if nil != managerErr {
        t.Fatalf("unexpected manager error: %v", managerErr)
    }

    applicationInstance.RegisterService(
        serializer.ServiceSerializerManager,
        func(resolver containercontract.Resolver) (*serializer.SerializerManager, error) {
            return ownManager, nil
        },
        container.RegisterOptions{},
    )

    kernelInstance := applicationInstance.Boot()

    resolved, resolveErr := container.FromResolver[*serializer.SerializerManager](
        kernelInstance.ServiceContainer(),
        serializer.ServiceSerializerManager,
    )
    if nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if ownManager != resolved {
        t.Fatalf("expected the application's own serializer manager to be served")
    }
}

func TestBootContainer_TheValidatorAndUrlGeneratorAreSubstitutedNotCollided(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvFiles: testhelper.NewEmbeddedEnvFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    ownValidator := validation.NewValidator()
    ownUrlGenerator := http.NewUrlGenerator(http.NewRouteRegistry())

    applicationInstance.RegisterService(
        validation.ServiceValidator,
        func(resolver containercontract.Resolver) (*validation.Validator, error) {
            return ownValidator, nil
        },
        container.RegisterOptions{},
    )

    applicationInstance.RegisterService(
        http.ServiceUrlGenerator,
        func(resolver containercontract.Resolver) (httpcontract.UrlGenerator, error) {
            return ownUrlGenerator, nil
        },
        container.RegisterOptions{},
    )

    kernelInstance := applicationInstance.Boot()

    resolvedValidator, validatorErr := container.FromResolver[*validation.Validator](
        kernelInstance.ServiceContainer(),
        validation.ServiceValidator,
    )
    if nil != validatorErr {
        t.Fatalf("unexpected validator resolve error: %v", validatorErr)
    }

    if ownValidator != resolvedValidator {
        t.Fatalf("expected the application's own validator to be served")
    }

    resolvedUrlGenerator, urlGeneratorErr := container.FromResolver[httpcontract.UrlGenerator](
        kernelInstance.ServiceContainer(),
        http.ServiceUrlGenerator,
    )
    if nil != urlGeneratorErr {
        t.Fatalf("unexpected url generator resolve error: %v", urlGeneratorErr)
    }

    if ownUrlGenerator != resolvedUrlGenerator {
        t.Fatalf("expected the application's own url generator to be served")
    }
}

/* TestBootContainer_TheDefaultSerializerAnswersItsDocumentedResolvers pins the id the two published resolvers read. SerializerMustFromRuntime and SerializerFromRuntime were documented with the id nothing registered, so the Must door panicked for every caller and the soft one answered nil — by construction, on every boot the framework has ever performed. */
func TestBootContainer_TheDefaultSerializerAnswersItsDocumentedResolvers(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvFiles: testhelper.NewEmbeddedEnvFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    kernelInstance := applicationInstance.Boot()

    resolved, resolveErr := container.FromResolver[serializercontract.Serializer](
        kernelInstance.ServiceContainer(),
        serializer.ServiceSerializer,
    )
    if nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if nil == resolved {
        t.Fatalf("expected the default serializer to be registered")
    }

    if false == strings.HasPrefix(resolved.ContentType(), "application/json") {
        t.Fatalf("expected the json serializer as the default, got %q", resolved.ContentType())
    }
}

/* TestBootContainer_TheApplicationsOwnDefaultSerializerIsSubstitutedNotCollided pins the gate over the same id, so registering a default serializer is a substitution rather than the boot collision every ungated framework id answered with. */
func TestBootContainer_TheApplicationsOwnDefaultSerializerIsSubstitutedNotCollided(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvFiles: testhelper.NewEmbeddedEnvFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    ownSerializer := serializer.NewPlainTextSerializer()

    applicationInstance.RegisterService(
        serializer.ServiceSerializer,
        func(resolver containercontract.Resolver) (serializercontract.Serializer, error) {
            return ownSerializer, nil
        },
        container.RegisterOptions{},
    )

    kernelInstance := applicationInstance.Boot()

    resolved, resolveErr := container.FromResolver[serializercontract.Serializer](
        kernelInstance.ServiceContainer(),
        serializer.ServiceSerializer,
    )
    if nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if ownSerializer != resolved {
        t.Fatalf("expected the application's own default serializer to be served")
    }
}

type scopedRequestProbe struct {
    value string
}

func newScopedServiceApplication(kernelInstance *testKernel) *Application {
    return &Application{
        ctx:                 context.Background(),
        configuration:       nil,
        runtimeFlags:        NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp}),
        kernel:              kernelInstance,
        embeddedPublicFiles: nil,
        modules:             nil,
        cliCommands:         nil,
        httpRouteRegistrars: nil,
        httpMiddlewares:     nil,
    }
}
func TestApplication_RegisterScopedAfterBootPanics(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvFiles: testhelper.NewEmbeddedEnvFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    applicationInstance.Boot()

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterScopedService(
            "app.request.probe",
            func(resolver containercontract.Resolver) (*os.File, error) {
                return nil, nil
            },
            container.RegisterOptions{},
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
        container.RegisterOptions{TypeRegistrationDisabled: true},
    )

    applicationInstance.RegisterScopedService(
        "app.thing",
        func(resolver containercontract.Resolver) (*scopedRequestProbe, error) {
            return &scopedRequestProbe{value: "scoped"}, nil
        },
        container.RegisterOptions{TypeRegistrationDisabled: true},
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

/* recordingCloseTransport records whether the container's teardown ever reached it. */
type recordingCloseTransport struct {
    closed atomic.Bool
}

func (instance *recordingCloseTransport) Send(runtimeInstance runtimecontract.Runtime, envelope messagebuscontract.Envelope) error {
    return nil
}

func (instance *recordingCloseTransport) Receive(runtimeInstance runtimecontract.Runtime) (<-chan messagebuscontract.Envelope, error) {
    return nil, nil
}

func (instance *recordingCloseTransport) Ack(runtimeInstance runtimecontract.Runtime, envelope messagebuscontract.Envelope) error {
    return nil
}

func (instance *recordingCloseTransport) Nack(runtimeInstance runtimecontract.Runtime, envelope messagebuscontract.Envelope, requeue bool) error {
    return nil
}

func (instance *recordingCloseTransport) Close() error {
    instance.closed.Store(true)

    return nil
}

/* the http process publishes through a routing that holds the transport value directly, so it never resolves the transports map; the container still closes the registered transports, or the broker connection lives exactly as long as the process. */
func TestBoot_TheRegisteredTransportsAreClosedByAProcessThatNeverResolvesTheMap(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvFiles: testhelper.NewEmbeddedEnvFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    transport := &recordingCloseTransport{}

    messagebus.RegisterTransports(
        applicationInstance,
        map[string]messagebuscontract.Transport{"async": transport},
    )

    kernelInstance := applicationInstance.Boot()

    if closeErr := kernelInstance.ServiceContainer().Close(); nil != closeErr {
        t.Fatalf("unexpected container close error: %v", closeErr)
    }

    if false == transport.closed.Load() {
        t.Fatalf("expected the boot-built closer to close the registered transport")
    }
}

/* the consume process still gets its ordered teardown: the edge is recorded on the RESOLUTION, so a closer already built at boot is no less a dependency of the map than one the map's own provider built. */
func TestBoot_ResolvingTheTransportsMapStillOrdersTheTeardownAfterTheBootBuild(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvFiles: testhelper.NewEmbeddedEnvFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    transport := &recordingCloseTransport{}

    messagebus.RegisterTransports(
        applicationInstance,
        map[string]messagebuscontract.Transport{"async": transport},
    )

    kernelInstance := applicationInstance.Boot()
    serviceContainer := kernelInstance.ServiceContainer()

    resolved := messagebus.TransportsMustFromResolver(serviceContainer)
    if transport != resolved["async"] {
        t.Fatalf("expected the registered transport to resolve")
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected container close error: %v", closeErr)
    }

    if false == transport.closed.Load() {
        t.Fatalf("expected the resolved transports to still be closed")
    }
}

/* an application that registered nothing must not gain a service it never asked for, and the boot must not fail looking for one. */
func TestBoot_NoTransportsRegisteredBuildsNoCloser(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvFiles: testhelper.NewEmbeddedEnvFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    kernelInstance := applicationInstance.Boot()

    if true == kernelInstance.ServiceContainer().Has(messagebus.ServiceTransportsCloser) {
        t.Fatalf("expected no transports closer where no transports were registered")
    }
}

func TestBootContainer_TheJournalOfAConsoleProcessReopensOnSighup(t *testing.T) {
    directory := t.TempDir()
    logPath := filepath.Join(directory, "application.log")
    rotatedPath := filepath.Join(directory, "application.log.1")

    applicationInstance := newEnvironmentRefusalApplication(t, config.ModeCli, map[string]string{config.EnvKey: "dev", config.LogPathKey: logPath})
    applicationInstance.bootContainer()

    logger, resolveErr := logging.LoggerFromContainer(applicationInstance.kernel.ServiceContainer())
    if nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }
    defer func() { _ = applicationInstance.kernel.ServiceContainer().Close() }()

    logger.Emergency("before rotation", nil)

    renameErr := os.Rename(logPath, rotatedPath)
    if nil != renameErr {
        t.Fatalf("unexpected rename error: %v", renameErr)
    }

    killErr := syscall.Kill(os.Getpid(), syscall.SIGHUP)
    if nil != killErr {
        t.Fatalf("unexpected kill error: %v", killErr)
    }

    deadline := time.Now().Add(5 * time.Second)
    for {
        logger.Emergency("after rotation", nil)

        content, readErr := os.ReadFile(logPath)
        if nil == readErr && true == strings.Contains(string(content), "after rotation") {
            return
        }

        if true == time.Now().After(deadline) {
            t.Fatalf("the console journal was not reopened after the signal; the path answered %v", readErr)
        }

        time.Sleep(10 * time.Millisecond)
    }
}

/* a relative MELODY_LOG_PATH is anchored to the project directory and its parent directories are created before the open, so the console journal lands under the project and not under the working directory of the process */
func TestBootContainer_ARelativeLogPathIsAnchoredToTheProjectDirectoryUnderACreatedParent(t *testing.T) {
    relativePath := filepath.Join("var", "log", "nested", "application.log")

    applicationInstance := newEnvironmentRefusalApplication(t, config.ModeCli, map[string]string{config.EnvKey: "dev", config.LogPathKey: relativePath})
    applicationInstance.bootContainer()

    logger, resolveErr := logging.LoggerFromContainer(applicationInstance.kernel.ServiceContainer())
    if nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }
    defer func() { _ = applicationInstance.kernel.ServiceContainer().Close() }()

    logger.Emergency("anchored", nil)

    anchoredPath := filepath.Join(applicationInstance.Configuration().Kernel().ProjectDir(), relativePath)

    content, readErr := os.ReadFile(anchoredPath)
    if nil != readErr {
        t.Fatalf("expected the journal at %q under the project directory, got %v", anchoredPath, readErr)
    }

    if false == strings.Contains(string(content), "anchored") {
        t.Fatalf("expected the journal line under the project directory, got %q", string(content))
    }
}

type teardownOrderProbeBus struct{}

func (instance *teardownOrderProbeBus) Dispatch(runtimeInstance runtimecontract.Runtime, message any, stamps ...messagebuscontract.Stamp) (messagebuscontract.Envelope, error) {
    return nil, nil
}

func TestBoot_TheRegisteredBusesCloseBeforeTheTransportsTheyRouteTo(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvFiles: testhelper.NewEmbeddedEnvFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    messagebus.RegisterTransports(
        applicationInstance,
        map[string]messagebuscontract.Transport{"async": &recordingCloseTransport{}},
    )

    for _, busName := range []string{messagebus.ServiceBus, messagebus.ServiceConsumeBus} {
        applicationInstance.RegisterService(
            busName,
            func(resolver containercontract.Resolver) (messagebuscontract.Bus, error) {
                return &teardownOrderProbeBus{}, nil
            },
            container.RegisterOptions{TypeRegistrationDisabled: true},
        )
    }

    serviceContainer := applicationInstance.Boot().ServiceContainer()
    defer func() { _ = serviceContainer.Close() }()

    for _, busName := range []string{messagebus.ServiceBus, messagebus.ServiceConsumeBus} {
        _ = container.MustFromResolver[messagebuscontract.Bus](serviceContainer, busName)
    }

    plan := serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    }).TeardownPlan()

    for _, busName := range []string{messagebus.ServiceBus, messagebus.ServiceConsumeBus} {
        ordered := false
        for _, entry := range plan {
            if "service:"+busName == entry.NodeKey && true == slices.Contains(entry.Dependencies, "service:"+messagebus.ServiceTransportsCloser) {
                ordered = true
            }
        }

        if false == ordered {
            t.Fatalf("expected %s ordered before the transports closer, got %+v", busName, plan)
        }
    }
}

/* the after-boot refusals of RegisterService and RegisterScopedService name the service that was registered too late, as the parameter and configuration siblings name theirs. */
func TestApplicationRegisterService_AfterBootRefusalNamesTheService(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvFiles: testhelper.NewEmbeddedEnvFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    applicationInstance.Boot()

    for name, register := range map[string]func(){
        "service": func() {
            applicationInstance.RegisterService("service.late", func(resolver containercontract.Resolver) (*os.File, error) { return nil, nil }, container.RegisterOptions{})
        },
        "scoped service": func() {
            applicationInstance.RegisterScopedService("service.late", func(resolver containercontract.Resolver) (*os.File, error) { return nil, nil }, container.RegisterOptions{})
        },
    } {
        t.Run(name, func(t *testing.T) {
            defer func() {
                panicErr, isPanicErr := recover().(*exception.Error)
                if false == isPanicErr {
                    t.Fatalf("expected the late registration to be refused with an exception error")
                }

                if "service.late" != panicErr.Context()["serviceName"] {
                    t.Fatalf("expected the refusal to name the service, got %v", panicErr.Context())
                }
            }()

            register()
        })
    }
}

/* a log directory that cannot be created is refused naming the directory beside the file: the directory is what MkdirAll refused. */
func TestNewContainerLogger_RefusesADirectoryItCannotCreateNamingTheDirectory(t *testing.T) {
    inTheWay := filepath.Join(t.TempDir(), "file")
    if writeErr := os.WriteFile(inTheWay, []byte("x"), 0o644); nil != writeErr {
        t.Fatalf("could not put a file in the way: %v", writeErr)
    }

    logPath := filepath.Join(inTheWay, "app.log")

    defer func() {
        panicErr, isPanicErr := recover().(*exception.Error)
        if false == isPanicErr || "failed to create the log directory" != panicErr.Message() {
            t.Fatalf("expected the directory to be refused, got %v", panicErr)
        }

        if inTheWay != panicErr.Context()["directory"] || logPath != panicErr.Context()["path"] {
            t.Fatalf("expected the refusal to name the directory beside the file, got %v", panicErr.Context())
        }
    }()

    _ = newContainerLogger(logPath, loggingcontract.LevelDebug, nil, clock.NewSystemClock(), false, os.Stdout)
}

/* without MELODY_LOG_PATH a command journals on standard error, so what it prints on standard output, a document redirected to a file included, carries no log line; the http process journals on standard output */
func TestBootContainer_TheConsoleJournalOfACommandIsStandardError(t *testing.T) {
    for _, testCase := range []struct {
        mode     string
        onStderr bool
    }{
        {mode: config.ModeCli, onStderr: true},
        {mode: config.ModeHttp, onStderr: false},
    } {
        stdoutRead, stdoutWrite, stdoutPipeErr := os.Pipe()
        stderrRead, stderrWrite, stderrPipeErr := os.Pipe()
        if nil != stdoutPipeErr || nil != stderrPipeErr {
            t.Fatalf("unexpected pipe error: %v %v", stdoutPipeErr, stderrPipeErr)
        }

        originalStdout, originalStderr := os.Stdout, os.Stderr
        os.Stdout, os.Stderr = stdoutWrite, stderrWrite

        applicationInstance := newEnvironmentRefusalApplication(t, testCase.mode, map[string]string{config.EnvKey: "dev", config.LogPathKey: ""})
        applicationInstance.bootContainer()

        logger, resolveErr := logging.LoggerFromContainer(applicationInstance.kernel.ServiceContainer())
        if nil == resolveErr {
            logger.Emergency("console journal probe", nil)
        }

        os.Stdout, os.Stderr = originalStdout, originalStderr
        _ = applicationInstance.kernel.ServiceContainer().Close()
        _ = stdoutWrite.Close()
        _ = stderrWrite.Close()

        if nil != resolveErr {
            t.Fatalf("%s: unexpected resolve error: %v", testCase.mode, resolveErr)
        }

        stdoutContent, _ := io.ReadAll(stdoutRead)
        stderrContent, _ := io.ReadAll(stderrRead)

        if testCase.onStderr != strings.Contains(string(stderrContent), "console journal probe") || testCase.onStderr == strings.Contains(string(stdoutContent), "console journal probe") {
            t.Fatalf("%s: expected the record on stderr=%v, got stdout %q and stderr %q", testCase.mode, testCase.onStderr, stdoutContent, stderrContent)
        }
    }
}
