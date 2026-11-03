package cors

import (
    "errors"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
)

func assertPanicsWithContext(t *testing.T, callback func(), expectedMessage string, key string, expectedValue string) {
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

        if expectedValue != refusal.Context()[key] {
            t.Fatalf("expected the refusal to carry %s %q, got %v", key, expectedValue, refusal.Context()[key])
        }
    }()

    callback()
}
