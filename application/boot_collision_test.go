package application

import (
    "strings"
    "testing"

    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/config"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
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

    application.RegisterConfiguration("module.test", "first")
    application.RegisterConfiguration("module.test", "second")

    if 1 != len(application.bootCollisions) {
        t.Fatalf("expected one recorded collision, got %d", len(application.bootCollisions))
    }

    if bootCollisionKindConfiguration != application.bootCollisions[0].kind {
        t.Fatalf("unexpected collision kind: %s", application.bootCollisions[0].kind)
    }

    if "first" != application.moduleConfigurations["module.test"] {
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
    application.RegisterConfiguration("module.test", "first")
    application.RegisterConfiguration("module.test", "second")
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

/* @info the report exists to say where the duplicate came from; a fixed frame count named whichever delegation layer sat between the user's call and the recording, so the origin must be asserted to land in the caller's file whatever the registration path */
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
