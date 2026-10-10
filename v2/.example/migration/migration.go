package migration

import (
    "github.com/uptrace/bun/migrate"
)

/* Migrations is the single source of this example's schema on mysql: the five repository providers run it at first resolution through EnsureMigrated, and the db:* command family runs the same set from the operator's side. The set is a SINGLE migration holding the whole schema, since the example has one state and a volume in an older shape is brought to it by example:db:reset. Every statement creates its table IF NOT EXISTS, for a volume provisioned before the set and for several processes applying it at once. The bun bookkeeping tables keep their default names in this major's own database, which no other major writes. The journal table travels in this set, since this major keeps the journal on the catalogue's mysql connection. */
var Migrations = migrate.NewMigrations()

/* SchemaTableNameList names every table this example's set owns, in the order the schema drops them. It is
   the list the reset command shows an operator before it destroys anything, read from the schema itself
   rather than written a second time beside it. */
func SchemaTableNameList() []string {
    return append([]string{}, schemaTableNameList...)
}
