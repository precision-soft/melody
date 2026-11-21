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

func TestStringSliceStrict_PresentNilReportsUnset(t *testing.T) {
    parameterBag := NewParameterBag()
    parameterBag.Set("tags", nil)

    values, exists, err := StringSliceStrict(parameterBag, "tags")
    if true == exists {
        t.Fatalf("expected StringSliceStrict to report a nil value as unset")
    }
    if nil != values {
        t.Fatalf("expected no values, got %v", values)
    }
    if nil != err {
        t.Fatalf("expected no error, got %v", err)
    }
}
