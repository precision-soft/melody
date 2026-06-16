package validation

import (
    "testing"
)

func requireNoValidationErrors(t *testing.T, err error) {
    t.Helper()

    if nil == err {
        return
    }

    t.Fatalf("expected no validation errors, got: %s", err.Error())
}

func requireValidationErrors(t *testing.T, err error) ValidationErrors {
    t.Helper()

    if nil == err {
        t.Fatalf("expected validation errors")
    }

    validationErrors, ok := err.(ValidationErrors)
    if false == ok {
        t.Fatalf("expected ValidationErrors type, got: %T", err)
    }

    if false == validationErrors.HasErrors() {
        t.Fatalf("expected validation errors")
    }

    return validationErrors
}

func pointerOf(value string) *string {
    return &value
}
