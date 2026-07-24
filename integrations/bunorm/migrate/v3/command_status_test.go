package migrate

import (
    "strings"
    "testing"

    "github.com/uptrace/bun/migrate"
)

func TestStatusCommand_ListsAppliedAndPendingMigrations(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook("20240101000000")

    runtimeInstance := newRuntimeWithDatabase(t, database)

    migrations := migrate.NewMigrations()
    migrations.Add(migrate.Migration{Name: "20240101000000", Comment: "create_users"})
    migrations.Add(migrate.Migration{Name: "20240202000000", Comment: "create_orders"})

    command := NewStatusCommand(migrations, DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if false == strings.Contains(rendered, "| manager | <default>") {
        t.Fatalf("missing manager detail in %q", rendered)
    }

    if false == strings.Contains(rendered, "| applied | 1") {
        t.Fatalf("missing applied count in %q", rendered)
    }

    if false == strings.Contains(rendered, "| status  | 1 pending") {
        t.Fatalf("missing pending count in %q", rendered)
    }

    appliedBlockIndex := strings.Index(rendered, "APPLIED")
    pendingBlockIndex := strings.Index(rendered, "PENDING")

    if 0 > appliedBlockIndex || 0 > pendingBlockIndex {
        t.Fatalf("missing APPLIED/PENDING blocks in %q", rendered)
    }

    if false == strings.Contains(rendered[appliedBlockIndex:pendingBlockIndex], "20240101000000") {
        t.Fatalf("applied migration not listed in the APPLIED block: %q", rendered)
    }

    if false == strings.Contains(rendered[pendingBlockIndex:], "20240202000000") {
        t.Fatalf("pending migration not listed in the PENDING block: %q", rendered)
    }
}

func TestStatusCommand_NoMigrationsWarns(t *testing.T) {
    database, _ := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    command := NewStatusCommand(migrate.NewMigrations(), DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if false == strings.Contains(rendered, "WARNING: no migrations found") {
        t.Fatalf("missing warning in %q", rendered)
    }
}
