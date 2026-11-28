package migrate

import (
    "strings"
    "testing"

    "github.com/uptrace/bun/migrate"
)

func TestInitializeCommand_CreatesMigrationTables(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    command := NewInitializeCommand(migrate.NewMigrations(), resolvedDefaultOptions)

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if false == strings.Contains(rendered, "migrations tables initialized") {
        t.Fatalf("missing success message in %q", rendered)
    }

    migrationsTableIndex := recorder.firstIndexMatching(func(query string) bool {
        return strings.HasPrefix(query, "CREATE TABLE") && mentionsMigrationsTable(query)
    })
    locksTableIndex := recorder.firstIndexMatching(func(query string) bool {
        return strings.HasPrefix(query, "CREATE TABLE") && strings.Contains(query, "bun_migration_locks")
    })

    if 0 > migrationsTableIndex {
        t.Fatalf("migrations table was not created: %v", recorder.recordedQueries())
    }

    if 0 > locksTableIndex {
        t.Fatalf("locks table was not created: %v", recorder.recordedQueries())
    }
}

func TestInitializeCommand_UnknownManagerFails(t *testing.T) {
    database, _ := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    command := NewInitializeCommand(migrate.NewMigrations(), resolvedDefaultOptions)

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color", "--manager", "unknown")
    if nil == runErr {
        t.Fatal("expected an error for an unknown manager name")
    }

    if true == strings.Contains(rendered, "ERROR:") {
        t.Fatalf("the returned failure must not be pre-printed by the command, got: %q", rendered)
    }
}
