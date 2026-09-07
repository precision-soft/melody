package migration

import (
    "github.com/uptrace/bun/migrate"
)

/* Migrations is the single source of this example's schema on mysql. The five repository constructors and the two-factor build step run it at first use through EnsureMigrated, and the db:* command family the bunorm/migrate module registers runs the same set from the operator's side, so neither door can drift from the other.

   The set is a SINGLE migration holding the whole schema, because this application has no history: an example has one state, the present one, and the schema is the statement of that state rather than the record of how it got there. A volume left in an older shape is brought to the present one by example:db:reset, not by a step that repairs its past.

   Every statement of that migration tolerates a volume provisioned before the set, and several processes of this example applying it at the same time: the tables are created IF NOT EXISTS, and the constraint — MySQL having no ADD KEY IF NOT EXISTS — is added only after the catalogue is asked whether the key is already there. The bun bookkeeping tables keep their default names (bun_migrations and bun_migration_locks), because the module builds its migrator without the table-name options; they live in this example's own database, which no other major writes, so nothing else touches their rows. The identifier is the same on all three majors, which is harmless for the reason it was ever allowed to differ: each major holds its schema in a database of its own, so two sets never meet in one bun_migrations table.

   The set covers the six tables this example owns plus the one constraint it declares — a unique key on the folded spelling of a username, which is what holds a name against two callers that pass the repository's read-then-write check at the same moment. The tables are the four catalogue ones, the journal — which travels here rather than in a context of its own, because this major keeps it on the same mysql connection as the catalogue — and the two-factor enrollment table, which neither frozen major carries. It does not cover the tables the framework's own modules create: the outbox store and the audit registry each open their schema through the door of the module that owns it, and a set belonging to the application would claim tables it does not own. */
var Migrations = migrate.NewMigrations()

/* SchemaTableNameList names every table this example's set owns, in the order the schema drops them. It is
   the list the reset command shows an operator before it destroys anything, read from the schema itself
   rather than written a second time beside it. */
func SchemaTableNameList() []string {
    return append([]string{}, schemaTableNameList...)
}
