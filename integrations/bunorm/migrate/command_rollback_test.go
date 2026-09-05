package migrate

import (
    "bytes"
    "context"
    "errors"
    "io"
    "strings"
    "testing"

    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/uptrace/bun/migrate"
)

func TestRollbackCommand_RollsBackLastGroupUnderLock(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook("20240101000000")

    runtimeInstance := newRuntimeWithDatabase(t, database)

    downCalls := 0
    migrations := newSingleMigrationSet("20240101000000", "create_users", nil, &downCalls)

    command := NewRollbackCommand(migrations, DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if 1 != downCalls {
        t.Fatalf("expected the down migration to run once, ran %d times", downCalls)
    }

    if false == strings.Contains(rendered, "migrations rolled back successfully") {
        t.Fatalf("missing success message in %q", rendered)
    }

    lockIndex := recorder.firstIndexMatching(isLockInsert)
    markUnappliedIndex := recorder.firstIndexMatching(func(query string) bool {
        return strings.HasPrefix(query, "DELETE") &&
            strings.Contains(query, "bun_migrations") &&
            false == strings.Contains(query, "bun_migration_locks")
    })
    unlockIndex := recorder.firstIndexMatching(isUnlockDelete)

    if 0 > lockIndex || 0 > markUnappliedIndex || 0 > unlockIndex {
        t.Fatalf("missing expected statements: %v", recorder.recordedQueries())
    }

    if false == (lockIndex < markUnappliedIndex && markUnappliedIndex < unlockIndex) {
        t.Fatalf("rollback did not run inside the migration lock: %v", recorder.recordedQueries())
    }
}

/* a rollback that fails part way names the group it was walking on the way out: bun hands the group back beside the failure, and reporting nothing left the operator with only the failing migration — which schema changes were already undone could only be reconstructed from the migrations table by hand */
func TestRollbackCommand_AFailedRollbackNamesTheGroupItWasWalking(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook("20240101000000")

    runtimeInstance := newRuntimeWithDatabase(t, database)

    migrations := migrate.NewMigrations()
    migrations.Add(migrate.Migration{
        Name:    "20240101000000",
        Comment: "create_users",
        Up: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
            return nil
        },
        Down: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
            return errors.New("down refused")
        },
    })

    command := NewRollbackCommand(migrations, DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil == runErr {
        t.Fatalf("expected the failing down to fail the command")
    }

    if false == strings.Contains(rendered, "ROLLBACK GROUP MIGRATIONS") {
        t.Fatalf("expected the report to name the group the failed rollback was walking, got %q", rendered)
    }

    if false == strings.Contains(rendered, "20240101000000") {
        t.Fatalf("expected the group's migration named, got %q", rendered)
    }
}

func TestRollbackCommand_NoMigrationsWarns(t *testing.T) {
    database, _ := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    downCalls := 0
    migrations := newSingleMigrationSet("20240101000000", "create_users", nil, &downCalls)

    command := NewRollbackCommand(migrations, DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if 0 != downCalls {
        t.Fatalf("a never-applied migration was rolled back (%d times)", downCalls)
    }

    if false == strings.Contains(rendered, "WARNING: no migrations to rollback") {
        t.Fatalf("missing warning in %q", rendered)
    }
}

func TestRollbackCommand_FailedUnlockFailsTheCommand(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook("20240101000000")
    recorder.execHook = func(query string) error {
        if true == isUnlockDelete(query) {
            return context.DeadlineExceeded
        }

        return nil
    }

    runtimeInstance := newRuntimeWithDatabase(t, database)

    downCalls := 0
    migrations := newSingleMigrationSet("20240101000000", "create_users", nil, &downCalls)

    command := NewRollbackCommand(migrations, DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil == runErr {
        t.Fatal("expected the failed unlock to fail the command")
    }

    if 1 != downCalls {
        t.Fatalf("expected the rollback itself to have run once, ran %d times", downCalls)
    }

    if false == strings.Contains(rendered, "ERROR:") {
        t.Fatalf("unlock failure was not printed beside the exit code: %q", rendered)
    }
}

/* the same remedy-naming refusal the migrate sibling proves: bun's bare lock error names neither the database nor db:unlock, and rollback used to return it as it came */
func TestRollbackCommand_LockFailureNamesTheRemedy(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.execHook = func(query string) error {
        if true == isLockInsert(query) {
            return context.DeadlineExceeded
        }

        return nil
    }

    runtimeInstance := newRuntimeWithDatabase(t, database)

    downCalls := 0
    migrations := newSingleMigrationSet("20240101000000", "create_users", nil, &downCalls)

    command := NewRollbackCommand(migrations, DefaultOptions())

    _, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil == runErr {
        t.Fatal("expected an error when the rollback lock cannot be taken")
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

    if 0 != downCalls {
        t.Fatalf("rollback ran despite the lock failure (%d times)", downCalls)
    }
}

/* the rollback command hands its posture to the migrations the same way the migrate command does: through the context, with the process-wide fallback installed only for the length of the run */
func TestRollbackCommand_HandsItsPostureToTheMigrationsThroughTheContext(t *testing.T) {
    t.Cleanup(func() {
        processRunnerOption.Store(nil)
    })

    var elsewhere bytes.Buffer
    SetDefaultRunnerOption(RunnerOption{Writer: &elsewhere, NoColor: true})

    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook("20240101000000")
    runtimeInstance := newRuntimeWithDatabase(t, database)

    var seenDuringTheRun io.Writer
    carriedByTheContext := false
    migrations := migrate.NewMigrations()
    migrations.Add(migrate.Migration{
        Name:    "20240101000000",
        Comment: "create_users",
        Up: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
            return nil
        },
        Down: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
            seenDuringTheRun = resolveDefaultRunnerOption().Writer
            _, carriedByTheContext = runnerOptionFromContext(ctx)

            return RunQueries(ctx, database, "down", "20240101000000", []Query{{Name: "drop-users", SQL: "DROP TABLE users"}})
        },
    })

    rendered, runErr := runMigrationCommand(t, runtimeInstance, NewRollbackCommand(migrations, DefaultOptions()), "--no-color")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if false == strings.Contains(rendered, "[migration:down] 20240101000000 [1/1] executing: drop-users") {
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
