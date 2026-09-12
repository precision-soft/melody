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

func TestRegex_NonStringIsRejected(t *testing.T) {
    constraint := NewRegex(`^[A-Za-z0-9]+$`)

    validationError := constraint.Validate([]byte("<!-- injection -->"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: []byte passed a regex constraint without the pattern running")
    }
}

func TestRegex_OptionalityExitsAndTheAccessorsThatDescribeThePattern(t *testing.T) {
    constraint := NewRegex(`^[0-9]{3}$`)

    if `^[0-9]{3}$` != constraint.Pattern() {
        t.Fatalf("expected the accessor to answer the configured pattern, got %q", constraint.Pattern())
    }

    if nil == constraint.Compiled() {
        t.Fatalf("expected a compilable pattern to carry its compiled form")
    }

    if nil != constraint.Error() {
        t.Fatalf("expected a compilable pattern to carry no error, got %v", constraint.Error())
    }

    if validationError := constraint.Validate(nil, "field"); nil != validationError {
        t.Fatalf("expected an absent value to pass, got %v", validationError)
    }

    var absentPointer *string
    if validationError := constraint.Validate(absentPointer, "field"); nil != validationError {
        t.Fatalf("expected a nil pointer to pass, got %v", validationError)
    }
}

/* the state under test is reachable: NewRegex keeps the compilation failure instead of panicking, so a hand-registered constraint can sit in it. */
func TestRegex_AnUncompilablePatternRefusesEveryValueItIsAsked(t *testing.T) {
    constraint := NewRegex(`^[0-9`)

    if nil == constraint.Error() {
        t.Fatalf("expected the uncompilable pattern to carry its compilation error")
    }

    if nil != constraint.Compiled() {
        t.Fatalf("expected the uncompilable pattern to carry no compiled form")
    }

    if `^[0-9` != constraint.Pattern() {
        t.Fatalf("expected the accessor to answer the pattern as written, got %q", constraint.Pattern())
    }

    validationError := constraint.Validate("123", "field")
    if nil == validationError {
        t.Fatalf("expected an uncompilable pattern to refuse the value rather than pass it")
    }

    if "invalid validation pattern" != validationError.Message() {
        t.Fatalf("unexpected refusal message: %q", validationError.Message())
    }

    if ConstraintRegexErrorInvalidPattern != validationError.Code() {
        t.Fatalf("expected the refusal to be told apart from a mismatch, got %q", validationError.Code())
    }

    /* the optionality exits still come first: an uncompilable pattern must not turn an unfilled optional field into a failure */
    if validationError := constraint.Validate("", "field"); nil != validationError {
        t.Fatalf("expected an empty string to stay optional even under a broken pattern, got %v", validationError)
    }
}
