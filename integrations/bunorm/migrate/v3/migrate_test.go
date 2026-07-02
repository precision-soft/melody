package migrate

import (
    "bytes"
    "context"
    "errors"
    "io"
    "os"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/cli"
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
