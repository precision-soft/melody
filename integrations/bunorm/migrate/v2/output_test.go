package migrate

import (
    "bytes"
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v2/cli"
    "github.com/precision-soft/melody/v2/cli/output"
)

func newBufferedOutput(noColor bool) (*commandOutput, *bytes.Buffer) {
    buffer := &bytes.Buffer{}
    option := output.DefaultOption()
    option.NoColor = noColor

    return newCommandOutput(buffer, option), buffer
}

func TestCommandOutput_PrintSuccess(t *testing.T) {
    colored, coloredBuffer := newBufferedOutput(false)
    colored.printSuccess("done")

    expectedColored := cli.AnsiGreen + "done" + cli.AnsiReset + "\n"
    if expectedColored != coloredBuffer.String() {
        t.Fatalf("colored success = %q, want %q", coloredBuffer.String(), expectedColored)
    }

    plain, plainBuffer := newBufferedOutput(true)
    plain.printSuccess("done")

    if "done\n" != plainBuffer.String() {
        t.Fatalf("plain success = %q, want %q", plainBuffer.String(), "done\n")
    }
}

func TestCommandOutput_PrintWarning(t *testing.T) {
    colored, coloredBuffer := newBufferedOutput(false)
    colored.printWarning("careful")

    expectedColored := cli.AnsiYellow + cli.AnsiBold + "WARNING: careful" + cli.AnsiReset + "\n"
    if expectedColored != coloredBuffer.String() {
        t.Fatalf("colored warning = %q, want %q", coloredBuffer.String(), expectedColored)
    }

    plain, plainBuffer := newBufferedOutput(true)
    plain.printWarning("careful")

    if "WARNING: careful\n" != plainBuffer.String() {
        t.Fatalf("plain warning = %q, want %q", plainBuffer.String(), "WARNING: careful\n")
    }
}

func TestCommandOutput_PrintError(t *testing.T) {
    silent, silentBuffer := newBufferedOutput(true)
    silent.printError(nil)

    if 0 != silentBuffer.Len() {
        t.Fatalf("nil error produced output: %q", silentBuffer.String())
    }

    colored, coloredBuffer := newBufferedOutput(false)
    colored.printError(errors.New("boom"))

    expectedColored := cli.AnsiRed + cli.AnsiBold + "ERROR: boom" + cli.AnsiReset + "\n"
    if expectedColored != coloredBuffer.String() {
        t.Fatalf("colored error = %q, want %q", coloredBuffer.String(), expectedColored)
    }

    plain, plainBuffer := newBufferedOutput(true)
    plain.printError(errors.New("boom"))

    if "ERROR: boom\n" != plainBuffer.String() {
        t.Fatalf("plain error = %q, want %q", plainBuffer.String(), "ERROR: boom\n")
    }
}

func TestCommandOutput_PrintDatabaseBlockWithNullDatabase(t *testing.T) {
    instance, buffer := newBufferedOutput(true)

    instance.printDatabaseBlock(&databaseIdentity{
        CurrentDatabase: nil,
        Hostname:        "db.internal",
        Port:            3306,
        CurrentUser:     "app@%",
        Version:         "8.0.36",
    })

    rendered := buffer.String()

    if false == strings.Contains(rendered, "DATABASE") {
        t.Fatalf("missing DATABASE header in %q", rendered)
    }

    if false == strings.Contains(rendered, "| database | <null>") {
        t.Fatalf("nil current database not rendered as <null> in %q", rendered)
    }

    if false == strings.Contains(rendered, "| host     | db.internal") {
        t.Fatalf("missing host row in %q", rendered)
    }

    if false == strings.Contains(rendered, "| port     | 3306") {
        t.Fatalf("missing port row in %q", rendered)
    }

    if false == strings.Contains(rendered, "| user     | app@%") {
        t.Fatalf("missing user row in %q", rendered)
    }

    if false == strings.Contains(rendered, "| version  | 8.0.36") {
        t.Fatalf("missing version row in %q", rendered)
    }
}

func TestCommandOutput_PrintDatabaseBlockTruncatesLongValues(t *testing.T) {
    instance, buffer := newBufferedOutput(true)

    databaseName := "a_very_long_database_name_beyond_column"
    instance.printDatabaseBlock(&databaseIdentity{
        CurrentDatabase: &databaseName,
        Hostname:        "an-extremely-long-hostname.example.com",
        Port:            5432,
        CurrentUser:     "user",
        Version:         "16",
    })

    rendered := buffer.String()

    if false == strings.Contains(rendered, "| database | a_very_long_dat... |") {
        t.Fatalf("long database name not truncated to 18 characters in %q", rendered)
    }

    if false == strings.Contains(rendered, "| host     | an-extremely-lo... |") {
        t.Fatalf("long hostname not truncated to 18 characters in %q", rendered)
    }
}

