package migrate

import (
    "bytes"
    "context"
    "errors"
    "io"
    "os"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/cli"
)

func TestUp_ExecutesAllQueriesInOrder(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    buffer := &bytes.Buffer{}

    queries := []Query{
        {Name: "create table", Sql: "CREATE TABLE users (id INTEGER)"},
        {Name: "create index", Sql: "CREATE INDEX users_id ON users (id)"},
    }

    runErr := Up(context.Background(), database, "add_users", queries, RunnerOption{Writer: buffer, NoColor: true})
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    executed := recorder.recordedQueries()
    if 2 != len(executed) {
        t.Fatalf("expected 2 executed queries, got %d (%v)", len(executed), executed)
    }

    if "CREATE TABLE users (id INTEGER)" != executed[0] || "CREATE INDEX users_id ON users (id)" != executed[1] {
        t.Fatalf("queries executed out of order: %v", executed)
    }

    rendered := buffer.String()

    if false == strings.Contains(rendered, "[migration:up] add_users [1/2] executing: create table") {
        t.Fatalf("missing first executing line in %q", rendered)
    }

    if false == strings.Contains(rendered, "[migration:up] add_users [1/2] completed: create table") {
        t.Fatalf("missing first completed line in %q", rendered)
    }

    if false == strings.Contains(rendered, "[migration:up] add_users [2/2] executing: create index") {
        t.Fatalf("missing second executing line in %q", rendered)
    }

    if false == strings.Contains(rendered, "[migration:up] add_users: all 2 queries executed successfully") {
        t.Fatalf("missing success summary in %q", rendered)
    }
}

func TestDown_UsesDownDirection(t *testing.T) {
    database, _ := newFakeBunDatabase()
    buffer := &bytes.Buffer{}

    queries := []Query{{Name: "drop table", Sql: "DROP TABLE users"}}

    runErr := Down(context.Background(), database, "add_users", queries, RunnerOption{Writer: buffer, NoColor: true})
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if false == strings.Contains(buffer.String(), "[migration:down] add_users [1/1] executing: drop table") {
        t.Fatalf("missing down direction in %q", buffer.String())
    }
}

func TestRunQueries_StopsOnFailureAndReportsQuery(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.execHook = func(query string) error {
        if true == strings.Contains(query, "CREATE INDEX") {
            return errors.New("index already exists")
        }

        return nil
    }

    buffer := &bytes.Buffer{}
    queries := []Query{
        {Name: "create table", Sql: "CREATE TABLE users (id INTEGER)"},
        {Name: "create index", Sql: "CREATE INDEX users_id\n    ON users (id)"},
        {Name: "seed data", Sql: "INSERT INTO users VALUES (1)"},
    }

    runErr := RunQueries(context.Background(), database, "up", "add_users", queries, RunnerOption{Writer: buffer, NoColor: true})
    if nil == runErr {
        t.Fatal("expected an error when a query fails")
    }

    if false == strings.Contains(runErr.Error(), "migration add_users failed at step 2/3 (create index)") {
        t.Fatalf("error = %q, want step context", runErr.Error())
    }

    executed := recorder.recordedQueries()
    if 2 != len(executed) {
        t.Fatalf("execution did not stop at the failing query: %v", executed)
    }

    rendered := buffer.String()

    if false == strings.Contains(rendered, "[migration:up] add_users [2/3] FAILED: create index") {
        t.Fatalf("missing FAILED line in %q", rendered)
    }

    if false == strings.Contains(rendered, "ERROR: index already exists") {
        t.Fatalf("missing ERROR line in %q", rendered)
    }

    if false == strings.Contains(rendered, "QUERY:\n       CREATE INDEX users_id\n       ON users (id)") {
        t.Fatalf("failing query not rendered indented in %q", rendered)
    }
}

