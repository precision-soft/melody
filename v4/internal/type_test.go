package internal

import (
    "testing"
)

type stringifyProbe struct{}

func TestStringifyType_NamesTheNilRatherThanDereferencingIt(t *testing.T) {
    if "nil" != StringifyType(nil) {
        t.Fatalf("unexpected name for a nil value: %q", StringifyType(nil))
    }
}

func TestStringifyType_NamesTheTypeItWasHanded(t *testing.T) {
    for _, probe := range []struct {
        value    any
        expected string
    }{
        {value: 42, expected: "int"},
        {value: int64(42), expected: "int64"},
        {value: "text", expected: "string"},
        {value: 2.5, expected: "float64"},
        {value: true, expected: "bool"},
        {value: []string{}, expected: "[]string"},
        {value: map[string]int{}, expected: "map[string]int"},
        {value: stringifyProbe{}, expected: "internal.stringifyProbe"},
        {value: &stringifyProbe{}, expected: "*internal.stringifyProbe"},
    } {
        if probe.expected != StringifyType(probe.value) {
            t.Fatalf("expected %q for %#v, got %q", probe.expected, probe.value, StringifyType(probe.value))
        }
    }
}

func TestStringifyType_ATypedNilKeepsItsType(t *testing.T) {
    var typedNil *stringifyProbe

    if "*internal.stringifyProbe" != StringifyType(typedNil) {
        t.Fatalf("expected a typed nil to keep its type, got %q", StringifyType(typedNil))
    }
}
