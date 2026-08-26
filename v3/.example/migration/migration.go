package migration

import (
    "github.com/uptrace/bun/migrate"
)

/* Migrations is the single source of this example's schema on mysql. The five repository constructors and the two-factor build step run it at first use through EnsureMigrated, and the db:* command family the bunorm/migrate module registers runs the same set from the operator's side, so neither door can drift from the other.

   Every up statement creates its table with IF NOT EXISTS: the tables may already exist on a volume provisioned before the migration set, and several processes of this example may apply the set at the same time. The bun bookkeeping tables keep their default names (bun_migrations and bun_migration_locks), because the module builds its migrator without the table-name options; they live in this example's own database, which no other major writes, so nothing else touches their rows. The identifiers are this set's own creation dates rather than the ones v1 and v2 carry, and they need not agree: each major holds its schema in a database of its own, so two sets never meet in one bun_migrations table.

   The set covers the six tables this example owns: the four catalogue ones, the journal — which travels here rather than in a context of its own, because this major keeps it on the same mysql connection as the catalogue — and the two-factor enrollment table, which neither frozen major carries. It does not cover the tables the framework's own modules create: the outbox store and the audit registry each open their schema through the door of the module that owns it, and a set belonging to the application would claim tables it does not own. */
var Migrations = migrate.NewMigrations()
