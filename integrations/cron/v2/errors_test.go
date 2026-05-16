package cron

import (
    "errors"
    "path/filepath"
    "testing"

    clicontract "github.com/precision-soft/melody/v2/cli/contract"
)

func TestErrorsIsErrNoOutputPath(t *testing.T) {
    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0"}),
    }

    _, err := runGenerateCommand(t, commands, []string{})
    if nil == err {
        t.Fatalf("expected error when no output path is configured, got nil")
    }

    if false == errors.Is(err, ErrNoOutputPath) {
        t.Fatalf("expected errors.Is(err, ErrNoOutputPath) to be true, got: %v", err)
    }
}

func TestErrorsIsErrTemplateNotFound(t *testing.T) {
    tempDir := t.TempDir()

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0"}),
    }

    _, err := runGenerateCommand(t, commands, []string{
        "--out", filepath.Join(tempDir, "crontab"),
        "--logs-dir", filepath.Join(tempDir, "logs"),
        "--binary", "/usr/local/bin/fakeapp",
        "--user", "deploy",
        "--template", "missing-template",
    })
    if nil == err {
        t.Fatalf("expected error when template is unknown, got nil")
    }

    if false == errors.Is(err, ErrTemplateNotFound) {
        t.Fatalf("expected errors.Is(err, ErrTemplateNotFound) to be true, got: %v", err)
    }
}

func TestErrorsIsErrHeartbeatUserMissingFromPreflight(t *testing.T) {
    tempDir := t.TempDir()

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0"}),
    }

    _, err := runGenerateCommand(t, commands, []string{
        "--out", filepath.Join(tempDir, "crontab"),
        "--logs-dir", filepath.Join(tempDir, "logs"),
        "--binary", "/usr/local/bin/fakeapp",
        "--heartbeat-path", filepath.Join(tempDir, "heartbeat"),
    })
    if nil == err {
        t.Fatalf("expected error when heartbeat is enabled without user, got nil")
    }

    if false == errors.Is(err, ErrHeartbeatUserMissing) {
        t.Fatalf("expected errors.Is(err, ErrHeartbeatUserMissing) to be true, got: %v", err)
    }
}

func TestErrorsIsErrHeartbeatUserMissingFromRender(t *testing.T) {
    _, err := Render(nil, RenderOptions{
        HeartbeatCommand: []string{"/bin/echo", "alive"},
    })
    if nil == err {
        t.Fatalf("expected error when heartbeat command has no user, got nil")
    }

    if false == errors.Is(err, ErrHeartbeatUserMissing) {
        t.Fatalf("expected errors.Is(err, ErrHeartbeatUserMissing) to be true, got: %v", err)
    }
}

func TestErrorsIsErrEntryEmptyUser(t *testing.T) {
    entries := []Entry{
        {
            Name:     "no-user",
            Binary:   "/usr/local/bin/app",
            Args:     []string{"no-user"},
            Schedule: &Schedule{Minute: "0"},
        },
    }

    _, err := Render(entries, RenderOptions{})
    if nil == err {
        t.Fatalf("expected error when entry user is empty, got nil")
    }

    if false == errors.Is(err, ErrEntryEmptyUser) {
        t.Fatalf("expected errors.Is(err, ErrEntryEmptyUser) to be true, got: %v", err)
    }
}

func TestErrorsIsErrEntryEmptyCommandFromMissingBinary(t *testing.T) {
    entries := []Entry{
        {
            Name:     "no-command",
            User:     "www-data",
            Schedule: &Schedule{Minute: "0"},
        },
    }

    _, err := Render(entries, RenderOptions{})
    if nil == err {
        t.Fatalf("expected error when entry has no binary and no command, got nil")
    }

    if false == errors.Is(err, ErrEntryEmptyCommand) {
        t.Fatalf("expected errors.Is(err, ErrEntryEmptyCommand) to be true, got: %v", err)
    }
}

func TestErrorsIsErrEntryEmptyCommandFromEmptyTokens(t *testing.T) {
    entries := []Entry{
        {
            Name:     "empty-tokens",
            User:     "www-data",
            Schedule: &Schedule{Minute: "0"},
            Command:  []string{"", ""},
        },
    }

    _, err := Render(entries, RenderOptions{})
    if nil == err {
        t.Fatalf("expected error when Command tokens are all empty, got nil")
    }

    if false == errors.Is(err, ErrEntryEmptyCommand) {
        t.Fatalf("expected errors.Is(err, ErrEntryEmptyCommand) to be true, got: %v", err)
    }
}

