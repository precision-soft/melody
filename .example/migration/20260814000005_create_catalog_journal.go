package migration

import (
    "context"

    "github.com/uptrace/bun"
)

func init() {
    Migrations.MustRegister(upCreateCatalogJournal, downCreateCatalogJournal)
}

func upCreateCatalogJournal(ctx context.Context, database *bun.DB) error {
    _, execErr := database.ExecContext(ctx, createCatalogJournalTableSql)

    return execErr
}

func downCreateCatalogJournal(ctx context.Context, database *bun.DB) error {
    _, execErr := database.ExecContext(ctx, "DROP TABLE IF EXISTS `melody_example_v1_catalog_journal`")

    return execErr
}

/* the journal is the one table with a database-assigned identifier: entries are append-only and never constructed with an id of their own */
const createCatalogJournalTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v1_catalog_journal` (" +
    "`id` BIGINT NOT NULL AUTO_INCREMENT, " +
    "`actor` VARCHAR(255) NOT NULL, " +
    "`action` VARCHAR(255) NOT NULL, " +
    "`subject` VARCHAR(255) NOT NULL, " +
    "`subject_id` VARCHAR(255) NOT NULL, " +
    "`recorded_at` DATETIME(6) NOT NULL, " +
    "PRIMARY KEY (`id`))"
