package main

import (
    "context"

    migrate "github.com/precision-soft/melody/integrations/bunorm/migrate/v3"
)

/* runMigrateCheck drives the bunorm migrate runner against live postgres: an up migration that creates
and seeds a table, a read-back to prove it landed, then a down migration that rolls it away — the real
schema mutation a fake database cannot exercise. */
func runMigrateCheck(dsn string) {
    ctx := context.Background()

    database := openPostgres(dsn)
    defer database.Close()

    const table = "melody_e2e_migrate"

    up := []migrate.Query{
        {Name: "drop-if-exists", SQL: "DROP TABLE IF EXISTS " + table},
        {Name: "create-table", SQL: "CREATE TABLE " + table + " (id BIGINT PRIMARY KEY, label TEXT NOT NULL)"},
        {Name: "seed-row", SQL: "INSERT INTO " + table + " (id, label) VALUES (1, 'migrated')"},
    }

    if upErr := migrate.Up(ctx, database, "e2e_create_"+table, up); nil != upErr {
        fail("migrate: up: %v", upErr)
    }

    var label string
    scanErr := database.QueryRowContext(ctx, "SELECT label FROM "+table+" WHERE id = 1").Scan(&label)
    if nil != scanErr {
        fail("migrate: verify up: %v", scanErr)
    }
    if "migrated" != label {
        fail("migrate: seeded row wrong — wanted 'migrated', got %q", label)
    }
    pass("migrate up created and seeded %q on live postgres", table)

    down := []migrate.Query{
        {Name: "drop-table", SQL: "DROP TABLE " + table},
    }

    if downErr := migrate.Down(ctx, database, "e2e_drop_"+table, down); nil != downErr {
        fail("migrate: down: %v", downErr)
    }

    /* table is a compile-time constant, so inline it — the bun pgdriver binds ? not $1 placeholders */
    var stillExists bool
    existsErr := database.QueryRowContext(
        ctx,
        "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = '"+table+"')",
    ).Scan(&stillExists)
    if nil != existsErr {
        fail("migrate: verify down: %v", existsErr)
    }
    if true == stillExists {
        fail("migrate: table %q still present after the down migration", table)
    }
    pass("migrate down rolled the schema back (table dropped)")
}
