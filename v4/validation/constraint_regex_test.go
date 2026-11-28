package validation

import (
    validationcontract "github.com/precision-soft/melody/v4/validation/contract"
    "testing"
)

func TestRegex_PointerToStringMismatchIsRejected(t *testing.T) {
    constraint := NewRegex(`^[0-9]{3}$`)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, pointerOf("abcd"))

    if nil == validationError {
        t.Fatalf("fail-open: regex mismatch via *string passed validation")
    }
}

func TestRegex_WithParametersRefusesEmptyPattern(t *testing.T) {
    constraint := NewRegex(`^[0-9]$`)

    for _, parameters := range []map[string]string{
        {"pattern": ""},
        {"value": ""},
    } {
        configured, withParametersErr := constraint.WithParameters(parameters)

        if nil == withParametersErr || nil != configured {
            t.Fatalf("fail-open: an empty pattern was accepted for %v", parameters)
        }
    }
}

func TestRegex_NonStringIsRejected(t *testing.T) {
    constraint := NewRegex(`^[A-Za-z0-9]+$`)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, []byte("<!-- injection -->"))

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

    if validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, nil); nil != validationError {
        t.Fatalf("expected an absent value to pass, got %v", validationError)
    }

    var absentPointer *string
    if validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, absentPointer); nil != validationError {
        t.Fatalf("expected a nil pointer to pass, got %v", validationError)
    }
}

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

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, "123")
    if nil == validationError {
        t.Fatalf("expected an uncompilable pattern to refuse the value rather than pass it")
    }

    if "invalid validation pattern" != validationError.Message() {
        t.Fatalf("unexpected refusal message: %q", validationError.Message())
    }

    if ConstraintRegexErrorInvalidPattern != validationError.Code() {
        t.Fatalf("expected the refusal to be told apart from a mismatch, got %q", validationError.Code())
    }

    if validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, ""); nil != validationError {
        t.Fatalf("expected an empty string to stay optional even under a broken pattern, got %v", validationError)
    }
}

func TestRegex_TheConstructorRefusesAnEmptyPattern(t *testing.T) {
    constraint := NewRegex("")

    if nil == constraint.Error() || nil != constraint.Compiled() {
        t.Fatalf("expected the empty pattern to be kept as the constraint's error, got error=%v compiled=%v", constraint.Error(), constraint.Compiled())
    }

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, "anything")
    if nil == validationError || ConstraintRegexErrorInvalidPattern != validationError.Code() {
        t.Fatalf("expected a non-empty value to be refused as an invalid pattern, got %v", validationError)
    }

    if validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, ""); nil != validationError {
        t.Fatalf("expected the empty string to stay optional under the empty pattern, got %v", validationError)
    }
}

func TestRegexDeclarationError_AnswersTheCompileError(t *testing.T) {
    constraint := NewRegex("^[a$")

    if nil == constraint.DeclarationError() || constraint.Error() != constraint.DeclarationError() {
        t.Fatalf("expected the compile error as the declaration error, got %v", constraint.DeclarationError())
    }
}

func TestRegexDeclarationError_SoundPattern_Nil(t *testing.T) {
    if declarationErr := NewRegex("^a+$").DeclarationError(); nil != declarationErr {
        t.Fatalf("expected a sound pattern to report no declaration error, got %v", declarationErr)
    }
}