func TestRunQueries_NilWriterFallsBackToStdout(t *testing.T) {
    database, _ := newFakeBunDatabase()

    readEnd, writeEnd, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("failed to create pipe: %s", pipeErr.Error())
    }

    originalStdout := os.Stdout
    os.Stdout = writeEnd
    defer func() {
        os.Stdout = originalStdout
    }()

    runErr := RunQueries(
        context.Background(),
        database,
        "up",
        "add_users",
        []Query{{Name: "create table", Sql: "CREATE TABLE users (id INTEGER)"}},
        RunnerOption{Writer: nil, NoColor: true},
    )

    os.Stdout = originalStdout
    _ = writeEnd.Close()

    captured, readErr := io.ReadAll(readEnd)
    if nil != readErr {
        t.Fatalf("failed to read captured stdout: %s", readErr.Error())
    }

    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if false == strings.Contains(string(captured), "all 1 queries executed successfully") {
        t.Fatalf("nil writer did not fall back to stdout, captured %q", string(captured))
    }
}

func TestMigrationPrinter_ColoredOutput(t *testing.T) {
    buffer := &bytes.Buffer{}
    printer := &migrationPrinter{writer: buffer, noColor: false}

    printer.printExecuting("[migration:up] add_users [1/1]", "create table")
    printer.printCompleted("[migration:up] add_users [1/1]", "create table")
    printer.printFailed("[migration:up] add_users [1/1]", "create table", errors.New("boom"), "CREATE TABLE users")
    printer.printSuccess("up", "add_users", 1)

    rendered := buffer.String()

    if false == strings.Contains(rendered, cli.AnsiCyan+"[migration:up] add_users [1/1]"+cli.AnsiReset+" executing: create table") {
        t.Fatalf("missing colored executing line in %q", rendered)
    }

    if false == strings.Contains(rendered, cli.AnsiGreen+"create table"+cli.AnsiReset) {
        t.Fatalf("missing colored completed query name in %q", rendered)
    }

    if false == strings.Contains(rendered, cli.AnsiRed+"FAILED:"+cli.AnsiReset) {
        t.Fatalf("missing colored FAILED marker in %q", rendered)
    }

    if false == strings.Contains(rendered, cli.AnsiRed+cli.AnsiBold+" ERROR: boom"+cli.AnsiReset) {
        t.Fatalf("missing colored error line in %q", rendered)
    }

    if false == strings.Contains(rendered, cli.AnsiGreen+cli.AnsiBold+"[migration:up] add_users: all 1 queries executed successfully"+cli.AnsiReset) {
        t.Fatalf("missing colored success summary in %q", rendered)
    }
}

func TestFormatQueryForLog(t *testing.T) {
    formatted := formatQueryForLog("\n  CREATE TABLE users (\n      id INTEGER\n  )  \n")

    expected := "       CREATE TABLE users (\n       id INTEGER\n       )"
    if expected != formatted {
        t.Fatalf("formatted query = %q, want %q", formatted, expected)
    }
}

func TestRunQueries_EmptySetWarnsInsteadOfReportingSuccess(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    buffer := &bytes.Buffer{}

    runErr := RunQueries(
        context.Background(),
        database,
        "up",
        "20240101000000_create_users",
        nil,
        RunnerOption{Writer: buffer, NoColor: true},
    )
    if nil != runErr {
        t.Fatalf("expected an empty set to still succeed, got %v", runErr)
    }

    rendered := buffer.String()
    if false == strings.Contains(rendered, "WARNING") {
        t.Fatalf("expected the empty set to be reported as a warning, got %q", rendered)
    }

    if true == strings.Contains(rendered, "executed successfully") {
        t.Fatalf("expected no success line for a set that executed nothing, got %q", rendered)
    }

    if 0 != len(recorder.recordedQueries()) {
        t.Fatalf("expected no statement to reach the database, got %v", recorder.recordedQueries())
    }
}

