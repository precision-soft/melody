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

func upArchiveSchema(ctx context.Context, database *bun.DB) error {
    for _, statement := range archiveUpStatementList {
        if _, execErr := database.ExecContext(ctx, statement); nil != execErr {
            return execErr
        }
    }

    return nil
}

func downArchiveSchema(ctx context.Context, database *bun.DB) error {
    for _, statement := range archiveDownStatementList {
        if _, execErr := database.ExecContext(ctx, statement); nil != execErr {
            return execErr
        }
    }

    return nil
}

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

var archiveTableNameList = []string{
    CatalogReadingTableName,
}

var archiveDownStatementList = archiveDropStatementList(archiveTableNameList)

func archiveDropStatementList(tableNameList []string) []string {
    statementList := make([]string, 0, len(tableNameList))
    for _, table := range tableNameList {
        statementList = append(statementList, "DROP TABLE IF EXISTS "+table)
    }

    return statementList
}
