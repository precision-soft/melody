package migrate

import (
    "bytes"
    "context"
    "database/sql/driver"
    "encoding/json"
    "errors"
    "fmt"
    "github.com/precision-soft/melody/v4/bound"
    "strings"
    "testing"
    "time"

    melodycli "github.com/precision-soft/melody/v4/cli"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/uptrace/bun/migrate"
)

func TestMigrateCommand_TakesLockBeforeMigratingAndReleasesIt(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    upCalls := 0
    migrations := newSingleMigrationSet("20240101000000", "create_users", &upCalls, nil)

    command := NewMigrateCommand(migrations, resolvedDefaultOptions)

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
            mentionsMigrationsTable(query) &&
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

    command := NewMigrateCommand(migrations, resolvedDefaultOptions)

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
        "manager":       "<default> (dedicated migration connection)",
        "locksTable":    DefaultLocksTable,
        "unlockCommand": DefaultCommandPrefix + ":unlock",
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

    command := NewMigrateCommand(migrations, resolvedDefaultOptions)

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

    command := NewMigrateCommand(migrations, resolvedDefaultOptions)

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

    command := NewMigrateCommand(migrations, resolvedDefaultOptions)

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

func TestMigrateCommand_JsonCarriesTheDetailAtTheDefaultVerbosity(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook()

    runtimeInstance := newRuntimeWithDatabase(t, database)

    migrations := migrate.NewMigrations()
    migrations.Add(migrate.Migration{Name: "20240101000000", Comment: "create_users"})

    rendered, runErr := runMigrationCommand(t, runtimeInstance, NewMigrateCommand(migrations, resolvedDefaultOptions), "--format=json")
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

    textDatabase, textRecorder := newFakeBunDatabase()
    textRecorder.queryHook = appliedMigrationRowsHook()

    textRendered, textErr := runMigrationCommand(
        t,
        newRuntimeWithDatabase(t, textDatabase),
        NewMigrateCommand(migrations, resolvedDefaultOptions),
        "--no-color",
    )
    if nil != textErr {
        t.Fatalf("unexpected error: %s", textErr.Error())
    }

    if true == strings.Contains(textRendered, "APPLIED MIGRATIONS") {
        t.Fatalf("expected the bare text run to stay bare, got %q", textRendered)
    }
}

func TestMigrateCommand_ARunThatChangedTheSchemaSaysSoOnTheText(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook()

    migrations := migrate.NewMigrations()
    migrations.Add(migrate.Migration{Name: "20240101000000", Comment: "create_users"})

    rendered, runErr := runMigrationCommand(
        t,
        newRuntimeWithDatabase(t, database),
        NewMigrateCommand(migrations, resolvedDefaultOptions),
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

func TestMigrateCommand_TheSuccessLineDoesNotEnterTheMachineDocument(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook()

    migrations := migrate.NewMigrations()
    migrations.Add(migrate.Migration{Name: "20240101000000", Comment: "create_users"})

    rendered, runErr := runMigrationCommand(
        t,
        newRuntimeWithDatabase(t, database),
        NewMigrateCommand(migrations, resolvedDefaultOptions),
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
        NewMigrateCommand(migrations, resolvedDefaultOptions),
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
        NewMigrateCommand(migrations, resolvedDefaultOptions),
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
        NewMigrateCommand(migrations, resolvedDefaultOptions),
        "--no-color",
        "--verbose",
    )

    if nil == runErr {
        t.Fatal("expected the broken migration to fail the command")
    }

    if true == strings.Contains(rendered, "APPLIED MIGRATIONS") {
        t.Fatalf("a run that landed nothing must claim no applied migrations, got: %q", rendered)
    }

    if true == strings.Contains(rendered, "DETAILS") {
        t.Fatalf("a run that landed nothing must print no applied-group detail block, got: %q", rendered)
    }
}

func TestMigrateCommand_TheRunnersOwnProgressStaysOutOfTheMachineDocument(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook()

    runtimeInstance := newRuntimeWithDatabase(t, database)

    migrations := migrate.NewMigrations()
    migrations.Add(migrate.Migration{
        Name:    "20240101000000",
        Comment: "create_users",
        Up: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
            return RunQueries(
                ctx,
                database,
                "up",
                "20240101000000",
                []Query{{Name: "create-users", Sql: "CREATE TABLE users (id BIGINT)"}}, RunnerOptions{},
            )
        },
    })

    rendered, runErr := runMigrationCommand(
        t,
        runtimeInstance,
        NewMigrateCommand(migrations, resolvedDefaultOptions),
        "--format=json",
    )
    if nil != runErr {
        t.Fatalf("unexpected error: %s; rendered %q", runErr.Error(), rendered)
    }

    if true == strings.Contains(rendered, "[migration:up]") {
        t.Fatalf("the runner's per-query progress reached the machine document: %q", rendered)
    }

    var document map[string]any
    if unmarshalErr := json.Unmarshal([]byte(rendered), &document); nil != unmarshalErr {
        t.Fatalf("the output is not one json document: %v; got %q", unmarshalErr, rendered)
    }
}

func TestMigrateCommand_HandsItsPostureToTheMigrationsThroughTheContext(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    carriedByTheContext := false
    migrations := migrate.NewMigrations()
    migrations.Add(migrate.Migration{
        Name:    "20240101000000",
        Comment: "create_users",
        Up: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
            _, carriedByTheContext = runnerOptionsFromContext(ctx)

            return RunQueries(ctx, database, "up", "20240101000000", []Query{{Name: "create-users", Sql: "CREATE TABLE users (id BIGINT)"}}, RunnerOptions{})
        },
    })

    rendered, runErr := runMigrationCommand(t, runtimeInstance, NewMigrateCommand(migrations, resolvedDefaultOptions), "--no-color")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if false == strings.Contains(rendered, "[migration:up] 20240101000000 [1/1] executing: create-users") {
        t.Fatalf("the per-query line did not reach the command's writer through the context: %q", rendered)
    }

    if false == carriedByTheContext {
        t.Fatal("the migration was not handed the context carrying the command's posture")
    }
}

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

func TestMigrateCommand_AReportCutShortIsRefusedInsteadOfExitingZero(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    migrations := migrate.NewMigrations()
    migrations.Add(migrate.Migration{
        Name:    "20240101000000",
        Comment: "create_users",
        Up: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
            return RunQueries(ctx, database, "up", "20240101000000", []Query{{Name: "create-users", Sql: "CREATE TABLE users (id BIGINT)"}}, RunnerOptions{})
        },
    })

    banner := &failingOnWriter{marker: "applied 1 migration"}
    runErr := runMigrationCommandTo(t, banner, runtimeInstance, NewMigrateCommand(migrations, resolvedDefaultOptions), "--no-color")
    if nil == runErr || false == strings.Contains(runErr.Error(), "could not be written in full") {
        t.Fatalf("expected the truncated text report refused, got %v (written: %q)", runErr, banner.buffer.String())
    }

    var reported *exception.Error
    if false == errors.As(runErr, &reported) || nil == reported.CauseErr() || false == strings.Contains(reported.CauseErr().Error(), "broken pipe") {
        t.Fatalf("expected the refusal to carry the lost write, got %v", runErr)
    }

    queryLine := &failingOnWriter{marker: "executing:"}
    runErr = runMigrationCommandTo(t, queryLine, runtimeInstance, NewMigrateCommand(migrations, resolvedDefaultOptions), "--no-color")
    if nil == runErr || false == strings.Contains(runErr.Error(), "could not be written in full") {
        t.Fatalf("expected the report cut on the runner's line refused, got %v (written: %q)", runErr, queryLine.buffer.String())
    }

    document := &failingOnWriter{marker: "\"meta\""}
    runErr = runMigrationCommandTo(t, document, runtimeInstance, NewMigrateCommand(migrations, resolvedDefaultOptions), "--format=json")
    if nil == runErr || false == strings.Contains(runErr.Error(), "could not be written in full") {
        t.Fatalf("expected the truncated json document refused, got %v (written: %q)", runErr, document.buffer.String())
    }

    var whole bytes.Buffer
    if runErr = runMigrationCommandTo(t, &whole, runtimeInstance, NewMigrateCommand(migrations, resolvedDefaultOptions), "--no-color"); nil != runErr {
        t.Fatalf("expected a report written whole to pass, got %v", runErr)
    }
}

