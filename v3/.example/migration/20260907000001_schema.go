package migration

import (
    "context"

    "github.com/uptrace/bun"
)

func init() {
    Migrations.MustRegister(upSchema, downSchema)
}

/* UserUsernameIndexName is the one spelling of the index. The schema owns it, so this step and the repository that maps the driver's duplicate-key refusal onto the public message read the same constant: a name written in two places is a constraint and a refusal that can drift apart. */
const UserUsernameIndexName = "melody_example_v3_user_username_folded"

func upSchema(ctx context.Context, database *bun.DB) error {
    for _, statement := range schemaUpStatementList {
        if _, execErr := database.ExecContext(ctx, statement); nil != execErr {
            return execErr
        }
    }

    return addUserUsernameIndex(ctx, database)
}

func downSchema(ctx context.Context, database *bun.DB) error {
    if dropIndexErr := dropUserUsernameIndex(ctx, database); nil != dropIndexErr {
        return dropIndexErr
    }

    for _, statement := range schemaDownStatementList {
        if _, execErr := database.ExecContext(ctx, statement); nil != execErr {
            return execErr
        }
    }

    return nil
}

const createCategoryTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v3_category` (" +
    "`id` VARCHAR(255) NOT NULL, " +
    "`name` VARCHAR(255) NOT NULL, " +
    "PRIMARY KEY (`id`))"

const createCurrencyTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v3_currency` (" +
    "`id` VARCHAR(255) NOT NULL, " +
    "`code` VARCHAR(255) NOT NULL, " +
    "`name` VARCHAR(255) NOT NULL, " +
    "`rate` DOUBLE NOT NULL, " +
    "`rate_as_of` DATETIME(6) NOT NULL, " +
    "PRIMARY KEY (`id`))"

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

const createUserTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v3_user` (" +
    "`id` VARCHAR(255) NOT NULL, " +
    "`username` VARCHAR(255) NOT NULL, " +
    "`password` VARCHAR(255) NOT NULL, " +
    "`roles` VARCHAR(255) NOT NULL, " +
    "PRIMARY KEY (`id`))"

const createCatalogJournalTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v3_catalog_journal` (" +
    "`id` BIGINT NOT NULL AUTO_INCREMENT, " +
    "`request_id` VARCHAR(255) NOT NULL, " +
    "`actor` VARCHAR(255) NOT NULL, " +
    "`action` VARCHAR(255) NOT NULL, " +
    "`subject` VARCHAR(255) NOT NULL, " +
    "`subject_id` VARCHAR(255) NOT NULL, " +
    "`recorded_at` DATETIME(6) NOT NULL, " +
    "PRIMARY KEY (`id`))"

const createTwoFactorTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v3_two_factor` (" +
    "`user_identifier` VARCHAR(255) NOT NULL, " +
    "`secret` VARBINARY(512) NOT NULL, " +
    "`recovery_codes` VARBINARY(2048) NOT NULL, " +
    "`created_at` DATETIME NOT NULL, " +
    "PRIMARY KEY (`user_identifier`))"

const createUserUsernameIndexSql = "ALTER TABLE `melody_example_v3_user` " +
    "ADD UNIQUE KEY `" + UserUsernameIndexName + "` " +
    "((CAST(LOWER(`username`) AS CHAR(255) CHARACTER SET utf8mb4) COLLATE utf8mb4_bin))"

var schemaUpStatementList = []string{
    createCategoryTableSql,
    createCurrencyTableSql,
    createProductTableSql,
    createUserTableSql,
    createCatalogJournalTableSql,
    createTwoFactorTableSql,
}

var schemaTableNameList = []string{
    "melody_example_v3_two_factor",
    "melody_example_v3_catalog_journal",
    "melody_example_v3_user",
    "melody_example_v3_product",
    "melody_example_v3_currency",
    "melody_example_v3_category",
}

var schemaDownStatementList = dropStatementList(schemaTableNameList)

func dropStatementList(tableNameList []string) []string {
    statementList := make([]string, 0, len(tableNameList))
    for _, table := range tableNameList {
        statementList = append(statementList, "DROP TABLE IF EXISTS `"+table+"`")
    }

    return statementList
}

func addUserUsernameIndex(ctx context.Context, database *bun.DB) error {
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

func dropUserUsernameIndex(ctx context.Context, database *bun.DB) error {
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
