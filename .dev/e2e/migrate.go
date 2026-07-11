package main

import (
    "context"

    migrate "github.com/precision-soft/melody/integrations/bunorm/migrate/v3"
    bunmigrate "github.com/uptrace/bun/migrate"
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

    runMigrateContextCheck()
}

/* runMigrateContextCheck asserts a multi-database binary gets one command family per migration context: each
context carries its own command prefix, so db:crm:migrate and db:billing:migrate are distinct commands that
can never be confused for one another, and a base prefix handed to RegisterContextCommands is honoured.
The manager pin each family carries is covered by the package's own tests; what only shows up from the
outside is the command surface a multi-database application actually exposes. */
func runMigrateContextCheck() {
    contexts := []migrate.ContextConfig{
        {Name: "crm", Migrations: bunmigrate.NewMigrations()},
        {Name: "billing", Migrations: bunmigrate.NewMigrations(), Options: migrate.Options{ManagerName: "billing-replica"}},
    }

    commands := migrate.RegisterContextCommands(contexts, migrate.Options{})

    names := make(map[string]bool, len(commands))
    for _, command := range commands {
        if true == names[command.Name()] {
            fail("migrate contexts: command %q was registered twice across contexts", command.Name())
        }
        names[command.Name()] = true
    }

    wantedNames := []string{
        "db:crm:migrate", "db:crm:rollback", "db:crm:status",
        "db:billing:migrate", "db:billing:rollback", "db:billing:status",
    }
    for _, wanted := range wantedNames {
        if false == names[wanted] {
            fail("migrate contexts: command %q was not registered", wanted)
        }
    }
    pass("migrate registered one distinct command family per context (%d commands)", len(commands))

    /* a base prefix handed to RegisterContextCommands carries into every context's family */
    prefixed := migrate.RegisterContextCommands(contexts, migrate.Options{CommandPrefix: "schema"})

    prefixedNames := make(map[string]bool, len(prefixed))
    for _, command := range prefixed {
        prefixedNames[command.Name()] = true
    }

    if false == prefixedNames["schema:crm:migrate"] || false == prefixedNames["schema:billing:migrate"] {
        fail("migrate contexts: the base command prefix did not carry into the per-context families")
    }
    pass("migrate honoured the base command prefix across contexts (schema:<context>:migrate)")
}
