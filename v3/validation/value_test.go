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

func TestDereferenceValue_AnAbsentValueResolvesToNothing(t *testing.T) {
    resolved, ok := dereferenceValue(nil)
    if true == ok || nil != resolved {
        t.Fatalf("expected an absent value to resolve to nothing, got %#v, %t", resolved, ok)
    }

    var absentPointer *string
    resolved, ok = dereferenceValue(absentPointer)
    if true == ok || nil != resolved {
        t.Fatalf("expected a nil pointer to resolve to nothing, got %#v, %t", resolved, ok)
    }

    var absentInterface any
    resolved, ok = dereferenceValue(&absentInterface)
    if true == ok || nil != resolved {
        t.Fatalf("expected a pointer to a nil interface to resolve to nothing, got %#v, %t", resolved, ok)
    }
}

func TestDereferenceValue_WalksThroughStackedPointerAndInterfaceLayers(t *testing.T) {
    plain := "abc"
    onePointer := &plain
    twoPointers := &onePointer
    var wrapped any = twoPointers
    pointerToWrapped := &wrapped

    for _, layered := range []any{plain, onePointer, twoPointers, wrapped, pointerToWrapped} {
        resolved, ok := dereferenceValue(layered)
        if false == ok {
            t.Fatalf("expected %T to resolve", layered)
        }

        if "abc" != resolved {
            t.Fatalf("expected %T to resolve to the plain string, got %#v", layered, resolved)
        }
    }
}
