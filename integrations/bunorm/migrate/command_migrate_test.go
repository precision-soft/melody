package migrate

import (
    "context"
    "errors"
    "strings"
    "testing"

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

    if false == strings.Contains(runErr.Error(), "already locked") {
        t.Fatalf("error = %q, want the bun lock failure", runErr.Error())
    }

    if 0 != upCalls {
        t.Fatalf("migration ran despite the lock failure (%d times)", upCalls)
    }

    if 0 <= recorder.firstIndexMatching(isUnlockDelete) {
        t.Fatalf("a never-acquired lock was released: %v", recorder.recordedQueries())
    }

    /* the command no longer pre-prints the failure it returns: the cli runner's [error] line and the full log record already report it, and the third copy on the same console said nothing new */
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
