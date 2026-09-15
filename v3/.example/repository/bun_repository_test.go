package repository

import (
    "fmt"
    "testing"
)

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
