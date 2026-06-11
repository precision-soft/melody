package exception

import (
    "errors"
    "testing"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

func TestNewError_SetsFields(t *testing.T) {
    cause := errors.New("cause")

    err := NewError(
        "message",
        map[string]any{
            "key": "value",
        },
        cause,
    )

    if "message" != err.Error() {
        t.Fatalf("unexpected message: %s", err.Error())
    }

    if nil == err.Context() {
        t.Fatalf("expected context")
    }

    if "value" != err.Context()["key"] {
        t.Fatalf("unexpected context value")
    }

    if cause != err.CauseErr() {
        t.Fatalf("unexpected cause")
    }

    if loggingcontract.LevelError != err.Level() {
        t.Fatalf("unexpected level")
    }
}

func TestNewError_CopiesContextInConstructor(t *testing.T) {
    context := map[string]any{
        "key": "value",
    }

    err := NewError(
        "message",
        context,
        nil,
    )

    context["key"] = "changed"

    if "value" != err.Context()["key"] {
        t.Fatalf("expected constructor to copy context")
    }
}

func TestNewErrorWithLevel_OverridesLevel(t *testing.T) {
    err := newWithLevel(
        "message",
        nil,
        nil,
        loggingcontract.LevelInfo,
    )

    if loggingcontract.LevelInfo != err.Level() {
        t.Fatalf("unexpected level")
    }
}
