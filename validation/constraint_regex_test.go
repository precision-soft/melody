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

/* @info the regex constraint's optionality exits and its three accessors. The accessors had never been executed by anything, and they are the only way anything outside the package can tell a constraint that refused its pattern from one that compiled it: a Compiled that answered non-nil for a refused pattern, or an Error that answered nil, would make an introspection report a rule as armed while every value it sees is refused. */
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

/* @info a pattern that does not compile arms nothing, and every value it is asked about is refused by name rather than passed. The instance is reachable — NewRegex keeps the failure instead of panicking — so this is the state a hand-registered constraint sits in, and a Validate that fell through would enforce nothing on the field while the tag says it does. */
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
