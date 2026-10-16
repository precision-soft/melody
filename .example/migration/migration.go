package migration

import (
    "github.com/uptrace/bun/migrate"
)

/* Migrations is the single source of the catalog schema on mysql: the catalog repository providers run it at first resolution through EnsureMigrated, and the db:* command family runs the same set from the operator's side. Each set is a SINGLE migration holding the whole schema of its database, since the example has one state and a volume in an older shape is brought to it by example:db:reset. Every statement creates its table IF NOT EXISTS, for a volume provisioned before the set and for several processes applying it at once. The bun bookkeeping tables keep their default names in this major's own database, melody_example_v1: bun matches an applied migration by NAME, so three examples sharing one database would share one bun_migrations and the first set to land would answer for the others. */
var Migrations = migrate.NewMigrations()

/* JournalMigrations is the single source of the journal schema on postgres: the journal repository provider runs it at first resolution through EnsureJournalMigrated, and the db:journal:* command family runs the same set from the operator's side. The journal database is this major's own, melody_example_v1 on postgres, since bun keeps its bookkeeping per database and matches an applied migration by NAME, so a set sharing another major's database would skip its own step silently; the shared development database stays with the live integration suites. */
var JournalMigrations = migrate.NewMigrations()

/* SchemaTableNameList names every table this example's two sets own, catalog first and the journal last.
   It is the list the reset command shows an operator before it destroys anything, read from the schema
   itself rather than written a second time beside it. */
func SchemaTableNameList() []string {
    return append(append([]string{}, schemaTableNameList...), journalTableName)
}
