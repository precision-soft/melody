package bag

import (
    "testing"
)

func TestStringMapStringString_CopiesAndErrors(t *testing.T) {
    parameterBag := NewParameterBag()

    parameterBag.Set(
        "headers",
        map[string]string{
            "X-Test": "a",
        },
    )

    value, exists, err := StringMapStringString(parameterBag, "headers")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if "a" != value["X-Test"] {
        t.Fatalf("expected %q", "a")
    }

    value["X-Test"] = "changed"

    valueAgain, exists, err := StringMapStringString(parameterBag, "headers")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if "a" != valueAgain["X-Test"] {
        t.Fatalf("expected deep copy")
    }

    parameterBag.Set("bad", 123)
    _, exists, err = StringMapStringString(parameterBag, "bad")
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if nil == err {
        t.Fatalf("expected error")
    }
}

/* @info an absent name is unset rather than an empty map, so a caller can tell a map nobody configured from one configured empty */
func TestStringMapStringString_AnAbsentNameIsUnsetRatherThanAnEmptyMap(t *testing.T) {
    parameterBag := NewParameterBag()

    value, exists, mapErr := StringMapStringString(parameterBag, "missing")
    if true == exists {
        t.Fatalf("expected an absent name to be reported unset")
    }
    if nil != mapErr {
        t.Fatalf("expected no error for an absent name, got %v", mapErr)
    }
    if nil != value {
        t.Fatalf("expected no map for an absent name, got %v", value)
    }
}
