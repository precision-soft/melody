package cron

import (
    "bytes"
    "context"
    "testing"

    clicontract "github.com/precision-soft/melody/v2/cli/contract"
    melodyconfig "github.com/precision-soft/melody/v2/config"
    configcontract "github.com/precision-soft/melody/v2/config/contract"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    urfavecli "github.com/urfave/cli/v3"
)

type fakePlainCommand struct {
    commandName string
}

func newFakePlainCommand(name string) *fakePlainCommand {
    return &fakePlainCommand{commandName: name}
}

func (instance *fakePlainCommand) Name() string {
    return instance.commandName
}

func (instance *fakePlainCommand) Description() string {
    return "fake plain command"
}

func (instance *fakePlainCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *fakePlainCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
    return nil
}

type fakeCommandWithSchedule struct {
    fakePlainCommand
    config *EntryConfig
}

type testSchedule struct {
    Minute          string
    Hour            string
    DayOfMonth      string
    Month           string
    DayOfWeek       string
    User            string
    LogFileName     string
    LogFileNameRaw  bool
    LogDisabled     bool
    DestinationFile string
    Command         []string
    Instances       int
}

func (instance *testSchedule) toEntryConfig() *EntryConfig {
    if nil == instance {
        return nil
    }

    return &EntryConfig{
        Schedule: &Schedule{
            Minute:     instance.Minute,
            Hour:       instance.Hour,
            DayOfMonth: instance.DayOfMonth,
            Month:      instance.Month,
            DayOfWeek:  instance.DayOfWeek,
        },
        User:            instance.User,
        LogFileName:     instance.LogFileName,
        LogFileNameRaw:  instance.LogFileNameRaw,
        LogDisabled:     instance.LogDisabled,
        DestinationFile: instance.DestinationFile,
        Command:         instance.Command,
        Instances:       instance.Instances,
    }
}

func newFakeCommandWithSchedule(name string, schedule *testSchedule) *fakeCommandWithSchedule {
    return &fakeCommandWithSchedule{
        fakePlainCommand: fakePlainCommand{commandName: name},
        config:           schedule.toEntryConfig(),
    }
}

func newFakeCommandWithConfig(name string, config *EntryConfig) *fakeCommandWithSchedule {
    return &fakeCommandWithSchedule{
        fakePlainCommand: fakePlainCommand{commandName: name},
        config:           config,
    }
}

func buildConfigurationFromFakeCommands(commands []clicontract.Command) *Configuration {
    configuration := NewConfiguration()

    for _, command := range commands {
        scheduled, ok := command.(*fakeCommandWithSchedule)
        if false == ok {
            continue
        }

        if nil == scheduled.config {
            continue
        }

        configuration.Schedule(scheduled.Name(), scheduled.config)
    }

    return configuration
}

func runGenerateCommand(t *testing.T, providedCommands []clicontract.Command, extraArgs []string) (string, error) {
    t.Helper()

    return runGenerateCommandWithConfiguration(t, providedCommands, extraArgs, nil)
}

func runGenerateCommandWithRegistrar(
    t *testing.T,
    providedCommands []clicontract.Command,
    extraArgs []string,
    registrar func(*GenerateCommand),
) (string, error) {
    t.Helper()

    generateCommand := NewGenerateCommand(buildConfigurationFromFakeCommands(providedCommands))

    if nil != registrar {
        registrar(generateCommand)
    }

    var stdout bytes.Buffer

    subCommand := &urfavecli.Command{
        Name:  generateCommand.Name(),
        Flags: generateCommand.Flags(),
        Action: func(ctx context.Context, parsedCommand *urfavecli.Command) error {
            parsedCommand.Writer = &stdout

            return generateCommand.runWithConfiguration(parsedCommand, newStubConfiguration(nil))
        },
    }

    app := &urfavecli.Command{
        Name:     "test-app",
        Commands: []*urfavecli.Command{subCommand},
    }

    fullArgs := append([]string{"test-app", generateCommand.Name()}, extraArgs...)
    runErr := app.Run(context.Background(), fullArgs)

    return stdout.String(), runErr
}

func runGenerateCommandWithConfiguration(
    t *testing.T,
    providedCommands []clicontract.Command,
    extraArgs []string,
    configuration configcontract.Configuration,
) (string, error) {
    t.Helper()

    generateCommand := NewGenerateCommand(buildConfigurationFromFakeCommands(providedCommands))

    return dispatchGenerateCommand(generateCommand, configuration, extraArgs)
}

