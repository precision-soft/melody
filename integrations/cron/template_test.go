package cron

import (
    "strings"
    "testing"
)

func TestRenderProducesHeaderAndEntries(t *testing.T) {
    entries := []Entry{
        {
            Name:   "first",
            User:   "www-data",
            Binary: "/usr/local/bin/app",
            Args:   []string{"first"},
            Schedule: &Schedule{
                Minute: "0",
                Hour:   "*",
            },
            LogPath: "/var/log/app/first.log",
        },
    }

    content, err := Render(entries, RenderOptions{})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    if false == strings.Contains(content, "# GENERATED FILE") {
        t.Fatalf("expected GENERATED FILE marker, got:\n%s", content)
    }

    if false == strings.Contains(content, "# DO NOT EDIT LOCALLY") {
        t.Fatalf("expected do-not-edit warning, got:\n%s", content)
    }

    expectedLine := "0 * * * * www-data /usr/local/bin/app first >> '/var/log/app/first.log' 2>&1"
    if false == strings.Contains(content, expectedLine) {
        t.Fatalf("expected line %q in:\n%s", expectedLine, content)
    }
}

func TestRenderFillsScheduleWildcardsForEmptyFields(t *testing.T) {
    entries := []Entry{
        {
            Name:   "everything-wildcards",
            User:   "www-data",
            Binary: "/usr/local/bin/app",
            Args:   []string{"everything-wildcards"},
            Schedule: &Schedule{
                Minute: "*/5",
            },
            LogPath: "/var/log/app.log",
        },
    }

    content, err := Render(entries, RenderOptions{})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    expectedLine := "*/5 * * * * www-data /usr/local/bin/app everything-wildcards >> '/var/log/app.log' 2>&1"
    if false == strings.Contains(content, expectedLine) {
        t.Fatalf("expected wildcards line %q, got:\n%s", expectedLine, content)
    }
}

func TestRenderOmitsLogRedirectionWhenLogPathEmpty(t *testing.T) {
    entries := []Entry{
        {
            Name:   "no-log",
            User:   "www-data",
            Binary: "/usr/local/bin/app",
            Args:   []string{"no-log"},
            Schedule: &Schedule{
                Minute: "*",
            },
        },
    }

    content, err := Render(entries, RenderOptions{})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    if true == strings.Contains(content, " >> ") {
        t.Fatalf("expected no log redirection, got:\n%s", content)
    }

    if true == strings.Contains(content, "2>&1") {
        t.Fatalf("expected no stderr redirection, got:\n%s", content)
    }
}

func TestRenderReturnsErrorWhenEntryUserEmpty(t *testing.T) {
    entries := []Entry{
        {
            Name:   "missing-user",
            Binary: "/bin/foo",
            Args:   []string{"missing-user"},
            Schedule: &Schedule{
                Minute: "0",
            },
            LogPath: "/tmp/x.log",
        },
    }

    _, err := Render(entries, RenderOptions{})
    if nil == err {
        t.Fatalf("expected error when entry user is empty, got nil")
    }

    if false == strings.Contains(err.Error(), "missing-user") {
        t.Fatalf("expected error to mention command name, got: %v", err)
    }
}

func TestRenderReturnsErrorWhenEntryBinaryEmpty(t *testing.T) {
    entries := []Entry{
        {
            Name: "missing-binary",
            User: "www-data",
            Args: []string{"missing-binary"},
            Schedule: &Schedule{
                Minute: "0",
            },
        },
    }

    _, err := Render(entries, RenderOptions{})
    if nil == err {
        t.Fatalf("expected error when entry binary is empty, got nil")
    }

    if false == strings.Contains(err.Error(), "missing-binary") {
        t.Fatalf("expected error to mention command name, got: %v", err)
    }
}

func TestRenderUsesCustomUserWhenProvided(t *testing.T) {
    entries := []Entry{
        {
            Name:   "custom",
            User:   "ec2-user",
            Binary: "/bin/foo",
            Args:   []string{"custom"},
            Schedule: &Schedule{
                Minute: "0",
            },
            LogPath: "/tmp/x.log",
        },
    }

    content, err := Render(entries, RenderOptions{})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    if false == strings.Contains(content, " ec2-user /bin/foo ") {
        t.Fatalf("expected ec2-user user in line, got:\n%s", content)
    }
}

