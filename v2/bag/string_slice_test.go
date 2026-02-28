package bag

import (
    "testing"
)

func TestStringSlice_CopiesSlice_SupportsSingleString_AndHandlesInvalidType(t *testing.T) {
    parameterBag := NewParameterBag()

    parameterBag.Set("tags", []string{"a", "b"})
    values, exists := StringSlice(parameterBag, "tags")
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if 2 != len(values) {
        t.Fatalf("expected 2 values")
    }

    values[0] = "changed"

    valuesAgain, exists := StringSlice(parameterBag, "tags")
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if "a" != valuesAgain[0] {
        t.Fatalf("expected deep copy")
    }

    parameterBag.Set("one", "x")
    values, exists = StringSlice(parameterBag, "one")
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if 1 != len(values) {
        t.Fatalf("expected 1 value")
    }
    if "x" != values[0] {
        t.Fatalf("expected %q", "x")
    }

    parameterBag.Set("bad", 123)
    values, exists = StringSlice(parameterBag, "bad")
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if nil != values {
        t.Fatalf("expected nil values for invalid type")
    }
}

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

func TestStringAt_ErrorsAndHappyPath(t *testing.T) {
    parameterBag := NewParameterBag()

    _, exists, err := StringAt(parameterBag, "tags", 0)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if true == exists {
        t.Fatalf("expected exists false")
    }

    parameterBag.Set("tags", []string{"a", "b"})

    _, exists, err = StringAt(parameterBag, "tags", -1)
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if nil == err {
        t.Fatalf("expected error")
    }

    _, exists, err = StringAt(parameterBag, "tags", 2)
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if nil == err {
        t.Fatalf("expected error")
    }

    value, exists, err := StringAt(parameterBag, "tags", 1)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if "b" != value {
        t.Fatalf("expected %q, got %q", "b", value)
    }
}

func TestAppendString_AndAppendStringSlice(t *testing.T) {
    parameterBag := NewParameterBag()

    err := AppendString(parameterBag, "tags", "a")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    err = AppendString(parameterBag, "tags", "b")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    valuesAny, exists := parameterBag.Get("tags")
    if false == exists {
        t.Fatalf("expected exists true")
    }
    values := valuesAny.([]string)
    if 2 != len(values) {
        t.Fatalf("expected 2 values")
    }

    parameterBag.Set("single", "x")
    err = AppendString(parameterBag, "single", "y")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    valuesAny, exists = parameterBag.Get("single")
    if false == exists {
        t.Fatalf("expected exists true")
    }
    values = valuesAny.([]string)
    if 2 != len(values) {
        t.Fatalf("expected 2 values")
    }
    if "x" != values[0] {
        t.Fatalf("expected first value to be %q", "x")
    }
    if "y" != values[1] {
        t.Fatalf("expected second value to be %q", "y")
    }

    parameterBag.Set("bad", 123)
    err = AppendStringSlice(parameterBag, "bad", []string{"a", "b"})
    if nil == err {
        t.Fatalf("expected error")
    }

    valueAny, exists := parameterBag.Get("bad")
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if 123 != valueAny.(int) {
        t.Fatalf("expected value to remain unchanged on error")
    }
}
