package migration

import (
    "github.com/uptrace/bun/migrate"
)

/* Migrations is the single source of this example's schema on mysql: the repository constructors and the two-factor store's provider run it at first use through EnsureMigrated, and the db:* family runs the same set from the operator's side. It is one migration holding the whole schema, so a volume in an older shape is refused by name at the first resolution (see refuseSchemaDrift) until example:db:reset brings it here, and every statement tolerates a second run and concurrent processes. It covers the six tables this example owns and the unique key on the folded username, not the tables the framework's modules create. */
var Migrations = migrate.NewMigrations()

/* SchemaTableNameList names every table this example's set owns, in the order the schema drops them. It is
   the list the reset command shows an operator before it destroys anything, read from the schema itself
   rather than written a second time beside it. */
func SchemaTableNameList() []string {
    return append([]string{}, schemaTableNameList...)
}

/* ArchiveMigrations is the single source of the reading archive's schema, on postgres, run by EnsureArchiveMigrated at first resolution and by the db:archive:* context from the operator's side. It is a set of its own because bun_migrations is per database and bun matches an applied migration by name, and its identifier differs from the frozen major's 20260907000002 so the two sets can never collide in one database. It is written in the postgres dialect. */
var ArchiveMigrations = migrate.NewMigrations()

/* ArchiveTableNameList names the tables the archive set owns, the way SchemaTableNameList names the
   catalogue's: one list, read by the down that drops them and by the plan the reset command prints. */
func ArchiveTableNameList() []string {
    return append([]string{}, archiveTableNameList...)
}
