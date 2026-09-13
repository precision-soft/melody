package migration

import (
    "context"

    "github.com/uptrace/bun"
)

func init() {
    Migrations.MustRegister(upSchema, downSchema)
}

/* upSchema creates the four catalog tables in one step. The set is one migration rather than a history of them because this application has no history: an example has a single state, the present one, and its schema is the statement of that state. A volume left in an older shape is brought to it by example:db:reset, not by a step that repairs its past.

   Every statement is tolerant of a volume provisioned before the set, and of several processes of this example applying the set at the same time. */
func upSchema(ctx context.Context, database *bun.DB) error {
    for _, statement := range schemaUpStatementList {
        if _, execErr := database.ExecContext(ctx, statement); nil != execErr {
            return execErr
        }
    }

    return nil
}

/* downSchema drops what upSchema created, in the reverse order: the catalog carries no foreign keys today, and reversing the order anyway is what keeps the step correct the day one is added. */
func downSchema(ctx context.Context, database *bun.DB) error {
    for _, statement := range schemaDownStatementList {
        if _, execErr := database.ExecContext(ctx, statement); nil != execErr {
            return execErr
        }
    }

    return nil
}

/* the column definitions mirror the tables the bun create-table builder used to produce, captured from a live SHOW CREATE TABLE, so a volume provisioned before the migration set and one provisioned by it hold the same schema */
const createCategoryTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v1_category` (" +
    "`id` VARCHAR(255) NOT NULL, " +
    "`name` VARCHAR(255) NOT NULL, " +
    "PRIMARY KEY (`id`))"

const createCurrencyTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v1_currency` (" +
    "`id` VARCHAR(255) NOT NULL, " +
    "`code` VARCHAR(255) NOT NULL, " +
    "`name` VARCHAR(255) NOT NULL, " +
    "PRIMARY KEY (`id`))"

/* the timestamps are DATETIME(6) so the microsecond half of a Go time survives the round trip; DATETIME would silently floor it and the update stamp could compare equal to the creation stamp */
const createProductTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v1_product` (" +
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

/* the roles column holds the comma-joined role list the user repository writes; it is a single VARCHAR on purpose, the example having no role table to normalize into */
const createUserTableSql = "CREATE TABLE IF NOT EXISTS `melody_example_v1_user` (" +
    "`id` VARCHAR(255) NOT NULL, " +
    "`username` VARCHAR(255) NOT NULL, " +
    "`password` VARCHAR(255) NOT NULL, " +
    "`roles` VARCHAR(255) NOT NULL, " +
    "PRIMARY KEY (`id`))"

var schemaUpStatementList = []string{
    createCategoryTableSql,
    createCurrencyTableSql,
    createProductTableSql,
    createUserTableSql,
}

/* schemaTableNameList names the tables this migration owns, in the order it drops them — the reverse of
   the order it creates them in. The drop statements are DERIVED from it, so a table added to the schema
   cannot be left standing by a down that forgot it, and the reset command names the same list to the
   operator rather than a second copy of it. */
var schemaTableNameList = []string{
    "melody_example_v1_user",
    "melody_example_v1_product",
    "melody_example_v1_currency",
    "melody_example_v1_category",
}

var schemaDownStatementList = dropStatementList(schemaTableNameList)

func dropStatementList(tableNameList []string) []string {
    statementList := make([]string, 0, len(tableNameList))
    for _, table := range tableNameList {
        statementList = append(statementList, "DROP TABLE IF EXISTS `"+table+"`")
    }

    return statementList
}
