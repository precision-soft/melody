package repository

import (
    "context"
    "database/sql"

    bun "github.com/uptrace/bun"
)

/* affectedAtLeastOneRow answers whether a statement changed anything. A driver that does not report the count is read as "nothing was changed": the caller tells a write that landed from one that found no row, and a change nobody can confirm is not claimed. */
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

/* seedIfEmptyRows fills an empty table the constructor's migration set has already created, from the rows the caller builds. The table is read through the row type itself, so a caller cannot count one table and insert into another, and the insert ignores duplicate keys because several applications may reach an empty table at once. */
func seedIfEmptyRows[Row any](ctx context.Context, database *bun.DB, buildRows func() []*Row) error {
    count, countErr := database.
        NewSelect().
        Model((*Row)(nil)).
        Count(ctx)
    if nil != countErr {
        return countErr
    }

    if 0 < count {
        return nil
    }

    rowList := buildRows()

    _, insertErr := database.
        NewInsert().
        Model(&rowList).
        Ignore().
        Exec(ctx)

    return insertErr
}
