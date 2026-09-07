package migration

import (
    "github.com/uptrace/bun/migrate"
)

/* Migrations is the single source of the catalog schema on mysql. The four catalog repository providers run it at first resolution through EnsureMigrated, and the db:* command family the bunorm/migrate module registers runs the same set from the operator's side, so neither door can drift from the other.

   Each of the two sets is a SINGLE migration holding the whole schema of its database, because this application has no history: an example has one state, the present one, and the schema is the statement of that state rather than the record of how it got there. A volume left in an older shape is brought to the present one by example:db:reset, not by a step that repairs its past.

   Every statement of the catalog migration creates its table with IF NOT EXISTS: the tables may already exist on a volume provisioned before the migration set, and several processes of this example may apply the set at the same time. The bun bookkeeping tables keep their default names (bun_migrations and bun_migration_locks); they live in this example's own catalog database, melody_example_v1, which only this set writes.

   That database is this major's alone. The three examples share the development mysql and not a database in it, because bun matches an applied migration by NAME and the migrate module builds its migrator on the default bookkeeping tables: on one shared database the three sets would share one bun_migrations, and the first to land would answer for the others. */
var Migrations = migrate.NewMigrations()

/* JournalMigrations is the single source of the journal schema on postgres. The journal repository provider runs it at first resolution through EnsureJournalMigrated, and the db:journal:* command family runs the same set from the operator's side.

   The journal database is the shared development one (melody_test), which other harness probes also write — each with tables of its own. The bun bookkeeping tables keep their default names there too: bookkeeping is per database, and within melody_test only this set uses bun's migrator, so nothing else touches its rows. */
var JournalMigrations = migrate.NewMigrations()

/* SchemaTableNameList names every table this example's two sets own, catalog first and the journal last.
   It is the list the reset command shows an operator before it destroys anything, read from the schema
   itself rather than written a second time beside it. */
func SchemaTableNameList() []string {
    return append(append([]string{}, schemaTableNameList...), journalTableName)
}
