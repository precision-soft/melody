package exception

import (
    "errors"
    "testing"

    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
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

func TestNamedConstructors_CarryTheirLevel(t *testing.T) {
    constructorList := []struct {
        name        string
        constructor func(string, exceptioncontract.Context, error) *Error
        level       loggingcontract.Level
    }{
        {"NewEmergency", NewEmergency, loggingcontract.LevelEmergency},
        {"NewError", NewError, loggingcontract.LevelError},
        {"NewWarning", NewWarning, loggingcontract.LevelWarning},
        {"NewInfo", NewInfo, loggingcontract.LevelInfo},
    }

    cause := errors.New("cause")

    for _, constructorEntry := range constructorList {
        err := constructorEntry.constructor("message", map[string]any{"key": "value"}, cause)

        if constructorEntry.level != err.Level() {
            t.Fatalf("%s: expected level %q, got %q", constructorEntry.name, constructorEntry.level, err.Level())
        }

        if "message" != err.Message() {
            t.Fatalf("%s: unexpected message %q", constructorEntry.name, err.Message())
        }

        if "value" != err.Context()["key"] {
            t.Fatalf("%s: unexpected context %v", constructorEntry.name, err.Context())
        }

        if cause != err.CauseErr() {
            t.Fatalf("%s: unexpected cause", constructorEntry.name)
        }
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
