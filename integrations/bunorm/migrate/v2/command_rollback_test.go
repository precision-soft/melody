package migrate

import (
    "strings"
    "testing"
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
