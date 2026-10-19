package migrate

import (
    "bytes"
    "context"
    "database/sql/driver"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "strings"
    "testing"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/uptrace/bun/migrate"
)

func TestMigrateCommand_TakesLockBeforeMigratingAndReleasesIt(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    upCalls := 0
    migrations := newSingleMigrationSet("20240101000000", "create_users", &upCalls, nil)

    command := NewMigrateCommand(migrations, DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color", "--verbose")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if 1 != upCalls {
        t.Fatalf("expected the up migration to run once, ran %d times", upCalls)
    }

    lockIndex := recorder.firstIndexMatching(isLockInsert)
    statusIndex := recorder.firstIndexMatching(isMigrationStatusSelect)
    markAppliedIndex := recorder.firstIndexMatching(func(query string) bool {
        return strings.HasPrefix(query, "INSERT") &&
            strings.Contains(query, "bun_migrations") &&
            false == strings.Contains(query, "bun_migration_locks")
    })
    unlockIndex := recorder.firstIndexMatching(isUnlockDelete)

    if 0 > lockIndex || 0 > statusIndex || 0 > markAppliedIndex || 0 > unlockIndex {
        t.Fatalf("missing expected statements: %v", recorder.recordedQueries())
    }

    if false == (lockIndex < statusIndex) {
        t.Fatalf("migration lock was not taken before computing the pending set: %v", recorder.recordedQueries())
    }

    if false == (markAppliedIndex < unlockIndex) {
        t.Fatalf("migration lock was released before the migration was marked applied: %v", recorder.recordedQueries())
    }

    if len(recorder.recordedQueries())-1 != unlockIndex {
        t.Fatalf("unlock is not the final statement: %v", recorder.recordedQueries())
    }

    if false == strings.Contains(rendered, "| manager | <default>") {
        t.Fatalf("missing manager detail in %q", rendered)
    }

    if false == strings.Contains(rendered, "| applied | 1") {
        t.Fatalf("missing applied count detail in %q", rendered)
    }

    if false == strings.Contains(rendered, "APPLIED MIGRATIONS") {
        t.Fatalf("missing applied migrations block in %q", rendered)
    }

    if false == strings.Contains(rendered, "20240101000000") {
        t.Fatalf("missing applied migration name in %q", rendered)
    }
}

