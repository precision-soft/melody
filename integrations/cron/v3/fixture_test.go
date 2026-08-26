package cron

import (
    "bytes"
    "context"
    "testing"

    melodycli "github.com/precision-soft/melody/v3/cli"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyconfig "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/runtime"
    "io"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

func (instance *fakePlainCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
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

    return dispatchGenerateCommand(generateCommand, nil, extraArgs)
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

func runWithInjectedConfiguration(
    generateCommand *GenerateCommand,
    commandContext clicontract.Context,
    configuration configcontract.Configuration,
) error {
    if nil == configuration {
        configuration = newStubConfiguration(nil)
    }

    return generateCommand.runWithConfiguration(commandContext, configuration)
}

type stubConfiguration struct {
    parameters map[string]configcontract.Parameter
}

func newStubConfiguration(values map[string]string) *stubConfiguration {
    parameters := make(map[string]configcontract.Parameter, len(values))
    for name, value := range values {
        parameters[name] = melodyconfig.NewParameter(name, value, value, false)
    }

    return &stubConfiguration{parameters: parameters}
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
    return nil
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

/* the generate command is driven through the framework's own dispatch door, so the arguments are parsed against the flags the command declares and the command context it reads is the one production hands it. The injected configuration travels through a delegating command rather than through a hand-built parser, because the configuration seam sits inside runWithConfiguration and only the dispatch above it changed. */
type configuredGenerateCommand struct {
    *GenerateCommand
    configuration configcontract.Configuration
}

var _ clicontract.Command = (*configuredGenerateCommand)(nil)

func (instance *configuredGenerateCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
) error {
    return runWithInjectedConfiguration(instance.GenerateCommand, commandContext, instance.configuration)
}

func dispatchGenerateCommand(
    generateCommand *GenerateCommand,
    configuration configcontract.Configuration,
    extraArgs []string,
) (string, error) {
    var stdout bytes.Buffer

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    dispatched := &configuredGenerateCommand{
        GenerateCommand: generateCommand,
        configuration:   configuration,
    }

    runErr := melodycli.DispatchCommand(
        context.Background(),
        dispatched,
        runtimeInstance,
        append([]string{generateCommand.Name()}, extraArgs...),
        &stdout,
    )

    return stdout.String(), runErr
}

/* runnerDispatch drives the runner command through the framework's dispatch door while keeping the
Run(ctx, argv) shape the tests around it were written against: the arguments are parsed against the
flags the command declares, and the context it reads is the one production hands it. */
type runnerDispatch struct {
    runner          *RunnerCommand
    runtimeInstance runtimecontract.Runtime
    writer          io.Writer
}

func newRunnerDispatch(
    runner *RunnerCommand,
    runtimeInstance runtimecontract.Runtime,
    writer io.Writer,
) *runnerDispatch {
    return &runnerDispatch{
        runner:          runner,
        runtimeInstance: runtimeInstance,
        writer:          writer,
    }
}

func (instance *runnerDispatch) Run(ctx context.Context, arguments []string) error {
    runtimeInstance := instance.runtimeInstance
    if nil == runtimeInstance {
        runtimeInstance = newRunnerTestRuntime(ctx)
    }

    return melodycli.DispatchCommand(ctx, instance.runner, runtimeInstance, arguments, instance.writer)
}
