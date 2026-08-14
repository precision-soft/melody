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

func TestStringSliceStrict_CopiesTheStoredSliceAndLiftsASingleString(t *testing.T) {
    parameterBag := NewParameterBag()
    parameterBag.Set("repeated", []string{"a", "b"})
    parameterBag.Set("single", "melody")

    values, exists, valuesErr := StringSliceStrict(parameterBag, "repeated")
    if false == exists || nil != valuesErr {
        t.Fatalf("expected the repeated key to be read, got exists=%v err=%v", exists, valuesErr)
    }
    if 2 != len(values) || "a" != values[0] || "b" != values[1] {
        t.Fatalf("unexpected values: %#v", values)
    }

    values[0] = "mutated"

    stored, _ := parameterBag.Get("repeated")
    if "a" != stored.([]string)[0] {
        t.Fatalf("expected the stored slice to be isolated from mutations on the copy")
    }

    singleValues, singleExists, singleErr := StringSliceStrict(parameterBag, "single")
    if false == singleExists || nil != singleErr {
        t.Fatalf("expected the single occurrence to be read, got exists=%v err=%v", singleExists, singleErr)
    }
    if 1 != len(singleValues) || "melody" != singleValues[0] {
        t.Fatalf("expected a one-element slice, got %#v", singleValues)
    }

    _, missingExists, missingErr := StringSliceStrict(parameterBag, "missing")
    if true == missingExists || nil != missingErr {
        t.Fatalf("expected an absent key to be reported unset without an error")
    }
}
