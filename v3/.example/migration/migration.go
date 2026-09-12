package migration

import (
    "github.com/uptrace/bun/migrate"
)

/* Migrations defines this example's MySQL schema for both EnsureMigrated and the db:* commands. It is one migration for the example's current schema; example:db:reset replaces older example schemas rather than replaying an application upgrade history.

   Table creation is idempotent. The username constraint is checked in the catalogue before installation, with concurrent initialization handled by the migration path. Bun's default bookkeeping tables are confined to this major's own database, so a shared migration identifier across majors does not combine their histories.

   The set owns the four catalogue tables, the journal, the two-factor enrollment table, and the unique folded-username constraint. Outbox and audit tables remain owned and initialized by their respective modules. */
var Migrations = migrate.NewMigrations()

/* SchemaTableNameList names every table this example's set owns, in the order the schema drops them. It is
   the list the reset command shows an operator before it destroys anything, read from the schema itself
   rather than written a second time beside it. */
func SchemaTableNameList() []string {
    return append([]string{}, schemaTableNameList...)
}

/* ArchiveMigrations is the single source of the reading archive's schema, on postgres. The archive
   repository provider runs it at first resolution through EnsureArchiveMigrated, and the db:archive:*
   command family the bunorm/migrate module registers as a CONTEXT runs the same set from the operator's
   side, so neither door can drift from the other.

   It is a set of its own rather than a second step of the one above, and the reason is bun's bookkeeping
   rather than taste: bun_migrations is per database and bun matches an applied migration BY NAME, so two
   databases need two sets and one set could never span them. Its identifier is deliberately not the
   number the day would have given it — the frozen major's journal set already carries 20260907000002 —
   because two identifiers that collide are only harmless while the two sets never meet in one database,
   and the archive's whole reason for existing is that this example now has two.

   The dialect is postgres and it is written as postgres: TIMESTAMPTZ(6) where the mysql schema writes
   DATETIME(6), an unquoted identifier where it writes a backticked one. */
var ArchiveMigrations = migrate.NewMigrations()

/* ArchiveTableNameList names the tables the archive set owns, the way SchemaTableNameList names the
   catalogue's: one list, read by the down that drops them and by the plan the reset command prints. */
func ArchiveTableNameList() []string {
    return append([]string{}, archiveTableNameList...)
}
