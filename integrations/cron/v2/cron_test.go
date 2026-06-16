package cron

import (
    "bytes"
    "context"
    "testing"

    clicontract "github.com/precision-soft/melody/v2/cli/contract"
    melodyconfig "github.com/precision-soft/melody/v2/config"
    configcontract "github.com/precision-soft/melody/v2/config/contract"
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
