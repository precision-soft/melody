package repository

import (
    "database/sql"
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