func TestMigrateCommand_LockFailureAbortsWithoutMigratingOrUnlocking(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = lockCountHook(1)
    recorder.execHook = func(query string) error {
        if true == isLockInsert(query) {
            return context.DeadlineExceeded
        }

        return nil
    }

    runtimeInstance := newRuntimeWithDatabase(t, database)

    upCalls := 0
    migrations := newSingleMigrationSet("20240101000000", "create_users", &upCalls, nil)

    command := NewMigrateCommand(migrations, DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil == runErr {
        t.Fatal("expected an error when the migration lock cannot be taken")
    }

    if false == errors.Is(runErr, context.DeadlineExceeded) {
        t.Fatalf("the bun lock failure must survive as the cause, got %q", runErr.Error())
    }

    contextual, isContextual := runErr.(interface {
        Context() exceptioncontract.Context
    })
    if false == isContextual {
        t.Fatalf("the lock refusal carries no context at all: %q", runErr.Error())
    }

    lockContext := contextual.Context()
    for key, expected := range map[string]string{
        "manager":       "<default>",
        "locksTable":    migrationLocksTable,
        "unlockCommand": DefaultOptions().CommandPrefix + ":unlock",
    } {
        actual, present := lockContext[key]
        if false == present {
            t.Fatalf("the lock refusal does not name %s: %v", key, lockContext)
        }

        if expected != actual {
            t.Fatalf("%s = %v, want %q", key, actual, expected)
        }
    }

    if 0 != upCalls {
        t.Fatalf("migration ran despite the lock failure (%d times)", upCalls)
    }

    if 0 <= recorder.firstIndexMatching(isUnlockDelete) {
        t.Fatalf("a never-acquired lock was released: %v", recorder.recordedQueries())
    }

    /* the command does not pre-print the failure it returns: the cli runner's [error] line and the full log record report it */
    if true == strings.Contains(rendered, "ERROR:") {
        t.Fatalf("the returned failure must not be pre-printed by the command, got: %q", rendered)
    }
}

func TestMigrateCommand_NoPendingMigrationsWarns(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook("20240101000000")

    runtimeInstance := newRuntimeWithDatabase(t, database)

    upCalls := 0
    migrations := newSingleMigrationSet("20240101000000", "create_users", &upCalls, nil)

    command := NewMigrateCommand(migrations, DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if 0 != upCalls {
        t.Fatalf("an already applied migration ran again (%d times)", upCalls)
    }

    if false == strings.Contains(rendered, "WARNING: no pending migrations") {
        t.Fatalf("missing warning in %q", rendered)
    }

    if 0 > recorder.firstIndexMatching(isUnlockDelete) {
        t.Fatalf("migration lock was not released: %v", recorder.recordedQueries())
    }
}

func TestMigrateCommand_FailedUnlockFailsTheCommand(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.execHook = func(query string) error {
        if true == isUnlockDelete(query) {
            return context.DeadlineExceeded
        }

        return nil
    }

    runtimeInstance := newRuntimeWithDatabase(t, database)

    upCalls := 0
    migrations := newSingleMigrationSet("20240101000000", "create_users", &upCalls, nil)

    command := NewMigrateCommand(migrations, DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil == runErr {
        t.Fatal("expected the failed unlock to fail the command")
    }

    if 1 != upCalls {
        t.Fatalf("expected the migration itself to have run once, ran %d times", upCalls)
    }

    if false == strings.Contains(rendered, "ERROR:") {
        t.Fatalf("unlock failure was not printed beside the exit code: %q", rendered)
    }
}

func TestMigrateCommand_FailedMigrationKeepsItsErrorOverAFailedUnlock(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.execHook = func(query string) error {
        if true == isUnlockDelete(query) {
            return context.DeadlineExceeded
        }

        return nil
    }

    runtimeInstance := newRuntimeWithDatabase(t, database)

    migrations := migrate.NewMigrations()
    migrations.Add(migrate.Migration{
        Name:    "20240101000000",
        Comment: "create_users",
        Up: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
            return errors.New("up exploded")
        },
        Down: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
            return nil
        },
    })

    command := NewMigrateCommand(migrations, DefaultOptions())

    _, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil == runErr {
        t.Fatal("expected the failed migration to fail the command")
    }

    if false == strings.Contains(runErr.Error(), "up exploded") {
        t.Fatalf("expected the migration's own error as the verdict, got %q", runErr.Error())
    }

    if true == errors.Is(runErr, context.DeadlineExceeded) {
        t.Fatalf("expected the unlock failure not to replace the migration's error, got %q", runErr.Error())
    }
}

/* a pipeline reading .data.migrations to record what it deployed receives the detail at the default verbosity, since --verbose affects the plain-text output alone */
func TestMigrateCommand_JsonCarriesTheDetailAtTheDefaultVerbosity(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook()

    runtimeInstance := newRuntimeWithDatabase(t, database)

    migrations := migrate.NewMigrations()
    migrations.Add(migrate.Migration{Name: "20240101000000", Comment: "create_users"})

    rendered, runErr := runMigrationCommand(t, runtimeInstance, NewMigrateCommand(migrations, DefaultOptions()), "--format=json")
    if nil != runErr {
        t.Fatalf("unexpected error: %s; rendered %q", runErr.Error(), rendered)
    }

    document := struct {
        Data struct {
            Details    map[string]string   `json:"details"`
            Migrations map[string][]string `json:"migrations"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(rendered), &document); nil != decodeErr {
        t.Fatalf("failed to decode the document: %v; rendered %q", decodeErr, rendered)
    }

    if "1" != document.Data.Details["applied"] {
        t.Fatalf("expected the applied count in the document, got %#v in %q", document.Data.Details, rendered)
    }

    /* the group is computed inside the same block, so moving only the printing would leave it at its placeholder */
    if "" == document.Data.Details["group"] || "<none>" == document.Data.Details["group"] {
        t.Fatalf("expected the migration group in the document, got %#v", document.Data.Details["group"])
    }

    applied, hasApplied := document.Data.Migrations["applied"]
    if false == hasApplied || 1 != len(applied) {
        t.Fatalf("expected the applied migrations under the stable key, got %#v in %q", document.Data.Migrations, rendered)
    }

    if "20240101000000" != applied[0] {
        t.Fatalf("expected the applied migration to be named, got %q", applied[0])
    }

    /* verbosity still decides what the text rendering shows a person */
    textDatabase, textRecorder := newFakeBunDatabase()
    textRecorder.queryHook = appliedMigrationRowsHook()

    textRendered, textErr := runMigrationCommand(
        t,
        newRuntimeWithDatabase(t, textDatabase),
        NewMigrateCommand(migrations, DefaultOptions()),
        "--no-color",
    )
    if nil != textErr {
        t.Fatalf("unexpected error: %s", textErr.Error())
    }

    if true == strings.Contains(textRendered, "APPLIED MIGRATIONS") {
        t.Fatalf("expected the bare text run to stay bare, got %q", textRendered)
    }
}

/* TestMigrateCommand_ARunThatChangedTheSchemaSaysSoOnTheText pins the line a deploy log captures: a plain run, the shape a deploy script invokes, prints a line for the run that applied migrations and not only a warning for the run that did nothing, as the rollback sibling does. */
func TestMigrateCommand_ARunThatChangedTheSchemaSaysSoOnTheText(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook()

    migrations := migrate.NewMigrations()
    migrations.Add(migrate.Migration{Name: "20240101000000", Comment: "create_users"})

    rendered, runErr := runMigrationCommand(
        t,
        newRuntimeWithDatabase(t, database),
        NewMigrateCommand(migrations, DefaultOptions()),
        "--no-color",
    )
    if nil != runErr {
        t.Fatalf("unexpected error: %s; rendered %q", runErr.Error(), rendered)
    }

    if false == strings.Contains(rendered, "applied 1 migration") {
        t.Fatalf("expected the applied count on the plain text, got %q", rendered)
    }

    if false == strings.Contains(rendered, "<default>") {
        t.Fatalf("expected the manager label on the plain text, got %q", rendered)
    }

    if true == strings.Contains(rendered, "APPLIED MIGRATIONS") {
        t.Fatalf("expected the name list to stay behind --verbose, got %q", rendered)
    }
}

/* the machine document does not carry the success line: under json the same run already carries the applied count, the group and the names as structured fields, so a prose duplicate would be a second and weaker spelling of what the consumer has */
func TestMigrateCommand_TheSuccessLineDoesNotEnterTheMachineDocument(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook()

    migrations := migrate.NewMigrations()
    migrations.Add(migrate.Migration{Name: "20240101000000", Comment: "create_users"})

    rendered, runErr := runMigrationCommand(
        t,
        newRuntimeWithDatabase(t, database),
        NewMigrateCommand(migrations, DefaultOptions()),
        "--format=json",
    )
    if nil != runErr {
        t.Fatalf("unexpected error: %s; rendered %q", runErr.Error(), rendered)
    }

    document := struct {
        Data struct {
            Messages []string `json:"messages"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(rendered), &document); nil != decodeErr {
        t.Fatalf("failed to decode the document: %v; rendered %q", decodeErr, rendered)
    }

    for _, message := range document.Data.Messages {
        if true == strings.Contains(message, "applied 1 migration") {
            t.Fatalf("expected the text line to stay out of the machine document, got %#v", document.Data.Messages)
        }
    }
}

/* a group that fails part way through names what it already applied, on both renderings, so the operator can choose between re-running and rolling back without reading the database by hand */
func TestMigrateCommand_AFailedGroupNamesTheMigrationsThatLanded(t *testing.T) {
    applied := make([]string, 0)

    migrations := migrate.NewMigrations()
    for _, step := range []struct {
        name  string
        fails bool
    }{
        {name: "20240101000001", fails: false},
        {name: "20240101000002", fails: false},
        {name: "20240101000003", fails: true},
    } {
        stepName := step.name
        stepFails := step.fails

        migrations.Add(migrate.Migration{
            Name:    stepName,
            Comment: "probe",
            Up: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
                if true == stepFails {
                    return errors.New("the third migration exploded")
                }

                applied = append(applied, stepName)

                return nil
            },
            Down: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
                return nil
            },
        })
    }

    database, _ := newFakeBunDatabase()
    rendered, runErr := runMigrationCommand(
        t,
        newRuntimeWithDatabase(t, database),
        NewMigrateCommand(migrations, DefaultOptions()),
        "--no-color",
        "--verbose",
    )

    if nil == runErr {
        t.Fatal("expected the broken migration to fail the command")
    }

    if 2 != len(applied) {
        t.Fatalf("this test needs a partially applied group; %d migrations ran up", len(applied))
    }

    for _, name := range applied {
        if false == strings.Contains(rendered, name) {
            t.Fatalf("the failed run does not name the applied migration %s, got: %q", name, rendered)
        }
    }

    jsonDatabase, _ := newFakeBunDatabase()
    renderedJson, jsonErr := runMigrationCommand(
        t,
        newRuntimeWithDatabase(t, jsonDatabase),
        NewMigrateCommand(migrations, DefaultOptions()),
        "--no-color",
        "--format", "json",
    )

    if nil == jsonErr {
        t.Fatal("expected the broken migration to fail the json run too")
    }

    document := map[string]any{}
    if decodeErr := json.Unmarshal([]byte(renderedJson), &document); nil != decodeErr {
        t.Fatalf("the machine document does not parse: %v (%q)", decodeErr, renderedJson)
    }

    data, isData := document["data"].(map[string]any)
    if false == isData {
        t.Fatalf("the failed run carries no data object: %q", renderedJson)
    }

    migrationsBlock, isMigrationsBlock := data["migrations"].(map[string]any)
    if false == isMigrationsBlock {
        t.Fatalf("the failed run does not report the applied group: %v", data)
    }

    appliedNames, isAppliedNames := migrationsBlock["applied"].([]any)
    if false == isAppliedNames || 2 != len(appliedNames) {
        t.Fatalf("data.migrations.applied = %v, want the two migrations that landed", migrationsBlock["applied"])
    }
}