func TestMigrationCommands_APanicKeepsPrecedenceOverAFailedUnlock(t *testing.T) {
    for _, rollback := range []bool{false, true} {
        name := "migrate"
        if true == rollback {
            name = "rollback"
        }

        t.Run(name, func(t *testing.T) {
            database, recorder := newFakeBunDatabase()
            recorder.execHook = func(query string) error {
                if true == isUnlockDelete(query) {
                    return errors.New("unlock failed after panic")
                }

                return nil
            }
            if true == rollback {
                recorder.queryHook = appliedMigrationRowsHook("20240101000000")
            }

            panicValue := errors.New("migration body panicked")
            panicking := func(context.Context, *migrate.Migrator, *migrate.Migration) error {
                panic(panicValue)
            }
            migrations := migrate.NewMigrations()
            migrations.Add(migrate.Migration{Name: "20240101000000", Comment: "panic_probe", Up: panicking, Down: panicking})

            var command clicontract.Command = NewMigrateCommand(migrations, resolvedDefaultOptions)
            if true == rollback {
                command = NewRollbackCommand(migrations, resolvedDefaultOptions)
            }

            runtimeInstance := newRuntimeWithDatabase(t, database)
            buffer := &bytes.Buffer{}

            var recovered any
            func() {
                defer func() { recovered = recover() }()

                if dispatchErr := melodycli.DispatchCommand(context.Background(), command, runtimeInstance, []string{command.Name(), "--format=json"}, buffer, melodycli.DispatchOptions{}); nil != dispatchErr {
                    t.Errorf("expected the command to panic before it returned, got %v", dispatchErr)
                }
            }()

            if panicValue != recovered {
                t.Fatalf("expected the migration's own panic to reach the caller, got %v", recovered)
            }

            var document map[string]any
            if unmarshalErr := json.Unmarshal(buffer.Bytes(), &document); nil != unmarshalErr {
                t.Fatalf("expected one json document, got %v: %s", unmarshalErr, buffer.String())
            }

            rendered := buffer.String()
            if false == strings.Contains(rendered, command.Name()+" panicked") || false == strings.Contains(rendered, panicValue.Error()) {
                t.Fatalf("expected the document to name the panic, got %s", rendered)
            }
            if false == strings.Contains(rendered, "unlock failed after panic") || 0 > recorder.firstIndexMatching(isUnlockDelete) {
                t.Fatalf("expected the unlock failure kept beside the panic, got %s", rendered)
            }
        })
    }
}

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

    _, runErr := runMigrationCommand(t, newRuntimeWithDatabase(t, database), NewMigrateCommand(newSingleMigrationSet("20240101000000", "create_users", new(int), nil), resolvedDefaultOptions), "--no-color")
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

    _, runErr := runMigrationCommand(t, newRuntimeWithDatabase(t, database), NewMigrateCommand(newSingleMigrationSet("20240101000000", "create_users", new(int), nil), resolvedDefaultOptions), "--no-color")
    if nil == runErr || false == errors.Is(runErr, refused) || true == strings.Contains(runErr.Error(), "is held") {
        t.Fatalf("expected the lock refusal without the held text, got %v", runErr)
    }

    if _, namesUnlock := lockRefusalContextOf(t, runErr)["unlockCommand"]; true == namesUnlock {
        t.Fatal("expected no unlock command named when no lock is held")
    }
}