func TestCommandOutput_PrintDetailsBlockOrdersAndFiltersKeys(t *testing.T) {
    instance, buffer := newBufferedOutput(true)

    instance.printDetailsBlock(map[string]string{
        "name":       "create_users",
        "manager":    "<default>",
        "status":     "1 pending",
        "unexpected": "must not render",
    })

    rendered := buffer.String()

    if false == strings.Contains(rendered, "DETAILS") {
        t.Fatalf("missing DETAILS header in %q", rendered)
    }

    if true == strings.Contains(rendered, "unexpected") || true == strings.Contains(rendered, "must not render") {
        t.Fatalf("unknown key rendered in %q", rendered)
    }

    managerIndex := strings.Index(rendered, "| manager |")
    statusIndex := strings.Index(rendered, "| status  |")
    nameIndex := strings.Index(rendered, "| name    |")

    if 0 > managerIndex || 0 > statusIndex || 0 > nameIndex {
        t.Fatalf("missing detail rows in %q", rendered)
    }

    if false == (managerIndex < statusIndex && statusIndex < nameIndex) {
        t.Fatalf("detail rows are not rendered in the fixed key order: %q", rendered)
    }

    if true == strings.Contains(rendered, "| applied |") {
        t.Fatalf("absent key rendered in %q", rendered)
    }
}

func TestCommandOutput_PrintMigrationsBlock(t *testing.T) {
    empty, emptyBuffer := newBufferedOutput(true)
    empty.printMigrationsBlock("APPLIED", []string{})

    if 0 != emptyBuffer.Len() {
        t.Fatalf("empty migrations list produced output: %q", emptyBuffer.String())
    }

    instance, buffer := newBufferedOutput(true)
    longName := strings.Repeat("m", 60)
    instance.printMigrationsBlock("PENDING", []string{"20240101000000_create_users", longName})

    rendered := buffer.String()

    if false == strings.Contains(rendered, "PENDING") {
        t.Fatalf("missing block title in %q", rendered)
    }

    if false == strings.Contains(rendered, "| 20240101000000_create_users") {
        t.Fatalf("missing migration row in %q", rendered)
    }

    if false == strings.Contains(rendered, strings.Repeat("m", 46)+"...") {
        t.Fatalf("long migration name not truncated to 49 characters in %q", rendered)
    }
}

func TestCommandOutput_PrintFilesBlock(t *testing.T) {
    empty, emptyBuffer := newBufferedOutput(true)
    empty.printFilesBlock([]string{})

    if 0 != emptyBuffer.Len() {
        t.Fatalf("empty files list produced output: %q", emptyBuffer.String())
    }

    instance, buffer := newBufferedOutput(true)
    instance.printFilesBlock([]string{"/tmp/migrations/20240101000000_create_users.go"})

    rendered := buffer.String()

    if false == strings.Contains(rendered, "FILES") {
        t.Fatalf("missing FILES header in %q", rendered)
    }

    if false == strings.Contains(rendered, "  /tmp/migrations/20240101000000_create_users.go\n") {
        t.Fatalf("missing indented file line in %q", rendered)
    }
}

func TestCommandOutput_Newline(t *testing.T) {
    instance, buffer := newBufferedOutput(true)
    instance.newline()

    if "\n" != buffer.String() {
        t.Fatalf("newline wrote %q, want %q", buffer.String(), "\n")
    }
}

func TestTruncateString(t *testing.T) {
    if "short" != truncateString("short", 18) {
        t.Fatalf("short string was modified: %q", truncateString("short", 18))
    }

    exact := strings.Repeat("x", 18)
    if exact != truncateString(exact, 18) {
        t.Fatalf("string of exactly max length was modified: %q", truncateString(exact, 18))
    }

    truncated := truncateString(strings.Repeat("x", 30), 18)
    if strings.Repeat("x", 15)+"..." != truncated {
        t.Fatalf("truncated string = %q, want 15 characters plus ellipsis", truncated)
    }

    if 18 != len(truncated) {
        t.Fatalf("truncated string length = %d, want 18", len(truncated))
    }
}
