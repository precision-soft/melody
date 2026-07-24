package validation

import (
    "testing"
)

type namedString string

func TestDereferenceValue_NamedStringNormalizesToPlainString(t *testing.T) {
    resolved, ok := dereferenceValue(namedString("user@example.com"))
    if false == ok {
        t.Fatalf("expected named string to resolve")
    }

    if _, isString := resolved.(string); false == isString {
        t.Fatalf("expected named string to normalize to plain string, got %T", resolved)
    }
}

func TestDereferenceValue_PointerToNamedStringNormalizes(t *testing.T) {
    value := namedString("abc")

    resolved, ok := dereferenceValue(&value)
    if false == ok {
        t.Fatalf("expected pointer to named string to resolve")
    }

    if _, isString := resolved.(string); false == isString {
        t.Fatalf("expected pointer to named string to normalize to plain string, got %T", resolved)
    }
}
