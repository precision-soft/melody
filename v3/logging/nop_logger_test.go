package logging

import (
    "testing"
)

/* @info a typed nil stored in the interface is not equal to nil, so the plain nil check let it through and the first call panicked on the exact path this function exists to guard */
func TestEnsureLogger_ReplacesATypedNilLogger(t *testing.T) {
    var typedNil *jsonLogger

    ensured := EnsureLogger(typedNil)

    ensured.Info("message", nil)

    if _, isNop := ensured.(*nopLogger); false == isNop {
        t.Fatalf("expected a typed nil to be replaced by the nop logger, got %T", ensured)
    }
}

func TestEnsureLogger_KeepsAUsableLogger(t *testing.T) {
    logger := NewNopLogger()

    if logger != EnsureLogger(logger) {
        t.Fatalf("expected a usable logger to be kept")
    }
}