/* the failure rendering hands the terminal three foreign strings — the query name, the driver's error text and the statement — so each is escaped visibly, and the statement alone keeps its real line breaks, which are the readability of the query block. */
func TestMigrationPrinter_PrintFailedEscapesForeignTextButKeepsTheQueryLines(t *testing.T) {
    buffer := &bytes.Buffer{}
    printer := &migrationPrinter{writer: buffer, noColor: true}

    printer.printFailed(
        "[migration:up] add_users [1/1]",
        "create\rtable",
        errors.New("server said\x1b[2J"),
        "CREATE TABLE users (\n    id INT\x07\n)",
    )

    rendered := buffer.String()

    if false == strings.Contains(rendered, `create\rtable`) {
        t.Fatalf("expected the query name escaped, got:\n%s", rendered)
    }

    if false == strings.Contains(rendered, `server said\x1b[2J`) {
        t.Fatalf("expected the error text escaped, got:\n%s", rendered)
    }

    if false == strings.Contains(rendered, `INT\x07`) {
        t.Fatalf("expected the statement's control byte escaped, got:\n%s", rendered)
    }

    if false == strings.Contains(rendered, "       CREATE TABLE users (\n") {
        t.Fatalf("expected the statement to keep its real line breaks, got:\n%s", rendered)
    }

    if true == strings.Contains(rendered, "\x1b") || true == strings.Contains(rendered, "\x07") {
        t.Fatalf("a raw control byte reached the terminal:\n%s", rendered)
    }
}

/* the empty and the success lines of a run carry the migration name, which the runner did not write; they escape it in both colour modes, and the per-query lines, which carry it inside the prefix, are checked on a run of their own below */
func TestMigrationPrinter_EscapesTheMigrationNameOnTheEmptyAndSuccessLines(t *testing.T) {
    for _, noColor := range []bool{true, false} {
        buffer := &bytes.Buffer{}
        printer := &migrationPrinter{writer: buffer, noColor: noColor}

        printer.printEmpty("up", "2024\r0101_forged")
        printer.printSuccess("up", "2024\r0101_forged", 1)

        rendered := buffer.String()

        if 2 != strings.Count(rendered, `2024\r0101_forged`) {
            t.Fatalf("noColor=%v: expected the escaped name on both lines, got %q", noColor, rendered)
        }

        if true == strings.Contains(rendered, "\r") {
            t.Fatalf("noColor=%v: a raw carriage return survived: %q", noColor, rendered)
        }
    }
}

func TestRunQueries_ReadsTheOptionCarriedByTheContext(t *testing.T) {
    var carried bytes.Buffer
    ctx := withRunnerOption(context.Background(), RunnerOption{Writer: &carried, NoColor: true})

    if runErr := RunQueries(ctx, nil, "up", "20240101000000_probe", nil, RunnerOption{}); nil != runErr {
        t.Fatalf("run: %v", runErr)
    }

    if false == strings.Contains(carried.String(), "20240101000000_probe") {
        t.Fatalf("expected the run to print on the writer the context carries, got %q", carried.String())
    }
}

/* the prefix of every per-query line carries the migration name, the author's own text, so the executing, completed and failed lines escape it as they escape the query name beside it. The run's second query fails, so all three lines print, in both colour modes: the name is escaped on every line and no raw escape byte reaches the writer. */
func TestRunQueries_EscapesTheMigrationNameInsideThePrefixOfEveryPerQueryLine(t *testing.T) {
    for _, noColor := range []bool{true, false} {
        database, recorder := newFakeBunDatabase()
        recorder.execHook = func(query string) error {
            if true == strings.Contains(query, "CREATE INDEX") {
                return errors.New("index already exists")
            }

            return nil
        }

        buffer := &bytes.Buffer{}
        queries := []Query{
            {Name: "create table", Sql: "CREATE TABLE users (id INTEGER)"},
            {Name: "create index", Sql: "CREATE INDEX users_id ON users (id)"},
        }

        runErr := RunQueries(context.Background(), database, "up", "m\x1b[31mred", queries, RunnerOption{Writer: buffer, NoColor: noColor})
        if nil == runErr {
            t.Fatalf("noColor=%v: expected the second query to fail", noColor)
        }

        rendered := buffer.String()

        /* executing + completed for the first query, executing + FAILED for the second carry the prefix in both modes; the ERROR and QUERY lines carry it only without colour, where the colour is what sets them apart */
        prefixedLines := 4
        if true == noColor {
            prefixedLines = 6
        }

        if prefixedLines != strings.Count(rendered, `[migration:up] m\x1b[31mred [`) {
            t.Fatalf("noColor=%v: expected the escaped name in the prefix of all %d per-query lines, got %q", noColor, prefixedLines, rendered)
        }

        if true == strings.Contains(rendered, "m\x1b[31mred") {
            t.Fatalf("noColor=%v: the raw escape sequence of the migration name reached the writer: %q", noColor, rendered)
        }
    }
}

