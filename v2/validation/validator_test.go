package validation

import (
    "fmt"
    "testing"

    "github.com/precision-soft/melody/v2/container"
    "github.com/precision-soft/melody/v2/exception"
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

type payloadWithGreaterThanUnknownParam struct {
    Count int `json:"count" validate:"greaterThan(min=18)"`
}

type payloadWithLessThanUnknownParam struct {
    Count int `json:"count" validate:"lessThan(max=5)"`
}

type payloadWithMinLengthUnknownParam struct {
    Name string `json:"name" validate:"min(len=8)"`
}

type payloadWithMaxLengthUnknownParam struct {
    Name string `json:"name" validate:"max(limit=10)"`
}

type payloadWithRegexUnknownParam struct {
    Value string `json:"value" validate:"regex(re=^[0-9]{4}$)"`
}

type payloadWithGreaterThanShorthand struct {
    Count int `json:"count" validate:"greaterThan=5"`
}

func TestValidator_UnrecognizedParameterKeyFailsClosed(t *testing.T) {
    validatorInstance := NewValidator()

    /* a parameterizable constraint that receives parameters without its recognized key ("value", or "pattern"/"value" for regex) must fail closed rather than silently degrade to the registered default bound: a fail-open here would leave the field validated against a weaker constraint than the tag declares (regex(re=...) degrading to the match-all `.*` default, min(len=8) to a floor of 1) */
    for _, payload := range []any{
        payloadWithGreaterThanUnknownParam{Count: -1},
        payloadWithLessThanUnknownParam{Count: 100},
        payloadWithMinLengthUnknownParam{Name: ""},
        payloadWithMaxLengthUnknownParam{Name: "value"},
        payloadWithRegexUnknownParam{Value: "not-four-digits"},
    } {
        validationErrors := requireValidationErrors(t, validatorInstance.Validate(payload))

        validationError, ok := validationErrors[0].(*ValidationError)
        if false == ok {
            t.Fatalf("expected *ValidationError, got %T", validationErrors[0])
        }

        if ErrorInvalidRuleSyntax != validationError.Code() {
            t.Fatalf("expected an unrecognized constraint parameter to fail closed with code %q, got %q", ErrorInvalidRuleSyntax, validationError.Code())
        }
    }

    /* the shorthand form maps to the recognized `value` key, so it still configures and enforces the bound: 3 is not greater than 5, 9 is */
    requireValidationErrors(t, validatorInstance.Validate(payloadWithGreaterThanShorthand{Count: 3}))
    requireNoValidationErrors(t, validatorInstance.Validate(payloadWithGreaterThanShorthand{Count: 9}))
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

/* @info parameterized-constraint fail-closed */

type betweenLengthConstraint struct {
    min int
    max int
}

func (instance *betweenLengthConstraint) Validate(value any, field string) validationcontract.ValidationError {
    resolved, ok := dereferenceValue(value)
    if false == ok {
        return nil
    }

    stringValue := fmt.Sprintf("%v", resolved)
    if len(stringValue) < instance.min || len(stringValue) > instance.max {
        return NewValidationError(field, "length out of bounds", "betweenLength", nil)
    }

    return nil
}

func (instance *betweenLengthConstraint) WithParams(params map[string]string) (validationcontract.Constraint, error) {
    configured := &betweenLengthConstraint{min: instance.min, max: instance.max}

    if valueString, exists := params["min"]; true == exists {
        parsed, ok := parseIntStrict(valueString)
        if false == ok {
            return nil, exception.NewError("invalid between min parameter", nil, nil)
        }
        configured.min = parsed
    }

    if valueString, exists := params["max"]; true == exists {
        parsed, ok := parseIntStrict(valueString)
        if false == ok {
            return nil, exception.NewError("invalid between max parameter", nil, nil)
        }
        configured.max = parsed
    }

    return configured, nil
}

type rigidConstraint struct{}

func (instance *rigidConstraint) Validate(value any, field string) validationcontract.ValidationError {
    return nil
}

type payloadWithBetween struct {
    Code string `json:"code" validate:"betweenLength(min=2,max=3)"`
}

type payloadWithRigidParams struct {
    Code string `json:"code" validate:"rigid(strictness=high)"`
}

type payloadWithLessThan struct {
    Quantity int `json:"quantity" validate:"lessThan(value=100)"`
}

type payloadWithLessThanShorthand struct {
    Quantity int `json:"quantity" validate:"lessThan=100"`
}

func TestValidator_LessThanIsEnforcedAndNotUnknownRule(t *testing.T) {
    validatorInstance := NewValidator()

    requireValidationErrors(t, validatorInstance.Validate(payloadWithLessThan{Quantity: 100}))
    requireValidationErrors(t, validatorInstance.Validate(payloadWithLessThan{Quantity: 150}))
    requireNoValidationErrors(t, validatorInstance.Validate(payloadWithLessThan{Quantity: 99}))

    requireValidationErrors(t, validatorInstance.Validate(payloadWithLessThanShorthand{Quantity: 150}))
    requireNoValidationErrors(t, validatorInstance.Validate(payloadWithLessThanShorthand{Quantity: 99}))
}

func TestValidator_CustomParameterizedConstraintConsumesParams(t *testing.T) {
    validatorInstance := NewValidator()
    validatorInstance.RegisterConstraint("betweenLength", &betweenLengthConstraint{min: 0, max: 1000})

    tooShort := payloadWithBetween{Code: "a"}
    err := validatorInstance.Validate(tooShort)
    requireValidationErrors(t, err)

    inBounds := payloadWithBetween{Code: "ab"}
    err = validatorInstance.Validate(inBounds)
    requireNoValidationErrors(t, err)

    tooLong := payloadWithBetween{Code: "abcd"}
    err = validatorInstance.Validate(tooLong)
    requireValidationErrors(t, err)
}

func TestValidator_ParamsOnNonParameterizableConstraintFailClosed(t *testing.T) {
    validatorInstance := NewValidator()
    validatorInstance.RegisterConstraint("rigid", &rigidConstraint{})

    /* @important a parameterized constraint given parameters without its recognized key must be rejected as invalid rather than validated with the registered singleton (which would fail open) */
    err := validatorInstance.Validate(payloadWithRigidParams{Code: "anything"})
    validationErrors := requireValidationErrors(t, err)

    validationError, ok := validationErrors[0].(*ValidationError)
    if false == ok {
        t.Fatalf("expected *ValidationError")
    }

    if ErrorInvalidRuleSyntax != validationError.Code() {
        t.Fatalf("expected the invalid-rule code, got %q", validationError.Code())
    }
}

func TestValidator_ParamsOnNonParameterizableBuiltInFailClosed(t *testing.T) {
    validatorInstance := NewValidator()

    type payloadWithParameterizedEmail struct {
        Email string `json:"email" validate:"email(strict=yes)"`
    }

    err := validatorInstance.Validate(payloadWithParameterizedEmail{Email: "valid@example.com"})
    requireValidationErrors(t, err)
}

type nestedItem struct {
    Sku string `json:"sku" validate:"notBlank,regex=^[A-Z0-9]{8}$"`
}

type nestedOrder struct {
    Items []nestedItem `json:"items" validate:"notEmpty"`
    Bill  nestedItem   `json:"bill"`
}

type cyclicNode struct {
    Name string      `json:"name" validate:"notBlank"`
    Next *cyclicNode `json:"next"`
}

func findValidationErrorByField(validationErrors ValidationErrors, field string) validationcontract.ValidationError {
    for _, candidate := range validationErrors {
        if field == candidate.Field() {
            return candidate
        }
    }

    return nil
}

/** @info validate tags declared on nested struct fields and on slice-of-struct elements are enforced (the flat validator returned nil for these), with a path identifying the offending nested field. */
func TestValidator_EnforcesNestedConstraints(t *testing.T) {
    validatorInstance := NewValidator()

    payload := nestedOrder{
        Items: []nestedItem{{Sku: ""}},
        Bill:  nestedItem{Sku: "###"},
    }

    err := validatorInstance.Validate(payload)
    validationErrors := requireValidationErrors(t, err)

    if nil == findValidationErrorByField(validationErrors, "items[0].sku") {
        t.Fatalf("expected an error on items[0].sku, got: %s", validationErrors.Error())
    }

    if nil == findValidationErrorByField(validationErrors, "bill.sku") {
        t.Fatalf("expected an error on bill.sku, got: %s", validationErrors.Error())
    }
}

/** @info a fully valid nested payload still passes so the cascade adds no false rejections. */
func TestValidator_AcceptsValidNestedPayload(t *testing.T) {
    validatorInstance := NewValidator()

    payload := nestedOrder{
        Items: []nestedItem{{Sku: "ABCD1234"}},
        Bill:  nestedItem{Sku: "ZZZZ9999"},
    }

    err := validatorInstance.Validate(payload)
    requireNoValidationErrors(t, err)
}

/** @info a self-referential value must not hang or overflow the stack during the recursive cascade. */
func TestValidator_CyclicPayloadTerminates(t *testing.T) {
    validatorInstance := NewValidator()

    node := &cyclicNode{Name: "root"}
    node.Next = node

    err := validatorInstance.Validate(node)
    requireNoValidationErrors(t, err)
}
