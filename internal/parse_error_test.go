package internal

import (
    "testing"

    "github.com/precision-soft/melody/exception"
)

/* @info the refused value enters the context for the scalar shapes an operator acts on — a numeric refusal that named neither the value nor its magnitude could not be acted on; non-scalar shapes stay out */
func TestParseError_CarriesScalarValuesInContext(t *testing.T) {
    for _, probe := range []struct {
        value    any
        expected any
    }{
        {value: "text", expected: "text"},
        {value: 2.5, expected: 2.5},
        {value: 42, expected: 42},
        {value: int64(43), expected: int64(43)},
    } {
        parseErr := ParseError("probe", "int", probe.value, nil)
        contextValue, exists := parseErr.Context()["value"]
        if false == exists || probe.expected != contextValue {
            t.Fatalf("expected context value %#v for %#v, got %#v (exists=%v)", probe.expected, probe.value, contextValue, exists)
        }
    }

    structErr := ParseError("probe", "int", struct{ hidden string }{hidden: "x"}, nil)
    _, exists := structErr.Context()["value"]
    if true == exists {
        t.Fatalf("expected a non-scalar value to stay out of the context")
    }
}

func TestParseError_MessageNamesValidityWithCause(t *testing.T) {
    withoutCause := ParseError("probe", "int", "x", nil)
    if "parameter is not a 'int'" != withoutCause.Error() {
        t.Fatalf("unexpected message without cause: %q", withoutCause.Error())
    }

    withCause := ParseError("probe", "int", "x", exception.NewError("cause", nil, nil))
    if "parameter is not a valid 'int'" != withCause.Error() {
        t.Fatalf("unexpected message with cause: %q", withCause.Error())
    }
}