/* a run that fails on its first migration reports no applied block at all, rather than an empty one claiming a partial state */
func TestMigrateCommand_AGroupThatLandedNothingReportsNoAppliedBlock(t *testing.T) {
    migrations := migrate.NewMigrations()
    migrations.Add(migrate.Migration{
        Name:    "20240101000001",
        Comment: "probe",
        Up: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
            return errors.New("the first migration exploded")
        },
        Down: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
            return nil
        },
    })

    database, _ := newFakeBunDatabase()
    rendered, runErr := runMigrationCommand(
        t,
        newRuntimeWithDatabase(t, database),
        NewMigrateCommand(migrations, DefaultOptions()),
        "--no-color",
        "--verbose",
    )

    if nil == runErr {
        t.Fatal("expected the broken migration to fail the command")
    }

    if true == strings.Contains(rendered, "APPLIED MIGRATIONS") {
        t.Fatalf("a run that landed nothing must claim no applied migrations, got: %q", rendered)
    }

    /* the details block is the half of the report that the empty-set guard actually defends: printMigrationsBlock refuses an empty list on its own, but printDetailsBlock would happily render "applied | 0" beside a group id, which reads as a partial state where there is none */
    if true == strings.Contains(rendered, "DETAILS") {
        t.Fatalf("a run that landed nothing must print no applied-group detail block, got: %q", rendered)
    }
}

