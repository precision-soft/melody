package cron

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "syscall"
    "testing"
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    melodyconfig "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/runtime"
)

func TestNewGenerateCommandIdentity(t *testing.T) {
    command := NewGenerateCommand(nil)

    if "melody:cron:generate" != command.Name() {
        t.Fatalf("Name() = %q, want %q", command.Name(), "melody:cron:generate")
    }

    if "" == command.Description() {
        t.Fatalf("Description() should not be empty")
    }

    /* the command carries its 12 own flags plus the standard set every melody command accepts — without the standard set, the framework's -v/-vv rewrite into --verbosity killed exactly this command with "flag provided but not defined" */
    flags := command.Flags()
    expectedFlagCount := 12 + len(output.StandardFlags())
    if expectedFlagCount != len(flags) {
        t.Fatalf("expected %d flags (12 own + the standard set), got %d", expectedFlagCount, len(flags))
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

func TestRunCreatesLogsDirWhenItDoesNotExist(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    logsDir := filepath.Join(tempDir, "var", "log", "cron")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    _, err := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", logsDir,
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "apache",
        },
        newStubConfiguration(nil),
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    info, statErr := os.Stat(logsDir)
    if nil != statErr {
        t.Fatalf("expected logs dir %q to exist after generate, got stat error: %v", logsDir, statErr)
    }
    if false == info.IsDir() {
        t.Fatalf("expected %q to be a directory", logsDir)
    }
}

func TestRunErrorsWhenLogsDirCannotBeCreated(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    blockingFile := filepath.Join(tempDir, "blocking")
    if writeErr := os.WriteFile(blockingFile, []byte("placeholder"), 0o644); nil != writeErr {
        t.Fatalf("setup: could not create blocking file: %v", writeErr)
    }
    logsDir := filepath.Join(blockingFile, "logs")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    _, err := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", logsDir,
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "apache",
        },
        newStubConfiguration(nil),
    )
    if nil == err {
        t.Fatalf("expected error when logs dir cannot be created, got nil")
    }
    if false == strings.Contains(err.Error(), "could not create the logs directory") {
        t.Fatalf("expected error to mention logs directory creation failure, got: %v", err)
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

func TestRunAutoDerivesHeartbeatFromLogsDirWhenOptInEnabled(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    logsDir := filepath.Join(tempDir, "logs")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    configuration := newStubConfiguration(map[string]string{
        ParameterHeartbeatAutoEnabled: "true",
    })

    _, err := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", logsDir,
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "apache",
        },
        configuration,
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    expectedHeartbeat := "* * * * * apache /bin/touch " + filepath.Join(logsDir, "heartbeat.crontab")
    if false == strings.Contains(content, expectedHeartbeat) {
        t.Fatalf("expected auto-derived heartbeat line %q in:\n%s", expectedHeartbeat, content)
    }
}

func TestRunDoesNotAutoDeriveHeartbeatWhenOptInDisabled(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    logsDir := filepath.Join(tempDir, "logs")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    configuration := newStubConfiguration(nil)

    _, err := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", logsDir,
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "apache",
        },
        configuration,
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    if true == strings.Contains(content, "/bin/touch ") {
        t.Fatalf("expected no heartbeat line when ParameterHeartbeatAutoEnabled is not set, got:\n%s", content)
    }
}

func TestRunPrefersExplicitHeartbeatPathOverAutoDerive(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    logsDir := filepath.Join(tempDir, "logs")
    explicitHeartbeatPath := filepath.Join(tempDir, "custom-heartbeat.crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    configuration := newStubConfiguration(map[string]string{
        ParameterHeartbeatAutoEnabled: "true",
        ParameterHeartbeatPath:        explicitHeartbeatPath,
    })

    _, err := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", logsDir,
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "apache",
        },
        configuration,
    )
    if nil != err {
        t.Fatalf("Run returned unexpected error: %v", err)
    }

    body, _ := os.ReadFile(outputPath)
    content := string(body)

    expectedHeartbeat := "* * * * * apache /bin/touch " + explicitHeartbeatPath
    if false == strings.Contains(content, expectedHeartbeat) {
        t.Fatalf("expected explicit heartbeat line %q to win over auto-derive in:\n%s", expectedHeartbeat, content)
    }

    autoDerivedPath := filepath.Join(logsDir, "heartbeat.crontab")
    if true == strings.Contains(content, "/bin/touch "+autoDerivedPath) {
        t.Fatalf("expected auto-derived heartbeat line at %q NOT to appear when explicit is set, got:\n%s", autoDerivedPath, content)
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

func TestRunRendersK8sTemplateWithImageAndNamespaceFlags(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "cron.yaml")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("outbox:dispatch", &testSchedule{Minute: "*/5"}),
    }

    _, runErr := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--template", "k8s",
            "--image", "registry/curatorium:latest",
            "--namespace", "curatorium",
            "--restart-policy", "Never",
        },
    )
    if nil != runErr {
        t.Fatalf("Run returned unexpected error: %v", runErr)
    }

    body, readErr := os.ReadFile(outputPath)
    if nil != readErr {
        t.Fatalf("failed to read output: %v", readErr)
    }

    content := string(body)
    expectedFragments := []string{
        "kind: CronJob",
        "name: \"outbox-dispatch\"",
        "namespace: \"curatorium\"",
        "schedule: \"*/5 * * * *\"",
        "image: \"registry/curatorium:latest\"",
        "restartPolicy: \"Never\"",
        "- \"outbox:dispatch\"",
    }
    for _, fragment := range expectedFragments {
        if false == strings.Contains(content, fragment) {
            t.Fatalf("expected rendered k8s manifest to contain %q, got:\n%s", fragment, content)
        }
    }
}

func TestRunK8sTemplateRejectsResourceNameCollisionAcrossDestinationFiles(t *testing.T) {
    /* the namespace is one global flag, so two commands that sanitize to the same resource name collide on kubectl apply even when split across distinct destination files; Render only sees one destination, so the CLI must reject the clash across the whole set */
    tempDir := t.TempDir()
    defaultOutputPath := filepath.Join(tempDir, "cron.yaml")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("outbox:dispatch", &testSchedule{Minute: "0"}),
        newFakeCommandWithSchedule("outbox-dispatch", &testSchedule{
            Minute:          "0",
            DestinationFile: "other.yaml",
        }),
    }

    _, runErr := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", defaultOutputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--template", "k8s",
            "--image", "registry/curatorium:latest",
        },
    )
    if nil == runErr {
        t.Fatalf("expected an error when two commands map to the same k8s resource name across destination files, got nil")
    }

    if false == errors.Is(runErr, ErrK8sDuplicateName) {
        t.Fatalf("expected ErrK8sDuplicateName, got: %v", runErr)
    }

    if false == strings.Contains(runErr.Error(), "outbox-dispatch") {
        t.Fatalf("expected the error to name the colliding resource, got: %v", runErr)
    }
}

