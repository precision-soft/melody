package application

import (
    nethttp "net/http"
    "strings"
    "testing"

    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/config"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/http"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

type mapEnvironmentSource struct {
    values map[string]string
}

func (instance *mapEnvironmentSource) Load() (map[string]string, error) {
    return instance.values, nil
}

func newCollisionTestConfiguration(t *testing.T) *config.Configuration {
    t.Helper()

    environment, environmentErr := config.NewEnvironment(&mapEnvironmentSource{values: map[string]string{}})
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, t.TempDir())
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    return configuration
}

func newCollisionTestApplication(t *testing.T) *Application {
    t.Helper()

    return &Application{
        configuration:        newCollisionTestConfiguration(t),
        kernel:               newTestKernel(),
        cliCommands:          make([]clicontract.Command, 0),
        moduleConfigurations: make(map[string]any),
    }
}

type namedTestCommand struct {
    name string
}

func (instance *namedTestCommand) Name() string {
    return instance.name
}

func (instance *namedTestCommand) Description() string {
    return "test command"
}

func (instance *namedTestCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *namedTestCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    return nil
}

func stringProvider(value string) func(resolver containercontract.Resolver) (string, error) {
    return func(resolver containercontract.Resolver) (string, error) {
        return value, nil
    }
}

func TestBootCollision_DuplicateServiceIdIsRecordedAndFirstWins(t *testing.T) {
    application := newCollisionTestApplication(t)

    application.RegisterService("service.test.value", stringProvider("first"))
    application.RegisterService("service.test.value", stringProvider("second"))

    if 1 != len(application.bootCollisions) {
        t.Fatalf("expected one recorded collision, got %d", len(application.bootCollisions))
    }

    if bootCollisionKindService != application.bootCollisions[0].kind || "service.test.value" != application.bootCollisions[0].name {
        t.Fatalf("unexpected collision: %+v", application.bootCollisions[0])
    }

    resolved := container.MustFromResolver[string](application.kernel.ServiceContainer(), "service.test.value")
    if "first" != resolved {
        t.Fatalf("expected the first registration to win, got: %s", resolved)
    }
}

func TestBootCollision_DuplicateServiceTypeIsRecordedUnderStrictDefault(t *testing.T) {
    application := newCollisionTestApplication(t)

    type marker struct{ value string }

    application.RegisterService("service.test.first", func(resolver containercontract.Resolver) (*marker, error) {
        return &marker{value: "first"}, nil
    })
    application.RegisterService("service.test.second", func(resolver containercontract.Resolver) (*marker, error) {
        return &marker{value: "second"}, nil
    })

    if 1 != len(application.bootCollisions) {
        t.Fatalf("expected one recorded collision, got %d", len(application.bootCollisions))
    }

    if bootCollisionKindServiceType != application.bootCollisions[0].kind {
        t.Fatalf("unexpected collision kind: %s", application.bootCollisions[0].kind)
    }
}

func TestBootCollision_NonStrictTypeRegistrationIsNotRecorded(t *testing.T) {
    application := newCollisionTestApplication(t)

    type marker struct{ value string }

    application.RegisterService("service.test.first", func(resolver containercontract.Resolver) (*marker, error) {
        return &marker{value: "first"}, nil
    })
    application.RegisterService(
        "service.test.second",
        func(resolver containercontract.Resolver) (*marker, error) {
            return &marker{value: "second"}, nil
        },
        container.WithTypeRegistration(false),
    )

    if 0 != len(application.bootCollisions) {
        t.Fatalf("expected no recorded collisions, got %d", len(application.bootCollisions))
    }
}

/* a name claimed at one lifetime and then declared at the other is the same wiring mistake seen from the other side: it joins the aggregated report rather than ending the boot on its own, from either direction */
func TestBootCollision_ANameClaimedAtTheOtherLifetimeIsRecordedFromBothDirections(t *testing.T) {
    singletonFirst := newCollisionTestApplication(t)

    singletonFirst.RegisterService("test.scoped.value", stringProvider("first"))
    singletonFirst.RegisterScopedService("test.scoped.value", stringProvider("second"))

    if 1 != len(singletonFirst.bootCollisions) {
        t.Fatalf("expected one recorded collision, got %d", len(singletonFirst.bootCollisions))
    }
    if bootCollisionKindScopedService != singletonFirst.bootCollisions[0].kind {
        t.Fatalf("unexpected collision kind: %s", singletonFirst.bootCollisions[0].kind)
    }

    scopedFirst := newCollisionTestApplication(t)

    scopedFirst.RegisterScopedService("test.scoped.value", stringProvider("first"))
    scopedFirst.RegisterService("test.scoped.value", stringProvider("second"))

    if 1 != len(scopedFirst.bootCollisions) {
        t.Fatalf("expected one recorded collision, got %d", len(scopedFirst.bootCollisions))
    }
    if bootCollisionKindScopedService != scopedFirst.bootCollisions[0].kind {
        t.Fatalf("unexpected collision kind: %s", scopedFirst.bootCollisions[0].kind)
    }
}