/* dispatchGenerateCommand runs a generate command a test has already built — and may have registered its own dialect on — through the cli engine, with the configuration injected. */
func dispatchGenerateCommand(
    generateCommand *GenerateCommand,
    configuration configcontract.Configuration,
    extraArgs []string,
) (string, error) {
    var stdout bytes.Buffer

    subCommand := &urfavecli.Command{
        Name:  generateCommand.Name(),
        Flags: generateCommand.Flags(),
        Action: func(ctx context.Context, parsedCommand *urfavecli.Command) error {
            parsedCommand.Writer = &stdout

            return runWithInjectedConfiguration(generateCommand, parsedCommand, configuration)
        },
    }

    app := &urfavecli.Command{
        Name:     "test-app",
        Commands: []*urfavecli.Command{subCommand},
    }

    fullArgs := append([]string{"test-app", generateCommand.Name()}, extraArgs...)
    runErr := app.Run(context.Background(), fullArgs)

    return stdout.String(), runErr
}

func runWithInjectedConfiguration(
    generateCommand *GenerateCommand,
    commandContext *clicontract.CommandContext,
    configuration configcontract.Configuration,
) error {
    if nil == configuration {
        configuration = newStubConfiguration(nil)
    }

    return generateCommand.runWithConfiguration(commandContext, configuration)
}

/* stubApplicationName is the cli name the stub configuration answers, the identity the ownership line of every generated destination carries: every configuration the framework builds carries a non-empty one, so the double does too, and a test about the nameless case builds its stub without one. */
const stubApplicationName = "melody-cron-test"

type stubCliConfiguration struct {
    name string
}

func (instance *stubCliConfiguration) Name() string {
    return instance.name
}

func (instance *stubCliConfiguration) Description() string {
    return "cron test application"
}

type stubConfiguration struct {
    parameters map[string]configcontract.Parameter
    cli        configcontract.CliConfiguration
}

func newStubConfiguration(values map[string]string) *stubConfiguration {
    return newStubConfigurationNamed(values, stubApplicationName)
}

/* newStubConfigurationNamed answers the stub under the given application name; an empty name leaves the cli configuration absent, the shape of a double that never declared one */
func newStubConfigurationNamed(values map[string]string, applicationName string) *stubConfiguration {
    parameters := make(map[string]configcontract.Parameter, len(values))
    for name, value := range values {
        parameters[name] = melodyconfig.NewParameter(name, value, value, false)
    }

    configuration := &stubConfiguration{parameters: parameters}
    if "" != applicationName {
        configuration.cli = &stubCliConfiguration{name: applicationName}
    }

    return configuration
}

func (instance *stubConfiguration) Get(name string) configcontract.Parameter {
    return instance.parameters[name]
}

func (instance *stubConfiguration) MustGet(name string) configcontract.Parameter {
    return instance.parameters[name]
}

func (instance *stubConfiguration) RegisterRuntime(name string, value any) {}

func (instance *stubConfiguration) RegisterRuntimeSecret(name string, value any) {
}

func (instance *stubConfiguration) MarkSecret(name string) bool {
    return false
}

func (instance *stubConfiguration) Resolve() error {
    return nil
}

func (instance *stubConfiguration) Cli() configcontract.CliConfiguration {
    return instance.cli
}

func (instance *stubConfiguration) Kernel() configcontract.KernelConfiguration {
    return nil
}

func (instance *stubConfiguration) Http() configcontract.HttpConfiguration {
    return nil
}

func (instance *stubConfiguration) Names() []string {
    return nil
}

type stubKernelConfiguration struct {
    projectDirectory string
}

func (instance *stubKernelConfiguration) DefaultMode() string {
    return "http"
}

func (instance *stubKernelConfiguration) ProcessRole() string {
    return ""
}

func (instance *stubKernelConfiguration) Env() string {
    return "dev"
}

func (instance *stubKernelConfiguration) ProjectDir() string {
    return instance.projectDirectory
}

func (instance *stubKernelConfiguration) LogsDir() string {
    return ""
}

func (instance *stubKernelConfiguration) CacheDir() string {
    return ""
}

func (instance *stubKernelConfiguration) LogPath() string {
    return ""
}

func (instance *stubKernelConfiguration) LogLevel() loggingcontract.Level {
    return loggingcontract.LevelInfo
}

type stubConfigurationWithKernel struct {
    *stubConfiguration
    kernel configcontract.KernelConfiguration
}

func (instance *stubConfigurationWithKernel) Kernel() configcontract.KernelConfiguration {
    return instance.kernel
}

func newStubConfigurationWithProjectDirectory(values map[string]string, projectDirectory string) *stubConfigurationWithKernel {
    return &stubConfigurationWithKernel{
        stubConfiguration: newStubConfiguration(values),
        kernel:            &stubKernelConfiguration{projectDirectory: projectDirectory},
    }
}
