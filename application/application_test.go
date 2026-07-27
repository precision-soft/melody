package application

import (
    "context"
    "os"
    "strings"
    "testing"

    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/config"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func TestAssertPanics_UsesRecover(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        exception.Panic(exception.NewError("test", nil, nil))
    }, "test")
}

/* servingProbeApplicationCommand reports what the configuration answers while the command is running, which is the only moment the question means anything: the marker is set between boot and dispatch and nothing else in the process observes the transition. */
type servingProbeApplicationCommand struct {
    ran        bool
    resolveErr error
}

func (instance *servingProbeApplicationCommand) Name() string {
    return "probe:serving"
}

func (instance *servingProbeApplicationCommand) Description() string {
    return "reports whether the configuration refuses a resolve while the command runs"
}

func (instance *servingProbeApplicationCommand) Flags() []clicontract.Flag {
    return []clicontract.Flag{}
}

func (instance *servingProbeApplicationCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    instance.ran = true

    configuration := config.ConfigMustFromContainer(runtimeInstance.Container())
    instance.resolveErr = configuration.Resolve()

    return nil
}

/* @info Run must tell the configuration the wiring phase is over before it dispatches anything, or a late Resolve silently rewrites parameters under services that already read them. The config package tests what MarkServing does; nothing tested that Run calls it, so deleting the call left both ./application/... and ./config/... green. This drives the real Run in cli mode and asks the configuration from inside the command. */
func TestRun_MarksTheConfigurationServingBeforeItDispatches(t *testing.T) {
    originalArguments := os.Args
    os.Args = []string{"probe", "probe:serving"}
    defer func() { os.Args = originalArguments }()

    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    probe := &servingProbeApplicationCommand{}
    applicationInstance.RegisterCliCommand(probe)

    applicationInstance.Run(context.Background())

    if false == probe.ran {
        t.Fatal("the probe command never ran, so the assertion below would be vacuous")
    }

    if nil == probe.resolveErr {
        t.Fatal("expected the configuration to refuse a resolve while the command runs, which means Run never marked it serving")
    }

    if false == strings.Contains(probe.resolveErr.Error(), "begun serving") {
        t.Fatalf("expected the refusal to name the serving phase, got %q", probe.resolveErr.Error())
    }
}
