package migration

import (
    "context"

    "github.com/uptrace/bun"
)

func init() {
    Migrations.MustRegister(upCreateCategory, downCreateCategory)
}

func upCreateCategory(ctx context.Context, database *bun.DB) error {
    _, execErr := database.ExecContext(ctx, createCategoryTableSql)

    return execErr
}

func downCreateCategory(ctx context.Context, database *bun.DB) error {
    _, execErr := database.ExecContext(ctx, "DROP TABLE IF EXISTS `melody_example_v2_category`")

    return execErr
}

/* the column definitions mirror the table the bun create-table builder used to produce, captured from a live SHOW CREATE TABLE, so a volume provisioned before the migration set and one provisioned by it hold the same schema */
const createCategoryTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v2_category` (" +
    "`id` VARCHAR(255) NOT NULL, " +
    "`name` VARCHAR(255) NOT NULL, " +
    "PRIMARY KEY (`id`))"
