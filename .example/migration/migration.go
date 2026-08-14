package migration

import (
    "github.com/uptrace/bun/migrate"
)

/*
Migrations is the single source of the example schema. The five repository
providers run it at first resolution through EnsureMigrated, and the db:*
command family the bunorm/migrate module registers runs the same set from the
operator's side, so neither door can drift from the other.

Every up statement creates its table with IF NOT EXISTS: the tables may already
exist on a volume provisioned before the migration set, and several example
processes sharing one database may apply the set at the same time. The bun
bookkeeping tables keep their default names (bun_migrations and
bun_migration_locks), which carry no major prefix; only this example ships a
migration set today, so nothing else writes them.
*/
var Migrations = migrate.NewMigrations()
