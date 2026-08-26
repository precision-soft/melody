package migration

import (
    "github.com/uptrace/bun/migrate"
)

/* Migrations is the single source of this example's schema on mysql. The five repository providers run it at first resolution through EnsureMigrated, and the db:* command family the bunorm/migrate module registers runs the same set from the operator's side, so neither door can drift from the other.

   Every up statement creates its table with IF NOT EXISTS: the tables may already exist on a volume provisioned before the migration set, and several processes of this example may apply the set at the same time. The bun bookkeeping tables keep their default names (bun_migrations and bun_migration_locks), because the module builds its migrator without the table-name options; they live in this example's own database, which no other major writes, so nothing else touches their rows. The identifiers are this set's own creation dates rather than the ones v1 carries, and they need not agree: each major holds its schema in a database of its own, so two sets never meet in one bun_migrations table.

   The journal table travels in this set instead of a context of its own, because this major keeps the journal on the same mysql connection as the catalogue, where v1 gives it a second database on postgres. */
var Migrations = migrate.NewMigrations()
