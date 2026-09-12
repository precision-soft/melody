package migrate

import (
    "encoding/json"
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

/* under --format=json the command writes the one machine-readable document the silenced banner promises — the flag was declared and validated here long before it was honoured. */
func TestStatusCommand_JsonFormatRendersOneMachineReadableDocument(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook("20240101000000")

    runtimeInstance := newRuntimeWithDatabase(t, database)

    migrations := migrate.NewMigrations()
    migrations.Add(migrate.Migration{Name: "20240101000000", Comment: "create_users"})
    migrations.Add(migrate.Migration{Name: "20240202000000", Comment: "create_orders"})

    command := NewStatusCommand(migrations, DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--format=json", "--verbosity=1")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    var document map[string]any
    if unmarshalErr := json.Unmarshal([]byte(rendered), &document); nil != unmarshalErr {
        t.Fatalf("the output is not one json document: %v; got %q", unmarshalErr, rendered)
    }

    meta, hasMeta := document["meta"].(map[string]any)
    if false == hasMeta {
        t.Fatalf("the document carries no meta, got %q", rendered)
    }

    if command.Name() != meta["command"] {
        t.Fatalf("meta.command = %v, want %q", meta["command"], command.Name())
    }

    if nil != document["error"] {
        t.Fatalf("expected no error in the document, got %v", document["error"])
    }
}

/* a failed run reports through the same document: the failure rides in the envelope's error, not as a raw text line that would corrupt the parse. */
func TestStatusCommand_JsonFormatReportsTheFailureInTheDocument(t *testing.T) {
    runtimeInstance := newRuntimeWithDatabase(t, nil)

    command := NewStatusCommand(migrate.NewMigrations(), Options{ManagerName: "missing", CommandPrefix: "db"})

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--format=json")
    if nil == runErr {
        t.Fatal("expected the unknown manager to fail the run")
    }

    var document map[string]any
    if unmarshalErr := json.Unmarshal([]byte(rendered), &document); nil != unmarshalErr {
        t.Fatalf("the failing output is not one json document: %v; got %q", unmarshalErr, rendered)
    }

    if nil == document["error"] {
        t.Fatalf("expected the failure inside the document, got %q", rendered)
    }
}
