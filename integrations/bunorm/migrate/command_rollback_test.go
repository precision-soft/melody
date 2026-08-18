package migrate

import (
    "context"
    "errors"
    "strings"
    "testing"

    exceptioncontract "github.com/precision-soft/melody/exception/contract"
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
