package application

import (
    "context"
    "errors"
    "os"
    "path/filepath"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/cache"
    cachecontract "github.com/precision-soft/melody/v3/cache/contract"
    "github.com/precision-soft/melody/v3/clock"
    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/event"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/messagebus"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/security"
    securityconfig "github.com/precision-soft/melody/v3/security/config"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
    "github.com/precision-soft/melody/v3/serializer"
    serializercontract "github.com/precision-soft/melody/v3/serializer/contract"
    "github.com/precision-soft/melody/v3/session"
    sessioncontract "github.com/precision-soft/melody/v3/session/contract"
    "github.com/precision-soft/melody/v3/validation"
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
        ctx:                  context.Background(),
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
        context.Background(),
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
        ctx:                  context.Background(),
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
                clock.NewSystemClock(),
                false,
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

    logger := newContainerLogger(logPath, loggingcontract.LevelDebug, nil, clock.NewSystemClock(), false)
    if nil == logger {
        t.Fatalf("expected a logger")
    }

    if _, statErr := os.Stat(logPath); nil != statErr {
        t.Fatalf("expected the log file to be created, got %v", statErr)
    }
}

func TestBootContainer_TheApplicationsOwnLoggerIsSubstitutedNotCollided(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
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
        context.Background(),
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

    logger := newContainerLogger(logPath, loggingcontract.LevelDebug, nil, clock.NewSystemClock(), false)

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
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

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

/* TestBootContainer_TheValidatorAndUrlGeneratorAreSubstitutedNotCollided pins the other two the boot used to make unsubstitutable. Both have exported constructors, so a replacement built outside is a whole answer — which is the line that separates them from the router, the dispatcher and the clock, where a gate would promise a substitution the request path would then ignore. */
func TestBootContainer_TheValidatorAndUrlGeneratorAreSubstitutedNotCollided(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    ownValidator := validation.NewValidator()
    ownUrlGenerator := http.NewUrlGenerator(http.NewRouteRegistry())

    applicationInstance.RegisterService(
        validation.ServiceValidator,
        func(resolver containercontract.Resolver) (*validation.Validator, error) {
            return ownValidator, nil
        },
    )

    applicationInstance.RegisterService(
        http.ServiceUrlGenerator,
        func(resolver containercontract.Resolver) (httpcontract.UrlGenerator, error) {
            return ownUrlGenerator, nil
        },
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
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

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
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    ownSerializer := serializer.NewPlainTextSerializer()

    applicationInstance.RegisterService(
        serializer.ServiceSerializer,
        func(resolver containercontract.Resolver) (serializercontract.Serializer, error) {
            return ownSerializer, nil
        },
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
        runtimeFlags:        NewRuntimeFlags(config.ModeHttp),
        kernel:              kernelInstance,
        embeddedPublicFiles: nil,
        modules:             nil,
        cliCommands:         nil,
        httpRouteRegistrars: nil,
        httpMiddlewares:     nil,
    }
}
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

/* the http process is the case the closer was never built for: it publishes through a routing that holds the transport value directly, so it resolves the transports map never, and before this the container closed nothing it had not been asked to build — the broker connection lived exactly as long as the process. */
func TestBoot_TheRegisteredTransportsAreClosedByAProcessThatNeverResolvesTheMap(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

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
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

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
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    kernelInstance := applicationInstance.Boot()

    if true == kernelInstance.ServiceContainer().Has(messagebus.ServiceTransportsCloser) {
        t.Fatalf("expected no transports closer where no transports were registered")
    }
}