func TestRunK8sTemplateWarnsWhenHeartbeatConfigured(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "cron.yaml")
    heartbeatPath := filepath.Join(tempDir, "heartbeat.crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("outbox:dispatch", &testSchedule{Minute: "*/5"}),
    }

    stdout, runErr := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--template", "k8s",
            "--image", "registry/curatorium:latest",
            "--heartbeat-path", heartbeatPath,
        },
    )
    if nil != runErr {
        t.Fatalf("Run returned unexpected error: %v", runErr)
    }

    if false == strings.Contains(stdout, "heartbeat options are set but the k8s template ignores them") {
        t.Fatalf("expected a heartbeat-ignored warning for the k8s template, got: %q", stdout)
    }
}

func TestRunK8sTemplateIgnoresUnmatchedHeartbeatDestination(t *testing.T) {
    /* the k8s template declares heartbeat options ignored (it never emits a heartbeat CronJob), so a --heartbeat-destination that matches no written destination must not hard-fail the command the way it does for the crontab template */
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "cron.yaml")
    heartbeatPath := filepath.Join(tempDir, "heartbeat.crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("outbox:dispatch", &testSchedule{Minute: "*/5"}),
    }

    stdout, runErr := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--template", "k8s",
            "--image", "registry/curatorium:latest",
            "--heartbeat-path", heartbeatPath,
            "--heartbeat-destination", "missing-crontab",
        },
    )
    if nil != runErr {
        t.Fatalf("k8s template must not fail on an unmatched --heartbeat-destination it declares ignored, got: %v", runErr)
    }

    if false == strings.Contains(stdout, "heartbeat options are set but the k8s template ignores them") {
        t.Fatalf("expected the heartbeat-ignored warning for the k8s template, got: %q", stdout)
    }

    body, readErr := os.ReadFile(outputPath)
    if nil != readErr {
        t.Fatalf("expected the k8s manifest to be written, got: %v", readErr)
    }
    if false == strings.Contains(string(body), "kind: CronJob") {
        t.Fatalf("expected a k8s CronJob manifest, got:\n%s", string(body))
    }
}

func TestRunK8sTemplateWithHeartbeatAndEmptyConfigurationWritesNothing(t *testing.T) {
    /* heartbeat is crontab-only; with the k8s template an empty Configuration must not synthesize a heartbeat-only destination, so no file is written and the missing image is never demanded */
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "cron.yaml")
    heartbeatPath := filepath.Join(tempDir, "heartbeat.crontab")

    commands := []clicontract.Command{
        newFakePlainCommand("debug:router"),
    }

    stdout, runErr := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--template", "k8s",
            "--heartbeat-path", heartbeatPath,
        },
    )
    if nil != runErr {
        t.Fatalf("Run returned unexpected error: %v", runErr)
    }

    if _, statErr := os.Stat(outputPath); false == os.IsNotExist(statErr) {
        t.Fatalf("expected no file at %s for k8s template with an empty configuration; statErr=%v", outputPath, statErr)
    }

    if true == strings.Contains(stdout, "crontab") {
        t.Fatalf("k8s template output must not mention crontab, got: %q", stdout)
    }

    if false == strings.Contains(stdout, "heartbeat options are set but the k8s template ignores them") {
        t.Fatalf("expected the heartbeat-ignored warning for the k8s template, got: %q", stdout)
    }

    if false == strings.Contains(stdout, "nothing to write") {
        t.Fatalf("expected a 'nothing to write' message for the k8s template with an empty configuration, got: %q", stdout)
    }
}

func TestRunCrontabTemplateDoesNotWarnAboutHeartbeat(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    heartbeatPath := filepath.Join(tempDir, "heartbeat.crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("outbox:dispatch", &testSchedule{Minute: "*/5"}),
    }

    stdout, runErr := runGenerateCommand(
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
    if nil != runErr {
        t.Fatalf("Run returned unexpected error: %v", runErr)
    }

    if true == strings.Contains(stdout, "the k8s template ignores them") {
        t.Fatalf("crontab template must not emit the k8s heartbeat warning, got: %q", stdout)
    }
}

func TestRunK8sTemplateErrorsWhenImageMissing(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "cron.yaml")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("outbox:dispatch", &testSchedule{Minute: "*/5"}),
    }

    _, runErr := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--template", "k8s",
        },
    )
    if nil == runErr {
        t.Fatalf("expected error when k8s template runs without an image, got nil")
    }
    if false == strings.Contains(runErr.Error(), "image") {
        t.Fatalf("expected error to mention the missing image, got: %v", runErr)
    }
}

func TestRunK8sTemplateSuffixesNamesForMultiInstanceCommand(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "cron.yaml")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("outbox:dispatch", &testSchedule{Minute: "*/5", Instances: 2}),
    }

    _, runErr := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--template", "k8s",
            "--image", "registry/curatorium:latest",
        },
    )
    if nil != runErr {
        t.Fatalf("Run returned unexpected error for a multi-instance command: %v", runErr)
    }

    body, readErr := os.ReadFile(outputPath)
    if nil != readErr {
        t.Fatalf("failed to read output: %v", readErr)
    }

    content := string(body)
    for _, fragment := range []string{"name: \"outbox-dispatch-1\"", "name: \"outbox-dispatch-2\""} {
        if false == strings.Contains(content, fragment) {
            t.Fatalf("expected per-instance suffixed CronJob name %q, got:\n%s", fragment, content)
        }
    }

    if 2 != strings.Count(content, "kind: CronJob") {
        t.Fatalf("expected two CronJob documents for a two-instance command, got:\n%s", content)
    }
}

func TestRunK8sTemplateWithHeartbeatAndNoUserSucceedsAndWarns(t *testing.T) {
    /* the k8s template ignores the heartbeat, so a heartbeat-configured k8s run with no --user must not fail with ErrHeartbeatUserMissing (a crontab-only requirement); it succeeds and warns that the heartbeat is dropped */
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "cron.yaml")
    heartbeatPath := filepath.Join(tempDir, "heartbeat.crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("outbox:dispatch", &testSchedule{Minute: "*/5"}),
    }

    stdout, runErr := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--template", "k8s",
            "--image", "registry/curatorium:latest",
            "--heartbeat-path", heartbeatPath,
        },
    )
    if nil != runErr {
        t.Fatalf("Run returned unexpected error for a heartbeat-configured k8s run without a user: %v", runErr)
    }

    if true == errors.Is(runErr, ErrHeartbeatUserMissing) {
        t.Fatalf("k8s ignores the heartbeat; it must not demand a user, got: %v", runErr)
    }

    if false == strings.Contains(stdout, "heartbeat options are set but the k8s template ignores them") {
        t.Fatalf("expected the heartbeat-ignored warning for the k8s template, got: %q", stdout)
    }
}

