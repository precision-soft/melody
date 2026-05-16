package cron

import (
    "bytes"
    "context"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "testing"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyconfig "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    urfavecli "github.com/urfave/cli/v3"
)

func TestNewGenerateCommandIdentity(t *testing.T) {
    command := NewGenerateCommand(nil)

    if "melody:cron:generate" != command.Name() {
        t.Fatalf("Name() = %q, want %q", command.Name(), "melody:cron:generate")
    }

    if "" == command.Description() {
        t.Fatalf("Description() should not be empty")
    }

    flags := command.Flags()
    if 8 != len(flags) {
        t.Fatalf("expected 8 flags, got %d", len(flags))
    }
}

func TestRunWritesFileWithEntriesForCommandsImplementingMetadata(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("product:list", &testSchedule{Minute: "0", Hour: "3"}),
        newFakeCommandWithSchedule("billing:cleanup", &testSchedule{Minute: "*/15"}),
        newFakePlainCommand("debug:router"),
    }

    stdout, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v\nstdout=%s", err, stdout)
    }

    body, readErr := os.ReadFile(outputPath)
    if nil != readErr {
        t.Fatalf("failed to read crontab output %s: %v", outputPath, readErr)
    }

    content := string(body)

    expectedListLine := "0 3 * * * deploy /usr/local/bin/fakeapp product:list >> '" + filepath.Join(tempDir, "logs", "product-list.log") + "' 2>&1"
    if false == strings.Contains(content, expectedListLine) {
        t.Fatalf("expected product:list line %q in:\n%s", expectedListLine, content)
    }

    expectedCleanupLine := "*/15 * * * * deploy /usr/local/bin/fakeapp billing:cleanup >> '" + filepath.Join(tempDir, "logs", "billing-cleanup.log") + "' 2>&1"
    if false == strings.Contains(content, expectedCleanupLine) {
        t.Fatalf("expected billing:cleanup line %q in:\n%s", expectedCleanupLine, content)
    }

    if true == strings.Contains(content, "debug:router") {
        t.Fatalf("debug:router does not implement Metadata; must not appear in:\n%s", content)
    }
}

func TestRunSkipsCommandsWhoseCronScheduleReturnsNil(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("nil:schedule", nil),
        newFakeCommandWithSchedule("real:schedule", &testSchedule{Minute: "30"}),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, readErr := os.ReadFile(outputPath)
    if nil != readErr {
        t.Fatalf("failed to read crontab output: %v", readErr)
    }

    content := string(body)
    if true == strings.Contains(content, "nil:schedule") {
        t.Fatalf("commands with nil CronSchedule() must be skipped; got:\n%s", content)
    }

    if false == strings.Contains(content, "real:schedule") {
        t.Fatalf("real:schedule entry missing in:\n%s", content)
    }
}

func TestRunWritesHeartbeatOnlyFileWhenNoEntriesAndHeartbeatPathProvided(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    heartbeatPath := filepath.Join(tempDir, "heartbeat.crontab")

    commands := []clicontract.Command{
        newFakePlainCommand("debug:router"),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--heartbeat-path", heartbeatPath,
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, readErr := os.ReadFile(outputPath)
    if nil != readErr {
        t.Fatalf("failed to read crontab output: %v", readErr)
    }

    content := string(body)
    expectedHeartbeat := "* * * * * deploy /bin/touch " + heartbeatPath
    if false == strings.Contains(content, expectedHeartbeat) {
        t.Fatalf("expected heartbeat line %q in:\n%s", expectedHeartbeat, content)
    }
}

func TestRunDoesNotWriteFileWhenNoEntriesAndNoHeartbeat(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakePlainCommand("debug:router"),
    }

    stdout, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    if false == strings.Contains(stdout, "nothing to write") {
        t.Fatalf("expected stdout to contain 'nothing to write', got: %q", stdout)
    }

    if _, statErr := os.Stat(outputPath); false == os.IsNotExist(statErr) {
        t.Fatalf("expected no file at %s when no entries and no heartbeat; statErr=%v", outputPath, statErr)
    }
}

func TestRunHonorsPerCommandUserOverride(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("custom:user", &testSchedule{Minute: "0", User: "ec2-user"}),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    if false == strings.Contains(content, " ec2-user /usr/local/bin/fakeapp custom:user ") {
        t.Fatalf("expected per-command user 'ec2-user' to override --user default, got:\n%s", content)
    }
}

func TestRunHonorsPerCommandLogFileNameOverride(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("custom:log", &testSchedule{Minute: "0", LogFileName: "explicit.log"}),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    expectedRedirect := ">> '" + filepath.Join(tempDir, "logs", "explicit.log") + "' 2>&1"
    if false == strings.Contains(content, expectedRedirect) {
        t.Fatalf("expected per-command log file name override %q in:\n%s", expectedRedirect, content)
    }
}

func TestRunPreservesRegistrationOrder(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("zeta:job", &testSchedule{Minute: "0"}),
        newFakeCommandWithSchedule("alpha:job", &testSchedule{Minute: "0"}),
        newFakeCommandWithSchedule("mike:job", &testSchedule{Minute: "0"}),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    zetaIndex := strings.Index(content, "zeta:job")
    alphaIndex := strings.Index(content, "alpha:job")
    mikeIndex := strings.Index(content, "mike:job")

    if -1 == zetaIndex || -1 == alphaIndex || -1 == mikeIndex {
        t.Fatalf("expected all three entries; zeta=%d alpha=%d mike=%d", zetaIndex, alphaIndex, mikeIndex)
    }

    if false == (zetaIndex < alphaIndex && alphaIndex < mikeIndex) {
        t.Fatalf("expected registration order zeta < alpha < mike; got positions %d %d %d", zetaIndex, alphaIndex, mikeIndex)
    }
}

