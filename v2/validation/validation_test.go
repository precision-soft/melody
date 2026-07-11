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

/* @info regex shorthand fail-open + comma-in-meta */

type payloadWithRegexShorthand struct {
    Value string `validate:"regex=^abc$"`
}

type payloadWithRegexShorthandCommaInCharClass struct {
    Value string `validate:"regex=^[a,b]$"`
}

type payloadWithRegexShorthandCommaInQuantifier struct {
    Value string `validate:"regex=^a{1,2}$"`
}

func TestValidator_RegexShorthandIsEnforcedNotFailOpen(t *testing.T) {
    validatorInstance := NewValidator()

    requireValidationErrors(t, validatorInstance.Validate(payloadWithRegexShorthand{Value: "does-not-match"}))
    requireNoValidationErrors(t, validatorInstance.Validate(payloadWithRegexShorthand{Value: "abc"}))
}

func TestValidator_RegexShorthandWithCommaMatchesParenthesizedForm(t *testing.T) {
    validatorInstance := NewValidator()

    requireNoValidationErrors(t, validatorInstance.Validate(payloadWithRegexShorthandCommaInCharClass{Value: "a"}))
    requireNoValidationErrors(t, validatorInstance.Validate(payloadWithRegexShorthandCommaInCharClass{Value: "b"}))
    requireValidationErrors(t, validatorInstance.Validate(payloadWithRegexShorthandCommaInCharClass{Value: "z"}))

    requireNoValidationErrors(t, validatorInstance.Validate(payloadWithRegexShorthandCommaInQuantifier{Value: "aa"}))
    requireValidationErrors(t, validatorInstance.Validate(payloadWithRegexShorthandCommaInQuantifier{Value: "aaa"}))
}