func TestLockRefusal_ACancelledContextSaysCancelled(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = lockCountHook(1)

    cancelled, cancel := context.WithCancel(context.Background())
    cancel()

    refusal := lockRefusal(cancelled, database, context.Canceled, "primary", DefaultLocksTable, "db:unlock")
    if false == strings.Contains(refusal.Error(), "cancelled") || false == errors.Is(refusal, context.Canceled) {
        t.Fatalf("expected the cancelled refusal, got %v", refusal)
    }

    if 0 <= recorder.firstIndexMatching(func(query string) bool { return true == strings.Contains(query, "bun_migration_locks") }) {
        t.Fatalf("expected the locks table not asked under a cancelled context: %v", recorder.recordedQueries())
    }
}

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

func TestMigrateCommand_AnEmptyMigrationIsWarnedInTheJsonDocument(t *testing.T) {
    for _, format := range []string{"--format=json", "--no-color"} {
        database, recorder := newFakeBunDatabase()
        recorder.queryHook = appliedMigrationRowsHook()

        migrations := migrate.NewMigrations()
        migrations.Add(migrate.Migration{
            Name:    "20240101000000",
            Comment: "built_nothing",
            Up: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
                return RunQueries(ctx, migrator.DB(), "up", migration.Name, nil, RunnerOptions{})
            },
        })

        rendered, runErr := runMigrationCommand(t, newRuntimeWithDatabase(t, database), NewMigrateCommand(migrations, resolvedDefaultOptions), format)
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

        rendered, runErr := runMigrationCommand(t, newRuntimeWithDatabase(t, database), NewMigrateCommand(migrations, resolvedDefaultOptions), "--format=json")
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

func TestMigrateCommand_AMarkFailureReportsTheLastMigrationAsRanNotRecorded(t *testing.T) {
    for _, markFails := range []bool{true, false} {
        database, recorder := newFakeBunDatabase()
        recorder.queryHook = appliedMigrationRowsHook()
        recorder.execHook = func(query string) error {
            if true == markFails && true == strings.HasPrefix(query, "INSERT") && true == mentionsMigrationsTable(query) && true == strings.Contains(query, "20240103000000") {
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

        rendered, runErr := runMigrationCommand(t, newRuntimeWithDatabase(t, database), NewMigrateCommand(migrations, resolvedDefaultOptions), "--format=json")
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

        markerWritten := 0 <= recorder.firstIndexMatching(func(query string) bool {
            return true == isLockInsert(query) && true == strings.Contains(query, "ranNotRecorded:bun_migrations:20240103000000")
        })
        if markFails != markerWritten {
            t.Fatalf("markFails=%v: expected the marker written only for a mark failure, got %v in %v", markFails, markerWritten, recorder.recordedQueries())
        }
    }
}

func TestWriteRanNotRecordedMarker_WritesOnACancelledCommandContext(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    markerContextErr := errors.New("the marker insert never reached the driver")
    markerHasDeadline := false
    recorder.execContextHook = func(ctx context.Context, query string) {
        if true == isLockInsert(query) && true == strings.Contains(query, "ranNotRecorded:bun_migrations:20240103000000") {
            markerContextErr = ctx.Err()
            _, markerHasDeadline = ctx.Deadline()
        }
    }

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    outputInstance, buffer := newBufferedOutput(true)
    writeRanNotRecordedMarker(cancelledContext, database, migrationTables{migrations: "bun_migrations", locks: DefaultLocksTable}, "20240103000000", outputInstance, commandConfigUnlockTimeoutRule.Default)

    if nil != markerContextErr || false == markerHasDeadline {
        t.Fatalf("expected the marker written on a live context with a deadline, got %v, deadline=%v; output %q", markerContextErr, markerHasDeadline, buffer.String())
    }

    if true == strings.Contains(buffer.String(), "could not be written") {
        t.Fatalf("expected no warning for a written marker, got %q", buffer.String())
    }
}

func TestMigrateCommand_AStandingRanNotRecordedMarkerRefusesTheRunUntilForce(t *testing.T) {
    for _, force := range []bool{false, true} {
        database, recorder := newFakeBunDatabase()
        recorder.queryHook = func(query string) ([]string, [][]driver.Value, error) {
            if true == strings.HasPrefix(query, "SELECT") && true == strings.Contains(query, "ranNotRecorded:") {
                return []string{"table_name"}, [][]driver.Value{{"ranNotRecorded:bun_migrations:20240101000000"}, {"ranNotRecorded:bun_migrations_other:20240101000000"}}, nil
            }

            return appliedMigrationRowsHook()(query)
        }

        upCalls := 0
        migrations := newSingleMigrationSet("20240101000000", "create_users", &upCalls, nil)

        arguments := []string{"--no-color"}
        if true == force {
            arguments = append(arguments, "--"+MigrateForceFlagName)
        }

        _, runErr := runMigrationCommand(t, newRuntimeWithDatabase(t, database), NewMigrateCommand(migrations, resolvedDefaultOptions), arguments...)

        markerDeleted := 0 <= recorder.firstIndexMatching(func(query string) bool {
            return true == strings.HasPrefix(query, "DELETE") && true == strings.Contains(query, "'ranNotRecorded:bun_migrations:20240101000000'") && false == strings.Contains(query, "bun_migrations_other")
        })

        if false == force {
            var exceptionErr *exception.Error
            if nil == runErr || 0 != upCalls || true == markerDeleted || false == errors.As(runErr, &exceptionErr) || MigrateForceFlagName != exceptionErr.Context()["flag"] || "[20240101000000]" != fmt.Sprint(exceptionErr.Context()["migrations"]) {
                t.Fatalf("expected the run refused before any Up naming the migration and the flag, got %v, %d Ups, deleted=%v", runErr, upCalls, markerDeleted)
            }

            if 0 > recorder.firstIndexMatching(isUnlockDelete) {
                t.Fatalf("expected the lock released after the refusal: %v", recorder.recordedQueries())
            }

            continue
        }

        if nil != runErr || 1 != upCalls || false == markerDeleted {
            t.Fatalf("expected --force to clear the marker and run, got %v, %d Ups, deleted=%v in %v", runErr, upCalls, markerDeleted, recorder.recordedQueries())
        }
    }
}

func TestMigrateCommand_ReleasesTheLockUnderItsUnlockTimeout(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    remaining := unlockDeadlineRecorder(recorder)
    runtimeInstance := newRuntimeWithDatabase(t, database)

    upCalls := 0
    migrations := newSingleMigrationSet("20240101000000", "create_users", &upCalls, nil)

    options := resolvedDefaultOptions
    options.UnlockTimeout = bound.Of(2 * time.Second)

    if _, runErr := runMigrationCommand(t, runtimeInstance, NewMigrateCommand(migrations, options), "--no-color"); nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if *remaining <= time.Second || *remaining > 2*time.Second {
        t.Fatalf("expected the unlock to run under the 2s unlock timeout, got %s left", *remaining)
    }
}