func TestRunPreservesRegistrationOrderWithinSharedDestination(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "default.crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("zeta:job", &testSchedule{Minute: "0", DestinationFile: "shared.crontab"}),
        newFakeCommandWithSchedule("alpha:job", &testSchedule{Minute: "0", DestinationFile: "shared.crontab"}),
        newFakeCommandWithSchedule("mike:job", &testSchedule{Minute: "0", DestinationFile: "shared.crontab"}),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, readErr := os.ReadFile(filepath.Join(tempDir, "shared.crontab"))
    if nil != readErr {
        t.Fatalf("expected shared.crontab to exist: %v", readErr)
    }
    content := string(body)

    zetaIndex := strings.Index(content, "zeta:job")
    alphaIndex := strings.Index(content, "alpha:job")
    mikeIndex := strings.Index(content, "mike:job")

    if -1 == zetaIndex || -1 == alphaIndex || -1 == mikeIndex {
        t.Fatalf("expected all three entries in shared destination; zeta=%d alpha=%d mike=%d", zetaIndex, alphaIndex, mikeIndex)
    }

    if false == (zetaIndex < alphaIndex && alphaIndex < mikeIndex) {
        t.Fatalf("expected registration order zeta < alpha < mike in shared destination; got positions %d %d %d", zetaIndex, alphaIndex, mikeIndex)
    }
}

func TestRunDefaultsLogFileNameFromSanitizedCommandName(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("ns:foo:bar", &testSchedule{Minute: "0"}),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    expectedRedirect := ">> '" + filepath.Join(tempDir, "logs", "ns-foo-bar.log") + "' 2>&1"
    if false == strings.Contains(content, expectedRedirect) {
        t.Fatalf("expected sanitized log file name %q in:\n%s", expectedRedirect, content)
    }
}

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

func TestRunUsesParameterDefaultsWhenFlagsNotProvided(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    configuration := newStubConfiguration(map[string]string{
        ParameterUser:    "deploy",
        ParameterLogsDir: filepath.Join(tempDir, "param-logs"),
        ParameterBinary:  "/opt/melody/app",
    })

    _, err := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{"--out", outputPath},
        configuration,
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    expectedLine := "0 2 * * * deploy /opt/melody/app backup:run >> '" + filepath.Join(tempDir, "param-logs", "backup-run.log") + "' 2>&1"
    if false == strings.Contains(content, expectedLine) {
        t.Fatalf("expected line %q sourced from parameters in:\n%s", expectedLine, content)
    }
}

func TestRunCLIFlagOverridesParameterDefault(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    configuration := newStubConfiguration(map[string]string{
        ParameterUser:   "deploy",
        ParameterBinary: "/opt/melody/app",
    })

    _, err := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "cli-logs"),
            "--binary", "/usr/local/bin/cli-override",
            "--user", "cli-user",
        },
        configuration,
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    expectedLine := "0 2 * * * cli-user /usr/local/bin/cli-override backup:run >> '" + filepath.Join(tempDir, "cli-logs", "backup-run.log") + "' 2>&1"
    if false == strings.Contains(content, expectedLine) {
        t.Fatalf("expected CLI flag values to override parameters in:\n%s", content)
    }
}

func TestRunErrorsWhenNoOutputPathConfigured(t *testing.T) {
    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    _, err := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
        newStubConfiguration(nil),
    )
    if nil == err {
        t.Fatalf("expected error when no output path is configured, got nil")
    }

    if false == strings.Contains(err.Error(), "no output path configured") {
        t.Fatalf("expected error to mention missing output path, got: %v", err)
    }
}

func TestRunErrorsWhenLogsDirMissingAndCommandWantsLogging(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    _, err := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
        newStubConfiguration(nil),
    )
    if nil == err {
        t.Fatalf("expected error when logs-dir is missing and a command wants logging, got nil")
    }

    if false == strings.Contains(err.Error(), "no logs-dir is configured") {
        t.Fatalf("expected error to mention missing logs-dir, got: %v", err)
    }
}

func TestRunSucceedsWithoutLogsDirWhenAllSchedulesDisableLogging(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("quiet:job", &testSchedule{
            Minute:      "0",
            Hour:        "2",
            LogDisabled: true,
        }),
    }

    _, err := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
        newStubConfiguration(nil),
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error when all schedules disable logging: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    if true == strings.Contains(string(body), " >> '") {
        t.Fatalf("expected no log redirection, got:\n%s", string(body))
    }
}

func TestRunUsesDestinationFileParameterWhenFlagNotProvided(t *testing.T) {
    tempDir := t.TempDir()
    parameterOutputPath := filepath.Join(tempDir, "from-param.crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    configuration := newStubConfiguration(map[string]string{
        ParameterDestinationFile: parameterOutputPath,
    })

    _, err := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
        configuration,
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    if _, statErr := os.Stat(parameterOutputPath); nil != statErr {
        t.Fatalf("expected file at %s sourced from ParameterDestinationFile, got: %v", parameterOutputPath, statErr)
    }
}

