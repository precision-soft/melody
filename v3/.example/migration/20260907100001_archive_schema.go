package migration

import (
    "context"

    "github.com/uptrace/bun"
)

func init() {
    ArchiveMigrations.MustRegister(upArchiveSchema, downArchiveSchema)
}

/* CatalogReadingTableName is the one spelling of the archive's table. The schema owns it, so this step, the repository that reads and writes it and the reset command's plan all read the same constant. */
const CatalogReadingTableName = "melody_example_v3_catalog_reading"

/* upArchiveSchema creates the one table the archive owns. Like the catalogue set beside it, it is a single migration rather than a history of them, because this application has no history: an example has a single state, the present one, and the schema is the statement of that state. A volume left in an older shape is brought to it by example:db:reset, not by a step that repairs its past.

   The statement tolerates a volume provisioned before the set and several processes of this example applying it at the same time — postgres has CREATE TABLE IF NOT EXISTS, so unlike the catalogue's unique key this needs no read of the catalog to be idempotent.

   taken_at is the PRIMARY KEY rather than a surrogate, and that is the archive's identity rather than a convenience: a reading is the catalogue as it stood at one instant, so two rows at one instant are the same reading recorded twice. It is what makes a duplicated refresh a conflict the repository can name instead of a second row nobody can tell from the first. */
func upArchiveSchema(ctx context.Context, database *bun.DB) error {
    for _, statement := range archiveUpStatementList {
        if _, execErr := database.ExecContext(ctx, statement); nil != execErr {
            return execErr
        }
    }

    return nil
}

/* downArchiveSchema reverses upArchiveSchema. */
func downArchiveSchema(ctx context.Context, database *bun.DB) error {
    for _, statement := range archiveDownStatementList {
        if _, execErr := database.ExecContext(ctx, statement); nil != execErr {
            return execErr
        }
    }

    return nil
}

/* the postgres spelling of the catalogue's DATETIME(6) is TIMESTAMPTZ(6), and the difference is not only notation: the catalogue's stamps are wall times in a column with no zone, while a reading carries the instant it was taken at and postgres keeps that instant with its offset. The archive is written in UTC by the service that fills it, so the two agree on the value; the column is what makes the agreement checkable. */
const createCatalogReadingTableSql = "CREATE TABLE IF NOT EXISTS " + CatalogReadingTableName + " (" +
    "taken_at TIMESTAMPTZ(6) NOT NULL PRIMARY KEY, " +
    "headline TEXT NOT NULL, " +
    "payload TEXT NOT NULL, " +
    "product_count INTEGER NOT NULL, " +
    "journal_count INTEGER NOT NULL" +
    ")"

var archiveUpStatementList = []string{
    createCatalogReadingTableSql,
}

/* archiveTableNameList names the tables this migration owns, in the order it drops them. The drop
   statements are DERIVED from it, so a table added to the archive schema cannot be left standing by a
   down that forgot it, and the reset command names the same list to the operator rather than a second
   copy of it. */
var archiveTableNameList = []string{
    CatalogReadingTableName,
}

var archiveDownStatementList = archiveDropStatementList(archiveTableNameList)

/* the catalogue's dropStatementList cannot be reused: it quotes identifiers the MySQL way, with backticks, which postgres reads as an unterminated string rather than as a name. The two dialects each get the spelling they parse. */
func archiveDropStatementList(tableNameList []string) []string {
    statementList := make([]string, 0, len(tableNameList))
    for _, table := range tableNameList {
        statementList = append(statementList, "DROP TABLE IF EXISTS "+table)
    }

    return statementList
}
