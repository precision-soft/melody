package bag

import (
    "errors"
    "testing"

    "github.com/precision-soft/melody/v3/internal"
)

func TestCreateConversionError_MessageAndContext(t *testing.T) {
    err := internal.ParseError("n", "int", "not-a-number", errors.New("cause"))
    if nil == err {
        t.Fatalf("expected error")
    }
    if "parameter is not a valid 'int'" != err.Message() {
        t.Fatalf("unexpected message: %q", err.Message())
    }
    if nil == err.Context() {
        t.Fatalf("expected context")
    }
    if "n" != err.Context()["parameterName"] {
        t.Fatalf("expected parameterName in context")
    }

    err = internal.ParseError("n", "int", 123, nil)
    if nil == err {
        t.Fatalf("expected error")
    }
    if "parameter is not a 'int'" != err.Message() {
        t.Fatalf("unexpected message: %q", err.Message())
    }
}
