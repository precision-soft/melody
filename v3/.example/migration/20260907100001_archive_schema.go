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

/* upArchiveSchema creates the one table the archive owns, a single migration for the reason the catalogue set is one. Like the catalogue set, it does not adopt a volume that already holds its tables without recording it (see beginSchemaSet), and postgres's CREATE TABLE IF NOT EXISTS makes its statement idempotent. taken_at is the primary key because a reading is the catalogue at one instant, so a duplicated refresh is a conflict the repository can name rather than a second row. */
func upArchiveSchema(ctx context.Context, database *bun.DB) error {
    if beginErr := beginSchemaSet(ctx, database, archiveSchemaSetRecord, archiveTableNameList); nil != beginErr {
        return beginErr
    }

    for _, statement := range archiveUpStatementList {
        /* the record's own table is created by beginSchemaSet, ahead of the row it holds; it stays in the list for the fingerprint and the drift check, which read the whole schema */
        if archiveSchemaSetRecord.createTableSql == statement {
            continue
        }

        if _, execErr := database.ExecContext(ctx, statement); nil != execErr {
            return execErr
        }
    }

    return sealSchemaSet(ctx, database, archiveSchemaSetRecord)
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
    createArchiveSchemaFingerprintTableSql,
}

/* archiveTableNameList names the tables this migration owns, in the order it drops them. The drop
   statements are DERIVED from it, so a table added to the archive schema cannot be left standing by a
   down that forgot it, and the reset command names the same list to the operator rather than a second
   copy of it. */
var archiveTableNameList = []string{
    ArchiveSchemaFingerprintTableName,
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
