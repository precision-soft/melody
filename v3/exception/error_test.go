package exception

import (
    "errors"
    "testing"
)

func TestError_AlreadyLoggedFlag(t *testing.T) {
    err := NewError("message", nil, nil)

    if true == err.AlreadyLogged() {
        t.Fatalf("expected default alreadyLogged to be false")
    }

    err.MarkAsLogged()

    if false == err.AlreadyLogged() {
        t.Fatalf("expected alreadyLogged to be true")
    }
}

func TestIs_UsesUnwrap(t *testing.T) {
    base := errors.New("base")

    ex := NewError("wrapped", nil, base)

    if false == errors.Is(ex, base) {
        t.Fatalf("expected Is to match base via Unwrap")
    }
}