func TestRunUsesHeartbeatPathParameterWhenFlagNotProvided(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    parameterHeartbeatPath := filepath.Join(tempDir, "from-param-heartbeat.crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    configuration := newStubConfiguration(map[string]string{
        ParameterHeartbeatPath: parameterHeartbeatPath,
    })

    _, err := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
        configuration,
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    expectedHeartbeat := "* * * * * deploy /bin/touch " + parameterHeartbeatPath
    if false == strings.Contains(content, expectedHeartbeat) {
        t.Fatalf("expected heartbeat line %q sourced from ParameterHeartbeatPath in:\n%s", expectedHeartbeat, content)
    }
}

func TestRunErrorsWhenParameterUserEmptyAndNoFlag(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    configuration := newStubConfiguration(map[string]string{
        ParameterUser: "",
    })

    _, err := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
        },
        configuration,
    )
    if nil == err {
        t.Fatalf("expected error when user cascade resolves to empty, got nil")
    }

    if false == strings.Contains(err.Error(), "has no user") {
        t.Fatalf("expected error to mention missing user, got: %v", err)
    }
    if false == strings.Contains(err.Error(), "melody.cron.user") {
        t.Fatalf("expected error to mention the melody.cron.user parameter, got: %v", err)
    }
}

type capturingRegistrar struct {
    values map[string]any
}

func (instance *capturingRegistrar) RegisterParameter(name string, value any) {
    instance.values[name] = value
}

func TestRegisterDefaultParametersWiresExpectedDefaults(t *testing.T) {
    captured := &capturingRegistrar{values: map[string]any{}}

    RegisterDefaultParameters(captured)

    expected := map[string]string{
        ParameterDestinationFile: "%kernel.project_dir%/generated_conf/cron/crontab",
        ParameterLogsDir:         "%kernel.logs_dir%/cron",
        ParameterTemplate:        TemplateNameCrontab,
    }

    for name, want := range expected {
        got, ok := captured.values[name]
        if false == ok {
            t.Fatalf("RegisterDefaultParameters did not register %s", name)
        }
        if want != got {
            t.Fatalf("parameter %s = %v, want %q", name, got, want)
        }
    }

    if len(expected) != len(captured.values) {
        t.Fatalf("RegisterDefaultParameters registered %d parameters, want %d (%+v)", len(captured.values), len(expected), captured.values)
    }

    if _, registeredBinary := captured.values[ParameterBinary]; true == registeredBinary {
        t.Fatalf("RegisterDefaultParameters must not register %s; got %v", ParameterBinary, captured.values[ParameterBinary])
    }
    if _, registeredHeartbeat := captured.values[ParameterHeartbeatPath]; true == registeredHeartbeat {
        t.Fatalf("RegisterDefaultParameters must not register %s; got %v", ParameterHeartbeatPath, captured.values[ParameterHeartbeatPath])
    }
    if _, registeredUser := captured.values[ParameterUser]; true == registeredUser {
        t.Fatalf("RegisterDefaultParameters must not register %s; got %v", ParameterUser, captured.values[ParameterUser])
    }
}

func TestRunUsesParameterUserAsHeartbeatUser(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    heartbeatPath := filepath.Join(tempDir, "heartbeat.crontab")

    configuration := newStubConfiguration(map[string]string{
        ParameterUser: "param-user",
    })

    _, err := runGenerateCommandWithConfiguration(
        t,
        nil,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--heartbeat-path", heartbeatPath,
        },
        configuration,
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    expectedHeartbeat := "* * * * * param-user /bin/touch " + heartbeatPath
    if false == strings.Contains(content, expectedHeartbeat) {
        t.Fatalf("expected heartbeat to use ParameterUser %q in:\n%s", expectedHeartbeat, content)
    }
}