/* the command's writer reaches the migrations through the context the migrator hands them, not through the process-wide fallback: with the fallback pointing elsewhere, the only way the per-query line lands on the command's writer is the context */
func TestMigrateCommand_HandsItsPostureToTheMigrationsThroughTheContext(t *testing.T) {
    t.Cleanup(func() {
        processRunnerOption.Store(nil)
    })

    var elsewhere bytes.Buffer
    SetDefaultRunnerOption(RunnerOption{Writer: &elsewhere, NoColor: true})

    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    var seenDuringTheRun io.Writer
    carriedByTheContext := false
    migrations := migrate.NewMigrations()
    migrations.Add(migrate.Migration{
        Name:    "20240101000000",
        Comment: "create_users",
        Up: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
            seenDuringTheRun = resolveDefaultRunnerOption().Writer
            _, carriedByTheContext = runnerOptionFromContext(ctx)

            return RunQueries(ctx, database, "up", "20240101000000", []Query{{Name: "create-users", SQL: "CREATE TABLE users (id BIGINT)"}})
        },
    })

    rendered, runErr := runMigrationCommand(t, runtimeInstance, NewMigrateCommand(migrations, DefaultOptions()), "--no-color")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if false == strings.Contains(rendered, "[migration:up] 20240101000000 [1/1] executing: create-users") {
        t.Fatalf("the per-query line did not reach the command's writer through the context: %q", rendered)
    }

    if "" != elsewhere.String() {
        t.Fatalf("the per-query line reached the process-wide fallback instead: %q", elsewhere.String())
    }

    /* the context is the channel, not the fallback: with the fallback installed for the run, a migration handed the plain runtime context would still print on the command's writer, so what separates the two is the option the context carries */
    if false == carriedByTheContext {
        t.Fatal("the migration was not handed the context carrying the command's posture")
    }

    if &elsewhere == seenDuringTheRun {
        t.Fatal("expected the command to install its own posture as the fallback for the length of the run")
    }

    if &elsewhere != resolveDefaultRunnerOption().Writer {
        t.Fatalf("expected the command to put the fallback back on the way out, got %v", resolveDefaultRunnerOption().Writer)
    }
}

