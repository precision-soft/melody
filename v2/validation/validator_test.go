package validation

import (
    "fmt"
    "testing"

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

type payloadWithRegexShorthand struct {
    Value string `validate:"regex=^abc$"`
}

type payloadWithRegexShorthandCommaInCharClass struct {
    Value string `validate:"regex=^[a,b]$"`
}

type payloadWithRegexShorthandCommaInQuantifier struct {
    Value string `validate:"regex=^a{1,2}$"`
}

type payloadWithInvalidTag struct {
    Name string `validate:"min(1))"`
}

type payloadWithLessThan struct {
    Quantity int `json:"quantity" validate:"lessThan(value=100)"`
}

type payloadWithLessThanShorthand struct {
    Quantity int `json:"quantity" validate:"lessThan=100"`
}

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

func TestValidator_LessThanIsEnforcedAndNotUnknownRule(t *testing.T) {
    validatorInstance := NewValidator()

    requireValidationErrors(t, validatorInstance.Validate(payloadWithLessThan{Quantity: 100}))
    requireValidationErrors(t, validatorInstance.Validate(payloadWithLessThan{Quantity: 150}))
    requireNoValidationErrors(t, validatorInstance.Validate(payloadWithLessThan{Quantity: 99}))

    requireValidationErrors(t, validatorInstance.Validate(payloadWithLessThanShorthand{Quantity: 150}))
    requireNoValidationErrors(t, validatorInstance.Validate(payloadWithLessThanShorthand{Quantity: 99}))
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
        t.Fatalf("unexpected code %q", validationError.Code())
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
            t.Fatalf("expected a malformed numeric parameter to fail closed with code %q, got %q", ErrorInvalidRuleSyntax, validationError.Code())
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

type payloadWithGreaterThan struct {
    Age int `json:"age" validate:"greaterThan=0"`
}

type payloadWithGreaterThanFloat struct {
    Price float64 `json:"price" validate:"greaterThan=0"`
}

func TestValidator_GreaterThanConstraint_PassesForPositiveValue(t *testing.T) {
    validatorInstance := NewValidator()

    err := validatorInstance.Validate(payloadWithGreaterThan{Age: 25})
    requireNoValidationErrors(t, err)
}

func TestValidator_GreaterThanConstraint_FailsForZero(t *testing.T) {
    validatorInstance := NewValidator()

    err := validatorInstance.Validate(payloadWithGreaterThan{Age: 0})
    _ = requireValidationErrors(t, err)
}

func TestValidator_GreaterThanConstraint_FailsForNegativeValue(t *testing.T) {
    validatorInstance := NewValidator()

    err := validatorInstance.Validate(payloadWithGreaterThan{Age: -5})
    _ = requireValidationErrors(t, err)
}

func TestValidator_GreaterThanConstraint_Float64PassesForPositiveValue(t *testing.T) {
    validatorInstance := NewValidator()

    err := validatorInstance.Validate(payloadWithGreaterThanFloat{Price: 0.01})
    requireNoValidationErrors(t, err)
}

func TestValidator_GreaterThanConstraint_Float64FailsForZero(t *testing.T) {
    validatorInstance := NewValidator()

    err := validatorInstance.Validate(payloadWithGreaterThanFloat{Price: 0.0})
    _ = requireValidationErrors(t, err)
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

/* @info validate tags declared on nested struct fields and on slice-of-struct elements are enforced, with a path identifying the offending nested field. */
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

/* @info a fully valid nested payload still passes so the cascade adds no false rejections. */
func TestValidator_AcceptsValidNestedPayload(t *testing.T) {
    validatorInstance := NewValidator()

    payload := nestedOrder{
        Items: []nestedItem{{Sku: "ABCD1234"}},
        Bill:  nestedItem{Sku: "ZZZZ9999"},
    }

    err := validatorInstance.Validate(payload)
    requireNoValidationErrors(t, err)
}

/* @info a self-referential value must not hang or overflow the stack during the recursive cascade. */
func TestValidator_CyclicPayloadTerminates(t *testing.T) {
    validatorInstance := NewValidator()

    node := &cyclicNode{Name: "root"}
    node.Next = node

    err := validatorInstance.Validate(node)
    requireNoValidationErrors(t, err)
}

/* @info a field encoding/json never populates must not be validated: its permanent zero value would fail the tag on every request, while the openapi mirror rightly omits the field from the schema — the whole endpoint would advertise one contract and reject another */
func TestValidateStruct_SkipsAFieldJsonNeverPopulates(t *testing.T) {
    type payload struct {
        Internal string `json:"-" validate:"notBlank"`
        Name     string `json:"name"`
    }

    validator := NewValidator()

    validateErr := validator.Validate(&payload{Name: "ok"})
    if nil != validateErr {
        t.Fatalf("expected the json:\"-\" field to be skipped, got %v", validateErr)
    }
}

type shadowedStamp struct {
    UpdatedBy string `json:"updatedBy" validate:"notBlank"`
}

type shadowingPayload struct {
    shadowedStamp
    UpdatedBy string `json:"updatedBy"`
}

/* @info the outer field shadows the promoted one under encoding/json's dominance, so the payload can never populate the embed's field; validating its permanent zero value rejected every request while the schema mirror documented only the winner */
func TestValidateStruct_SkipsAPromotedFieldShadowedByTheOuterOne(t *testing.T) {
    validator := NewValidator()

    validateErr := validator.Validate(&shadowingPayload{UpdatedBy: ""})
    if nil != validateErr {
        t.Fatalf("expected the shadowed promoted field to be skipped, got %v", validateErr)
    }
}

type ambiguousLeft struct {
    Origin string `validate:"notBlank"`
}

type ambiguousRight struct {
    Origin string `validate:"notBlank"`
}

type ambiguousPayload struct {
    ambiguousLeft
    ambiguousRight
}

/* @info two promoted fields claiming one name at equal depth are the ambiguity encoding/json drops — no field is populated, so none is validated */
func TestValidateStruct_DropsAnAmbiguousPromotedName(t *testing.T) {
    validator := NewValidator()

    validateErr := validator.Validate(&ambiguousPayload{})
    if nil != validateErr {
        t.Fatalf("expected the ambiguous name to be dropped, got %v", validateErr)
    }
}

type taggedTwin struct {
    Source string `json:"Origin" validate:"notBlank"`
}

type untaggedTwin struct {
    Origin string `validate:"notBlank"`
}

type taggedWinsPayload struct {
    taggedTwin
    untaggedTwin
}

/* @info at equal depth a single explicitly json-named field beats the untagged one, so only the tagged twin's tag runs — against the value the payload actually lands in */
func TestValidateStruct_TaggedPromotedFieldBeatsTheUntaggedTwin(t *testing.T) {
    validator := NewValidator()

    validateErr := validator.Validate(&taggedWinsPayload{
        taggedTwin: taggedTwin{Source: "set"},
    })
    if nil != validateErr {
        t.Fatalf("expected only the tagged twin to be validated, got %v", validateErr)
    }

    failingErr := validator.Validate(&taggedWinsPayload{})
    if nil == failingErr {
        t.Fatalf("expected the tagged twin's tag to still run")
    }
}

/* @info encoding/json populates the exported fields promoted through an unexported embed, so their tags run — the walk must include what a payload reaches */
func TestValidateStruct_ValidatesFieldsPromotedThroughAnUnexportedEmbed(t *testing.T) {
    validator := NewValidator()

    validateErr := validator.Validate(&struct {
        shadowedStamp
        Name string `json:"name"`
    }{})
    if nil == validateErr {
        t.Fatalf("expected the promoted notBlank to run against the empty field")
    }
}

type nilEmbedPayload struct {
    *shadowedStamp
    Name string `json:"name"`
}

/* @info a nil pointer embed leaves nothing to validate, but its promoted names keep their place in the dominance so shadowing stays what it would be were the embed present */
func TestValidateStruct_ToleratesANilPointerEmbed(t *testing.T) {
    validator := NewValidator()

    validateErr := validator.Validate(&nilEmbedPayload{Name: "ok"})
    if nil != validateErr {
        t.Fatalf("expected the nil embed to be tolerated, got %v", validateErr)
    }
}

type TaggedEmbed struct {
    Name string `json:"embedName"`
}

type taggedEmbedPayload struct {
    TaggedEmbed `validate:"notEmpty"`
    Title       string `json:"title"`
}

/* @info a constraint declared on the embed itself runs against the embed value: the promoted fields are payload-populated, so the tag is satisfiable and must not vanish with the flattening */
func TestValidateStruct_AppliesTheTagDeclaredOnAPromotedEmbed(t *testing.T) {
    validator := NewValidator()

    validateErr := validator.Validate(&taggedEmbedPayload{})
    if nil == validateErr {
        t.Fatalf("expected the embed's own tag to run")
    }
}

type payloadWithPaddedSkipTag struct {
    Anything string `validate:" - "`
}

/* @info a padded " - " is the skip marker, not an unknown rule that would reject every value */
func TestValidator_PaddedDashSkipsValidation(t *testing.T) {
    validatorInstance := NewValidator()

    requireNoValidationErrors(t, validatorInstance.Validate(payloadWithPaddedSkipTag{Anything: "x"}))
    requireNoValidationErrors(t, validatorInstance.Validate(payloadWithPaddedSkipTag{}))
}

type payloadWithValueLessGreaterThan struct {
    Count int `json:"count" validate:"greaterThan"`
}

/* @info a value-less greaterThan runs with the constraint's registered default and enforces > 0 */
func TestValidator_ValueLessGreaterThanEnforcesThePositiveDefault(t *testing.T) {
    validatorInstance := NewValidator()

    requireValidationErrors(t, validatorInstance.Validate(payloadWithValueLessGreaterThan{Count: 0}))
    requireNoValidationErrors(t, validatorInstance.Validate(payloadWithValueLessGreaterThan{Count: 1}))
}
