package validation

import (
    "testing"

    "github.com/precision-soft/melody/v2/container"
    validationcontract "github.com/precision-soft/melody/v2/validation/contract"
)

type testPayload struct {
    Email string `json:"email" validate:"notBlank,email"`
    Name  string `json:"name" validate:"notBlank,min=3,max=10"`
}

type customPayload struct {
    Code string `json:"code" validate:"my_custom"`
}

type customConstraint struct{}

func (instance *customConstraint) Validate(value any, field string) validationcontract.ValidationError {
    stringValue, ok := value.(string)
    if false == ok {
        return NewValidationError("", "invalid type", "invalid_type", nil)
    }

    if "ABC" != stringValue {
        return NewValidationError(
            "",
            "must be abc",
            "not_abc",
            map[string]any{
                "expected": "ABC",
                "actual":   stringValue,
            },
        )
    }

    return nil
}

type payloadWithUnknownRule struct {
    Name string `json:"name" validate:"unknownRule"`
}

type payloadWithMalformedGreaterThan struct {
    Quantity int `json:"quantity" validate:"greaterThan(value=abc)"`
}

type payloadWithMalformedMinLength struct {
    Name string `json:"name" validate:"min(value=notanumber)"`
}

type payloadWithFractionalMaxLength struct {
    Name string `json:"name" validate:"max(value=3.9)"`
}

type payloadWithJsonName struct {
    Value string `json:"my_value" validate:"notBlank"`
}

type payloadWithPrivateField struct {
    value string `validate:"notBlank"`
    Name  string `validate:"notBlank"`
}

type payloadWithIgnoredTag struct {
    Name string `validate:"-"`
}

type payloadWithRegex struct {
    Value string `validate:"regex(pattern=^abc$)"`
}

type payloadWithRegexCommaInCharClass struct {
    Value string `validate:"regex(pattern=^[a,b]$)"`
}

type payloadWithRegexCommaInQuantifier struct {
    Value string `validate:"regex(pattern=^a{1,2}$)"`
}

type payloadWithInvalidTag struct {
    Name string `validate:"min(1))"`
}

func TestValidator_DetectsErrors(t *testing.T) {
    validatorInstance := NewValidator()

    payload := testPayload{}

    err := validatorInstance.Validate(payload)
    validationErrors := requireValidationErrors(t, err)

    if len(validationErrors) < 2 {
        t.Fatalf("expected at least 2 errors, got %d", len(validationErrors))
    }
}

func TestValidator_AcceptsValidData(t *testing.T) {
    validatorInstance := NewValidator()

    payload := testPayload{
        Email: "user@example.com",
        Name:  "John Doe",
    }

    err := validatorInstance.Validate(payload)
    requireNoValidationErrors(t, err)
}

func TestValidator_CustomConstraint(t *testing.T) {
    validatorInstance := NewValidator()
    validatorInstance.RegisterConstraint("my_custom", &customConstraint{})

    payload := customPayload{
        Code: "XYZ",
    }

    err := validatorInstance.Validate(payload)
    validationErrors := requireValidationErrors(t, err)

    validationError, ok := validationErrors[0].(*ValidationError)
    if false == ok {
        t.Fatalf("expected *ValidationError")
    }

    if "Code" != validationError.Field() && "code" != validationError.Field() {
        t.Fatalf("expected field to be set by validator")
    }

    payload.Code = "ABC"
    err = validatorInstance.Validate(payload)
    requireNoValidationErrors(t, err)
}

func TestValidator_RegisterConstraint_PanicsOnDuplicateName(t *testing.T) {
    validatorInstance := NewValidator()
    validatorInstance.RegisterConstraint("my_custom", &customConstraint{})

    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic")
        }
    }()

    validatorInstance.RegisterConstraint("my_custom", &customConstraint{})
}

func TestValidator_ReturnsUnknownRuleError(t *testing.T) {
    validatorInstance := NewValidator()

    err := validatorInstance.Validate(payloadWithUnknownRule{Name: "x"})
    validationErrors := requireValidationErrors(t, err)

    validationError, ok := validationErrors[0].(*ValidationError)
    if false == ok {
        t.Fatalf("expected *ValidationError")
    }

    if ErrorUnknownRule != validationError.Code() {
        t.Fatalf("unexpected code `" + validationError.Code() + "`")
    }
}

func TestValidator_MalformedNumericParameterFailsClosed(t *testing.T) {
    validatorInstance := NewValidator()

    /* a non-numeric constraint parameter must be rejected, not silently degraded to a default bound */
    for _, payload := range []any{
        payloadWithMalformedGreaterThan{Quantity: 5},
        payloadWithMalformedMinLength{Name: "anything"},
    } {
        validationErrors := requireValidationErrors(t, validatorInstance.Validate(payload))

        validationError, ok := validationErrors[0].(*ValidationError)
        if false == ok {
            t.Fatalf("expected *ValidationError, got %T", validationErrors[0])
        }

        if ErrorInvalidRuleSyntax != validationError.Code() {
            t.Fatalf("expected a malformed numeric parameter to fail closed with code `" + ErrorInvalidRuleSyntax + "`, got `" + validationError.Code() + "`")
        }
    }

    /* a valid leading integer (3.9 -> 3) is still accepted, so the field is enforced rather than rejected as malformed */
    requireNoValidationErrors(t, validatorInstance.Validate(payloadWithFractionalMaxLength{Name: "abc"}))
    requireValidationErrors(t, validatorInstance.Validate(payloadWithFractionalMaxLength{Name: "abcd"}))
}