func TestRenderEmitsHeartbeatWhenConfigured(t *testing.T) {
    content, err := Render(nil, RenderOptions{
        HeartbeatUser: "www-data",
        HeartbeatPath: "/var/log/cron/heartbeat.crontab",
    })
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    if false == strings.Contains(content, "* * * * * www-data /bin/touch /var/log/cron/heartbeat.crontab") {
        t.Fatalf("expected heartbeat line, got:\n%s", content)
    }
}

func TestRenderReturnsErrorWhenHeartbeatUserEmpty(t *testing.T) {
    _, err := Render(nil, RenderOptions{
        HeartbeatPath: "/var/log/cron/heartbeat.crontab",
    })

    if nil == err {
        t.Fatalf("expected error when heartbeat path is set without a user, got nil")
    }

    if false == strings.Contains(err.Error(), "heartbeat user") {
        t.Fatalf("expected error to mention heartbeat user, got: %v", err)
    }
}

func TestRenderOmitsHeartbeatWhenPathEmpty(t *testing.T) {
    entries := []Entry{
        {
            Name:   "any",
            User:   "www-data",
            Binary: "/bin/foo",
            Args:   []string{"any"},
            Schedule: &Schedule{
                Minute: "0",
            },
            LogPath: "/tmp/x.log",
        },
    }

    content, err := Render(entries, RenderOptions{})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    if true == strings.Contains(content, "/bin/touch") {
        t.Fatalf("expected no heartbeat line, got:\n%s", content)
    }
}

func TestRenderHandlesMultipleArgsCorrectly(t *testing.T) {
    entries := []Entry{
        {
            Name:   "multi-arg",
            User:   "www-data",
            Binary: "/bin/echo",
            Args:   []string{"foo", "bar", "baz"},
            Schedule: &Schedule{
                Minute: "0",
            },
            LogPath: "/tmp/x.log",
        },
    }

    content, err := Render(entries, RenderOptions{})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    expectedLine := "0 * * * * www-data /bin/echo foo bar baz >> '/tmp/x.log' 2>&1"
    if false == strings.Contains(content, expectedLine) {
        t.Fatalf("expected line %q, got:\n%s", expectedLine, content)
    }
}

func TestRenderEndsWithFooter(t *testing.T) {
    content, err := Render(nil, RenderOptions{})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    if false == strings.HasSuffix(content, "#############################################################################\n") {
        t.Fatalf("expected trailing footer block, got:\n%s", content)
    }
}

func TestRenderUsesScheduleCommandWhenProvided(t *testing.T) {
    entries := []Entry{
        {
            Name: "wrapped",
            User: "www-data",
            Schedule: &Schedule{
                Minute: "0",
                Hour:   "5",
            },
            Command: []string{"/usr/bin/flock", "-n", "/tmp/lock", "/opt/melody/app", "wrapped"},
            LogPath: "/var/log/wrapped.log",
        },
    }

    content, err := Render(entries, RenderOptions{})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    expectedLine := "0 5 * * * www-data /usr/bin/flock -n /tmp/lock /opt/melody/app wrapped >> '/var/log/wrapped.log' 2>&1"
    if false == strings.Contains(content, expectedLine) {
        t.Fatalf("expected custom command line %q in:\n%s", expectedLine, content)
    }
}

func TestRenderReturnsErrorWhenEntryHasNeitherCommandNorBinary(t *testing.T) {
    entries := []Entry{
        {
            Name:     "no-command",
            User:     "www-data",
            Schedule: &Schedule{Minute: "0"},
        },
    }

    _, err := Render(entries, RenderOptions{})
    if nil == err {
        t.Fatalf("expected error when entry has neither Command nor Binary, got nil")
    }

    if false == strings.Contains(err.Error(), "no-command") {
        t.Fatalf("expected error to mention command name, got: %v", err)
    }
}

