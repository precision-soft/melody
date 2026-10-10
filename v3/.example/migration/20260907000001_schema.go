package migration

import (
    "context"

    "github.com/uptrace/bun"
)

func init() {
    Migrations.MustRegister(upSchema, downSchema)
}

/* CurrencyCodeIndexName is the one spelling of the currency code's unique key, read by the repository that maps its refusal onto a taken code: the conversion finds a currency by its code, so two rows under one code would leave the answer to whichever the listing returns first. */
const CurrencyCodeIndexName = "melody_example_v3_currency_code"

/* UserUsernameIndexName is the one spelling of the unique key on the folded username. The schema owns it, so this step and the repository that maps the driver's duplicate-key refusal onto the public message read the same constant: a name written in two places is a constraint and a refusal that can drift apart. */
const UserUsernameIndexName = "melody_example_v3_user_username_normalized"

/* ProductCurrencyForeignKeyName and ProductCategoryForeignKeyName are the spellings of the two references a product holds, read by the repositories that map the database's refusal: a currency or a category a product names cannot be deleted from under it, and a product cannot name one that does not exist. */
const (
    ProductCurrencyForeignKeyName = "melody_example_v3_product_currency"
    ProductCategoryForeignKeyName = "melody_example_v3_product_category"
)

/* IdentifierSequenceTableName holds, per entity, the highest identifier suffix ever minted, so a deleted entity's identifier is never handed to the next one: its audit history and anything that still names it would otherwise follow the identifier to the new holder. */
const IdentifierSequenceTableName = "melody_example_v3_identifier_sequence"

/* UserSessionTableName is the index of the sessions each account holds, which the sign-in door reads to keep an account under its cap; the session storage itself cannot be asked which sessions belong to an account. */
const UserSessionTableName = "melody_example_v3_user_session"

/* upSchema creates the tables this example owns, every constraint declared with its table, in one step: an example has a single state, and a volume in an older shape is refused by its fingerprint and brought to this one by example:db:reset. The set does not adopt a volume that already holds its tables without recording it (see beginSchemaSet); its statements still tolerate a second run, since the tables are created IF NOT EXISTS. */
func upSchema(ctx context.Context, database *bun.DB) error {
    built, beginErr := beginSchemaSet(ctx, database, catalogueSchemaSetRecord, schemaTableNameList)
    if nil != beginErr {
        return beginErr
    }

    if true == built {
        return nil
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

    return sealSchemaSet(ctx, database, catalogueSchemaSetRecord)
}

/* downSchema reverses upSchema: the tables go in the reverse order of their creation, so a table is dropped before the one it references. */
func downSchema(ctx context.Context, database *bun.DB) error {
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
    "`code` VARCHAR(255) COLLATE utf8mb4_bin NOT NULL, " +
    "`name` VARCHAR(255) NOT NULL, " +
    "`rate` DOUBLE NOT NULL, " +
    "`rate_as_of` DATETIME(6) NOT NULL, " +
    "`provider_rate_as_of` DATETIME(6) NOT NULL, " +
    "PRIMARY KEY (`id`), " +
    "UNIQUE KEY `" + CurrencyCodeIndexName + "` (`code`))"

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
    "PRIMARY KEY (`id`), " +
    "CONSTRAINT `" + ProductCurrencyForeignKeyName + "` FOREIGN KEY (`currency_id`) REFERENCES `melody_example_v3_currency` (`id`) ON DELETE RESTRICT, " +
    "CONSTRAINT `" + ProductCategoryForeignKeyName + "` FOREIGN KEY (`category_id`) REFERENCES `melody_example_v3_category` (`id`) ON DELETE RESTRICT)"

/* the password column holds a bcrypt digest, never a password; the audit registry is told so by the model's own redact tag, and the width is the one the model produced. username_normalized is the name as NormalizedUsername folds it, written by the application: the identity a username has is the one Go's folding gives it, which the cache keys and the listeners share, and MySQL's LOWER() folds some scripts otherwise, so the key and the lookup compare that column byte for byte. */
const createUserTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v3_user` (" +
    "`id` VARCHAR(255) COLLATE utf8mb4_bin NOT NULL, " +
    "`username` VARCHAR(255) NOT NULL, " +
    "`username_normalized` VARCHAR(255) COLLATE utf8mb4_bin NOT NULL, " +
    "`password` VARCHAR(255) NOT NULL, " +
    "`roles` VARCHAR(255) NOT NULL, " +
    "PRIMARY KEY (`id`), " +
    "UNIQUE KEY `" + UserUsernameIndexName + "` (`username_normalized`))"

/* a session row goes with its account, and the index on the account and the instant is the order the sign-in door drops the oldest in */
const createUserSessionTableSql = "CREATE TABLE IF NOT EXISTS `" + UserSessionTableName + "` (" +
    "`session_id` VARCHAR(255) COLLATE utf8mb4_bin NOT NULL, " +
    "`user_identifier` VARCHAR(255) COLLATE utf8mb4_bin NOT NULL, " +
    "`created_at` DATETIME(6) NOT NULL, " +
    "PRIMARY KEY (`session_id`), " +
    "KEY `melody_example_v3_user_session_account` (`user_identifier`, `created_at`), " +
    "CONSTRAINT `melody_example_v3_user_session_user` FOREIGN KEY (`user_identifier`) REFERENCES `melody_example_v3_user` (`id`) ON DELETE CASCADE)"

const createIdentifierSequenceTableSql = "CREATE TABLE IF NOT EXISTS `" + IdentifierSequenceTableName + "` (" +
    "`entity` VARCHAR(64) NOT NULL, " +
    "`highest_suffix` BIGINT NOT NULL, " +
    "PRIMARY KEY (`entity`))"

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

/* the two secret columns are VARBINARY because bunorm's EncryptedString writes a sealed byte string, not text, and the widths hold the sealed spelling. The identifier references the user table, so the row goes with its account: a surviving row would name an account that is gone, and the database releases it whether or not the deletion subscriber runs. Both columns compare under utf8mb4_bin, which is what lets the key be declared. */
const createTwoFactorTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v3_two_factor` (" +
    "`user_identifier` VARCHAR(255) COLLATE utf8mb4_bin NOT NULL, " +
    "`secret` VARBINARY(512) NOT NULL, " +
    "`recovery_codes` VARBINARY(2048) NOT NULL, " +
    "`created_at` DATETIME NOT NULL, " +
    "PRIMARY KEY (`user_identifier`), " +
    "CONSTRAINT `melody_example_v3_two_factor_user` FOREIGN KEY (`user_identifier`) REFERENCES `melody_example_v3_user` (`id`) ON DELETE CASCADE)"

var schemaUpStatementList = []string{
    createCategoryTableSql,
    createCurrencyTableSql,
    createProductTableSql,
    createUserTableSql,
    createUserSessionTableSql,
    createIdentifierSequenceTableSql,
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
    IdentifierSequenceTableName,
    UserSessionTableName,
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