/* failingOnWriter refuses every payload that carries the marker and writes the rest through: the truncation lands on ONE line, so which line decides which door is being proven — a writer that failed for good after the first refusal would have the command's own banner refuse on behalf of the runner's line */
type failingOnWriter struct {
    marker string
    buffer bytes.Buffer
}

func (instance *failingOnWriter) Write(payload []byte) (int, error) {
    if true == strings.Contains(string(payload), instance.marker) {
        return 0, errors.New("write: broken pipe")
    }

    return instance.buffer.Write(payload)
}

/* a text write the report lost is remembered, so a report cut short by a full disk is refused rather than exiting zero, the class the framework's table printer guards with its error-tracking writer; finish refuses on the first lost write in text mode and under the json document alike, and the command's own failure still wins when there is one. */
func TestMigrateCommand_AReportCutShortIsRefusedInsteadOfExitingZero(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    migrations := migrate.NewMigrations()
    migrations.Add(migrate.Migration{
        Name:    "20240101000000",
        Comment: "create_users",
        Up: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
            return RunQueries(ctx, database, "up", "20240101000000", []Query{{Name: "create-users", SQL: "CREATE TABLE users (id BIGINT)"}})
        },
    })

    /* the text report cut on the command's own banner */
    banner := &failingOnWriter{marker: "applied 1 migration"}
    runErr := runMigrationCommandTo(t, banner, runtimeInstance, NewMigrateCommand(migrations, DefaultOptions()), "--no-color")
    if nil == runErr || false == strings.Contains(runErr.Error(), "could not be written in full") {
        t.Fatalf("expected the truncated text report refused, got %v (written: %q)", runErr, banner.buffer.String())
    }

    var reported *exception.Error
    if false == errors.As(runErr, &reported) || nil == reported.CauseErr() || false == strings.Contains(reported.CauseErr().Error(), "broken pipe") {
        t.Fatalf("expected the refusal to carry the lost write, got %v", runErr)
    }

    /* the text report cut on a per-query line of the migration itself, which prints through the command's writer */
    queryLine := &failingOnWriter{marker: "executing:"}
    runErr = runMigrationCommandTo(t, queryLine, runtimeInstance, NewMigrateCommand(migrations, DefaultOptions()), "--no-color")
    if nil == runErr || false == strings.Contains(runErr.Error(), "could not be written in full") {
        t.Fatalf("expected the report cut on the runner's line refused, got %v (written: %q)", runErr, queryLine.buffer.String())
    }

    /* the json document cut mid-way */
    document := &failingOnWriter{marker: "\"meta\""}
    runErr = runMigrationCommandTo(t, document, runtimeInstance, NewMigrateCommand(migrations, DefaultOptions()), "--format=json")
    if nil == runErr || false == strings.Contains(runErr.Error(), "could not be written in full") {
        t.Fatalf("expected the truncated json document refused, got %v (written: %q)", runErr, document.buffer.String())
    }

    /* a report written whole is not refused */
    var whole bytes.Buffer
    if runErr = runMigrationCommandTo(t, &whole, runtimeInstance, NewMigrateCommand(migrations, DefaultOptions()), "--no-color"); nil != runErr {
        t.Fatalf("expected a report written whole to pass, got %v", runErr)
    }
}