func TestRenderEmitsHeartbeatCommandWhenProvided(t *testing.T) {
    content, err := Render(nil, RenderOptions{
        HeartbeatUser:    "monitor",
        HeartbeatCommand: []string{"/usr/bin/curl", "-fsS", "https://heartbeat.example.com/ping"},
    })
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    expectedLine := "* * * * * monitor /usr/bin/curl -fsS https://heartbeat.example.com/ping"
    if false == strings.Contains(content, expectedLine) {
        t.Fatalf("expected custom heartbeat line %q in:\n%s", expectedLine, content)
    }

    if true == strings.Contains(content, "/bin/touch") {
        t.Fatalf("expected /bin/touch fallback to be skipped when HeartbeatCommand is set; got:\n%s", content)
    }
}

func TestRenderHeartbeatCommandTakesPrecedenceOverPath(t *testing.T) {
    content, err := Render(nil, RenderOptions{
        HeartbeatUser:    "monitor",
        HeartbeatPath:    "/var/log/heartbeat",
        HeartbeatCommand: []string{"/bin/echo", "alive"},
    })
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    if true == strings.Contains(content, "/bin/touch") {
        t.Fatalf("expected HeartbeatCommand to override HeartbeatPath; got:\n%s", content)
    }

    if false == strings.Contains(content, "* * * * * monitor /bin/echo alive") {
        t.Fatalf("expected HeartbeatCommand line in:\n%s", content)
    }
}

func TestRenderReturnsErrorWhenHeartbeatCommandHasNoUser(t *testing.T) {
    _, err := Render(nil, RenderOptions{
        HeartbeatCommand: []string{"/bin/echo", "alive"},
    })

    if nil == err {
        t.Fatalf("expected error when HeartbeatCommand set without HeartbeatUser, got nil")
    }

    if false == strings.Contains(err.Error(), "heartbeat user") {
        t.Fatalf("expected error to mention heartbeat user, got: %v", err)
    }
}

func TestRenderShellQuotesArgsContainingSpaces(t *testing.T) {
    entries := []Entry{
        {
            Name:   "with-space-arg",
            User:   "www-data",
            Binary: "/usr/bin/echo",
            Args:   []string{"hello world", "next"},
            Schedule: &Schedule{
                Minute: "0",
            },
            LogPath: "/var/log/x.log",
        },
    }

    content, err := Render(entries, RenderOptions{})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    expectedLine := "0 * * * * www-data /usr/bin/echo 'hello world' next >> '/var/log/x.log' 2>&1"
    if false == strings.Contains(content, expectedLine) {
        t.Fatalf("expected per-token quoting around the spaced arg, got:\n%s", content)
    }
}

func TestRenderShellQuotesScheduleCommandTokens(t *testing.T) {
    entries := []Entry{
        {
            Name: "wrapped",
            User: "www-data",
            Schedule: &Schedule{
                Minute: "0",
            },
            Command: []string{"/usr/bin/flock", "-n", "/tmp/lock with space", "/opt/melody/app", "wrapped"},
            LogPath: "/var/log/wrapped.log",
        },
    }

    content, err := Render(entries, RenderOptions{})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    expectedLine := "0 * * * * www-data /usr/bin/flock -n '/tmp/lock with space' /opt/melody/app wrapped >> '/var/log/wrapped.log' 2>&1"
    if false == strings.Contains(content, expectedLine) {
        t.Fatalf("expected per-token quote on the spaced token, got:\n%s", content)
    }
}

func TestRenderEscapesEmbeddedSingleQuoteInLogPath(t *testing.T) {
    entries := []Entry{
        {
            Name:   "with-quote",
            User:   "www-data",
            Binary: "/bin/echo",
            Args:   []string{"hello"},
            Schedule: &Schedule{
                Minute: "0",
            },
            LogPath: "/var/log/o'reilly.log",
        },
    }

    content, err := Render(entries, RenderOptions{})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    expectedRedirect := ` >> '/var/log/o'\''reilly.log' 2>&1`
    if false == strings.Contains(content, expectedRedirect) {
        t.Fatalf("expected POSIX-escaped single quote in log path, got:\n%s", content)
    }
}

