package migration

import (
    "context"

    "github.com/uptrace/bun"
)

func init() {
    Migrations.MustRegister(upCreateUser, downCreateUser)
}

func upCreateUser(ctx context.Context, database *bun.DB) error {
    _, execErr := database.ExecContext(ctx, createUserTableSql)

    return execErr
}

func downCreateUser(ctx context.Context, database *bun.DB) error {
    _, execErr := database.ExecContext(ctx, "DROP TABLE IF EXISTS `melody_example_v2_user`")

    return execErr
}

/* the roles column holds the comma-joined role list the user repository writes; it is a single VARCHAR on purpose, the example having no role table to normalize into */
const createUserTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v2_user` (" +
    "`id` VARCHAR(255) NOT NULL, " +
    "`username` VARCHAR(255) NOT NULL, " +
    "`password` VARCHAR(255) NOT NULL, " +
    "`roles` VARCHAR(255) NOT NULL, " +
    "PRIMARY KEY (`id`))"
