package migration

import (
    "context"

    "github.com/uptrace/bun"
)

func init() {
    Migrations.MustRegister(upCreateTwoFactor, downCreateTwoFactor)
}

func upCreateTwoFactor(ctx context.Context, database *bun.DB) error {
    _, execErr := database.ExecContext(ctx, createTwoFactorTableSql)

    return execErr
}

func downCreateTwoFactor(ctx context.Context, database *bun.DB) error {
    _, execErr := database.ExecContext(ctx, "DROP TABLE IF EXISTS `melody_example_v3_two_factor`")

    return execErr
}

/* the two secret columns are VARBINARY because bunorm's EncryptedString writes a sealed byte string, not text: a character set would try to interpret ciphertext and a collation would compare it. The widths are the ones the model declares, and they hold the sealed spelling rather than the plaintext — the marker, key identifier, nonce and tag travel with it. This table is the one neither frozen major carries. */
const createTwoFactorTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v3_two_factor` (" +
    "`user_identifier` VARCHAR(255) NOT NULL, " +
    "`secret` VARBINARY(512) NOT NULL, " +
    "`recovery_codes` VARBINARY(2048) NOT NULL, " +
    "`created_at` DATETIME NOT NULL, " +
    "PRIMARY KEY (`user_identifier`))"