func TestRenderQuotesHeartbeatPathWithSpaces(t *testing.T) {
    content, err := Render(nil, RenderOptions{
        HeartbeatUser: "www-data",
        HeartbeatPath: "/var/log/has space/heartbeat",
    })
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    expectedLine := "* * * * * www-data /bin/touch '/var/log/has space/heartbeat'"
    if false == strings.Contains(content, expectedLine) {
        t.Fatalf("expected quoted heartbeat path, got:\n%s", content)
    }
}

func TestRenderShellQuotesHeartbeatCommandTokens(t *testing.T) {
    content, err := Render(nil, RenderOptions{
        HeartbeatUser:    "monitor",
        HeartbeatCommand: []string{"/usr/bin/curl", "-fsS", "https://heartbeat.example.com/ping?token=$X"},
    })
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    expectedLine := `* * * * * monitor /usr/bin/curl -fsS 'https://heartbeat.example.com/ping?token=$X'`
    if false == strings.Contains(content, expectedLine) {
        t.Fatalf("expected per-token quoting in heartbeat command, got:\n%s", content)
    }
}

func TestRenderQuotesEmptyStringToken(t *testing.T) {
    entries := []Entry{
        {
            Name:   "empty-arg",
            User:   "www-data",
            Binary: "/bin/echo",
            Args:   []string{"", "after"},
            Schedule: &Schedule{
                Minute: "0",
            },
            LogPath: "/tmp/x.log",
        },
    }

    content, err := Render(entries, RenderOptions{})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    expectedLine := "0 * * * * www-data /bin/echo '' after >> '/tmp/x.log' 2>&1"
    if false == strings.Contains(content, expectedLine) {
        t.Fatalf("expected empty token rendered as '', got:\n%s", content)
    }
}

func TestRenderRejectsEntryArgContainingPercent(t *testing.T) {
    entries := []Entry{
        {
            Name:   "stamper",
            User:   "www-data",
            Binary: "/usr/local/bin/app",
            Args:   []string{"--time=%H"},
            Schedule: &Schedule{
                Minute: "0",
            },
        },
    }

    _, err := Render(entries, RenderOptions{})
    if nil == err {
        t.Fatalf("expected error when entry arg contains %%, got nil")
    }

    if false == strings.Contains(err.Error(), "%") || false == strings.Contains(err.Error(), "stamper") {
        t.Fatalf("expected error to mention %% and entry name, got: %v", err)
    }
}

func TestRenderRejectsCustomCommandContainingPercent(t *testing.T) {
    entries := []Entry{
        {
            Name: "stamper",
            User: "www-data",
            Schedule: &Schedule{
                Minute: "0",
            },
            Command: []string{"/bin/date", "+%Y"},
        },
    }

    _, err := Render(entries, RenderOptions{})
    if nil == err {
        t.Fatalf("expected error when custom Command contains %%, got nil")
    }
}

func TestRenderRejectsLogPathContainingPercent(t *testing.T) {
    entries := []Entry{
        {
            Name:   "rotated",
            User:   "www-data",
            Binary: "/usr/local/bin/app",
            Args:   []string{"rotated"},
            Schedule: &Schedule{
                Minute: "0",
            },
            LogPath: "/var/log/app/%Y.log",
        },
    }

    _, err := Render(entries, RenderOptions{})
    if nil == err {
        t.Fatalf("expected error when LogPath contains %%, got nil")
    }

    if false == strings.Contains(err.Error(), "log path") {
        t.Fatalf("expected error to mention log path, got: %v", err)
    }
}

func TestRenderRejectsHeartbeatCommandContainingPercent(t *testing.T) {
    options := RenderOptions{
        HeartbeatUser:    "www-data",
        HeartbeatCommand: []string{"/bin/echo", "100%"},
    }

    _, err := Render(nil, options)
    if nil == err {
        t.Fatalf("expected error when HeartbeatCommand contains %%, got nil")
    }

    if false == strings.Contains(err.Error(), "heartbeat command") {
        t.Fatalf("expected error to mention heartbeat command, got: %v", err)
    }
}

