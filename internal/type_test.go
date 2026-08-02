package internal

import (
    "testing"
)

type stringifyProbe struct{}

/* @info every refusal that names the type it was handed goes through here — the unsupported body type of the http client, the actual type of a parameter conversion — so a nil reaching it has to be NAMED rather than dereferenced: reflect.TypeOf answers nil for a nil interface, and String() on that nil is a panic inside the very error that was being built to describe the failure. */
func TestStringifyType_NamesTheNilRatherThanDereferencingIt(t *testing.T) {
    if "nil" != StringifyType(nil) {
        t.Fatalf("unexpected name for a nil value: %q", StringifyType(nil))
    }
}

/* @info the name has to be the type's own spelling, qualified enough that an operator reading a refusal can tell two same-shaped types apart. */
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

/* @info a TYPED nil is not the untyped one: it carries a type, and the refusal that names it is what tells a reader a nil pointer of a known type arrived rather than nothing at all. */
func TestStringifyType_ATypedNilKeepsItsType(t *testing.T) {
    var typedNil *stringifyProbe

    if "*internal.stringifyProbe" != StringifyType(typedNil) {
        t.Fatalf("expected a typed nil to keep its type, got %q", StringifyType(typedNil))
    }
}
