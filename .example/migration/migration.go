package migration

import (
    "github.com/uptrace/bun/migrate"
)

/*
Migrations is the single source of the catalog schema on mysql. The four
catalog repository providers run it at first resolution through
EnsureMigrated, and the db:* command family the bunorm/migrate module
registers runs the same set from the operator's side, so neither door can
drift from the other.

Every up statement creates its table with IF NOT EXISTS: the tables may already
exist on a volume provisioned before the migration set, and several example
processes sharing one database may apply the set at the same time. The bun
bookkeeping tables keep their default names (bun_migrations and
bun_migration_locks); they live in the catalog database, which only this set
writes.
*/
var Migrations = migrate.NewMigrations()

/*
JournalMigrations is the single source of the journal schema on postgres. The
journal repository provider runs it at first resolution through
EnsureJournalMigrated, and the db:journal:* command family runs the same set
from the operator's side.

The journal database is the shared development one (melody_test), which other
harness probes also write — each with tables of its own. The bun bookkeeping
tables keep their default names there too: bookkeeping is per database, and
within melody_test only this set uses bun's migrator, so nothing else touches
its rows.
*/
var JournalMigrations = migrate.NewMigrations()
