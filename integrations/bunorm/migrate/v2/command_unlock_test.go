package migrate

import (
    "strings"
    "testing"

    "github.com/uptrace/bun/migrate"
)

func TestUnlockCommand_DeletesMigrationLock(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    command := NewUnlockCommand(migrate.NewMigrations(), DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color", "--verbose")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if false == strings.Contains(rendered, "migrations table unlocked") {
        t.Fatalf("missing success message in %q", rendered)
    }

    if false == strings.Contains(rendered, "| status  | unlocked") {
        t.Fatalf("missing verbose status detail in %q", rendered)
    }

    if 0 > recorder.firstIndexMatching(isUnlockDelete) {
        t.Fatalf("no delete on the locks table was issued: %v", recorder.recordedQueries())
    }
}
