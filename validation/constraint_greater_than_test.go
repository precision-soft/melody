package validation

import (
    "math"
    "testing"
)

func TestGreaterThan_RejectsNaN(t *testing.T) {
    constraint := NewGreaterThan(0)

    if nil == constraint.Validate(math.NaN(), "score") {
        t.Fatalf("greaterThan must reject a NaN float (NaN compares false against every bound), got no error")
    }

    if nil != constraint.Validate(5.0, "score") {
        t.Fatalf("greaterThan(0) must still accept a finite value above the bound")
    }
}