func TestRenderRejectsHeartbeatPathContainingPercent(t *testing.T) {
    options := RenderOptions{
        HeartbeatUser: "www-data",
        HeartbeatPath: "/tmp/%H.beat",
    }

    _, err := Render(nil, options)
    if nil == err {
        t.Fatalf("expected error when HeartbeatPath contains %%, got nil")
    }

    if false == strings.Contains(err.Error(), "heartbeat path") {
        t.Fatalf("expected error to mention heartbeat path, got: %v", err)
    }
}

func TestRenderRejectsEntryArgContainingNewline(t *testing.T) {
    entries := []Entry{
        {
            Name:   "stamper",
            User:   "www-data",
            Binary: "/usr/local/bin/app",
            Args:   []string{"line1\nline2"},
            Schedule: &Schedule{
                Minute: "0",
            },
        },
    }

    _, err := Render(entries, RenderOptions{})
    if nil == err {
        t.Fatalf("expected error when entry arg contains newline, got nil")
    }

    if false == strings.Contains(err.Error(), "stamper") {
        t.Fatalf("expected error to mention entry name, got: %v", err)
    }
}

func TestRenderRejectsLogPathContainingNewline(t *testing.T) {
    entries := []Entry{
        {
            Name:   "rotated",
            User:   "www-data",
            Binary: "/usr/local/bin/app",
            Args:   []string{"rotated"},
            Schedule: &Schedule{
                Minute: "0",
            },
            LogPath: "/var/log/app\nrotated.log",
        },
    }

    _, err := Render(entries, RenderOptions{})
    if nil == err {
        t.Fatalf("expected error when LogPath contains newline, got nil")
    }

    if false == strings.Contains(err.Error(), "log path") {
        t.Fatalf("expected error to mention log path, got: %v", err)
    }
}

func TestRenderRejectsHeartbeatCommandContainingNewline(t *testing.T) {
    options := RenderOptions{
        HeartbeatUser:    "www-data",
        HeartbeatCommand: []string{"/bin/echo", "line1\nline2"},
    }

    _, err := Render(nil, options)
    if nil == err {
        t.Fatalf("expected error when HeartbeatCommand contains newline, got nil")
    }

    if false == strings.Contains(err.Error(), "heartbeat command") {
        t.Fatalf("expected error to mention heartbeat command, got: %v", err)
    }
}

func TestRenderRejectsHeartbeatPathContainingNewline(t *testing.T) {
    options := RenderOptions{
        HeartbeatUser: "www-data",
        HeartbeatPath: "/tmp/beat\nfile",
    }

    _, err := Render(nil, options)
    if nil == err {
        t.Fatalf("expected error when HeartbeatPath contains newline, got nil")
    }

    if false == strings.Contains(err.Error(), "heartbeat path") {
        t.Fatalf("expected error to mention heartbeat path, got: %v", err)
    }
}

func TestRenderRejectsScheduleCommandWithOnlyEmptyTokens(t *testing.T) {
    entries := []Entry{
        {
            Name: "stamper",
            User: "www-data",
            Schedule: &Schedule{
                Minute: "0",
            },
            Command: []string{"", "", ""},
        },
    }

    _, err := Render(entries, RenderOptions{})
    if nil == err {
        t.Fatalf("expected error when Command tokens are all empty, got nil")
    }

    if false == strings.Contains(err.Error(), "stamper") || false == strings.Contains(err.Error(), "every token is empty") {
        t.Fatalf("expected error to mention entry name and empty tokens, got: %v", err)
    }
}

func TestRenderRejectsScheduleCommandContainingCarriageReturn(t *testing.T) {
    entries := []Entry{
        {
            Name: "stamper",
            User: "www-data",
            Schedule: &Schedule{
                Minute: "0",
            },
            Command: []string{"/bin/echo", "value\r"},
        },
    }

    _, err := Render(entries, RenderOptions{})
    if nil == err {
        t.Fatalf("expected error when Schedule.Command token contains carriage return, got nil")
    }
}

func TestValidateNoForbiddenCharsRejectsForbiddenChar(t *testing.T) {
    err := ValidateNoForbiddenChars([]string{"clean", "with%percent"}, CrontabForbiddenChars, "test context")
    if nil == err {
        t.Fatalf("expected error for token containing %%")
    }

    if false == strings.Contains(err.Error(), "test context") {
        t.Fatalf("expected error to mention the context, got: %v", err)
    }

    if false == strings.Contains(err.Error(), "with%percent") {
        t.Fatalf("expected error to quote the offending token, got: %v", err)
    }
}