func TestRunGroupsEntriesByDestinationFile(t *testing.T) {
    tempDir := t.TempDir()
    defaultOutputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("default:job", &testSchedule{Minute: "0", Hour: "1"}),
        newFakeCommandWithSchedule("billing:job", &testSchedule{
            Minute:          "0",
            Hour:            "2",
            DestinationFile: "billing-crontab",
        }),
        newFakeCommandWithSchedule("absolute:job", &testSchedule{
            Minute:          "30",
            DestinationFile: filepath.Join(tempDir, "abs-crontab"),
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", defaultOutputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    defaultBody, defaultErr := os.ReadFile(defaultOutputPath)
    if nil != defaultErr {
        t.Fatalf("expected default crontab at %s, got: %v", defaultOutputPath, defaultErr)
    }
    if false == strings.Contains(string(defaultBody), "default:job") {
        t.Fatalf("expected default crontab to contain default:job entry, got:\n%s", string(defaultBody))
    }
    if true == strings.Contains(string(defaultBody), "billing:job") {
        t.Fatalf("default crontab must not contain billing:job entry, got:\n%s", string(defaultBody))
    }

    billingPath := filepath.Join(tempDir, "billing-crontab")
    billingBody, billingErr := os.ReadFile(billingPath)
    if nil != billingErr {
        t.Fatalf("expected billing crontab at %s, got: %v", billingPath, billingErr)
    }
    if false == strings.Contains(string(billingBody), "billing:job") {
        t.Fatalf("expected billing crontab to contain billing:job entry, got:\n%s", string(billingBody))
    }

    absolutePath := filepath.Join(tempDir, "abs-crontab")
    absoluteBody, absoluteErr := os.ReadFile(absolutePath)
    if nil != absoluteErr {
        t.Fatalf("expected absolute-destination crontab at %s, got: %v", absolutePath, absoluteErr)
    }
    if false == strings.Contains(string(absoluteBody), "absolute:job") {
        t.Fatalf("expected absolute crontab to contain absolute:job entry, got:\n%s", string(absoluteBody))
    }
}

func TestRunWritesHeartbeatToEveryDestinationFile(t *testing.T) {
    tempDir := t.TempDir()
    defaultOutputPath := filepath.Join(tempDir, "crontab")
    heartbeatPath := filepath.Join(tempDir, "heartbeat.crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("default:job", &testSchedule{Minute: "0", Hour: "1"}),
        newFakeCommandWithSchedule("billing:job", &testSchedule{
            Minute:          "0",
            Hour:            "2",
            DestinationFile: "billing-crontab",
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", defaultOutputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--heartbeat-path", heartbeatPath,
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    expectedHeartbeat := "* * * * * deploy /bin/touch " + heartbeatPath

    defaultBody, _ := os.ReadFile(defaultOutputPath)
    if false == strings.Contains(string(defaultBody), expectedHeartbeat) {
        t.Fatalf("expected heartbeat in default crontab, got:\n%s", string(defaultBody))
    }

    billingBody, _ := os.ReadFile(filepath.Join(tempDir, "billing-crontab"))
    if false == strings.Contains(string(billingBody), expectedHeartbeat) {
        t.Fatalf("expected heartbeat in billing crontab, got:\n%s", string(billingBody))
    }
}

func TestRunUsesPerCommandCommandOverride(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("wrapped:job", &testSchedule{
            Minute:  "0",
            Hour:    "5",
            Command: []string{"/usr/bin/nice", "-n", "10", "/opt/melody/app", "wrapped:job", "--flag=val"},
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    expectedLine := "0 5 * * * deploy /usr/bin/nice -n 10 /opt/melody/app wrapped:job --flag=val >> '"
    if false == strings.Contains(content, expectedLine) {
        t.Fatalf("expected custom Command line %q in:\n%s", expectedLine, content)
    }

    if true == strings.Contains(content, " /usr/local/bin/fakeapp wrapped:job ") {
        t.Fatalf("default binary must be ignored when Schedule.Command is set; got:\n%s", content)
    }
}

func TestRunDisablesLogRedirectionWhenLogDisabled(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("quiet:job", &testSchedule{
            Minute:      "0",
            Hour:        "4",
            LogDisabled: true,
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    if true == strings.Contains(content, "quiet-job.log") {
        t.Fatalf("expected no log redirection when LogDisabled=true, got:\n%s", content)
    }

    if true == strings.Contains(content, " >> '") {
        t.Fatalf("expected no `>>` redirect when LogDisabled=true, got:\n%s", content)
    }

    expectedLine := "0 4 * * * deploy /usr/local/bin/fakeapp quiet:job"
    if false == strings.Contains(content, expectedLine) {
        t.Fatalf("expected line without log redirection %q in:\n%s", expectedLine, content)
    }
}

func TestRunGroupsMultipleEntriesIntoSameDestinationFile(t *testing.T) {
    tempDir := t.TempDir()
    defaultOutputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("billing:run", &testSchedule{
            Minute:          "0",
            Hour:            "2",
            DestinationFile: "billing-crontab",
        }),
        newFakeCommandWithSchedule("billing:reconcile", &testSchedule{
            Minute:          "30",
            Hour:            "2",
            DestinationFile: "billing-crontab",
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", defaultOutputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    billingPath := filepath.Join(tempDir, "billing-crontab")
    body, readErr := os.ReadFile(billingPath)
    if nil != readErr {
        t.Fatalf("expected billing crontab at %s, got: %v", billingPath, readErr)
    }
    content := string(body)

    runIndex := strings.Index(content, "billing:run")
    reconcileIndex := strings.Index(content, "billing:reconcile")
    if -1 == runIndex || -1 == reconcileIndex {
        t.Fatalf("expected both billing entries in same file; run=%d reconcile=%d in:\n%s", runIndex, reconcileIndex, content)
    }

    if false == (runIndex < reconcileIndex) {
        t.Fatalf("expected registration order billing:run < billing:reconcile; got positions %d %d", runIndex, reconcileIndex)
    }

    if _, statErr := os.Stat(defaultOutputPath); false == os.IsNotExist(statErr) {
        t.Fatalf("expected no default-destination file when all entries route elsewhere; statErr=%v", statErr)
    }
}

func TestRunHeartbeatOnlyInCustomDestinationsWhenNoDefaultBucket(t *testing.T) {
    tempDir := t.TempDir()
    defaultOutputPath := filepath.Join(tempDir, "crontab")
    heartbeatPath := filepath.Join(tempDir, "heartbeat.crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("billing:job", &testSchedule{
            Minute:          "0",
            Hour:            "2",
            DestinationFile: "billing-crontab",
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", defaultOutputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--heartbeat-path", heartbeatPath,
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    expectedHeartbeat := "* * * * * deploy /bin/touch " + heartbeatPath

    billingBody, billingErr := os.ReadFile(filepath.Join(tempDir, "billing-crontab"))
    if nil != billingErr {
        t.Fatalf("expected billing crontab to be written, got: %v", billingErr)
    }
    if false == strings.Contains(string(billingBody), expectedHeartbeat) {
        t.Fatalf("expected heartbeat in billing crontab, got:\n%s", string(billingBody))
    }

    if _, statErr := os.Stat(defaultOutputPath); false == os.IsNotExist(statErr) {
        t.Fatalf("expected default-destination file to NOT exist when no default-bucket entries; statErr=%v", statErr)
    }
}

func TestRunAcceptsNilCommandProviderResult(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    stdout, err := runGenerateCommand(
        t,
        nil,
        []string{
            "--out", outputPath,
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error for nil commands: %v", err)
    }

    if false == strings.Contains(stdout, "nothing to write") {
        t.Fatalf("expected nothing-to-write message for nil commands, got: %q", stdout)
    }

    if _, statErr := os.Stat(outputPath); false == os.IsNotExist(statErr) {
        t.Fatalf("expected no file at %s for nil commands without heartbeat; statErr=%v", outputPath, statErr)
    }
}

func TestRunExpandsMultipleInstancesIntoSeparateEntries(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("billing:run", &testSchedule{
            Minute:    "0",
            Hour:      "2",
            Instances: 4,
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    for index := 1; index <= 4; index++ {
        expectedLine := fmt.Sprintf("0 2 * * * deploy /usr/local/bin/fakeapp billing:run --max-instances=4 --instance-index=%d >> '"+filepath.Join(tempDir, "logs", "billing-run-%d.log")+"' 2>&1", index, index)
        if false == strings.Contains(content, expectedLine) {
            t.Fatalf("expected instance %d line %q in:\n%s", index, expectedLine, content)
        }
    }
}

func TestRunSingleInstanceOmitsMaxInstancesFlag(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("billing:run", &testSchedule{
            Minute:    "0",
            Hour:      "2",
            Instances: 1,
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    if true == strings.Contains(content, "--max-instances") {
        t.Fatalf("expected no --max-instances flag when Instances=1, got:\n%s", content)
    }

    expectedLine := "0 2 * * * deploy /usr/local/bin/fakeapp billing:run >> '" + filepath.Join(tempDir, "logs", "billing-run.log") + "' 2>&1"
    if false == strings.Contains(content, expectedLine) {
        t.Fatalf("expected single line %q in:\n%s", expectedLine, content)
    }
}

func TestRunMultiInstancePreservesCustomCommandPartsAcrossInstances(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("wrapped:job", &testSchedule{
            Minute:    "0",
            Hour:      "5",
            Instances: 2,
            Command:   []string{"/usr/bin/flock", "-n", "/var/run/wrapped.lock", "/opt/myapp", "wrapped:job"},
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    if 2 != strings.Count(content, "/usr/bin/flock -n /var/run/wrapped.lock /opt/myapp wrapped:job") {
        t.Fatalf("expected exactly 2 entries with custom command parts, got:\n%s", content)
    }

    if true == strings.Contains(content, "--max-instances") {
        t.Fatalf("custom Command override should NOT receive --max-instances flag injection; got:\n%s", content)
    }

    for index := 1; index <= 2; index++ {
        expectedLogPath := filepath.Join(tempDir, "logs", fmt.Sprintf("wrapped-job-%d.log", index))
        if false == strings.Contains(content, expectedLogPath) {
            t.Fatalf("expected per-instance log path %q in:\n%s", expectedLogPath, content)
        }
    }
}

func TestRunRawLogFileNameKeepsColonsInAutoName(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("hotel:channel-manager:sync-allotment", &testSchedule{
            Minute:         "0",
            Hour:           "2",
            LogFileNameRaw: true,
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    expectedLog := filepath.Join(tempDir, "logs", "hotel:channel-manager:sync-allotment.log")
    if false == strings.Contains(content, expectedLog) {
        t.Fatalf("expected raw (unsanitized) log filename %q in:\n%s", expectedLog, content)
    }
}

func TestRunSanitizedLogFileNameReplacesColons(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("hotel:channel-manager:sync-allotment", &testSchedule{
            Minute: "0",
            Hour:   "2",
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    expectedLog := filepath.Join(tempDir, "logs", "hotel-channel-manager-sync-allotment.log")
    if false == strings.Contains(content, expectedLog) {
        t.Fatalf("expected sanitized log filename %q in:\n%s", expectedLog, content)
    }
}

func TestRunFallsBackToOsExecutableWhenBinaryNotConfigured(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    _, err := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--user", "deploy",
        },
        newStubConfiguration(nil),
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    executable, executableErr := os.Executable()
    if nil != executableErr {
        t.Fatalf("could not resolve os.Executable for assertion: %v", executableErr)
    }
    expectedBinary, _ := filepath.Abs(executable)

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    expectedFragment := expectedBinary + " backup:run"
    if false == strings.Contains(content, expectedFragment) {
        t.Fatalf("expected binary to fall back to os.Executable() %q in:\n%s", expectedFragment, content)
    }
}

func TestRunSucceedsWithoutBinaryWhenAllSchedulesHaveCommandOverride(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("wrapped:job", &testSchedule{
            Minute:  "0",
            Command: []string{"/usr/bin/flock", "-n", "/tmp/lock", "/opt/app", "wrapped"},
        }),
    }

    _, err := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--user", "deploy",
        },
        newStubConfiguration(nil),
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error when every schedule sets Command: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    if true == strings.Contains(content, "/proc/self/exe") {
        t.Fatalf("expected no fallback binary token when all schedules set Command, got:\n%s", content)
    }
}

func TestRunHeartbeatCommandFlagOverridesHeartbeatPath(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    heartbeatPath := filepath.Join(tempDir, "heartbeat.crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--heartbeat-path", heartbeatPath,
            "--heartbeat-command", "/usr/bin/curl",
            "--heartbeat-command", "-fsS",
            "--heartbeat-command", "https://example.com/ping",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    expectedLine := "* * * * * deploy /usr/bin/curl -fsS https://example.com/ping"
    if false == strings.Contains(content, expectedLine) {
        t.Fatalf("expected --heartbeat-command line in:\n%s", content)
    }

    if true == strings.Contains(content, "/bin/touch") {
        t.Fatalf("heartbeat-command must override heartbeat-path; /bin/touch must not appear in:\n%s", content)
    }
}

func TestRunHeartbeatDestinationDefaultRestrictsToDefault(t *testing.T) {
    tempDir := t.TempDir()
    defaultOutputPath := filepath.Join(tempDir, "crontab")
    heartbeatPath := filepath.Join(tempDir, "heartbeat.crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("default:job", &testSchedule{Minute: "0", Hour: "1"}),
        newFakeCommandWithSchedule("billing:job", &testSchedule{
            Minute:          "0",
            Hour:            "2",
            DestinationFile: "billing-crontab",
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", defaultOutputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--heartbeat-path", heartbeatPath,
            "--heartbeat-destination", "default",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    expectedHeartbeat := "* * * * * deploy /bin/touch " + heartbeatPath

    defaultBody, _ := os.ReadFile(defaultOutputPath)
    if false == strings.Contains(string(defaultBody), expectedHeartbeat) {
        t.Fatalf("expected heartbeat in default crontab, got:\n%s", string(defaultBody))
    }

    billingBody, _ := os.ReadFile(filepath.Join(tempDir, "billing-crontab"))
    if true == strings.Contains(string(billingBody), expectedHeartbeat) {
        t.Fatalf("did not expect heartbeat in billing crontab when --heartbeat-destination=default, got:\n%s", string(billingBody))
    }
}

func TestRunHeartbeatDestinationRelativePath(t *testing.T) {
    tempDir := t.TempDir()
    defaultOutputPath := filepath.Join(tempDir, "crontab")
    heartbeatPath := filepath.Join(tempDir, "heartbeat.crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("default:job", &testSchedule{Minute: "0", Hour: "1"}),
        newFakeCommandWithSchedule("billing:job", &testSchedule{
            Minute:          "0",
            Hour:            "2",
            DestinationFile: "billing-crontab",
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", defaultOutputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--heartbeat-path", heartbeatPath,
            "--heartbeat-destination", "billing-crontab",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    expectedHeartbeat := "* * * * * deploy /bin/touch " + heartbeatPath

    defaultBody, _ := os.ReadFile(defaultOutputPath)
    if true == strings.Contains(string(defaultBody), expectedHeartbeat) {
        t.Fatalf("did not expect heartbeat in default crontab, got:\n%s", string(defaultBody))
    }

    billingBody, _ := os.ReadFile(filepath.Join(tempDir, "billing-crontab"))
    if false == strings.Contains(string(billingBody), expectedHeartbeat) {
        t.Fatalf("expected heartbeat in billing crontab, got:\n%s", string(billingBody))
    }
}

func TestRunHeartbeatDestinationErrorsOnUnmatched(t *testing.T) {
    tempDir := t.TempDir()
    defaultOutputPath := filepath.Join(tempDir, "crontab")
    heartbeatPath := filepath.Join(tempDir, "heartbeat.crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("default:job", &testSchedule{Minute: "0", Hour: "1"}),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", defaultOutputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--heartbeat-path", heartbeatPath,
            "--heartbeat-destination", "missing-crontab",
        },
    )
    if nil == err {
        t.Fatalf("expected error for unmatched --heartbeat-destination value")
    }

    if false == strings.Contains(err.Error(), "missing-crontab") {
        t.Fatalf("expected error to mention the offending value, got: %v", err)
    }
}

func TestRunRejectsLogFileNameEscapingLogsDir(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("escaping:log", &testSchedule{
            Minute:      "0",
            LogFileName: "../escape.log",
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil == err {
        t.Fatalf("expected error when Schedule.LogFileName escapes logs dir")
    }

    if false == strings.Contains(err.Error(), "escapes") {
        t.Fatalf("expected error to mention path escape, got: %v", err)
    }
}

func TestRunRejectsDestinationFileEscapingDefaultDir(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("escaping:job", &testSchedule{
            Minute:          "0",
            DestinationFile: "../escape-crontab",
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil == err {
        t.Fatalf("expected error when relative DestinationFile escapes the default dir")
    }

    if false == strings.Contains(err.Error(), "escapes") {
        t.Fatalf("expected error to mention path escape, got: %v", err)
    }
}

func TestRunInstancesWithLogDisabledEmitsNoLogPaths(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("quiet:multi", &testSchedule{
            Minute:      "0",
            Hour:        "2",
            Instances:   3,
            LogDisabled: true,
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    if true == strings.Contains(content, " >> ") {
        t.Fatalf("expected no log redirection for any instance when LogDisabled=true, got:\n%s", content)
    }

    for index := 1; index <= 3; index++ {
        expectedLine := fmt.Sprintf("0 2 * * * deploy /usr/local/bin/fakeapp quiet:multi --max-instances=3 --instance-index=%d", index)
        if false == strings.Contains(content, expectedLine) {
            t.Fatalf("expected instance %d line %q in:\n%s", index, expectedLine, content)
        }
    }
}

func TestRunInstancesPreservesCompositeLogExtension(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{
            Minute:      "0",
            Hour:        "2",
            Instances:   2,
            LogFileName: "archive.tar.gz",
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    for index := 1; index <= 2; index++ {
        expectedLog := filepath.Join(tempDir, "logs", fmt.Sprintf("archive-%d.tar.gz", index))
        if false == strings.Contains(content, expectedLog) {
            t.Fatalf("expected composite-extension log %q in:\n%s", expectedLog, content)
        }
    }
}

func TestRunInstancesPreservesRawLogFileName(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("hotel:channel-manager:sync", &testSchedule{
            Minute:         "0",
            Hour:           "2",
            Instances:      2,
            LogFileNameRaw: true,
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    for index := 1; index <= 2; index++ {
        expectedLog := filepath.Join(tempDir, "logs", fmt.Sprintf("hotel:channel-manager:sync-%d.log", index))
        if false == strings.Contains(content, expectedLog) {
            t.Fatalf("expected raw multi-instance log %q in:\n%s", expectedLog, content)
        }
    }
}

func TestRunAtomicWriteLeavesNoTempFileOnSuccess(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    entries, readErr := os.ReadDir(tempDir)
    if nil != readErr {
        t.Fatalf("could not read temp dir: %v", readErr)
    }

    for _, entry := range entries {
        if true == strings.HasSuffix(entry.Name(), ".tmp") {
            t.Fatalf("expected no leftover .tmp file after successful write; found %q", entry.Name())
        }
    }

    if _, statErr := os.Stat(outputPath); nil != statErr {
        t.Fatalf("expected destination file at %s, got: %v", outputPath, statErr)
    }
}

func TestRunAtomicWriteHandlesConcurrentRuns(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    logsDir := filepath.Join(tempDir, "logs")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    const runs = 5
    errCh := make(chan error, runs)

    for runIndex := 0; runIndex < runs; runIndex++ {
        go func() {
            _, runErr := runGenerateCommand(
                t,
                commands,
                []string{
                    "--out", outputPath,
                    "--logs-dir", logsDir,
                    "--binary", "/usr/local/bin/fakeapp",
                    "--user", "deploy",
                },
            )
            errCh <- runErr
        }()
    }

    for runIndex := 0; runIndex < runs; runIndex++ {
        if runErr := <-errCh; nil != runErr {
            t.Fatalf("concurrent Run returned unexpected error: %v", runErr)
        }
    }

    if _, statErr := os.Stat(outputPath); nil != statErr {
        t.Fatalf("expected destination file at %s after concurrent runs, got: %v", outputPath, statErr)
    }

    entries, readErr := os.ReadDir(tempDir)
    if nil != readErr {
        t.Fatalf("could not read temp dir: %v", readErr)
    }

    for _, entry := range entries {
        if true == strings.HasSuffix(entry.Name(), ".tmp") {
            t.Fatalf("expected no leftover .tmp file after concurrent runs; found %q", entry.Name())
        }
    }

    finalContent, readContentErr := os.ReadFile(outputPath)
    if nil != readContentErr {
        t.Fatalf("could not read destination file: %v", readContentErr)
    }
    if false == strings.Contains(string(finalContent), "backup:run") {
        t.Fatalf("expected destination to contain backup:run entry, got:\n%s", finalContent)
    }
}

func TestRunHeartbeatOnlyMessageInStdout(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    heartbeatPath := filepath.Join(tempDir, "heartbeat.crontab")

    stdout, err := runGenerateCommand(
        t,
        nil,
        []string{
            "--out", outputPath,
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--heartbeat-path", heartbeatPath,
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    if false == strings.Contains(stdout, "heartbeat-only crontab") {
        t.Fatalf("expected heartbeat-only message in stdout, got: %q", stdout)
    }
}

type fakeTemplate struct {
    name    string
    content string
    called  *int
}

func (instance *fakeTemplate) Name() string {
    return instance.name
}

func (instance *fakeTemplate) Render(entries []Entry, options RenderOptions) (string, error) {
    if nil != instance.called {
        *instance.called++
    }
    return instance.content, nil
}

func TestRegisterTemplateAddsToRegistry(t *testing.T) {
    command := NewGenerateCommand(nil)

    callCount := 0
    command.RegisterTemplate(&fakeTemplate{name: "fake", content: "fake-output\n", called: &callCount})

    resolved, lookupErr := command.resolveTemplate("fake")
    if nil != lookupErr {
        t.Fatalf("expected to resolve fake template, got: %v", lookupErr)
    }
    if "fake" != resolved.Name() {
        t.Fatalf("resolved template name = %q, want %q", resolved.Name(), "fake")
    }
}

func TestResolveTemplateErrorsOnUnknownName(t *testing.T) {
    command := NewGenerateCommand(nil)

    _, lookupErr := command.resolveTemplate("nope")
    if nil == lookupErr {
        t.Fatalf("expected error for unknown template, got nil")
    }
    if false == strings.Contains(lookupErr.Error(), "nope") || false == strings.Contains(lookupErr.Error(), "crontab") {
        t.Fatalf("expected error to mention the unknown name and registered list, got: %v", lookupErr)
    }
}

func TestDefaultTemplateRegisteredOnConstruction(t *testing.T) {
    command := NewGenerateCommand(nil)

    resolved, lookupErr := command.resolveTemplate(TemplateNameCrontab)
    if nil != lookupErr {
        t.Fatalf("expected crontab template to be registered by default, got: %v", lookupErr)
    }
    if _, ok := resolved.(*CrontabTemplate); false == ok {
        t.Fatalf("default template = %T, want *CrontabTemplate", resolved)
    }
}

func TestRunDispatchesToTemplateFromFlag(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "out")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("billing:run", &testSchedule{Minute: "0"}),
    }

    _, runErr := runGenerateCommandWithRegistrar(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--template", "json-cron",
        },
        func(command *GenerateCommand) {
            command.RegisterTemplate(&fakeTemplate{name: "json-cron", content: "{\"jobs\":[]}\n"})
        },
    )
    if nil != runErr {
        t.Fatalf("Run returned unexpected error: %v", runErr)
    }

    body, readErr := os.ReadFile(outputPath)
    if nil != readErr {
        t.Fatalf("failed to read output: %v", readErr)
    }
    if "{\"jobs\":[]}\n" != string(body) {
        t.Fatalf("expected fake template output, got %q", string(body))
    }
}

func TestRunErrorsWhenTemplateNameUnknown(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "out")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("billing:run", &testSchedule{Minute: "0"}),
    }

    _, runErr := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--template", "k8s_cronjob",
        },
    )
    if nil == runErr {
        t.Fatalf("expected error for unknown template name, got nil")
    }
    if false == strings.Contains(runErr.Error(), "k8s_cronjob") {
        t.Fatalf("expected error to mention requested template name, got: %v", runErr)
    }
}

func TestRunInstancesZeroIsNormalizedToOne(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{
            Minute:    "0",
            Hour:      "2",
            Instances: 0,
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    if true == strings.Contains(content, "--max-instances") {
        t.Fatalf("Instances=0 must be normalized to 1 (no --max-instances flag), got:\n%s", content)
    }

    expectedLine := "0 2 * * * deploy /usr/local/bin/fakeapp backup:run >> '" + filepath.Join(tempDir, "logs", "backup-run.log") + "' 2>&1"
    if false == strings.Contains(content, expectedLine) {
        t.Fatalf("expected single normalized entry %q in:\n%s", expectedLine, content)
    }
}

func TestRunInstancesNegativeIsNormalizedToOne(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{
            Minute:    "0",
            Hour:      "2",
            Instances: -3,
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    if true == strings.Contains(content, "--max-instances") {
        t.Fatalf("negative Instances must be normalized to 1, got:\n%s", content)
    }

    if 1 != strings.Count(content, "backup:run") {
        t.Fatalf("expected exactly one entry for negative Instances, got:\n%s", content)
    }
}

func TestRunHeartbeatDestinationAbsolutePath(t *testing.T) {
    tempDir := t.TempDir()
    defaultOutputPath := filepath.Join(tempDir, "crontab")
    heartbeatPath := filepath.Join(tempDir, "heartbeat.crontab")
    absoluteDestination := filepath.Join(tempDir, "absolute-crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("default:job", &testSchedule{Minute: "0", Hour: "1"}),
        newFakeCommandWithSchedule("absolute:job", &testSchedule{
            Minute:          "30",
            DestinationFile: absoluteDestination,
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", defaultOutputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--heartbeat-path", heartbeatPath,
            "--heartbeat-destination", absoluteDestination,
        },
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    expectedHeartbeat := "* * * * * deploy /bin/touch " + heartbeatPath

    absoluteBody, absoluteErr := os.ReadFile(absoluteDestination)
    if nil != absoluteErr {
        t.Fatalf("expected absolute destination at %s, got: %v", absoluteDestination, absoluteErr)
    }
    if false == strings.Contains(string(absoluteBody), expectedHeartbeat) {
        t.Fatalf("expected heartbeat in absolute destination, got:\n%s", string(absoluteBody))
    }

    defaultBody, _ := os.ReadFile(defaultOutputPath)
    if true == strings.Contains(string(defaultBody), expectedHeartbeat) {
        t.Fatalf("did not expect heartbeat in default crontab when --heartbeat-destination is an absolute path; got:\n%s", string(defaultBody))
    }
}

func TestRunErrorsWhenHeartbeatEnabledWithoutUser(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    heartbeatPath := filepath.Join(tempDir, "heartbeat")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--heartbeat-path", heartbeatPath,
        },
    )
    if nil == err {
        t.Fatalf("expected pre-flight error when heartbeat is enabled but no user is configured, got nil")
    }

    if false == strings.Contains(err.Error(), "heartbeat") || false == strings.Contains(err.Error(), "user") {
        t.Fatalf("expected error to mention heartbeat and user, got: %v", err)
    }
}

func TestRunCascadesParameterWhenOutFlagIsEmpty(t *testing.T) {
    tempDir := t.TempDir()
    parameterOutputPath := filepath.Join(tempDir, "from-parameter-crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    configuration := newStubConfiguration(map[string]string{
        ParameterDestinationFile: parameterOutputPath,
        ParameterLogsDir:         filepath.Join(tempDir, "param-logs"),
        ParameterUser:            "deploy",
    })

    _, err := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{
            "--out", "",
            "--binary", "/usr/local/bin/fakeapp",
        },
        configuration,
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error when --out is empty and parameter is set: %v", err)
    }

    if _, statErr := os.Stat(parameterOutputPath); nil != statErr {
        t.Fatalf("expected crontab at parameter destination %s, got: %v", parameterOutputPath, statErr)
    }
}

func TestAtomicWriteFileRollsBackTemporaryOnRenameFailure(t *testing.T) {
    tempDir := t.TempDir()
    destination := filepath.Join(tempDir, "blocking")

    if mkdirErr := os.Mkdir(destination, 0o755); nil != mkdirErr {
        t.Fatalf("setup: could not create blocking directory: %v", mkdirErr)
    }

    writeErr := atomicWriteFile(destination, []byte("payload"), 0o644)
    if nil == writeErr {
        t.Fatalf("expected atomicWriteFile to fail when destination is an existing directory; got nil")
    }

    entries, readErr := os.ReadDir(tempDir)
    if nil != readErr {
        t.Fatalf("could not read temp dir: %v", readErr)
    }

    for _, entry := range entries {
        if "blocking" == entry.Name() {
            continue
        }
        t.Fatalf("expected no temporary residue after rollback; found %q", entry.Name())
    }
}