func TestErrorsIsErrForbiddenCharacter(t *testing.T) {
    err := ValidateNoForbiddenChars([]string{"safe", "bad\nnewline"}, CrontabForbiddenChars, "test")
    if nil == err {
        t.Fatalf("expected error when token contains forbidden character, got nil")
    }

    if false == errors.Is(err, ErrForbiddenCharacter) {
        t.Fatalf("expected errors.Is(err, ErrForbiddenCharacter) to be true, got: %v", err)
    }
}

func TestErrorsIsErrFieldContainsWhitespaceFromUser(t *testing.T) {
    err := validateUserField("entry user", "bad user")
    if nil == err {
        t.Fatalf("expected error when user contains whitespace, got nil")
    }

    if false == errors.Is(err, ErrFieldContainsWhitespace) {
        t.Fatalf("expected errors.Is(err, ErrFieldContainsWhitespace) to be true, got: %v", err)
    }
}

func TestErrorsIsErrFieldContainsWhitespaceFromSchedule(t *testing.T) {
    entry := Entry{
        Name:     "bad-schedule",
        Schedule: &Schedule{Minute: "0 5"},
    }

    err := validateScheduleFields(entry)
    if nil == err {
        t.Fatalf("expected error when schedule field contains whitespace, got nil")
    }

    if false == errors.Is(err, ErrFieldContainsWhitespace) {
        t.Fatalf("expected errors.Is(err, ErrFieldContainsWhitespace) to be true, got: %v", err)
    }
}

func TestErrorsIsErrDestinationEscapeFromLogFileName(t *testing.T) {
    tempDir := t.TempDir()

    commands := []clicontract.Command{
        newFakeCommandWithConfig("escape:log", &EntryConfig{
            Schedule:    &Schedule{Minute: "0"},
            LogFileName: "../escape.log",
        }),
    }

    _, err := runGenerateCommand(t, commands, []string{
        "--out", filepath.Join(tempDir, "crontab"),
        "--logs-dir", filepath.Join(tempDir, "logs"),
        "--binary", "/usr/local/bin/fakeapp",
        "--user", "deploy",
    })
    if nil == err {
        t.Fatalf("expected error when LogFileName escapes logs dir, got nil")
    }

    if false == errors.Is(err, ErrDestinationEscape) {
        t.Fatalf("expected errors.Is(err, ErrDestinationEscape) to be true, got: %v", err)
    }
}

func TestErrorsIsErrDestinationEscapeFromDestinationFile(t *testing.T) {
    tempDir := t.TempDir()

    commands := []clicontract.Command{
        newFakeCommandWithConfig("escape:dest", &EntryConfig{
            Schedule:        &Schedule{Minute: "0"},
            DestinationFile: "../escape-crontab",
        }),
    }

    _, err := runGenerateCommand(t, commands, []string{
        "--out", filepath.Join(tempDir, "crontab"),
        "--logs-dir", filepath.Join(tempDir, "logs"),
        "--binary", "/usr/local/bin/fakeapp",
        "--user", "deploy",
    })
    if nil == err {
        t.Fatalf("expected error when DestinationFile escapes default dir, got nil")
    }

    if false == errors.Is(err, ErrDestinationEscape) {
        t.Fatalf("expected errors.Is(err, ErrDestinationEscape) to be true, got: %v", err)
    }
}

func TestErrorsIsErrNoLogsDir(t *testing.T) {
    tempDir := t.TempDir()

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("needs:logs", &testSchedule{Minute: "0"}),
    }

    _, err := runGenerateCommand(t, commands, []string{
        "--out", filepath.Join(tempDir, "crontab"),
        "--binary", "/usr/local/bin/fakeapp",
        "--user", "deploy",
    })
    if nil == err {
        t.Fatalf("expected error when logs-dir is missing and command needs logging, got nil")
    }

    if false == errors.Is(err, ErrNoLogsDir) {
        t.Fatalf("expected errors.Is(err, ErrNoLogsDir) to be true, got: %v", err)
    }
}

func TestErrorsIsErrHeartbeatDestinationUnmatched(t *testing.T) {
    tempDir := t.TempDir()

    commands := []clicontract.Command{
        newFakeCommandWithSchedule("default:job", &testSchedule{Minute: "0"}),
    }

    _, err := runGenerateCommand(t, commands, []string{
        "--out", filepath.Join(tempDir, "crontab"),
        "--logs-dir", filepath.Join(tempDir, "logs"),
        "--binary", "/usr/local/bin/fakeapp",
        "--user", "deploy",
        "--heartbeat-path", filepath.Join(tempDir, "heartbeat"),
        "--heartbeat-destination", "missing-crontab",
    })
    if nil == err {
        t.Fatalf("expected error when --heartbeat-destination does not match any destination, got nil")
    }

    if false == errors.Is(err, ErrHeartbeatDestinationUnmatched) {
        t.Fatalf("expected errors.Is(err, ErrHeartbeatDestinationUnmatched) to be true, got: %v", err)
    }
}