func TestValidateNoForbiddenCharsAllowsCleanTokens(t *testing.T) {
    err := ValidateNoForbiddenChars([]string{"safe", "tokens", "only"}, CrontabForbiddenChars, "test context")
    if nil != err {
        t.Fatalf("expected nil error for clean tokens, got: %v", err)
    }
}

func TestValidateNoForbiddenCharsWithCustomList(t *testing.T) {
    custom := []ForbiddenChar{
        {Char: '\t', Reason: "tabs break YAML"},
    }

    err := ValidateNoForbiddenChars([]string{"has\ttab"}, custom, "yaml entry")
    if nil == err {
        t.Fatalf("expected error for tab character")
    }

    if false == strings.Contains(err.Error(), "yaml entry") {
        t.Fatalf("expected error to mention the context, got: %v", err)
    }
}

func TestValidateNoForbiddenCharsEmptyTokensReturnsNil(t *testing.T) {
    err := ValidateNoForbiddenChars(nil, CrontabForbiddenChars, "test context")
    if nil != err {
        t.Fatalf("expected nil error for empty tokens, got: %v", err)
    }
}

func TestBuiltinTemplatesReturnsCrontab(t *testing.T) {
    templates := BuiltinTemplates()
    if 1 != len(templates) {
        t.Fatalf("expected exactly one builtin template, got %d", len(templates))
    }

    if TemplateNameCrontab != templates[0].Name() {
        t.Fatalf("expected builtin template name %q, got %q", TemplateNameCrontab, templates[0].Name())
    }
}

func TestShellQuoteIfNeededEmptyStringYieldsTwoQuotes(t *testing.T) {
    if "''" != shellQuoteIfNeeded("") {
        t.Fatalf("shellQuoteIfNeeded(\"\") = %q, want %q", shellQuoteIfNeeded(""), "''")
    }
}

func TestShellQuoteIfNeededLeavesSafeTokensUnchanged(t *testing.T) {
    safe := "command-name"

    if safe != shellQuoteIfNeeded(safe) {
        t.Fatalf("shellQuoteIfNeeded(%q) = %q, want unchanged", safe, shellQuoteIfNeeded(safe))
    }
}

func TestShellQuoteIfNeededQuotesWhenSpacePresent(t *testing.T) {
    token := "hello world"
    expected := "'hello world'"

    if expected != shellQuoteIfNeeded(token) {
        t.Fatalf("shellQuoteIfNeeded(%q) = %q, want %q", token, shellQuoteIfNeeded(token), expected)
    }
}

func TestShellQuoteIfNeededQuotesWhenMetacharPresent(t *testing.T) {
    token := "echo$HOME"
    quoted := shellQuoteIfNeeded(token)

    if false == strings.HasPrefix(quoted, "'") || false == strings.HasSuffix(quoted, "'") {
        t.Fatalf("expected single-quoted output for %q, got %q", token, quoted)
    }
}

func TestSingleQuoteEscapesEmbeddedSingleQuote(t *testing.T) {
    expected := `'it'\''s'`

    if expected != singleQuote("it's") {
        t.Fatalf("singleQuote(%q) = %q, want %q", "it's", singleQuote("it's"), expected)
    }
}

func TestJoinShellTokensJoinsWithSpaces(t *testing.T) {
    expected := "alpha 'with space' beta"

    if expected != joinShellTokens([]string{"alpha", "with space", "beta"}) {
        t.Fatalf("joinShellTokens result = %q, want %q", joinShellTokens([]string{"alpha", "with space", "beta"}), expected)
    }
}

func TestRenderRejectsWhitespaceInScheduleField(t *testing.T) {
    entries := []Entry{
        {
            Name:   "broken",
            User:   "www-data",
            Binary: "/usr/local/bin/app",
            Args:   []string{"broken"},
            Schedule: &Schedule{
                Minute: "0 30",
                Hour:   "*",
            },
            LogPath: "/var/log/app/broken.log",
        },
    }

    _, err := Render(entries, RenderOptions{})
    if nil == err {
        t.Fatalf("expected error when Schedule.Minute contains whitespace, got nil")
    }

    if false == strings.Contains(err.Error(), "whitespace in Schedule.Minute") {
        t.Fatalf("expected error to mention whitespace in Schedule.Minute, got: %v", err)
    }
}

