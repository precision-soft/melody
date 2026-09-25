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

/* upSchema creates the six tables this example owns and declares the one constraint it needs, in one step: an example has a single state, and a volume in an older shape is brought to it by example:db:reset. The set does not adopt a volume that already holds its tables without recording it (see beginSchemaSet); its statements still tolerate a second run, since the tables are created IF NOT EXISTS and the constraint, MySQL having no ADD KEY IF NOT EXISTS, is added only after the catalogue is asked whether it is there. The constraint comes last because it stands on a table this step creates. */
func upSchema(ctx context.Context, database *bun.DB) error {
    if beginErr := beginSchemaSet(ctx, database, catalogueSchemaSetRecord, schemaTableNameList); nil != beginErr {
        return beginErr
    }

    for _, statement := range schemaUpStatementList {
        /* the record's own table is created by beginSchemaSet, ahead of the row it holds; it stays in the list for the fingerprint and the drift check, which read the whole schema */
        if catalogueSchemaSetRecord.createTableSql == statement {
            continue
        }

        if _, execErr := database.ExecContext(ctx, statement); nil != execErr {
            return execErr
        }
    }

    if indexErr := addUserUsernameIndex(ctx, database); nil != indexErr {
        return indexErr
    }

    return sealSchemaSet(ctx, database, catalogueSchemaSetRecord)
}

/* downSchema reverses upSchema: the constraint goes first, because the table it stands on is one of the ones the drops below take away, and the tables go in the reverse order of their creation. */
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

/* every column that holds an entity identifier is compared under utf8mb4_bin, because the identity of an id is exact everywhere else in this application: the in-memory repositories compare it byte for byte and the cache keys carry it as spelled, and under the table's default utf8mb4_0900_ai_ci a lookup by id would fold case and accents, find `CUR-EUR` as `cur-eur` and cache it under a key nothing invalidates. */
const createCategoryTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v3_category` (" +
    "`id` VARCHAR(255) COLLATE utf8mb4_bin NOT NULL, " +
    "`name` VARCHAR(255) NOT NULL, " +
    "PRIMARY KEY (`id`))"

/* the rate is a DOUBLE beside the code, and the reading's two instants are DATETIME(6) for the reason the product
   timestamps are: two readings taken inside the same second are ordered by the microseconds, and a refresh that
   lands twice in one second would otherwise collapse into one instant. rate_as_of is when the reading was taken,
   moved onto this application's clock, and every judgement of order reads it; provider_rate_as_of is the
   provider's own stamp, as it came, and names the reading — see entity.RateQuote. Both say how old the reading
   is rather than how long ago this application happened to write it. */
const createCurrencyTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v3_currency` (" +
    "`id` VARCHAR(255) COLLATE utf8mb4_bin NOT NULL, " +
    "`code` VARCHAR(255) NOT NULL, " +
    "`name` VARCHAR(255) NOT NULL, " +
    "`rate` DOUBLE NOT NULL, " +
    "`rate_as_of` DATETIME(6) NOT NULL, " +
    "`provider_rate_as_of` DATETIME(6) NOT NULL, " +
    "PRIMARY KEY (`id`))"

/* the two instants are DATETIME(6) because the model declares them so: a product created and updated inside the same second is ordered by the microseconds, and a DATETIME without them would collapse the pair */
const createProductTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v3_product` (" +
    "`id` VARCHAR(255) COLLATE utf8mb4_bin NOT NULL, " +
    "`name` VARCHAR(255) NOT NULL, " +
    "`description` VARCHAR(255) NOT NULL, " +
    "`category_id` VARCHAR(255) COLLATE utf8mb4_bin NOT NULL, " +
    "`price` DOUBLE NOT NULL, " +
    "`currency_id` VARCHAR(255) COLLATE utf8mb4_bin NOT NULL, " +
    "`stock` BIGINT NOT NULL, " +
    "`created_at` DATETIME(6) NOT NULL, " +
    "`updated_at` DATETIME(6) NOT NULL, " +
    "PRIMARY KEY (`id`))"

/* the password column holds a bcrypt digest, never a password; the audit registry is told so by the model's own redact tag, and the width is the one the model produced */
const createUserTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v3_user` (" +
    "`id` VARCHAR(255) COLLATE utf8mb4_bin NOT NULL, " +
    "`username` VARCHAR(255) NOT NULL, " +
    "`password` VARCHAR(255) NOT NULL, " +
    "`roles` VARCHAR(255) NOT NULL, " +
    "PRIMARY KEY (`id`))"

/* the journal lives on mysql beside the catalogue on this major, so the identity column is written in the mysql spelling; v1 keeps the same table on postgres and spells it GENERATED BY DEFAULT AS IDENTITY. The request identifier is this major's own column: an entry names the call that caused it, and it is empty for a change made outside a request. */
const createCatalogJournalTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v3_catalog_journal` (" +
    "`id` BIGINT NOT NULL AUTO_INCREMENT, " +
    "`request_id` VARCHAR(255) NOT NULL, " +
    "`actor` VARCHAR(255) NOT NULL, " +
    "`action` VARCHAR(255) NOT NULL, " +
    "`subject` VARCHAR(255) NOT NULL, " +
    "`subject_id` VARCHAR(255) NOT NULL, " +
    "`recorded_at` DATETIME(6) NOT NULL, " +
    "PRIMARY KEY (`id`))"

/* the two secret columns are VARBINARY because bunorm's EncryptedString writes a sealed byte string, not text, and the widths hold the sealed spelling. The identifier references the user table, so the row goes with its account: identifiers are minted as the highest suffix plus one, a surviving row would enroll the next holder of the identifier with the former holder's secret, and the database releases it whether or not the deletion subscriber runs. Both columns compare under utf8mb4_bin, which is what lets the key be declared. */
const createTwoFactorTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v3_two_factor` (" +
    "`user_identifier` VARCHAR(255) COLLATE utf8mb4_bin NOT NULL, " +
    "`secret` VARBINARY(512) NOT NULL, " +
    "`recovery_codes` VARBINARY(2048) NOT NULL, " +
    "`created_at` DATETIME NOT NULL, " +
    "PRIMARY KEY (`user_identifier`), " +
    "CONSTRAINT `melody_example_v3_two_factor_user` FOREIGN KEY (`user_identifier`) REFERENCES `melody_example_v3_user` (`id`) ON DELETE CASCADE)"

/* the index is on LOWER(username) cast to the binary collation because that expression is the identity this application gives a username: NormalizedUsername folds case and nothing else, and the lookup door compares on utf8mb4_bin. On the column's own utf8mb4_0900_ai_ci the constraint would refuse 'ana' beside 'ána', which the application considers different, and admit 'Ana' beside 'ana', which it considers the same. */
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
    createSchemaFingerprintTableSql,
}

/* schemaTableNameList names the tables this migration owns, in the order it drops them — the reverse of
   the order it creates them in. The drop statements are DERIVED from it, so a table added to the schema
   cannot be left standing by a down that forgot it, and the reset command names the same list to the
   operator rather than a second copy of it. */
var schemaTableNameList = []string{
    SchemaFingerprintTableName,
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

/* every step tolerates a second run of the set, the run it began and did not finish; MySQL has no ADD KEY IF NOT EXISTS, so the tolerance is spelled by asking the catalogue first, or a second run would fail on a duplicate index name. */
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