/* a lock refused because the locks table is missing does not send the operator to the unlock command, which cannot clear a lock nobody holds; bun's error stays the cause */
func TestMigrateCommand_ALockRefusedByAMissingLocksTableDoesNotNameTheUnlockCommand(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    missingTable := errors.New("Error 1146 (42S02): Table 'melody.bun_migration_locks' doesn't exist")
    recorder.queryHook = func(query string) ([]string, [][]driver.Value, error) {
        if true == strings.Contains(query, "bun_migration_locks") {
            return nil, nil, missingTable
        }

        return []string{}, nil, nil
    }
    recorder.execHook = func(query string) error {
        if true == isLockInsert(query) {
            return missingTable
        }

        return nil
    }

    _, runErr := runMigrationCommand(t, newRuntimeWithDatabase(t, database), NewMigrateCommand(newSingleMigrationSet("20240101000000", "create_users", new(int), nil), DefaultOptions()), "--no-color")
    if nil == runErr || false == errors.Is(runErr, missingTable) {
        t.Fatalf("expected the lock refusal with bun's error as its cause, got %v", runErr)
    }

    if false == strings.Contains(runErr.Error(), "missing or unreachable") {
        t.Fatalf("expected the refusal to say the locks table is missing or unreachable, got %q", runErr.Error())
    }

    if _, namesUnlock := lockRefusalContextOf(t, runErr)["unlockCommand"]; true == namesUnlock {
        t.Fatalf("expected no unlock command named for a missing locks table, got %v", lockRefusalContextOf(t, runErr))
    }
}

/* a lock refused while the locks table holds no row is a transient failure, answered without a remedy */
func TestMigrateCommand_ALockRefusedWithNoLockHeldNamesNoRemedy(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = lockCountHook(0)
    refused := errors.New("connection reset by peer")
    recorder.execHook = func(query string) error {
        if true == isLockInsert(query) {
            return refused
        }

        return nil
    }

    _, runErr := runMigrationCommand(t, newRuntimeWithDatabase(t, database), NewMigrateCommand(newSingleMigrationSet("20240101000000", "create_users", new(int), nil), DefaultOptions()), "--no-color")
    if nil == runErr || false == errors.Is(runErr, refused) || true == strings.Contains(runErr.Error(), "is held") {
        t.Fatalf("expected the lock refusal without the held text, got %v", runErr)
    }

    if _, namesUnlock := lockRefusalContextOf(t, runErr)["unlockCommand"]; true == namesUnlock {
        t.Fatal("expected no unlock command named when no lock is held")
    }
}

