package repository

import (
    "database/sql"
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