/* the type is claimed across lifetimes too, and reported the same way: a request-scoped service whose type a singleton already declares would otherwise make a by-type lookup answer one of the two at random */
func TestBootCollision_ATypeClaimedAtTheOtherLifetimeIsRecordedFromBothDirections(t *testing.T) {
    type marker struct{ value string }

    markerProvider := func(value string) func(resolver containercontract.Resolver) (*marker, error) {
        return func(resolver containercontract.Resolver) (*marker, error) {
            return &marker{value: value}, nil
        }
    }

    singletonFirst := newCollisionTestApplication(t)

    singletonFirst.RegisterService("test.scoped.first", markerProvider("first"))
    singletonFirst.RegisterScopedService("test.scoped.second", markerProvider("second"))

    if 1 != len(singletonFirst.bootCollisions) {
        t.Fatalf("expected one recorded collision, got %d", len(singletonFirst.bootCollisions))
    }
    if bootCollisionKindScopedServiceType != singletonFirst.bootCollisions[0].kind {
        t.Fatalf("unexpected collision kind: %s", singletonFirst.bootCollisions[0].kind)
    }

    scopedFirst := newCollisionTestApplication(t)

    scopedFirst.RegisterScopedService("test.scoped.first", markerProvider("first"))
    scopedFirst.RegisterService("test.scoped.second", markerProvider("second"))

    if 1 != len(scopedFirst.bootCollisions) {
        t.Fatalf("expected one recorded collision, got %d", len(scopedFirst.bootCollisions))
    }
    if bootCollisionKindScopedServiceType != scopedFirst.bootCollisions[0].kind {
        t.Fatalf("unexpected collision kind: %s", scopedFirst.bootCollisions[0].kind)
    }
}

/* a duplicate is the one registration failure that is collected instead of thrown: anything else — a provider that is not a provider at all — ends the boot where it was written, because no later phase can make it valid */
func TestRegisterService_StaysFailFastForAFailureThatIsNotACollision(t *testing.T) {
    singletonApplication := newCollisionTestApplication(t)

    testhelper.AssertPanicsWithError(t, func() {
        singletonApplication.RegisterService("test.scoped.broken", "not a provider at all")
    }, "provider must be a function")

    if 0 != len(singletonApplication.bootCollisions) {
        t.Fatalf("expected a non-collision failure to stay out of the aggregated report, got %+v", singletonApplication.bootCollisions)
    }

    scopedApplication := newCollisionTestApplication(t)

    testhelper.AssertPanicsWithError(t, func() {
        scopedApplication.RegisterScopedService("test.scoped.broken", "not a provider at all")
    }, "provider must be a function")

    if 0 != len(scopedApplication.bootCollisions) {
        t.Fatalf("expected a non-collision failure to stay out of the aggregated report, got %+v", scopedApplication.bootCollisions)
    }
}

/* the scoped door closes at boot exactly as the singleton one does: the scopes are built by then, and a registration arriving here would be visible to some requests and not others */
func TestRegisterScopedService_RefusesAfterBoot(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)
    applicationInstance.booted = true

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterScopedService("test.scoped.value", stringProvider("late"))
    }, "may not register scoped services after boot")
}

func TestBootCollision_DuplicateParameterIsRecorded(t *testing.T) {
    application := newCollisionTestApplication(t)

    application.RegisterParameter("app.test.parameter", "first")
    application.RegisterParameter("app.test.parameter", "second")

    if 1 != len(application.bootCollisions) {
        t.Fatalf("expected one recorded collision, got %d", len(application.bootCollisions))
    }

    if bootCollisionKindParameter != application.bootCollisions[0].kind {
        t.Fatalf("unexpected collision kind: %s", application.bootCollisions[0].kind)
    }

    if "first" != application.configuration.Get("app.test.parameter").String() {
        t.Fatalf("expected the first parameter registration to win")
    }
}

func TestBootCollision_DuplicateConfigurationIsRecorded(t *testing.T) {
    application := newCollisionTestApplication(t)

    /* the registry accepts exactly one name in this major, so the duplicate path is exercised on it */
    application.RegisterConfiguration(loggingcontract.LoggingConfigurationName, "first")
    application.RegisterConfiguration(loggingcontract.LoggingConfigurationName, "second")

    if 1 != len(application.bootCollisions) {
        t.Fatalf("expected one recorded collision, got %d", len(application.bootCollisions))
    }

    if bootCollisionKindConfiguration != application.bootCollisions[0].kind {
        t.Fatalf("unexpected collision kind: %s", application.bootCollisions[0].kind)
    }

    if "first" != application.moduleConfigurations[loggingcontract.LoggingConfigurationName] {
        t.Fatalf("expected the first configuration registration to win")
    }
}

