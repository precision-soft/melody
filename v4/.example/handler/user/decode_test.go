package user

import (
    "errors"
    "strings"
    "testing"
)

func TestDecodeOneDocument_AcceptsOneDocumentAndItsWhitespace(t *testing.T) {
    var target map[string]any
    if decodeErr := decodeOneDocument(strings.NewReader("{\"username\":\"ada\"}\n  "), &target); nil != decodeErr || "ada" != target["username"] {
        t.Fatalf("expected the document decoded, got %v, %v", target, decodeErr)
    }
}

func TestDecodeOneDocument_RefusesASecondDocument(t *testing.T) {
    var target map[string]any
    if decodeErr := decodeOneDocument(strings.NewReader("{\"username\":\"ada\"}{\"roles\":[\"ROLE_ADMIN\"]}"), &target); false == errors.Is(decodeErr, errTrailingData) {
        t.Fatalf("expected the trailing document refused, got %v", decodeErr)
    }
}

func TestDecodeOneDocument_RefusesTrailingGarbage(t *testing.T) {
    var target map[string]any
    if decodeErr := decodeOneDocument(strings.NewReader("{\"username\":\"ada\"} ]"), &target); nil == decodeErr {
        t.Fatal("expected trailing garbage refused")
    }
}