func TestRunQueries_EscapesTheDirectionOnEveryLine(t *testing.T) {
    database, _ := newFakeBunDatabase()
    buffer := &bytes.Buffer{}
    queries := []Query{{Name: "create table", Sql: "CREATE TABLE users (id INTEGER)"}}

    runErr := RunQueries(context.Background(), database, "up\rinjected", "add_users", queries, RunnerOption{Writer: buffer, NoColor: true})
    if nil != runErr {
        t.Fatalf("expected the run to succeed, got %v", runErr)
    }

    emptyErr := RunQueries(context.Background(), database, "up\rinjected", "empty", nil, RunnerOption{Writer: buffer, NoColor: true})
    if nil != emptyErr {
        t.Fatalf("expected the empty run to succeed, got %v", emptyErr)
    }

    rendered := buffer.String()
    if true == strings.Contains(rendered, "\r") {
        t.Fatalf("expected no raw carriage return from the direction, got %q", rendered)
    }
    /* executing and completed for the query, the success line, the empty line */
    if 4 != strings.Count(rendered, `[migration:up\rinjected]`) {
        t.Fatalf("expected the escaped direction on all four lines, got %q", rendered)
    }
}

/* an explicit option that names no writer prints under the command's posture, so a migration that passes one to set the colour alone does not print past the writer its command chose */
func TestUp_AnOptionWithoutAWriterPrintsUnderTheCommandsPosture(t *testing.T) {
    var commandWriter bytes.Buffer
    ctx := withRunnerOption(context.Background(), RunnerOption{Writer: &commandWriter, NoColor: true})

    if upErr := Up(ctx, nil, "probe", nil, RunnerOption{NoColor: true}); nil != upErr {
        t.Fatalf("expected the empty set to succeed, got %v", upErr)
    }

    if false == strings.Contains(commandWriter.String(), "[migration:up] probe") {
        t.Fatalf("expected the line printed on the command's writer, got %q", commandWriter.String())
    }
}

func TestRunQueries_AZeroOptionFollowsTheCommandsColourPosture(t *testing.T) {
    for _, commandNoColor := range []bool{true, false} {
        var commandWriter bytes.Buffer
        ctx := withRunnerOption(context.Background(), RunnerOption{Writer: &commandWriter, NoColor: commandNoColor})

        if runErr := RunQueries(ctx, nil, "up", "probe", nil, RunnerOption{}); nil != runErr {
            t.Fatalf("expected the empty set to succeed, got %v", runErr)
        }

        if commandNoColor == strings.Contains(commandWriter.String(), "\x1b[") {
            t.Fatalf("command noColor=%v: expected the line to follow the command's colour posture, got %q", commandNoColor, commandWriter.String())
        }
    }
}

func TestRunQueries_AnOptionTurningColourOffWinsOverAColourPosture(t *testing.T) {
    var commandWriter bytes.Buffer
    ctx := withRunnerOption(context.Background(), RunnerOption{Writer: &commandWriter})

    if runErr := RunQueries(ctx, nil, "up", "probe", nil, RunnerOption{NoColor: true}); nil != runErr {
        t.Fatalf("expected the empty set to succeed, got %v", runErr)
    }

    if true == strings.Contains(commandWriter.String(), "\x1b[") {
        t.Fatalf("expected the option's plain output, got %q", commandWriter.String())
    }
}
