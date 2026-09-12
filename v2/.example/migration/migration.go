package migration

import (
    "github.com/uptrace/bun/migrate"
)

/* Migrations is the single source of this example's schema on mysql. The five repository providers run it at first resolution through EnsureMigrated, and the db:* command family the bunorm/migrate module registers runs the same set from the operator's side, so neither door can drift from the other.

   The set is a SINGLE migration holding the whole schema, because this application has no history: an example has one state, the present one, and the schema is the statement of that state rather than the record of how it got there. A volume left in an older shape is brought to the present one by example:db:reset, not by a step that repairs its past.

   Every statement of that migration creates its table with IF NOT EXISTS: the tables may already exist on a volume provisioned before the migration set, and several processes of this example may apply the set at the same time. The bun bookkeeping tables keep their default names (bun_migrations and bun_migration_locks), because the module builds its migrator without the table-name options; they live in this example's own database, which no other major writes, so nothing else touches their rows. The identifier is the same on all three majors, which is harmless for the reason it was ever allowed to differ: each major holds its schema in a database of its own, so two sets never meet in one bun_migrations table.

   The journal table travels in this set instead of a context of its own, because this major keeps the journal on the same mysql connection as the catalogue, where v1 gives it a second database on postgres. */
var Migrations = migrate.NewMigrations()

/* SchemaTableNameList names every table this example's set owns, in the order the schema drops them. It is
   the list the reset command shows an operator before it destroys anything, read from the schema itself
   rather than written a second time beside it. */
func SchemaTableNameList() []string {
    return append([]string{}, schemaTableNameList...)
}