/* a lock refused under a cancelled context says it was cancelled, and asks the locks table nothing */
func TestLockRefusal_ACancelledContextSaysCancelled(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = lockCountHook(1)

    cancelled, cancel := context.WithCancel(context.Background())
    cancel()

    refusal := lockRefusal(cancelled, database, context.Canceled, "primary", "db:unlock")
    if false == strings.Contains(refusal.Error(), "cancelled") || false == errors.Is(refusal, context.Canceled) {
        t.Fatalf("expected the cancelled refusal, got %v", refusal)
    }

    if 0 <= recorder.firstIndexMatching(func(query string) bool { return true == strings.Contains(query, "bun_migration_locks") }) {
        t.Fatalf("expected the locks table not asked under a cancelled context: %v", recorder.recordedQueries())
    }
}

/* lockRefusalContextOf answers the context the lock refusal carries */
func lockRefusalContextOf(t *testing.T, refusal error) exceptioncontract.Context {
    t.Helper()

    contextual, isContextual := refusal.(interface {
        Context() exceptioncontract.Context
    })
    if false == isContextual {
        t.Fatalf("the lock refusal carries no context: %v", refusal)
    }

    return contextual.Context()
}

/* an empty migration's warning reaches the json document: the per-query lines go to a discarded writer under --format=json, and the warning went with them while the migration was listed as applied; the text mode prints it as before */
func TestMigrateCommand_AnEmptyMigrationIsWarnedInTheJsonDocument(t *testing.T) {
    for _, format := range []string{"--format=json", "--no-color"} {
        database, recorder := newFakeBunDatabase()
        recorder.queryHook = appliedMigrationRowsHook()

        migrations := migrate.NewMigrations()
        migrations.Add(migrate.Migration{
            Name:    "20240101000000",
            Comment: "built_nothing",
            Up: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
                return RunQueries(ctx, migrator.DB(), "up", migration.Name, nil)
            },
        })

        rendered, runErr := runMigrationCommand(t, newRuntimeWithDatabase(t, database), NewMigrateCommand(migrations, DefaultOptions()), format)
        if nil != runErr {
            t.Fatalf("%s: unexpected error: %v; rendered %q", format, runErr, rendered)
        }

        if "--no-color" == format {
            if false == strings.Contains(rendered, "no queries to execute") {
                t.Fatalf("expected the text mode to print the warning, got %q", rendered)
            }

            continue
        }

        document := struct {
            Warnings []struct {
                Message string `json:"message"`
            } `json:"warnings"`
        }{}
        if decodeErr := json.Unmarshal([]byte(rendered), &document); nil != decodeErr {
            t.Fatalf("failed to decode the document: %v; rendered %q", decodeErr, rendered)
        }

        warned := false
        for _, warning := range document.Warnings {
            if true == strings.Contains(warning.Message, "no queries to execute") && true == strings.Contains(warning.Message, "20240101000000") {
                warned = true
            }
        }

        if false == warned {
            t.Fatalf("expected the empty migration warned in the json document, got %q", rendered)
        }
    }
}