func TestValidator_MapsJsonTagNameAsField(t *testing.T) {
    validatorInstance := NewValidator()

    err := validatorInstance.Validate(payloadWithJsonName{Value: ""})
    validationErrors := requireValidationErrors(t, err)

    validationError, ok := validationErrors[0].(*ValidationError)
    if false == ok {
        t.Fatalf("expected *ValidationError")
    }

    if "my_value" != validationError.Field() {
        t.Fatalf("expected json field name")
    }
}

func TestValidator_SkipsUnexportedFields(t *testing.T) {
    validatorInstance := NewValidator()

    err := validatorInstance.Validate(payloadWithPrivateField{
        value: "",
        Name:  "",
    })
    validationErrors := requireValidationErrors(t, err)

    if 1 != len(validationErrors) {
        t.Fatalf("expected 1 error")
    }
}

func TestValidator_IgnoresValidateDashTag(t *testing.T) {
    validatorInstance := NewValidator()

    err := validatorInstance.Validate(payloadWithIgnoredTag{Name: ""})
    requireNoValidationErrors(t, err)
}

func TestValidator_Validate_ReturnsEmptyWhenNilInput(t *testing.T) {
    validatorInstance := NewValidator()

    err := validatorInstance.Validate(nil)
    requireNoValidationErrors(t, err)
}

func TestValidator_Validate_ReturnsEmptyWhenNonStruct(t *testing.T) {
    validatorInstance := NewValidator()

    err := validatorInstance.Validate("x")
    requireNoValidationErrors(t, err)
}

func TestValidator_Validate_WorksWithPointerToStruct(t *testing.T) {
    validatorInstance := NewValidator()

    payload := &testPayload{}

    err := validatorInstance.Validate(payload)
    _ = requireValidationErrors(t, err)
}

func TestValidator_Validate_TypedNilPointer_ReturnsEmptyWithoutPanic(t *testing.T) {
    validatorInstance := NewValidator()

    defer func() {
        if nil != recover() {
            t.Fatalf("did not expect panic")
        }
    }()

    var payload *testPayload = nil

    err := validatorInstance.Validate(payload)
    requireNoValidationErrors(t, err)
}

func TestValidator_RegexConstraint_WithPatternParam(t *testing.T) {
    validatorInstance := NewValidator()

    err := validatorInstance.Validate(payloadWithRegex{Value: "zzz"})
    _ = requireValidationErrors(t, err)

    err = validatorInstance.Validate(payloadWithRegex{Value: "abc"})
    requireNoValidationErrors(t, err)
}

func TestValidator_RegexConstraint_AllowsCommaInsideCharClass(t *testing.T) {
    validatorInstance := NewValidator()

    err := validatorInstance.Validate(payloadWithRegexCommaInCharClass{Value: "z"})
    _ = requireValidationErrors(t, err)

    err = validatorInstance.Validate(payloadWithRegexCommaInCharClass{Value: "a"})
    requireNoValidationErrors(t, err)
}

func TestValidator_RegexConstraint_AllowsCommaInsideQuantifier(t *testing.T) {
    validatorInstance := NewValidator()

    err := validatorInstance.Validate(payloadWithRegexCommaInQuantifier{Value: "aaa"})
    _ = requireValidationErrors(t, err)

    err = validatorInstance.Validate(payloadWithRegexCommaInQuantifier{Value: "a"})
    requireNoValidationErrors(t, err)

    err = validatorInstance.Validate(payloadWithRegexCommaInQuantifier{Value: "aa"})
    requireNoValidationErrors(t, err)
}

func TestValidatorFromContainer_ReturnsNilWhenMissing(t *testing.T) {
    serviceContainer := container.NewContainer()

    validatorInstance := ValidatorFromContainer(serviceContainer)
    if nil != validatorInstance {
        t.Fatalf("expected nil")
    }
}

func TestValidatorMustFromContainer_PanicsWhenMissing(t *testing.T) {
    serviceContainer := container.NewContainer()

    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic")
        }
    }()

    _ = ValidatorMustFromContainer(serviceContainer)
}

func TestValidator_Validate_ReturnsInvalidRuleSyntaxErrorForInvalidTag(t *testing.T) {
    validatorInstance := NewValidator()

    err := validatorInstance.Validate(payloadWithInvalidTag{Name: "x"})
    validationErrors := requireValidationErrors(t, err)

    validationError, ok := validationErrors[0].(*ValidationError)
    if false == ok {
        t.Fatalf("expected *ValidationError")
    }

    if ErrorInvalidRuleSyntax != validationError.Code() {
        t.Fatalf("expected code `%s`, got `%s`", ErrorInvalidRuleSyntax, validationError.Code())
    }
}
