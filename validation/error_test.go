package validation

import (
    "testing"

    "github.com/precision-soft/melody/exception"
)

/* @info the constructor copies on the way in and Context copies on the way out to keep the map private; ToExceptionError handed out the live map, so a consumer mutating the exception's context mutated the validation error through it */
func TestValidationError_ToExceptionErrorDoesNotAliasTheContext(t *testing.T) {
    validationError := NewValidationError("field", "message", "code", map[string]any{"actual": 42})

    exceptionErr := validationError.ToExceptionError()

    melodyError, ok := exceptionErr.(*exception.Error)
    if false == ok {
        t.Fatalf("expected *exception.Error, got %T", exceptionErr)
    }

    nested, ok := melodyError.Context()["context"].(map[string]any)
    if false == ok {
        t.Fatalf("expected the nested validation context to be present")
    }

    nested["actual"] = "mutated"

    if 42 != validationError.Context()["actual"] {
        t.Fatalf("external mutation reached the validation error's internal context: %v", validationError.Context()["actual"])
    }
}
