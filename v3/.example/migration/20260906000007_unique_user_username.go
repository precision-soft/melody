package migration

import (
    "context"

    "github.com/uptrace/bun"
)

func init() {
    Migrations.MustRegister(upUniqueUserUsername, downUniqueUserUsername)
}

/* UserUsernameIndexName is the one spelling of the index. The schema owns it, so the up and the down
   step and the repository that maps the driver's duplicate-key refusal onto the public message all read
   the same constant: a name written in two places is a constraint and a refusal that can drift apart. */
const UserUsernameIndexName = "melody_example_v3_user_username_folded"

/* the index is on LOWER(username) cast to the binary collation because that expression, and only that
   expression, is the identity this application gives a username: NormalizedUsername folds case and
   nothing else, and the lookup door compares on utf8mb4_bin for the same reason. Indexed on the column
   as it stands, the constraint would follow the column's own utf8mb4_0900_ai_ci and refuse two names the
   application considers different — 'ana' and 'ána' — while admitting 'Ana' beside 'ana', which it
   considers the same. Measured on the running server: with this expression 'ana' and 'ANA' collide with
   'Ana', 'Ána' does not, which is exactly what the lookup answers. */
const createUserUsernameIndexSql = "ALTER TABLE `melody_example_v3_user` " +
    "ADD UNIQUE KEY `" + UserUsernameIndexName + "` " +
    "((CAST(LOWER(`username`) AS CHAR(255) CHARACTER SET utf8mb4) COLLATE utf8mb4_bin))"

/* the set's own discipline is that a step tolerates a volume provisioned before it — every table is
   created IF NOT EXISTS. MySQL has no ADD KEY IF NOT EXISTS, so the same tolerance is spelled by asking
   the catalogue first; without it a volume whose table was created by a build that already carries the
   key would fail the step on a duplicate index name. */
func upUniqueUserUsername(ctx context.Context, database *bun.DB) error {
    present, presentErr := userUsernameIndexIsPresent(ctx, database)
    if nil != presentErr {
        return presentErr
    }

    if true == present {
        return nil
    }

    _, execErr := database.ExecContext(ctx, createUserUsernameIndexSql)

    return execErr
}

func downUniqueUserUsername(ctx context.Context, database *bun.DB) error {
    present, presentErr := userUsernameIndexIsPresent(ctx, database)
    if nil != presentErr {
        return presentErr
    }

    if false == present {
        return nil
    }

    _, execErr := database.ExecContext(
        ctx,
        "ALTER TABLE `melody_example_v3_user` DROP INDEX `"+UserUsernameIndexName+"`",
    )

    return execErr
}

func userUsernameIndexIsPresent(ctx context.Context, database *bun.DB) (bool, error) {
    count := 0

    queryErr := database.NewRaw(
        "SELECT COUNT(*) FROM information_schema.STATISTICS "+
            "WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?",
        "melody_example_v3_user",
        UserUsernameIndexName,
    ).Scan(ctx, &count)
    if nil != queryErr {
        return false, queryErr
    }

    return 0 < count, nil
}
