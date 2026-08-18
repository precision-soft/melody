package migration

import (
    "context"

    "github.com/uptrace/bun"
)

func init() {
    Migrations.MustRegister(upCreateProduct, downCreateProduct)
}

func upCreateProduct(ctx context.Context, database *bun.DB) error {
    _, execErr := database.ExecContext(ctx, createProductTableSql)

    return execErr
}

func downCreateProduct(ctx context.Context, database *bun.DB) error {
    _, execErr := database.ExecContext(ctx, "DROP TABLE IF EXISTS `melody_example_v2_product`")

    return execErr
}

/* the timestamps are DATETIME(6) so the microsecond half of a Go time survives the round trip; DATETIME would silently floor it and the update stamp could compare equal to the creation stamp */
const createProductTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v2_product` (" +
    "`id` VARCHAR(255) NOT NULL, " +
    "`name` VARCHAR(255) NOT NULL, " +
    "`description` VARCHAR(255) NOT NULL, " +
    "`category_id` VARCHAR(255) NOT NULL, " +
    "`price` DOUBLE NOT NULL, " +
    "`currency_id` VARCHAR(255) NOT NULL, " +
    "`stock` BIGINT NOT NULL, " +
    "`created_at` DATETIME(6) NOT NULL, " +
    "`updated_at` DATETIME(6) NOT NULL, " +
    "PRIMARY KEY (`id`))"
