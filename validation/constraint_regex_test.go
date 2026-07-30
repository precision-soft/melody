package validation

import (
    "testing"
)

func TestRegex_PointerToStringMismatchIsRejected(t *testing.T) {
    constraint := NewRegex(`^[0-9]{3}$`)

    validationError := constraint.Validate(pointerOf("abcd"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: regex mismatch via *string passed validation")
    }
}

/* @info the empty pattern compiles to a regular expression that matches every string, so regex= silently enforced nothing */
func TestRegex_WithParamsRefusesEmptyPattern(t *testing.T) {
    constraint := NewRegex(`^[0-9]$`)

    for _, params := range []map[string]string{
        {"pattern": ""},
        {"value": ""},
    } {
        configured, withParamsErr := constraint.WithParams(params)

        if nil == withParamsErr || nil != configured {
            t.Fatalf("fail-open: an empty pattern was accepted for %v", params)
        }
    }
}

/* @info a pattern can never run against a non-string: []byte and interface fields silently bypassed the rule written for them */
func TestRegex_NonStringIsRejected(t *testing.T) {
    constraint := NewRegex(`^[A-Za-z0-9]+$`)

    validationError := constraint.Validate([]byte("<!-- injection -->"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: []byte passed a regex constraint without the pattern running")
    }
}
