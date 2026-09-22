package repository

import (
    "context"
    "database/sql"

    bun "github.com/uptrace/bun"
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

/* seedIfEmptyRows is the body the four seeded repositories had a copy of each.
What differs between them stays at the caller: which rows to build, and from
which seed list. The table is read through the row type itself, so a caller
cannot count one table and insert into another. */
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