func TestRunK8sTemplateSucceedsWithoutLogsDir(t *testing.T) {
    /* the k8s template logs to container stdout and never reads Entry.LogPath, so a k8s run must not inherit the crontab-only logs-dir requirement (ErrNoLogsDir) */
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "cron.yaml")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("outbox:dispatch", &testSchedule{Minute: "*/5"}),
    }

    _, runErr := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--template", "k8s",
            "--image", "registry/curatorium:latest",
        },
    )
    if nil != runErr {
        t.Fatalf("Run returned unexpected error for a k8s run without --logs-dir: %v", runErr)
    }

    if true == errors.Is(runErr, ErrNoLogsDir) {
        t.Fatalf("k8s does not redirect to log files; it must not demand a logs dir, got: %v", runErr)
    }

    body, readErr := os.ReadFile(outputPath)
    if nil != readErr {
        t.Fatalf("failed to read output: %v", readErr)
    }

    if false == strings.Contains(string(body), "kind: CronJob") {
        t.Fatalf("expected a CronJob manifest, got:\n%s", string(body))
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

/* The crontab-no-user dialect renders no user column at all, so the heartbeat line needs no user to place. Demanding --user turned a valid busybox-crond configuration into a hard error. */
func TestRunCrontabNoUserTemplateWithHeartbeatAndNoUserSucceeds(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    heartbeatPath := filepath.Join(tempDir, "heartbeat.crontab")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("outbox:dispatch", &testSchedule{Minute: "*/5"}),
    }

    _, runErr := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--template", "crontab-no-user",
            "--heartbeat-path", heartbeatPath,
        },
    )

    if true == errors.Is(runErr, ErrHeartbeatUserMissing) {
        t.Fatalf("the crontab-no-user dialect has no user column; it must not demand a user")
    }
    if nil != runErr {
        t.Fatalf("Run returned unexpected error: %v", runErr)
    }
}

func TestRunPruneEmptiesADestinationThisRunNoLongerProduces(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    retiredPath := filepath.Join(tempDir, "reports.crontab")

    baseArgs := []string{
        "--out", outputPath,
        "--logs-dir", filepath.Join(tempDir, "logs"),
        "--binary", "/usr/local/bin/fakeapp",
        "--user", "deploy",
    }

    _, firstErr := runGenerateCommand(
        t,
        []clicontract.Command{
            newFakeCommandWithConfig("reports:daily", &EntryConfig{
                Schedule:        &Schedule{Minute: "0", Hour: "3"},
                DestinationFile: "reports.crontab",
            }),
        },
        baseArgs,
    )
    if nil != firstErr {
        t.Fatalf("the first generation failed: %v", firstErr)
    }

    firstContent, firstReadErr := os.ReadFile(retiredPath)
    if nil != firstReadErr {
        t.Fatalf("the first generation wrote no %s: %v", retiredPath, firstReadErr)
    }

    if false == strings.Contains(string(firstContent), "reports:daily") {
        t.Fatalf("the first generation did not name the job: %s", firstContent)
    }

    /* the next version moves the same job to the default destination */
    stdout, secondErr := runGenerateCommand(
        t,
        []clicontract.Command{
            newFakeCommandWithConfig("reports:daily", &EntryConfig{
                Schedule: &Schedule{Minute: "0", Hour: "3"},
            }),
        },
        append(append([]string{}, baseArgs...), "--prune"),
    )
    if nil != secondErr {
        t.Fatalf("the second generation failed: %v", secondErr)
    }

    stale, staleErr := os.ReadFile(retiredPath)
    if nil != staleErr {
        t.Fatalf("expected the pruned destination to survive as an empty manifest: %v", staleErr)
    }

    if true == strings.Contains(string(stale), "reports:daily") {
        t.Fatalf("expected the retired destination to stop naming the job, got: %s", stale)
    }

    if false == strings.Contains(string(stale), CrontabOwnershipMarker) {
        t.Fatalf("expected the pruned destination to keep its ownership marker, got: %s", stale)
    }

    current, currentErr := os.ReadFile(outputPath)
    if nil != currentErr {
        t.Fatalf("the second generation wrote no %s: %v", outputPath, currentErr)
    }

    if false == strings.Contains(string(current), "reports:daily") {
        t.Fatalf("expected the job to live at its new destination, got: %s", current)
    }

    if false == strings.Contains(stdout, "pruned "+retiredPath) {
        t.Fatalf("expected the sweep to be reported, got: %q", stdout)
    }
}

func TestRunPruneIsReportedInTheJsonEnvelope(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    retiredPath := filepath.Join(tempDir, "reports.crontab")

    baseArgs := []string{
        "--out", outputPath,
        "--logs-dir", filepath.Join(tempDir, "logs"),
        "--binary", "/usr/local/bin/fakeapp",
        "--user", "deploy",
    }

    if _, firstErr := runGenerateCommand(
        t,
        []clicontract.Command{
            newFakeCommandWithConfig("reports:daily", &EntryConfig{
                Schedule:        &Schedule{Minute: "0", Hour: "3"},
                DestinationFile: "reports.crontab",
            }),
        },
        baseArgs,
    ); nil != firstErr {
        t.Fatalf("the first generation failed: %v", firstErr)
    }

    stdout, secondErr := runGenerateCommand(
        t,
        []clicontract.Command{
            newFakeCommandWithConfig("reports:daily", &EntryConfig{
                Schedule: &Schedule{Minute: "0", Hour: "3"},
            }),
        },
        append(append([]string{}, baseArgs...), "--prune", "--format=json"),
    )
    if nil != secondErr {
        t.Fatalf("the second generation failed: %v", secondErr)
    }

    document := map[string]any{}
    if unmarshalErr := json.Unmarshal([]byte(stdout), &document); nil != unmarshalErr {
        t.Fatalf("the envelope did not parse: %v, got %q", unmarshalErr, stdout)
    }

    data, hasData := document["data"].(map[string]any)
    if false == hasData {
        t.Fatalf("expected a data object, got %q", stdout)
    }

    pruned, hasPruned := data["pruned"].([]any)
    if false == hasPruned {
        t.Fatalf("expected pruned to be a list on every run, got %q", stdout)
    }

    if 1 != len(pruned) || retiredPath != pruned[0] {
        t.Fatalf("expected the swept destination to be named, got %v", pruned)
    }
}

func TestRunPruneLeavesAFileItCannotProveItWrote(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    foreignPath := filepath.Join(tempDir, "operator.crontab")

    foreignContent := "# written by the operator\n*/5 * * * * root /usr/local/bin/backup\n"
    if writeErr := os.WriteFile(foreignPath, []byte(foreignContent), 0o644); nil != writeErr {
        t.Fatalf("unexpected write error: %v", writeErr)
    }

    if _, runErr := runGenerateCommand(
        t,
        []clicontract.Command{
            newFakeCommandWithConfig("reports:daily", &EntryConfig{
                Schedule: &Schedule{Minute: "0", Hour: "3"},
            }),
        },
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--prune",
        },
    ); nil != runErr {
        t.Fatalf("the generation failed: %v", runErr)
    }

    survived, readErr := os.ReadFile(foreignPath)
    if nil != readErr {
        t.Fatalf("expected the operator's file to survive: %v", readErr)
    }

    if foreignContent != string(survived) {
        t.Fatalf("expected the operator's file untouched, got: %s", survived)
    }
}

