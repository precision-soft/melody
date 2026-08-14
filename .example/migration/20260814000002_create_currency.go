package migration

import (
    "context"

    "github.com/uptrace/bun"
)

func init() {
    Migrations.MustRegister(upCreateCurrency, downCreateCurrency)
}

func upCreateCurrency(ctx context.Context, database *bun.DB) error {
    _, execErr := database.ExecContext(ctx, createCurrencyTableSql)

    return execErr
}

func downCreateCurrency(ctx context.Context, database *bun.DB) error {
    _, execErr := database.ExecContext(ctx, "DROP TABLE IF EXISTS `melody_example_v1_currency`")

    return execErr
}

const createCurrencyTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v1_currency` (" +
    "`id` VARCHAR(255) NOT NULL, " +
    "`code` VARCHAR(255) NOT NULL, " +
    "`name` VARCHAR(255) NOT NULL, " +
    "PRIMARY KEY (`id`))"
