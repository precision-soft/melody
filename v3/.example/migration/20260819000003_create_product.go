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
    _, execErr := database.ExecContext(ctx, "DROP TABLE IF EXISTS `melody_example_v3_product`")

    return execErr
}

/* the two instants are DATETIME(6) because the model declares them so: a product created and updated inside the same second is ordered by the microseconds, and a DATETIME without them would collapse the pair */
const createProductTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v3_product` (" +
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