func TestRunPruneSweepsWhenTheConfigurationBecomesEmpty(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    baseArgs := []string{
        "--out", outputPath,
        "--logs-dir", filepath.Join(tempDir, "logs"),
        "--binary", "/usr/local/bin/fakeapp",
        "--user", "deploy",
    }

    if _, firstErr := runGenerateCommand(
        t,
        []clicontract.Command{
            newFakeCommandWithConfig("reports:daily", &EntryConfig{
                Schedule: &Schedule{Minute: "0", Hour: "3"},
            }),
        },
        baseArgs,
    ); nil != firstErr {
        t.Fatalf("the first generation failed: %v", firstErr)
    }

    stdout, secondErr := runGenerateCommand(
        t,
        nil,
        append(append([]string{}, baseArgs...), "--prune"),
    )
    if nil != secondErr {
        t.Fatalf("an empty configuration must stay a success, got: %v", secondErr)
    }

    if false == strings.Contains(stdout, "nothing to write") {
        t.Fatalf("expected the empty run to keep its message, got: %q", stdout)
    }

    swept, sweptErr := os.ReadFile(outputPath)
    if nil != sweptErr {
        t.Fatalf("expected the previously written destination to survive as an empty manifest: %v", sweptErr)
    }

    if true == strings.Contains(string(swept), "reports:daily") {
        t.Fatalf("expected the emptied configuration to stop the job, got: %s", swept)
    }
}

func TestRunReportsAnEmptyPrunedListWhenNothingIsSwept(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    stdout, runErr := runGenerateCommand(
        t,
        []clicontract.Command{
            newFakeCommandWithConfig("reports:daily", &EntryConfig{
                Schedule: &Schedule{Minute: "0", Hour: "3"},
            }),
        },
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--format=json",
        },
    )
    if nil != runErr {
        t.Fatalf("the generation failed: %v", runErr)
    }

    document := map[string]any{}
    if unmarshalErr := json.Unmarshal([]byte(stdout), &document); nil != unmarshalErr {
        t.Fatalf("the envelope did not parse: %v, got %q", unmarshalErr, stdout)
    }

    data := document["data"].(map[string]any)

    pruned, hasPruned := data["pruned"].([]any)
    if false == hasPruned {
        t.Fatalf("expected pruned to be a list even when nothing was swept, got %q", stdout)
    }

    if 0 != len(pruned) {
        t.Fatalf("expected an empty sweep, got %v", pruned)
    }
}

func TestRunWithoutPruneLeavesAStaleDestinationUntouched(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    retiredPath := filepath.Join(tempDir, "reports.crontab")

    baseArgs := []string{
        "--out", outputPath,
        "--logs-dir", filepath.Join(tempDir, "logs"),
        "--binary", "/usr/local/bin/fakeapp",
        "--user", "deploy",
    }

    if _, firstErr := runGenerateCommand(
        t,
        []clicontract.Command{
            newFakeCommandWithConfig("reports:daily", &EntryConfig{
                Schedule:        &Schedule{Minute: "0", Hour: "3"},
                DestinationFile: "reports.crontab",
            }),
        },
        baseArgs,
    ); nil != firstErr {
        t.Fatalf("the first generation failed: %v", firstErr)
    }

    if _, secondErr := runGenerateCommand(
        t,
        []clicontract.Command{
            newFakeCommandWithConfig("reports:daily", &EntryConfig{
                Schedule: &Schedule{Minute: "0", Hour: "3"},
            }),
        },
        baseArgs,
    ); nil != secondErr {
        t.Fatalf("the second generation failed: %v", secondErr)
    }

    stale, staleErr := os.ReadFile(retiredPath)
    if nil != staleErr {
        t.Fatalf("expected the stale destination to be left alone: %v", staleErr)
    }

    if false == strings.Contains(string(stale), "reports:daily") {
        t.Fatalf("expected the stale destination untouched without --prune, got: %s", stale)
    }
}

func TestGenerateCommand_TheSweepDoesNotOpenANamedPipe(t *testing.T) {
    directory := t.TempDir()

    if fifoErr := syscall.Mkfifo(filepath.Join(directory, "a-fifo.crontab"), 0o644); nil != fifoErr {
        t.Skipf("this platform has no fifo: %v", fifoErr)
    }

    completed := make(chan error, 1)
    go func() {
        _, runErr := runGenerateCommand(
            t,
            []clicontract.Command{newFakeCommandWithSchedule("job:probe", &testSchedule{Minute: "0", Hour: "*"})},
            []string{
                "--out", filepath.Join(directory, "current.crontab"),
                "--logs-dir", directory,
                "--binary", "/usr/bin/app",
                "--user", "root",
                "--prune",
            },
        )
        completed <- runErr
    }()

    select {
    case runErr := <-completed:
        if nil != runErr {
            t.Fatalf("the fifo must be skipped, not fail the run: %v", runErr)
        }
    case <-time.After(10 * time.Second):
        t.Fatal("the generator is wedged opening the fifo")
    }
}

func TestGenerateCommand_JsonFormatRendersOneDocument(t *testing.T) {
    tempDir := t.TempDir()

    stdout, runErr := runGenerateCommand(
        t,
        []clicontract.Command{newFakeCommandWithSchedule("report:daily", &testSchedule{Minute: "0"})},
        []string{
            "--out", filepath.Join(tempDir, "crontab"),
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--format=json",
        },
    )
    if nil != runErr {
        t.Fatalf("run: %v", runErr)
    }

    var document map[string]any
    if unmarshalErr := json.Unmarshal([]byte(stdout), &document); nil != unmarshalErr {
        t.Fatalf("the output is not one json document: %v; got %q", unmarshalErr, stdout)
    }

    meta, hasMeta := document["meta"].(map[string]any)
    if false == hasMeta {
        t.Fatalf("the document carries no meta, got %q", stdout)
    }

    if "melody:cron:generate" != meta["command"] {
        t.Fatalf("meta.command = %v", meta["command"])
    }

    data, hasData := document["data"].(map[string]any)
    if false == hasData {
        t.Fatalf("the document carries no data, got %q", stdout)
    }

    writes, hasWrites := data["writes"].([]any)
    if false == hasWrites || 0 == len(writes) {
        t.Fatalf("expected the write summary inside the document, got %v", data)
    }
}

