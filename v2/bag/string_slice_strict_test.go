package bag

import (
    "testing"
)

func TestStringSliceStrict_ReturnsErrorOnInvalidType(t *testing.T) {
    parameterBag := NewParameterBag()

    parameterBag.Set("bad", 123)

    _, exists, err := StringSliceStrict(parameterBag, "bad")
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if nil == err {
        t.Fatalf("expected error")
    }
}