func TestRenderAcceptsRangeAndStepNotation(t *testing.T) {
    entries := []Entry{
        {
            Name:   "valid",
            User:   "www-data",
            Binary: "/usr/local/bin/app",
            Args:   []string{"valid"},
            Schedule: &Schedule{
                Minute:     "*/15",
                Hour:       "9-17",
                DayOfMonth: "1,15",
                Month:      "*",
                DayOfWeek:  "mon-fri",
            },
            LogPath: "/var/log/app/valid.log",
        },
    }

    content, err := Render(entries, RenderOptions{})
    if nil != err {
        t.Fatalf("Render returned unexpected error for valid step/range/list syntax: %v", err)
    }

    if false == strings.Contains(content, "*/15 9-17 1,15 * mon-fri") {
        t.Fatalf("expected rendered expression to preserve step/range/list syntax, got:\n%s", content)
    }
}

func TestRenderRejectsEntryUserContainingWhitespace(t *testing.T) {
    entries := []Entry{
        {
            Name:   "bad-user",
            User:   "www data",
            Binary: "/usr/local/bin/app",
            Args:   []string{"bad-user"},
            Schedule: &Schedule{
                Minute: "0",
            },
        },
    }

    _, err := Render(entries, RenderOptions{})
    if nil == err {
        t.Fatalf("expected error when entry user contains whitespace, got nil")
    }

    if false == strings.Contains(err.Error(), "bad-user") {
        t.Fatalf("expected error to mention entry name, got: %v", err)
    }

    if false == strings.Contains(err.Error(), "whitespace") {
        t.Fatalf("expected error to mention whitespace, got: %v", err)
    }
}

func TestRenderRejectsEntryUserContainingNewline(t *testing.T) {
    entries := []Entry{
        {
            Name:   "bad-user",
            User:   "www\ndata",
            Binary: "/usr/local/bin/app",
            Args:   []string{"bad-user"},
            Schedule: &Schedule{
                Minute: "0",
            },
        },
    }

    _, err := Render(entries, RenderOptions{})
    if nil == err {
        t.Fatalf("expected error when entry user contains newline, got nil")
    }
}

func TestRenderRejectsHeartbeatUserContainingWhitespace(t *testing.T) {
    _, err := Render(nil, RenderOptions{
        HeartbeatUser: "www data",
        HeartbeatPath: "/var/log/heartbeat",
    })

    if nil == err {
        t.Fatalf("expected error when heartbeat user contains whitespace, got nil")
    }

    if false == strings.Contains(err.Error(), "heartbeat user") {
        t.Fatalf("expected error to mention heartbeat user, got: %v", err)
    }
}

func TestRenderRejectsHeartbeatUserContainingNewlineForCommand(t *testing.T) {
    _, err := Render(nil, RenderOptions{
        HeartbeatUser:    "www\ndata",
        HeartbeatCommand: []string{"/bin/echo", "alive"},
    })

    if nil == err {
        t.Fatalf("expected error when heartbeat user contains newline (command branch), got nil")
    }

    if false == strings.Contains(err.Error(), "heartbeat user") {
        t.Fatalf("expected error to mention heartbeat user, got: %v", err)
    }
}

func TestRenderRejectsCarriageReturnInScheduleField(t *testing.T) {
    entries := []Entry{
        {
            Name:   "stamper",
            User:   "www-data",
            Binary: "/usr/bin/app",
            Args:   []string{"stamper"},
            Schedule: &Schedule{
                Minute: "0\r",
                Hour:   "3",
            },
        },
    }

    _, err := Render(entries, RenderOptions{})
    if nil == err {
        t.Fatalf("expected error when Schedule.Minute contains carriage return, got nil")
    }

    if false == strings.Contains(err.Error(), "Minute") {
        t.Fatalf("expected error to mention the offending field Minute, got: %v", err)
    }
}
