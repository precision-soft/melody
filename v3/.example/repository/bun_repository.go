package repository

import (
    "context"
    "database/sql"
    "fmt"
    "regexp"

    "github.com/uptrace/bun"
)

/* affectedAtLeastOneRow answers whether a statement changed anything. A driver that declines to report the count returns an error rather than a number, and that is read as "nothing was changed" on purpose: the caller uses the answer to tell a write that landed from one that found no row, and claiming a change nobody can confirm would be the wrong half of that pair to guess. */
func affectedAtLeastOneRow(result sql.Result) bool {
    if nil == result {
        return false
    }

    affected, affectedErr := result.RowsAffected()
    if nil != affectedErr {
        return false
    }

    return 0 < affected
}

/* sqlIdentifierPattern is what a table or column name has to look like before it is allowed into a statement. Neither can be bound as a parameter — an identifier is part of the statement's structure, not a value in it — so the only defence is to refuse anything that is not a plain identifier before it is interpolated. */
var sqlIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

/* ensureColumn adds a column to a table that does not have it yet, and does nothing to a table that does.

It exists because the example creates its own tables and has no migration runner, while the database it creates them in outlives the code: a table created before a column was declared stays exactly as it was, CREATE TABLE IF NOT EXISTS reports success without touching it, and every later write fails on a column the model declares and the table does not have. MySQL has no ADD COLUMN IF NOT EXISTS, so the presence of the column is asked of information_schema first.

The definition is written by this package, never by a caller outside it; the table and column names are checked all the same, because an identifier reaching a statement unchecked is the one mistake this shape of code makes. */
func ensureColumn(
    ctx context.Context,
    database *bun.DB,
    tableName string,
    columnName string,
    columnDefinition string,
) error {
    if false == sqlIdentifierPattern.MatchString(tableName) {
        return fmt.Errorf("table name %q is not a plain sql identifier", tableName)
    }

    if false == sqlIdentifierPattern.MatchString(columnName) {
        return fmt.Errorf("column name %q is not a plain sql identifier", columnName)
    }

    present, presentErr := columnExists(ctx, database, tableName, columnName)
    if nil != presentErr {
        return presentErr
    }

    if true == present {
        return nil
    }

    _, alterErr := database.ExecContext(
        ctx,
        fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableName, columnName, columnDefinition),
    )
    if nil == alterErr {
        return nil
    }

    /* two processes reaching an old table at the same time both see the column missing and both try to add it; the one that loses is told the column is already there, which is the outcome it wanted. The presence is re-read rather than the error inspected, so this does not depend on a driver's error code */
    present, recheckErr := columnExists(ctx, database, tableName, columnName)
    if nil != recheckErr {
        return alterErr
    }

    if true == present {
        return nil
    }

    return alterErr
}

func columnExists(ctx context.Context, database *bun.DB, tableName string, columnName string) (bool, error) {
    count := 0

    scanErr := database.
        NewSelect().
        ColumnExpr("COUNT(*)").
        TableExpr("information_schema.columns").
        Where("table_schema = DATABASE()").
        Where("table_name = ?", tableName).
        Where("column_name = ?", columnName).
        Scan(ctx, &count)
    if nil != scanErr {
        return false, scanErr
    }

    return 0 < count, nil
}
