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
    _, execErr := database.ExecContext(ctx, "DROP TABLE IF EXISTS `melody_example_v3_user`")

    return execErr
}

/* the password column holds a bcrypt digest, never a password; the audit registry is told so by the model's own redact tag, and the width is the one the model produced */
const createUserTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v3_user` (" +
    "`id` VARCHAR(255) NOT NULL, " +
    "`username` VARCHAR(255) NOT NULL, " +
    "`password` VARCHAR(255) NOT NULL, " +
    "`roles` VARCHAR(255) NOT NULL, " +
    "PRIMARY KEY (`id`))"