func TestBootCollision_DuplicateCliCommandIsRecorded(t *testing.T) {
    application := newCollisionTestApplication(t)

    application.RegisterCliCommand(&namedTestCommand{name: "app:work"})
    application.RegisterCliCommand(&namedTestCommand{name: "app:work"})

    if 1 != len(application.bootCollisions) {
        t.Fatalf("expected one recorded collision, got %d", len(application.bootCollisions))
    }

    if bootCollisionKindCliCommand != application.bootCollisions[0].kind {
        t.Fatalf("unexpected collision kind: %s", application.bootCollisions[0].kind)
    }

    if 1 != len(application.cliCommands) {
        t.Fatalf("expected the first command registration to win, got %d", len(application.cliCommands))
    }
}

func TestBootCollision_PanicReportsEveryCollisionAtOnce(t *testing.T) {
    application := newCollisionTestApplication(t)

    application.RegisterService("service.test.value", stringProvider("first"))
    application.RegisterService("service.test.value", stringProvider("second"))
    application.RegisterParameter("app.test.parameter", "first")
    application.RegisterParameter("app.test.parameter", "second")
    application.RegisterConfiguration(loggingcontract.LoggingConfigurationName, "first")
    application.RegisterConfiguration(loggingcontract.LoggingConfigurationName, "second")
    application.RegisterCliCommand(&namedTestCommand{name: "app:work"})
    application.RegisterCliCommand(&namedTestCommand{name: "app:work"})

    recovered := func() (recovered any) {
        defer func() {
            recovered = recover()
        }()

        application.panicOnBootCollisions()

        return nil
    }()

    if nil == recovered {
        t.Fatalf("expected the aggregated report to panic")
    }

    report, isError := recovered.(error)
    if false == isError {
        t.Fatalf("expected the panic payload to be an error, got %T", recovered)
    }

    message := report.Error()
    if false == strings.Contains(message, "duplicate registrations detected at boot") {
        t.Fatalf("unexpected report message: %s", message)
    }

    if 4 != len(application.bootCollisions) {
        t.Fatalf("expected four recorded collisions, got %d", len(application.bootCollisions))
    }
}

func TestBootCollision_NoCollisionsMeansNoPanic(t *testing.T) {
    application := newCollisionTestApplication(t)

    application.RegisterService("service.test.value", stringProvider("first"))

    application.panicOnBootCollisions()
}

/* the report exists to say where the duplicate came from; a fixed frame count named whichever delegation layer sat between the user's call and the recording, so the origin must be asserted to land in the caller's file whatever the registration path */
func TestBootCollision_OriginNamesTheCallerNotTheFrameworkPlumbing(t *testing.T) {
    application := newCollisionTestApplication(t)

    application.RegisterService("service.test.value", stringProvider("first"))
    application.RegisterService("service.test.value", stringProvider("second"))

    if 1 != len(application.bootCollisions) {
        t.Fatalf("expected one recorded collision, got %d", len(application.bootCollisions))
    }

    if false == strings.Contains(application.bootCollisions[0].origin, "boot_collision_test.go") {
        t.Fatalf("expected the origin to name the registration call site, got %q", application.bootCollisions[0].origin)
    }
}

func bootCollisionTestHandler() httpcontract.Handler {
    return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return nil, nil
    }
}

/* a duplicate route used to panic one at a time from inside bootHttp, outside the aggregated report this file exists for; while the recorder is armed it joins the report — the first registration wins — and its origin lands on the registration call site, not on the router's plumbing */
func TestBootCollision_ADuplicateRouteJoinsTheAggregatedReportWhileTheRecorderIsArmed(t *testing.T) {
    routeRegistry := http.NewRouteRegistry()
    router := http.NewRouterWithRouteRegistry(routeRegistry)

    application := &Application{routeRegistry: routeRegistry}
    application.armRouteCollisionRecorder()

    router.Handle(nethttp.MethodGet, "/duplicate", bootCollisionTestHandler())
    router.Handle(nethttp.MethodGet, "/duplicate", bootCollisionTestHandler())

    if 1 != len(application.bootCollisions) {
        t.Fatalf("expected exactly one recorded collision, got %d", len(application.bootCollisions))
    }

    if http.BootCollisionKindHttpRoute != application.bootCollisions[0].kind {
        t.Fatalf("expected the http route kind, got %q", application.bootCollisions[0].kind)
    }

    if false == strings.Contains(application.bootCollisions[0].origin, "boot_collision_test.go") {
        t.Fatalf("expected the origin to name the registration call site, got %q", application.bootCollisions[0].origin)
    }

    if 1 != len(routeRegistry.RouteDefinitions()) {
        t.Fatalf("expected the first registration to win, got %d routes", len(routeRegistry.RouteDefinitions()))
    }

    testhelper.AssertPanicsWithError(t, func() {
        application.panicOnBootCollisions()
    }, "duplicate registrations detected at boot")

    /* disarming hands the registry back its immediate refusal, which is what anything registering outside the boot window meets */
    application.disarmRouteCollisionRecorder()

    testhelper.AssertPanicsWithError(t, func() {
        router.Handle(nethttp.MethodGet, "/duplicate", bootCollisionTestHandler())
    }, "route already registered")
}