func TestGenerateCommand_JsonNamesTheDestinationsWrittenBeforeTheFailure(t *testing.T) {
    tempDir := t.TempDir()

    /* the second destination cannot be created: its parent is a file, so MkdirAll fails after the first has been written */
    blockingFile := filepath.Join(tempDir, "blocked")
    if writeErr := os.WriteFile(blockingFile, []byte("not a directory"), 0o644); nil != writeErr {
        t.Fatalf("failed to place the blocking file: %v", writeErr)
    }

    commands := []clicontract.Command{
        newFakeCommandWithConfig("a:first", &EntryConfig{
            Schedule:        &Schedule{Minute: "0"},
            DestinationFile: filepath.Join(tempDir, "a-crontab"),
        }),
        newFakeCommandWithConfig("b:second", &EntryConfig{
            Schedule:        &Schedule{Minute: "0"},
            DestinationFile: filepath.Join(blockingFile, "b-crontab"),
        }),
    }

    stdout, runErr := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", filepath.Join(tempDir, "crontab"),
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--format=json",
        },
    )

    if nil == runErr {
        t.Fatalf("expected the unwritable destination to fail the run, got %q", stdout)
    }

    document := struct {
        Data struct {
            Writes []struct {
                Destination string `json:"destination"`
            } `json:"writes"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(stdout), &document); nil != decodeErr {
        t.Fatalf("expected one json document on a partial run, got %q: %v", stdout, decodeErr)
    }

    if 1 != len(document.Data.Writes) {
        t.Fatalf("expected the destination written before the failure, got %#v in %q", document.Data.Writes, stdout)
    }

    if filepath.Join(tempDir, "a-crontab") != document.Data.Writes[0].Destination {
        t.Fatalf("expected the first destination to be named, got %q", document.Data.Writes[0].Destination)
    }
}

func TestGenerateCommand_JsonReportsTheFailureAndWhatWasAlreadyWritten(t *testing.T) {
    tempDir := t.TempDir()

    stdout, runErr := runGenerateCommand(
        t,
        []clicontract.Command{
            newFakeCommandWithSchedule("product:list", &testSchedule{Minute: "0", Hour: "3"}),
        },
        []string{
            "--out", filepath.Join(tempDir, "crontab"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--format=json",
        },
    )

    if nil == runErr {
        t.Fatalf("expected the missing logs-dir to fail the run, got %q", stdout)
    }

    document := struct {
        Data struct {
            Writes []map[string]any `json:"writes"`
        } `json:"data"`
        Error *struct {
            Code    string         `json:"code"`
            Message string         `json:"message"`
            Details map[string]any `json:"details"`
            Cause   *struct {
                Message string              `json:"message"`
                Details map[string][]string `json:"details"`
            } `json:"cause"`
        } `json:"error"`
    }{}
    if decodeErr := json.Unmarshal([]byte(stdout), &document); nil != decodeErr {
        t.Fatalf("expected one json document on a failed run, got %q: %v", stdout, decodeErr)
    }

    if nil == document.Error {
        t.Fatalf("expected the failure inside the envelope, got %q", stdout)
    }

    if "cron.generateFailed" != document.Error.Code {
        t.Fatalf("expected the generate-failed code, got %q", document.Error.Code)
    }

    if nil == document.Error.Cause {
        t.Fatalf("expected the failure to carry its cause, got %q", stdout)
    }

    if false == strings.Contains(document.Error.Cause.Message, "logs-dir") {
        t.Fatalf("expected the cause to name the missing logs-dir, got %q", document.Error.Cause.Message)
    }

    /* the document used to flatten every failure to that one sentence — details and cause.details were nil on every run alike — while the journal, over the same value at the same instant, carried the context and the whole chain under it */
    if nil == document.Error.Details {
        t.Fatalf("expected the failure details to be an object, got %q", stdout)
    }

    if 0 == len(document.Error.Cause.Details["chain"]) {
        t.Fatalf("expected the cause chain inside the failure, got %q", stdout)
    }

    if false == strings.Contains(document.Error.Cause.Details["chain"][0], "logs-dir") {
        t.Fatalf("expected the chain to start at the failure itself, got %v", document.Error.Cause.Details["chain"])
    }

    /* the writes key stays an array on a failed run: a consumer keying on it must not meet a null */
    if nil == document.Data.Writes {
        t.Fatalf("expected an empty writes array on a failed run, got %q", stdout)
    }

    /* the success path must stay exactly what it was, or the guard above would pass for a command that reports every run as failed */
    successStdout, successErr := runGenerateCommand(
        t,
        []clicontract.Command{
            newFakeCommandWithSchedule("product:list", &testSchedule{Minute: "0", Hour: "3"}),
        },
        []string{
            "--out", filepath.Join(tempDir, "crontab"),
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--format=json",
        },
    )
    if nil != successErr {
        t.Fatalf("expected the successful run to stay successful, got %v", successErr)
    }

    successDocument := struct {
        Error *struct {
            Code string `json:"code"`
        } `json:"error"`
    }{}
    if decodeErr := json.Unmarshal([]byte(successStdout), &successDocument); nil != decodeErr {
        t.Fatalf("failed to decode the document: %v, got %q", decodeErr, successStdout)
    }

    if nil != successDocument.Error {
        t.Fatalf("expected no error on a successful run, got %q", successStdout)
    }
}

func TestGenerateCommand_TheTextBranchNamesWhatItProducedBeforeFailing(t *testing.T) {
    var stdout bytes.Buffer
    commandContext := &clicontract.StaticContext{WriterValue: &stdout}

    runErr := NewGenerateCommand(NewConfiguration()).reportWrites(
        commandContext,
        output.Option{Format: output.FormatTable},
        time.Now(),
        []destinationWrite{{Destination: "/etc/cron.d/app", Entries: 3}},
        []string{"/etc/cron.d/app-retired", "/etc/cron.d/app-moved"},
        "",
        nil,
        errors.New("the sweep stopped part way through"),
    )

    if nil == runErr {
        t.Fatal("the run's own failure must stay the verdict")
    }

    rendered := stdout.String()

    for _, expected := range []string{
        "pruned /etc/cron.d/app-retired",
        "pruned /etc/cron.d/app-moved",
        "wrote 3 entries to /etc/cron.d/app",
    } {
        if false == strings.Contains(rendered, expected) {
            t.Fatalf("the failed run does not report %q, got: %q", expected, rendered)
        }
    }
}

func TestGenerateCommand_AcceptsTheStandardVerbosityFlag(t *testing.T) {
    tempDir := t.TempDir()

    _, runErr := runGenerateCommand(
        t,
        []clicontract.Command{newFakeCommandWithSchedule("report:daily", &testSchedule{Minute: "0"})},
        []string{
            "--out", filepath.Join(tempDir, "crontab"),
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--verbosity=1",
        },
    )
    if nil != runErr {
        t.Fatalf("expected the standard verbosity flag to be accepted, got %v", runErr)
    }
}

func TestErrorDetailsOf_CarriesTheFailuresOwnContext(t *testing.T) {
    runErr := exception.NewError(
        "the cron manifest could not be renamed into place",
        exceptioncontract.Context{"destination": "/etc/cron.d/app", "source": "/tmp/app.tmp"},
        errors.New("permission denied"),
    )

    details := errorDetailsOf(runErr)

    if "/etc/cron.d/app" != details["destination"] {
        t.Fatalf("expected the failure's own context in the details, got %#v", details)
    }

    if "/tmp/app.tmp" != details["source"] {
        t.Fatalf("expected the failure's own context in the details, got %#v", details)
    }
}

func TestErrorDetailsOf_KeepsAnObjectWithoutAContext(t *testing.T) {
    details := errorDetailsOf(errors.New("bare failure"))

    if nil == details {
        t.Fatal("expected an empty object rather than nil")
    }

    if 0 != len(details) {
        t.Fatalf("expected the details to be empty for a bare failure, got %#v", details)
    }
}

func TestErrorCauseOf_StartsAtTheFailureAndCarriesTheChain(t *testing.T) {
    runErr := exception.NewError(
        "the cron manifest could not be renamed into place",
        nil,
        errors.New("permission denied"),
    )

    cause := errorCauseOf(runErr)
    if nil == cause {
        t.Fatal("expected the failure to carry its cause")
    }

    if false == strings.Contains(cause.Message, "could not be renamed") {
        t.Fatalf("expected the cause to be the failure's own sentence, got %q", cause.Message)
    }

    chain, isChain := cause.Details["chain"].([]string)
    if false == isChain || 2 > len(chain) {
        t.Fatalf("expected the whole chain beneath the failure, got %#v", cause.Details)
    }

    if false == strings.Contains(strings.Join(chain, " | "), "permission denied") {
        t.Fatalf("expected the chain to reach the bottom, got %#v", chain)
    }
}

func TestErrorCauseOf_AnswersNothingForAFailureWithoutACause(t *testing.T) {
    if nil != errorCauseOf(nil) {
        t.Fatal("expected no cause for a nil failure")
    }
}

func TestRunAnchorsARelativeParameterPathAtTheProjectDirectory(t *testing.T) {
    projectDirectory := t.TempDir()
    workingDirectory := t.TempDir()

    t.Chdir(workingDirectory)

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    configuration := newStubConfigurationWithProjectDirectory(
        map[string]string{
            ParameterUser:            "deploy",
            ParameterBinary:          "/opt/melody/app",
            ParameterDestinationFile: filepath.Join("var", "cron", "crontab"),
            ParameterLogsDir:         filepath.Join("var", "log", "cron"),
        },
        projectDirectory,
    )

    _, runErr := runGenerateCommandWithConfiguration(t, commands, nil, configuration)
    if nil != runErr {
        t.Fatalf("Run returned unexpected error: %v", runErr)
    }

    anchoredDestination := filepath.Join(projectDirectory, "var", "cron", "crontab")
    if _, statErr := os.Stat(anchoredDestination); nil != statErr {
        t.Fatalf("expected the relative parameter path under the project directory: %v", statErr)
    }

    if _, statErr := os.Stat(filepath.Join(workingDirectory, "var", "cron", "crontab")); nil == statErr {
        t.Fatal("expected nothing written under the working directory")
    }

    body, _ := os.ReadFile(anchoredDestination)
    if false == strings.Contains(string(body), filepath.Join(projectDirectory, "var", "log", "cron")) {
        t.Fatalf("expected the logs directory anchored at the project directory too, got:\n%s", string(body))
    }
}

func TestRunKeepsARelativeFlagPathRelativeToTheWorkingDirectory(t *testing.T) {
    projectDirectory := t.TempDir()
    workingDirectory := t.TempDir()

    t.Chdir(workingDirectory)

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    configuration := newStubConfigurationWithProjectDirectory(
        map[string]string{
            ParameterUser:    "deploy",
            ParameterBinary:  "/opt/melody/app",
            ParameterLogsDir: filepath.Join(projectDirectory, "var", "log", "cron"),
        },
        projectDirectory,
    )

    _, runErr := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{"--out", filepath.Join("var", "cron", "crontab")},
        configuration,
    )
    if nil != runErr {
        t.Fatalf("Run returned unexpected error: %v", runErr)
    }

    if _, statErr := os.Stat(filepath.Join(workingDirectory, "var", "cron", "crontab")); nil != statErr {
        t.Fatalf("expected the flag path to follow the working directory: %v", statErr)
    }

    if _, statErr := os.Stat(filepath.Join(projectDirectory, "var", "cron", "crontab")); nil == statErr {
        t.Fatal("expected the flag path not to be anchored at the project directory")
    }
}

func TestRunCreatesTheLogSubdirectoryTheLogFileNameNames(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    logsDir := filepath.Join(tempDir, "logs")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("nightly:report", &testSchedule{
            Minute:      "0",
            LogFileName: "nightly/report.log",
        }),
    }

    _, err := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", logsDir,
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != err {
        t.Fatalf("expected the subdirectory log file name to generate, got: %v", err)
    }

    subdirectoryInfo, statErr := os.Stat(filepath.Join(logsDir, "nightly"))
    if nil != statErr {
        t.Fatalf("expected the log subdirectory to exist after generation: %v", statErr)
    }

    if false == subdirectoryInfo.IsDir() {
        t.Fatal("expected the created log path to be a directory")
    }
}

func TestRun_RefusesAMalformedHeartbeatOptIn(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")
    logsDir := filepath.Join(tempDir, "logs")

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    configuration := newStubConfiguration(map[string]string{
        ParameterHeartbeatAutoEnabled: "ture",
    })

    _, err := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", logsDir,
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "apache",
        },
        configuration,
    )
    if nil == err {
        t.Fatal("a heartbeat opt-in that does not hold a boolean must fail generation, not silently disable the heartbeat")
    }

    reported, isReported := err.(*exception.Error)
    if false == isReported {
        t.Fatalf("expected an exception error, got %T", err)
    }

    if ParameterHeartbeatAutoEnabled != reported.Context()["parameter"] {
        t.Fatalf("the refusal must name the parameter, got %v", reported.Context()["parameter"])
    }
}

func TestGenerateCommand_ConfigurationResolvesThroughTheRunScope(t *testing.T) {
    containerConfiguration := newGenerateTestConfiguration(t)
    scopeConfiguration := newGenerateTestConfiguration(t)

    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        melodyconfig.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return containerConfiguration, nil
        },
    )

    scope := serviceContainer.NewScope()
    defer func() {
        _ = scope.Close()
    }()
    scope.MustOverrideProtectedInstance(melodyconfig.ServiceConfig, scopeConfiguration)

    runtimeInstance := runtime.New(context.Background(), scope, serviceContainer)

    resolved, resolveErr := configurationFromRuntime(runtimeInstance)
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    if scopeConfiguration != resolved {
        t.Fatal("expected the generator to honour the run scope's configuration, not the container's")
    }
}

func TestGenerateCommand_EntryArgumentsReachTheGeneratedLine(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    commands := []clicontract.Command{
        newFakeCommandWithConfig("product:list", &EntryConfig{
            Schedule:  &Schedule{Minute: "0", Hour: "3"},
            Arguments: []string{"--format=json", "--quiet"},
        }),
    }

    stdout, runErr := runGenerateCommand(
        t,
        commands,
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
        },
    )
    if nil != runErr {
        t.Fatalf("Run returned unexpected error: %v\nstdout=%s", runErr, stdout)
    }

    body, readErr := os.ReadFile(outputPath)
    if nil != readErr {
        t.Fatalf("failed to read crontab output %s: %v", outputPath, readErr)
    }

    expectedLine := "0 3 * * * deploy /usr/local/bin/fakeapp product:list --format=json --quiet >> '" + filepath.Join(tempDir, "logs", "product-list.log") + "' 2>&1"
    if false == strings.Contains(string(body), expectedLine) {
        t.Fatalf("expected the entry arguments on the generated line %q in:\n%s", expectedLine, string(body))
    }
}

func TestExpandEntriesForCommand_EveryEntryCarriesItsOwnSchedule(t *testing.T) {
    schedule := &Schedule{Minute: "0", Hour: "3"}

    entries, expandErr := expandEntriesForCommand(
        "reports:daily",
        &EntryConfig{Schedule: schedule, Instances: 2},
        "/usr/local/bin/fakeapp",
        "deploy",
        t.TempDir(),
        true,
    )
    if nil != expandErr {
        t.Fatalf("unexpected expand error: %v", expandErr)
    }

    if 2 != len(entries) {
        t.Fatalf("expected two entries, got %d", len(entries))
    }

    if entries[0].Schedule == schedule || entries[1].Schedule == schedule {
        t.Fatal("expected the entries to carry copies rather than the configured schedule itself")
    }

    if entries[0].Schedule == entries[1].Schedule {
        t.Fatal("expected the two instances to carry distinct schedules")
    }

    entries[0].Schedule.Defaults()

    if "3" != schedule.Hour || "" != schedule.DayOfMonth {
        t.Fatalf("expected the configured schedule untouched by a template calling Defaults, got %#v", schedule)
    }

    if "3" != entries[1].Schedule.Hour || "" != entries[1].Schedule.DayOfMonth {
        t.Fatalf("expected the sibling entry untouched, got %#v", entries[1].Schedule)
    }
}

func TestGenerateCommand_ARegisteredNoUserDialectNeedsNoUserForTheHeartbeat(t *testing.T) {
    directory := t.TempDir()

    _, runErr := runGenerateCommandWithRegistrar(
        t,
        []clicontract.Command{newFakeCommandWithSchedule("job:probe", &testSchedule{Minute: "0", Hour: "*"})},
        []string{
            "--out", filepath.Join(directory, "crontab"),
            "--logs-dir", directory,
            "--binary", "/usr/bin/app",
            "--template", "kubernetes-probe",
            "--heartbeat-path", filepath.Join(directory, "heartbeat.crontab"),
        },
        func(command *GenerateCommand) {
            command.RegisterTemplate(&noUserColumnTemplate{})
        },
    )

    if nil != runErr {
        t.Fatalf("a dialect that renders no user column must not demand a user, got: %v", runErr)
    }
}

type generateTestEnvironmentSource struct {
    values map[string]string
}

func (instance *generateTestEnvironmentSource) Load() (map[string]string, error) {
    copied := make(map[string]string, len(instance.values))
    for key, value := range instance.values {
        copied[key] = value
    }

    return copied, nil
}

func newGenerateTestConfiguration(t *testing.T) configcontract.Configuration {
    t.Helper()

    environment, environmentErr := melodyconfig.NewEnvironment(
        &generateTestEnvironmentSource{
            values: map[string]string{
                melodyconfig.EnvKey: melodyconfig.EnvDevelopment,
            },
        },
    )
    if nil != environmentErr {
        t.Fatalf("environment: %v", environmentErr)
    }

    configuration, configurationErr := melodyconfig.NewConfiguration(environment, t.TempDir())
    if nil != configurationErr {
        t.Fatalf("configuration: %v", configurationErr)
    }

    return configuration
}

type noUserColumnTemplate struct{}

func (instance *noUserColumnTemplate) Name() string {
    return "kubernetes-probe"
}

func (instance *noUserColumnTemplate) Render(entries []Entry, options RenderOptions) (string, error) {
    lines := []string{instance.OwnershipMarker()}
    for _, entry := range entries {
        lines = append(lines, entry.Name+" "+strings.Join(entry.Command, " "))
    }

    return strings.Join(lines, "\n") + "\n", nil
}

func (instance *noUserColumnTemplate) OwnershipMarker() string {
    return "# melody-cron-probe"
}

func (instance *noUserColumnTemplate) RendersUserColumn() bool {
    return false
}

/* the frozen majors have no builtin k8s dialect, so neither pins this: on this major the k8s template is owned, its manifests open with the marker, and a manifest the configuration no longer produces is emptied by the same sweep that reconciles a crontab directory */
func TestRunPruneSweepsAK8sManifestDirectory(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "cronjobs.yaml")
    retiredPath := filepath.Join(tempDir, "reports.yaml")

    baseArgs := []string{
        "--out", outputPath,
        "--template", "k8s",
        "--image", "registry.example/app:1",
    }

    if _, firstErr := runGenerateCommand(
        t,
        []clicontract.Command{
            newFakeCommandWithConfig("reports:daily", &EntryConfig{
                Schedule:        &Schedule{Minute: "0", Hour: "3"},
                DestinationFile: "reports.yaml",
            }),
        },
        baseArgs,
    ); nil != firstErr {
        t.Fatalf("the first generation failed: %v", firstErr)
    }

    if _, secondErr := runGenerateCommand(
        t,
        []clicontract.Command{
            newFakeCommandWithConfig("reports:daily", &EntryConfig{
                Schedule: &Schedule{Minute: "0", Hour: "3"},
            }),
        },
        append(append([]string{}, baseArgs...), "--prune"),
    ); nil != secondErr {
        t.Fatalf("the second generation failed: %v", secondErr)
    }

    stale, staleErr := os.ReadFile(retiredPath)
    if nil != staleErr {
        t.Fatalf("expected the pruned manifest to survive as an empty document: %v", staleErr)
    }

    if true == strings.Contains(string(stale), "reports-daily") {
        t.Fatalf("expected the retired manifest to stop naming the CronJob, got: %s", stale)
    }

    if false == strings.Contains(string(stale), CrontabOwnershipMarker) {
        t.Fatalf("expected the pruned manifest to keep its ownership marker, got: %s", stale)
    }

    current, currentErr := os.ReadFile(outputPath)
    if nil != currentErr {
        t.Fatalf("the second generation wrote no %s: %v", outputPath, currentErr)
    }

    if false == strings.Contains(string(current), "reports-daily") {
        t.Fatalf("expected the CronJob to live at its new destination, got: %s", current)
    }
}

/* the dropped-heartbeat warning must reach the machine document too: in json mode the cli prints nothing else, so a warning that only ever went to the text branch would leave the pipeline reading a clean document about a setting the run just ignored */
func TestRunK8sHeartbeatWarningReachesTheJsonEnvelope(t *testing.T) {
    tempDir := t.TempDir()

    stdout, runErr := runGenerateCommand(
        t,
        []clicontract.Command{
            newFakeCommandWithConfig("reports:daily", &EntryConfig{
                Schedule: &Schedule{Minute: "0", Hour: "3"},
            }),
        },
        []string{
            "--out", filepath.Join(tempDir, "cronjobs.yaml"),
            "--template", "k8s",
            "--image", "registry.example/app:1",
            "--heartbeat-path", filepath.Join(tempDir, "heartbeat.crontab"),
            "--user", "deploy",
            "--format=json",
        },
    )
    if nil != runErr {
        t.Fatalf("expected the ignored heartbeat to warn rather than fail, got %v", runErr)
    }

    document := struct {
        Warnings []struct {
            Code string `json:"code"`
        } `json:"warnings"`
    }{}
    if decodeErr := json.Unmarshal([]byte(stdout), &document); nil != decodeErr {
        t.Fatalf("the envelope did not parse: %v, got %q", decodeErr, stdout)
    }

    found := false
    for _, warning := range document.Warnings {
        if "cron.heartbeatIgnored" == warning.Code {
            found = true
        }
    }

    if false == found {
        t.Fatalf("expected the cron.heartbeatIgnored warning inside the envelope, got %q", stdout)
    }
}

/* the malformed opt-in fails under every template — only the derived path is the k8s template's to skip. A typo hidden by the template choice would resurface as a missing liveness line the day the deployment switches back to crontab, with nothing ever having said the parameter was broken. */
func TestRun_RefusesAMalformedHeartbeatOptInUnderK8s(t *testing.T) {
    tempDir := t.TempDir()

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    configuration := newStubConfiguration(map[string]string{
        ParameterHeartbeatAutoEnabled: "ture",
    })

    _, err := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{
            "--out", filepath.Join(tempDir, "cronjobs.yaml"),
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--template", "k8s",
            "--image", "registry.example/app:1",
        },
        configuration,
    )
    if nil == err {
        t.Fatal("a malformed heartbeat opt-in must fail generation under the k8s template too")
    }

    reported, isReported := err.(*exception.Error)
    if false == isReported {
        t.Fatalf("expected an exception error, got %T", err)
    }

    if ParameterHeartbeatAutoEnabled != reported.Context()["parameter"] {
        t.Fatalf("the refusal must name the parameter, got %v", reported.Context()["parameter"])
    }
}

/* the k8s template ignores the heartbeat, so a well-formed opt-in derives no path for it: deriving one would only arm the dropped-heartbeat warning on a setting nobody made for this dialect */
func TestRunK8sTemplateDoesNotAutoDeriveAHeartbeat(t *testing.T) {
    tempDir := t.TempDir()

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"}),
    }

    configuration := newStubConfiguration(map[string]string{
        ParameterHeartbeatAutoEnabled: "true",
    })

    stdout, runErr := runGenerateCommandWithConfiguration(
        t,
        commands,
        []string{
            "--out", filepath.Join(tempDir, "cronjobs.yaml"),
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--template", "k8s",
            "--image", "registry.example/app:1",
        },
        configuration,
    )
    if nil != runErr {
        t.Fatalf("Run returned unexpected error: %v", runErr)
    }

    if true == strings.Contains(stdout, "the k8s template ignores them") {
        t.Fatalf("expected no heartbeat to be derived for the k8s template, got: %q", stdout)
    }
}

/* the emptying is irreversible, so ownership must not be a substring question: a file that QUOTES the marker inside a longer line, or past the leading lines, is not one this generator wrote — and a custom dialect that suffixes the builtin marker (the ansible example) declares files of its own, which a substring match claimed for the builtin run */
func TestFileCarriesOwnershipMarker_MatchesOnlyAnExactLeadingLine(t *testing.T) {
    tempDir := t.TempDir()

    owned := filepath.Join(tempDir, "owned")
    if writeErr := os.WriteFile(owned, []byte("# GENERATED FILE\n# DO NOT EDIT LOCALLY\n"+CrontabOwnershipMarker+"\n0 3 * * * job\n"), 0o644); nil != writeErr {
        t.Fatalf("write owned: %v", writeErr)
    }

    quoting := filepath.Join(tempDir, "quoting")
    if writeErr := os.WriteFile(quoting, []byte("# deployment notes\n# files carrying `"+CrontabOwnershipMarker+"` are managed\n"), 0o644); nil != writeErr {
        t.Fatalf("write quoting: %v", writeErr)
    }

    late := filepath.Join(tempDir, "late")
    lateContent := strings.Repeat("# filler line\n", ownershipMarkerLineLimit) + CrontabOwnershipMarker + "\n"
    if writeErr := os.WriteFile(late, []byte(lateContent), 0o644); nil != writeErr {
        t.Fatalf("write late: %v", writeErr)
    }

    suffixed := filepath.Join(tempDir, "suffixed")
    if writeErr := os.WriteFile(suffixed, []byte(CrontabOwnershipMarker+" (ansible-cron)\n---\n"), 0o644); nil != writeErr {
        t.Fatalf("write suffixed: %v", writeErr)
    }

    if carries, checkErr := fileCarriesOwnershipMarker(owned, CrontabOwnershipMarker); nil != checkErr || false == carries {
        t.Fatalf("expected the generated header to prove ownership, got carries=%v err=%v", carries, checkErr)
    }

    if carries, checkErr := fileCarriesOwnershipMarker(quoting, CrontabOwnershipMarker); nil != checkErr || true == carries {
        t.Fatalf("expected the quoting file to stay the operator's, got carries=%v err=%v", carries, checkErr)
    }

    if carries, checkErr := fileCarriesOwnershipMarker(late, CrontabOwnershipMarker); nil != checkErr || true == carries {
        t.Fatalf("expected a marker past the leading lines to prove nothing, got carries=%v err=%v", carries, checkErr)
    }

    if carries, checkErr := fileCarriesOwnershipMarker(suffixed, CrontabOwnershipMarker); nil != checkErr || true == carries {
        t.Fatalf("expected the suffixed custom marker to stay the custom dialect's, got carries=%v err=%v", carries, checkErr)
    }

    if carries, checkErr := fileCarriesOwnershipMarker(suffixed, CrontabOwnershipMarker+" (ansible-cron)"); nil != checkErr || false == carries {
        t.Fatalf("expected the custom marker to prove its own files, got carries=%v err=%v", carries, checkErr)
    }
}

func TestGenerateCommand_PruneLeavesABystanderFileThatQuotesTheMarker(t *testing.T) {
    tempDir := t.TempDir()
    outputPath := filepath.Join(tempDir, "crontab")

    bystander := filepath.Join(tempDir, "README")
    bystanderContent := "# deployment notes\n# files carrying `" + CrontabOwnershipMarker + "` are managed by the generator\n"
    if writeErr := os.WriteFile(bystander, []byte(bystanderContent), 0o644); nil != writeErr {
        t.Fatalf("write bystander: %v", writeErr)
    }

    _, runErr := runGenerateCommand(
        t,
        []clicontract.Command{
            newFakeCommandWithConfig("reports:daily", &EntryConfig{
                Schedule: &Schedule{Minute: "0", Hour: "3"},
            }),
        },
        []string{
            "--out", outputPath,
            "--logs-dir", filepath.Join(tempDir, "logs"),
            "--binary", "/usr/local/bin/fakeapp",
            "--user", "deploy",
            "--prune",
        },
    )
    if nil != runErr {
        t.Fatalf("generation failed: %v", runErr)
    }

    surviving, readErr := os.ReadFile(bystander)
    if nil != readErr {
        t.Fatalf("read bystander: %v", readErr)
    }

    if bystanderContent != string(surviving) {
        t.Fatalf("expected the quoting bystander to survive the sweep byte-identical, got: %s", surviving)
    }
}