/* details.group is the bare id on success and on failure alike: a machine reader parsed "group #1 (...)" from one path and "1" from the other */
func TestMigrateCommand_TheGroupIsSpelledTheSameOnSuccessAndOnFailure(t *testing.T) {
    for _, failing := range []bool{false, true} {
        database, recorder := newFakeBunDatabase()
        recorder.queryHook = appliedMigrationRowsHook()

        migrations := migrate.NewMigrations()
        migrations.Add(migrate.Migration{Name: "20240101000000", Comment: "first"})
        migrations.Add(migrate.Migration{
            Name:    "20240102000000",
            Comment: "second",
            Up: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
                if true == failing {
                    return errors.New("the second migration failed")
                }

                return nil
            },
        })

        rendered, runErr := runMigrationCommand(t, newRuntimeWithDatabase(t, database), NewMigrateCommand(migrations, DefaultOptions()), "--format=json")
        if failing != (nil != runErr) {
            t.Fatalf("failing=%v: unexpected run error %v; rendered %q", failing, runErr, rendered)
        }

        document := struct {
            Data struct {
                Details map[string]string `json:"details"`
            } `json:"data"`
        }{}
        if decodeErr := json.Unmarshal([]byte(rendered), &document); nil != decodeErr {
            t.Fatalf("failing=%v: failed to decode the document: %v; rendered %q", failing, decodeErr, rendered)
        }

        if "1" != document.Data.Details["group"] {
            t.Fatalf("failing=%v: expected details.group to be the bare id, got %q in %q", failing, document.Data.Details["group"], rendered)
        }
    }
}

/* a migration whose Up ran and whose mark failed is reported apart, as ran but not recorded, so the operator is not steered to run it again; an Up failure keeps the report of the migrations before it alone */
func TestMigrateCommand_AMarkFailureReportsTheLastMigrationAsRanNotRecorded(t *testing.T) {
    for _, markFails := range []bool{true, false} {
        database, recorder := newFakeBunDatabase()
        recorder.queryHook = appliedMigrationRowsHook()
        recorder.execHook = func(query string) error {
            if true == markFails && true == strings.HasPrefix(query, "INSERT") && true == strings.Contains(query, "bun_migrations") && true == strings.Contains(query, "20240103000000") {
                return errors.New("Error 1205 (HY000): Lock wait timeout exceeded")
            }

            return nil
        }

        upCalls := 0
        migrations := migrate.NewMigrations()
        for _, name := range []string{"20240101000000", "20240102000000", "20240103000000"} {
            migrations.Add(migrate.Migration{
                Name:    name,
                Comment: "step",
                Up: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
                    upCalls++
                    if false == markFails && "20240103000000" == migration.Name {
                        return errors.New("the third migration failed")
                    }

                    return nil
                },
            })
        }

        rendered, runErr := runMigrationCommand(t, newRuntimeWithDatabase(t, database), NewMigrateCommand(migrations, DefaultOptions()), "--format=json")
        if nil == runErr || 3 != upCalls {
            t.Fatalf("markFails=%v: expected the run to fail after three Ups, got %v and %d Ups", markFails, runErr, upCalls)
        }

        document := struct {
            Data struct {
                Migrations map[string][]string `json:"migrations"`
            } `json:"data"`
            Warnings []struct {
                Message string `json:"message"`
            } `json:"warnings"`
        }{}
        if decodeErr := json.Unmarshal([]byte(rendered), &document); nil != decodeErr {
            t.Fatalf("markFails=%v: failed to decode the document: %v; rendered %q", markFails, decodeErr, rendered)
        }

        if "[20240101000000 20240102000000]" != fmt.Sprint(document.Data.Migrations["applied"]) {
            t.Fatalf("markFails=%v: expected the two recorded migrations applied, got %v", markFails, document.Data.Migrations["applied"])
        }

        ranNotRecorded := fmt.Sprint(document.Data.Migrations["ranNotRecorded"])
        warned := false
        for _, warning := range document.Warnings {
            if true == strings.Contains(warning.Message, "ran but was not recorded") {
                warned = true
            }
        }

        if true == markFails && ("[20240103000000]" != ranNotRecorded || false == warned) {
            t.Fatalf("expected the third migration reported as ran but not recorded, got %s, warned=%v; rendered %q", ranNotRecorded, warned, rendered)
        }

        if false == markFails && ("[]" != ranNotRecorded || true == warned) {
            t.Fatalf("expected an Up failure reported without a ran-not-recorded migration, got %s, warned=%v", ranNotRecorded, warned)
        }
    }
}
