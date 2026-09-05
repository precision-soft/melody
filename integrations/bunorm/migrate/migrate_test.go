package migrate

import (
    "bytes"
    "context"
    "errors"
    "io"
    "os"
    "strings"
    "testing"

    "github.com/precision-soft/melody/cli"
)

func TestDefaultRunnerOption(t *testing.T) {
    option := DefaultRunnerOption()

    if os.Stdout != option.Writer {
        t.Fatal("default runner option must write to stdout")
    }

    if false != option.NoColor {
        t.Fatal("default runner option must keep colors enabled")
    }
}

func TestUpWithOption_ExecutesAllQueriesInOrder(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    buffer := &bytes.Buffer{}

    queries := []Query{
        {Name: "create table", SQL: "CREATE TABLE users (id INTEGER)"},
        {Name: "create index", SQL: "CREATE INDEX users_id ON users (id)"},
    }

    runErr := UpWithOption(context.Background(), database, "add_users", queries, RunnerOption{Writer: buffer, NoColor: true})
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

func TestDownWithOption_UsesDownDirection(t *testing.T) {
    database, _ := newFakeBunDatabase()
    buffer := &bytes.Buffer{}

    queries := []Query{{Name: "drop table", SQL: "DROP TABLE users"}}

    runErr := DownWithOption(context.Background(), database, "add_users", queries, RunnerOption{Writer: buffer, NoColor: true})
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if false == strings.Contains(buffer.String(), "[migration:down] add_users [1/1] executing: drop table") {
        t.Fatalf("missing down direction in %q", buffer.String())
    }
}

func TestRunQueriesWithOption_StopsOnFailureAndReportsQuery(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.execHook = func(query string) error {
        if true == strings.Contains(query, "CREATE INDEX") {
            return errors.New("index already exists")
        }

        return nil
    }

    buffer := &bytes.Buffer{}
    queries := []Query{
        {Name: "create table", SQL: "CREATE TABLE users (id INTEGER)"},
        {Name: "create index", SQL: "CREATE INDEX users_id\n    ON users (id)"},
        {Name: "seed data", SQL: "INSERT INTO users VALUES (1)"},
    }

    runErr := RunQueriesWithOption(context.Background(), database, "up", "add_users", queries, RunnerOption{Writer: buffer, NoColor: true})
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

func TestRunQueriesWithOption_NilWriterFallsBackToStdout(t *testing.T) {
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

    runErr := RunQueriesWithOption(
        context.Background(),
        database,
        "up",
        "add_users",
        []Query{{Name: "create table", SQL: "CREATE TABLE users (id INTEGER)"}},
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

func TestRunQueriesWithOption_EmptySetWarnsInsteadOfReportingSuccess(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    buffer := &bytes.Buffer{}

    runErr := RunQueriesWithOption(
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

/* RunQueries reads the installed process default when the migration passes no option of its own: the parsed --no-color posture reaches the per-query lines whose signature bun fixes at (ctx, db). */
func TestRunQueries_ReadsTheInstalledProcessDefault(t *testing.T) {
    t.Cleanup(func() {
        processRunnerOption.Store(nil)
    })

    var buffer bytes.Buffer
    SetDefaultRunnerOption(RunnerOption{Writer: &buffer, NoColor: true})

    if runErr := RunQueries(context.Background(), nil, "up", "20240101000000_probe", nil); nil != runErr {
        t.Fatalf("run: %v", runErr)
    }

    rendered := buffer.String()
    if "" == rendered {
        t.Fatal("expected the empty-set warning on the installed writer")
    }

    if true == strings.Contains(rendered, "\x1b[") {
        t.Fatalf("expected the installed no-color posture to strip the escape codes, got %q", rendered)
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

/* the empty and the success lines of a run carry the migration name, which the runner did not write, and they were the two lines of the printer that let it through as sent while the executing, completed and failed lines escaped it; the name is escaped on all five, in both colour modes */
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

/* the option a command puts on the context is the one a run prints under, ahead of the process-wide fallback: the fallback is one value for the whole process, and a run reading it printed under whichever command had installed it last */
func TestRunQueries_ReadsTheOptionCarriedByTheContextBeforeTheProcessDefault(t *testing.T) {
    t.Cleanup(func() {
        processRunnerOption.Store(nil)
    })

    var fallback bytes.Buffer
    SetDefaultRunnerOption(RunnerOption{Writer: &fallback, NoColor: true})

    var carried bytes.Buffer
    ctx := withRunnerOption(context.Background(), RunnerOption{Writer: &carried, NoColor: true})

    if runErr := RunQueries(ctx, nil, "up", "20240101000000_probe", nil); nil != runErr {
        t.Fatalf("run: %v", runErr)
    }

    if false == strings.Contains(carried.String(), "20240101000000_probe") {
        t.Fatalf("expected the run to print on the writer the context carries, got %q", carried.String())
    }

    if "" != fallback.String() {
        t.Fatalf("expected nothing on the process-wide fallback, got %q", fallback.String())
    }
}

/* a command puts its posture back on the way out, and only when its own value is still the live one: the value installed for the run does not survive the run, and a command that finished while a later one still runs leaves that one's value where it is */
func TestRestoreDefaultRunnerOption_PutsBackOnlyOverItsOwnValue(t *testing.T) {
    t.Cleanup(func() {
        processRunnerOption.Store(nil)
    })

    var host bytes.Buffer
    SetDefaultRunnerOption(RunnerOption{Writer: &host, NoColor: true})

    var first bytes.Buffer
    firstInstalled, firstPrevious := swapDefaultRunnerOption(RunnerOption{Writer: &first, NoColor: true})

    if &first != resolveDefaultRunnerOption().Writer {
        t.Fatal("expected the swap to install the command's value for the length of its run")
    }

    var second bytes.Buffer
    secondInstalled, secondPrevious := swapDefaultRunnerOption(RunnerOption{Writer: &second, NoColor: true})

    restoreDefaultRunnerOption(firstInstalled, firstPrevious)
    if &second != resolveDefaultRunnerOption().Writer {
        t.Fatal("expected the first command's restore to leave the later command's value in place")
    }

    restoreDefaultRunnerOption(secondInstalled, secondPrevious)
    if firstInstalled != processRunnerOption.Load() {
        t.Fatal("expected the second command's restore to put back what it found, the first command's value")
    }

    restoreDefaultRunnerOption(firstInstalled, firstPrevious)
    if &host != resolveDefaultRunnerOption().Writer {
        t.Fatalf("expected the host's own value back once every command restored, got %v", resolveDefaultRunnerOption().Writer)
    }
}
