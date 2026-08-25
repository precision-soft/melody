package repository

import (
    "database/sql"
    "fmt"
    "testing"
)

type stubResult struct {
    affected    int64
    affectedErr error
}

func (instance stubResult) LastInsertId() (int64, error) {
    return 0, nil
}

func (instance stubResult) RowsAffected() (int64, error) {
    return instance.affected, instance.affectedErr
}

var _ sql.Result = stubResult{}

/* the answer feeds a caller that has to tell a write which landed from one that found no row, so
a driver that will not report a count must read as "nothing changed" rather than as a change nobody
can confirm */

func TestAffectedAtLeastOneRow(t *testing.T) {
    if true == affectedAtLeastOneRow(nil) {
        t.Fatalf("expected a missing result to report no change")
    }

    if true == affectedAtLeastOneRow(stubResult{affected: 0}) {
        t.Fatalf("expected zero affected rows to report no change")
    }

    if false == affectedAtLeastOneRow(stubResult{affected: 1}) {
        t.Fatalf("expected one affected row to report a change")
    }

    if true == affectedAtLeastOneRow(stubResult{affected: 3, affectedErr: fmt.Errorf("unsupported")}) {
        t.Fatalf("expected a driver that will not report a count to read as no change")
    }
}
