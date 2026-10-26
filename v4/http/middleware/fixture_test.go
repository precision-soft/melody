package middleware

import (
    "errors"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
)

func assertPanicsNamingField(t *testing.T, callback func(), expectedMessage string, field string) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatalf("expected a panic with the refusal %q", expectedMessage)
        }

        var refusal *exception.Error
        if false == errors.As(recoveredErr, &refusal) {
            t.Fatalf("expected the refusal %q, got %v", expectedMessage, recoveredErr)
        }

        if expectedMessage != refusal.Message() {
            t.Fatalf("expected the message %q, got %q", expectedMessage, refusal.Message())
        }

        if field != refusal.Context()["field"] {
            t.Fatalf("expected the refusal to name %q, got %v", field, refusal.Context()["field"])
        }
    }()

    callback()
}
