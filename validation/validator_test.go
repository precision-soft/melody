package validation

import (
    "encoding/json"
    "fmt"
    "reflect"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/exception"
    validationcontract "github.com/precision-soft/melody/validation/contract"
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

    /* a fractional bound is refused whole, not truncated: 3.9 read as 3 silently enforced a bound the tag does not declare, and on lessThan a truncated negative bound accepted values the tag as written refuses */
    fractionalErrors := requireValidationErrors(t, validatorInstance.Validate(payloadWithFractionalMaxLength{Name: "abc"}))

    fractionalError, ok := fractionalErrors[0].(*ValidationError)
    if false == ok {
        t.Fatalf("expected *ValidationError, got %T", fractionalErrors[0])
    }

    if ErrorInvalidRuleSyntax != fractionalError.Code() {
        t.Fatalf("expected a fractional bound to fail closed with code %q, got %q", ErrorInvalidRuleSyntax, fractionalError.Code())
    }
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

type StackedDiamondLeaf struct {
    CreatedBy string `validate:"notBlank"`
}

type StackedDiamondMiddle struct {
    StackedDiamondLeaf
}

type StackedDiamondFirstBranch struct {
    StackedDiamondMiddle
}

type StackedDiamondSecondBranch struct {
    StackedDiamondMiddle
}

type StackedDiamondPayload struct {
    StackedDiamondFirstBranch
    StackedDiamondSecondBranch
    Title string `json:"title" validate:"notBlank"`
}

func TestValidateStruct_AStackedDiamondKeepsThePopulatedNameValidated(t *testing.T) {
    decoded := StackedDiamondPayload{}

    if unmarshalErr := json.Unmarshal([]byte(`{"CreatedBy":"filled"}`), &decoded); nil != unmarshalErr {
        t.Fatalf("unexpected error: %v", unmarshalErr)
    }

    if "filled" != decoded.StackedDiamondFirstBranch.StackedDiamondMiddle.StackedDiamondLeaf.CreatedBy {
        t.Fatalf(
            "expected encoding/json to populate the name promoted past the diamond, got %q",
            decoded.StackedDiamondFirstBranch.StackedDiamondMiddle.StackedDiamondLeaf.CreatedBy,
        )
    }

    validator := NewValidator()

    validationErrors := requireValidationErrors(t, validator.Validate(&StackedDiamondPayload{Title: "set"}))

    for _, candidate := range validationErrors {
        if "CreatedBy" == candidate.Field() {
            return
        }
    }

    t.Fatalf("expected the diamond-promoted CreatedBy to be validated, got %v", validationErrors)
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

type valueEmbedPayload struct {
    shadowedStamp
    Name string `json:"name"`
}

func TestValidateStruct_ValidatesANilPointerEmbedAsAValueEmbed(t *testing.T) {
    validator := NewValidator()

    pointerErrors := requireValidationErrors(t, validator.Validate(&nilEmbedPayload{Name: "ok"}))
    valueErrors := requireValidationErrors(t, validator.Validate(&valueEmbedPayload{Name: "ok"}))

    if pointerErrors.Error() != valueErrors.Error() {
        t.Fatalf(
            "expected a nil pointer embed to validate exactly as a value embed, got %q against %q",
            pointerErrors.Error(),
            valueErrors.Error(),
        )
    }

    if "updatedBy: this field is required" != pointerErrors.Error() {
        t.Fatalf("expected the promoted notBlank to run against the zero value, got %q", pointerErrors.Error())
    }
}

type nilEmbedShadowedStamp struct {
    UpdatedBy string `json:"updatedBy" validate:"notBlank"`
    Reason    string `json:"reason" validate:"notBlank"`
}

type nilEmbedShadowingPayload struct {
    *nilEmbedShadowedStamp
    UpdatedBy string `json:"updatedBy"`
}

func TestValidateStruct_NilPointerEmbedKeepsItsPlaceInTheDominance(t *testing.T) {
    validator := NewValidator()

    validationErrors := requireValidationErrors(t, validator.Validate(&nilEmbedShadowingPayload{}))

    if "reason: this field is required" != validationErrors.Error() {
        t.Fatalf(
            "expected the shadowed promoted field to stay out and the unshadowed one to be reported, got %q",
            validationErrors.Error(),
        )
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

/* @info a value-less parameterized rule fails closed on every value: the registered singleton is a template for WithParams, and falling back to it enforced a configuration the tag never declared — a bare lessThan meant "less than 0" */
func TestValidator_ValueLessParameterizedRuleFailsClosed(t *testing.T) {
    validatorInstance := NewValidator()

    for _, count := range []int{0, 1} {
        validationErrors := requireValidationErrors(t, validatorInstance.Validate(payloadWithValueLessGreaterThan{Count: count}))

        validationError, ok := validationErrors[0].(*ValidationError)
        if false == ok {
            t.Fatalf("expected *ValidationError, got %T", validationErrors[0])
        }

        if ErrorInvalidRuleSyntax != validationError.Code() {
            t.Fatalf("expected a value-less parameterized rule to fail closed with code %q, got %q", ErrorInvalidRuleSyntax, validationError.Code())
        }
    }
}

type deepValidationNode struct {
    Name  string              `json:"name" validate:"notBlank"`
    Child *deepValidationNode `json:"child"`
}

func buildDeepValidationChain(levels int, deepestName string) *deepValidationNode {
    root := &deepValidationNode{Name: "node"}

    current := root
    for level := 2; level <= levels; level++ {
        current.Child = &deepValidationNode{Name: "node"}
        current = current.Child
    }

    current.Name = deepestName

    return root
}

func TestValidateStruct_EnforcesConstraintsAtTheDeepestAllowedNestingLevel(t *testing.T) {
    validatorInstance := NewValidator()

    requireValidationErrors(t, validatorInstance.Validate(buildDeepValidationChain(32, "")))
    requireNoValidationErrors(t, validatorInstance.Validate(buildDeepValidationChain(32, "leaf")))
}

func TestValidateStruct_FailsClosedPastTheNestingDepthLimit(t *testing.T) {
    validatorInstance := NewValidator()

    validationErrors := requireValidationErrors(t, validatorInstance.Validate(buildDeepValidationChain(33, "")))

    for _, validationError := range validationErrors {
        if ErrorNestingDepthExceeded == validationError.Code() {
            return
        }
    }

    t.Fatalf("expected a nesting-depth error past the limit, got %q", validationErrors.Error())
}

type countingConstraint struct {
    withParamsCount int
}

func (instance *countingConstraint) Validate(value any, field string) validationcontract.ValidationError {
    return nil
}

func (instance *countingConstraint) WithParams(params map[string]string) (validationcontract.Constraint, error) {
    instance.withParamsCount++

    return &countingConstraint{}, nil
}

type memoizedConstraintElement struct {
    Value string `json:"value" validate:"counting(mode=strict)"`
}

type memoizedConstraintPayload struct {
    Values []memoizedConstraintElement `json:"values"`
}

func TestValidator_ConstructsAParameterizedConstraintOncePerTag(t *testing.T) {
    validatorInstance := NewValidator()

    counting := &countingConstraint{}
    validatorInstance.RegisterConstraint("counting", counting)

    requireNoValidationErrors(
        t,
        validatorInstance.Validate(memoizedConstraintPayload{
            Values: make([]memoizedConstraintElement, 64),
        }),
    )

    if 1 != counting.withParamsCount {
        t.Fatalf("expected the constraint to be constructed once, got %d constructions", counting.withParamsCount)
    }
}

func TestValidator_ConstraintCacheKeyDistinguishesEquivalentLookingParameters(t *testing.T) {
    first := constraintCacheKey("min", map[string]string{"a": "b=c"})
    second := constraintCacheKey("min", map[string]string{"a=b": "c"})

    if first == second {
        t.Fatalf("expected distinct cache keys, both were %q", first)
    }

    third := constraintCacheKey("min", map[string]string{"value": "2", "mode": "strict"})
    fourth := constraintCacheKey("min", map[string]string{"mode": "strict", "value": "2"})

    if third != fourth {
        t.Fatalf("expected the same key regardless of map order, got %q and %q", third, fourth)
    }
}

type concurrentValidationElement struct {
    Code string `json:"code" validate:"regex(pattern=^[a-z]+$),min(value=2),concurrentCounting(mode=strict)"`
}

type concurrentValidationPayload struct {
    Values []concurrentValidationElement `json:"values"`
}

type concurrentCountingConstraint struct {
    constructionCount atomic.Int64
}

func (instance *concurrentCountingConstraint) Validate(value any, field string) validationcontract.ValidationError {
    return nil
}

func (instance *concurrentCountingConstraint) WithParams(params map[string]string) (validationcontract.Constraint, error) {
    instance.constructionCount.Add(1)

    return &concurrentCountingConstraint{}, nil
}

func TestValidator_ValidatesConcurrentlyWithMemoizedConstraints(t *testing.T) {
    validatorInstance := NewValidator()

    counting := &concurrentCountingConstraint{}
    validatorInstance.RegisterConstraint("concurrentCounting", counting)

    payload := concurrentValidationPayload{
        Values: make([]concurrentValidationElement, 32),
    }
    for index := range payload.Values {
        payload.Values[index].Code = "abc"
    }

    const workerCount = 8
    const iterationCount = 32
    resolved := make([]validationcontract.Constraint, workerCount)

    waitGroup := sync.WaitGroup{}

    for worker := 0; worker < workerCount; worker++ {
        waitGroup.Add(1)

        go func(slot int) {
            defer waitGroup.Done()

            for iteration := 0; iteration < iterationCount; iteration++ {
                if validateErr := validatorInstance.Validate(payload); nil != validateErr {
                    t.Errorf("unexpected validation error: %v", validateErr)

                    return
                }

                constraint, ok, _ := validatorInstance.createConstraintWithParams("concurrentCounting", map[string]string{"mode": "strict"})
                if false == ok {
                    t.Errorf("expected the parameterized constraint to resolve")

                    return
                }

                resolved[slot] = constraint
            }
        }(worker)
    }

    waitGroup.Wait()

    /* LoadOrStore lets several goroutines build a value on the first touch and keeps one, so the memo bounds constructions by the racing goroutines rather than pinning them at one; without it every validated value builds its own */
    constructionCount := counting.constructionCount.Load()
    if constructionCount < 1 || int64(workerCount) < constructionCount {
        t.Fatalf("expected at most one construction per goroutine, got %d", constructionCount)
    }

    if int64(workerCount*iterationCount) <= constructionCount {
        t.Fatalf("expected the memo to keep constructions far below the validated values, got %d", constructionCount)
    }

    for slot := 1; slot < workerCount; slot++ {
        if resolved[0] != resolved[slot] {
            t.Fatalf("expected every goroutine to share one constraint instance, worker %d resolved a different one", slot)
        }
    }
}

type selfEmbeddingPayload struct {
    *selfEmbeddingPayload
    Label string `json:"label" validate:"notBlank"`
}

type mutuallyEmbeddingLeft struct {
    *mutuallyEmbeddingRight
    Left string `json:"left" validate:"notBlank"`
}

type mutuallyEmbeddingRight struct {
    *mutuallyEmbeddingLeft
    Right string `json:"right" validate:"notBlank"`
}

func TestValidateStruct_TerminatesOnASelfReferentialNilPointerEmbed(t *testing.T) {
    validatorInstance := NewValidator()

    selfErrors := requireValidationErrors(t, validatorInstance.Validate(&selfEmbeddingPayload{}))
    if "label: this field is required" != selfErrors.Error() {
        t.Fatalf("expected the self-embedded field to be reported exactly once, got %q", selfErrors.Error())
    }

    mutualErrors := requireValidationErrors(t, validatorInstance.Validate(&mutuallyEmbeddingLeft{}))
    if "left: this field is required; right: this field is required" != mutualErrors.Error() {
        t.Fatalf("expected both sides of the cycle to be walked exactly once, got %q", mutualErrors.Error())
    }
}

type freeFormPayload struct {
    Name     string         `json:"name" validate:"notBlank"`
    Metadata map[string]any `json:"metadata"`
}

func buildFreeFormNesting(levels int) map[string]any {
    deepest := map[string]any{"leaf": "value"}

    current := deepest
    for level := 1; level < levels; level++ {
        current = map[string]any{"k": current}
    }

    return current
}

type depthCutSliceNode struct {
    Name     string              `json:"name" validate:"notBlank"`
    Children []depthCutSliceNode `json:"children"`
}

type depthCutMapNode struct {
    Name     string                     `json:"name" validate:"notBlank"`
    Children map[string]depthCutMapNode `json:"children"`
}

/* the depth cut follows the element type of a slice or a map, so a tag-bearing chain reached through either is still reported rather than passed */
func TestValidateStruct_FailsClosedPastTheNestingDepthLimitThroughCollections(t *testing.T) {
    sliceNode := depthCutSliceNode{Name: "leaf"}
    mapNode := depthCutMapNode{Name: "leaf"}

    for level := 0; level < 60; level++ {
        sliceNode = depthCutSliceNode{Name: "node", Children: []depthCutSliceNode{sliceNode}}
        mapNode = depthCutMapNode{Name: "node", Children: map[string]depthCutMapNode{"child": mapNode}}
    }

    validatorInstance := NewValidator()

    requireValidationErrors(t, validatorInstance.Validate(sliceNode))
    requireValidationErrors(t, validatorInstance.Validate(mapNode))
}

func TestValidator_FreeFormMapPayloadIsNotRejectedUpToTheNestingDepthLimit(t *testing.T) {
    validatorInstance := NewValidator()

    for _, levels := range []int{1, 8, 31, 32} {
        errs := validatorInstance.Validate(freeFormPayload{Name: "given", Metadata: buildFreeFormNesting(levels)})
        if nil != errs {
            t.Fatalf("expected %d levels of tag-free json to validate, got %v", levels, errs)
        }
    }

    for _, levels := range []int{33, 40, 80} {
        validationErrors := requireValidationErrors(
            t,
            validatorInstance.Validate(freeFormPayload{Name: "given", Metadata: buildFreeFormNesting(levels)}),
        )

        if ErrorNestingDepthExceeded != validationErrors[0].Code() {
            t.Fatalf("expected %d levels to be reported at the cut, got %q", levels, validationErrors.Error())
        }
    }
}

type promotedTimeCodecPayload struct {
    time.Time
    Name string `json:"name" validate:"notBlank"`
}

type ordinaryTimeFieldPayload struct {
    CreatedAt time.Time `json:"createdAt"`
    Name      string    `json:"name" validate:"notBlank"`
}

/* the embedded time.Time promotes the codec for the whole value, so the body is an RFC 3339 string: encoding/json fills the embedded time and never the sibling, and enforcing the sibling's constraint would reject every body the type can decode */
func TestValidateStruct_PromotedTimeCodecLeavesNoSiblingToValidate(t *testing.T) {
    payload := promotedTimeCodecPayload{}

    if unmarshalErr := json.Unmarshal([]byte(`"2026-01-01T00:00:00Z"`), &payload); nil != unmarshalErr {
        t.Fatalf("unexpected error: %v", unmarshalErr)
    }

    if "2026-01-01T00:00:00Z" != payload.Time.Format(time.RFC3339) {
        t.Fatalf("expected the promoted codec to own the decode, got %v", payload.Time)
    }

    if "" != payload.Name {
        t.Fatalf("expected the sibling to stay unpopulated, got %q", payload.Name)
    }

    validatorInstance := NewValidator()

    requireNoValidationErrors(t, validatorInstance.Validate(payload))

    encoded, marshalErr := json.Marshal(payload)
    if nil != marshalErr {
        t.Fatalf("unexpected error: %v", marshalErr)
    }

    if `"2026-01-01T00:00:00Z"` != string(encoded) {
        t.Fatalf("expected the promoted codec to own the wire form, got %s", encoded)
    }
}

/* only the promoted codec silences the walk: an ordinary time.Time field leaves the object shape intact, so its siblings are still enforced */
func TestValidateStruct_OrdinaryTimeFieldKeepsItsSiblingsValidated(t *testing.T) {
    validatorInstance := NewValidator()

    errs := requireValidationErrors(t, validatorInstance.Validate(ordinaryTimeFieldPayload{}))
    if "name: this field is required" != errs.Error() {
        t.Fatalf("expected the sibling constraint to be enforced, got %q", errs.Error())
    }
}

func TestIsPromotedValidationEmbed_FlattensATimeEmbedLikeEveryOtherEmbed(t *testing.T) {
    field, found := reflect.TypeOf(promotedTimeCodecPayload{}).FieldByName("Time")
    if false == found {
        t.Fatalf("expected the embedded time field to be reachable")
    }

    if false == isPromotedValidationEmbed(field) {
        t.Fatalf("expected an untagged time.Time embed to flatten like every other embed")
    }
}

type nilLeafDepthLeaf struct {
    Name string `json:"name" validate:"notBlank"`
}

type nilLeafDepthWrapper struct {
    Inner *nilLeafDepthWrapper `json:"inner"`
    Leaf  *nilLeafDepthLeaf    `json:"leaf"`
}

func buildNilLeafDepthChain(levels int) nilLeafDepthWrapper {
    outermost := &nilLeafDepthWrapper{}

    for level := 1; level < levels; level++ {
        outermost = &nilLeafDepthWrapper{Inner: outermost}
    }

    return *outermost
}

/* a nil pointer past the cut has nothing the cut could have truncated — the walk returns no errors for it at any depth — so reporting one would reject a payload for a member it sent as null */
func TestValidateStruct_DepthCutIgnoresANilMemberPastTheLimit(t *testing.T) {
    validatorInstance := NewValidator()

    requireNoValidationErrors(t, validatorInstance.Validate(buildNilLeafDepthChain(33)))
}

type taglessDepthNode struct {
    Child *taglessDepthNode `json:"child"`
    Name  string            `json:"name"`
}

func buildTaglessDepthChain(levels int) *taglessDepthNode {
    root := &taglessDepthNode{Name: "node"}

    current := root
    for level := 2; level <= levels; level++ {
        current.Child = &taglessDepthNode{Name: "node"}
        current = current.Child
    }

    return root
}

/* the counterpart of the depth error: a struct chain declaring no constraint at all is passed however deep it goes, so the cut reports a truncated subtree rather than every subtree */
func TestValidateStruct_TaglessChainPastTheNestingDepthLimitIsAccepted(t *testing.T) {
    validatorInstance := NewValidator()

    for _, levels := range []int{33, 40, 80} {
        errs := validatorInstance.Validate(buildTaglessDepthChain(levels))
        if nil != errs {
            t.Fatalf("expected %d tag-free levels to validate, got %v", levels, errs)
        }
    }
}

func TestValidateStruct_DepthCutReportsOnlyTheTruncatedSubtree(t *testing.T) {
    validatorInstance := NewValidator()

    validationErrors := requireValidationErrors(t, validatorInstance.Validate(buildDeepValidationChain(33, "")))

    if 1 != len(validationErrors) {
        t.Fatalf("expected exactly one error at the cut, got %q", validationErrors.Error())
    }

    if ErrorNestingDepthExceeded != validationErrors[0].Code() {
        t.Fatalf("expected the nesting-depth code, got %q", validationErrors[0].Code())
    }

    expectedPath := "child"
    for level := 3; level <= 33; level++ {
        expectedPath = expectedPath + ".child"
    }

    if expectedPath != validationErrors[0].Field() {
        t.Fatalf("expected the cut to name the truncated member %q, got %q", expectedPath, validationErrors[0].Field())
    }
}

type interfaceMemberLeaf struct {
    Name string `json:"name" validate:"notBlank"`
}

type interfaceMemberNode struct {
    Child   *interfaceMemberNode `json:"child"`
    Payload any                  `json:"payload"`
}

func buildInterfaceMemberChain(levels int, payload any) *interfaceMemberNode {
    node := &interfaceMemberNode{Payload: payload}

    for level := 1; level < levels; level++ {
        node = &interfaceMemberNode{Child: node}
    }

    return node
}

func TestValidateStruct_DepthCutAnswersForATagReachedThroughAnInterfaceMember(t *testing.T) {
    validatorInstance := NewValidator()

    belowErrors := requireValidationErrors(t, validatorInstance.Validate(buildInterfaceMemberChain(20, interfaceMemberLeaf{})))
    if ConstraintNotBlankErrorIsBlank != belowErrors[0].Code() {
        t.Fatalf("expected the tag below the cap to be enforced, got %q", belowErrors.Error())
    }

    pastErrors := requireValidationErrors(t, validatorInstance.Validate(buildInterfaceMemberChain(33, interfaceMemberLeaf{})))
    if ErrorNestingDepthExceeded != pastErrors[0].Code() {
        t.Fatalf("expected the cut to report the truncated subtree, got %q", pastErrors.Error())
    }
}

type interfaceSequenceNode struct {
    Children []interfaceSequenceNode `json:"children"`
    Payload  any                     `json:"payload"`
}

func buildInterfaceSequenceChain(levels int, payload any) interfaceSequenceNode {
    node := interfaceSequenceNode{Payload: payload}

    for level := 1; level < levels; level++ {
        node = interfaceSequenceNode{Children: []interfaceSequenceNode{node}}
    }

    return node
}

func TestValidateStruct_DepthCutIgnoresANilInterfaceMemberPastTheLimit(t *testing.T) {
    validatorInstance := NewValidator()

    requireNoValidationErrors(t, validatorInstance.Validate(buildInterfaceSequenceChain(33, nil)))

    filledErrors := requireValidationErrors(t, validatorInstance.Validate(buildInterfaceSequenceChain(33, interfaceMemberLeaf{})))
    if 1 != len(filledErrors) {
        t.Fatalf("expected exactly one error at the cut, got %q", filledErrors.Error())
    }

    if ErrorNestingDepthExceeded != filledErrors[0].Code() {
        t.Fatalf("expected the nesting-depth code, got %q", filledErrors[0].Code())
    }

    if false == strings.HasSuffix(filledErrors[0].Field(), ".payload") {
        t.Fatalf("expected the cut to land on the interface member itself, got %q", filledErrors[0].Field())
    }
}

func TestHoldsNoValidationMember_DistinguishesAnAbsentMemberFromAnEmptyOne(t *testing.T) {
    var absentInterface any

    emptySlice := []interfaceMemberLeaf{}
    emptyMap := map[string]interfaceMemberLeaf{}

    for label, value := range map[string]reflect.Value{
        "nil pointer":   reflect.ValueOf((*interfaceMemberLeaf)(nil)),
        "nil slice":     reflect.ValueOf([]interfaceMemberLeaf(nil)),
        "nil map":       reflect.ValueOf(map[string]interfaceMemberLeaf(nil)),
        "empty slice":   reflect.ValueOf(emptySlice),
        "empty map":     reflect.ValueOf(emptyMap),
        "nil interface": reflect.ValueOf(&absentInterface).Elem(),
    } {
        if false == holdsNoValidationMember(value) {
            t.Fatalf("expected a %s to hold no validation member", label)
        }
    }

    for label, value := range map[string]reflect.Value{
        "filled pointer": reflect.ValueOf(&interfaceMemberLeaf{}),
        "filled slice":   reflect.ValueOf([]interfaceMemberLeaf{{}}),
        "filled map":     reflect.ValueOf(map[string]interfaceMemberLeaf{"key": {}}),
    } {
        if true == holdsNoValidationMember(value) {
            t.Fatalf("expected a %s to hold a validation member", label)
        }
    }
}

type emptyMemberDepthLeaf struct {
    Name string `json:"name" validate:"notBlank"`
}

type emptyMemberDepthWrapper struct {
    Inner   *emptyMemberDepthWrapper        `json:"inner"`
    Items   []emptyMemberDepthLeaf          `json:"items"`
    ItemMap map[string]emptyMemberDepthLeaf `json:"itemMap"`
}

func buildEmptyMemberDepthChain(levels int, items []emptyMemberDepthLeaf) emptyMemberDepthWrapper {
    innermost := &emptyMemberDepthWrapper{
        Items:   items,
        ItemMap: map[string]emptyMemberDepthLeaf{},
    }

    for level := 1; level < levels; level++ {
        innermost = &emptyMemberDepthWrapper{Inner: innermost}
    }

    return *innermost
}

func TestValidateStruct_DepthCutIgnoresAnEmptyMemberPastTheLimit(t *testing.T) {
    validatorInstance := NewValidator()

    requireNoValidationErrors(t, validatorInstance.Validate(buildEmptyMemberDepthChain(33, []emptyMemberDepthLeaf{})))

    filledErrors := requireValidationErrors(t, validatorInstance.Validate(buildEmptyMemberDepthChain(33, []emptyMemberDepthLeaf{{}})))
    if 1 != len(filledErrors) {
        t.Fatalf("expected only the populated member to be reported, got %q", filledErrors.Error())
    }

    if ErrorNestingDepthExceeded != filledErrors[0].Code() {
        t.Fatalf("expected the nesting-depth code, got %q", filledErrors[0].Code())
    }

    if false == strings.HasSuffix(filledErrors[0].Field(), ".items") {
        t.Fatalf("expected the cut to name the populated member, got %q", filledErrors[0].Field())
    }
}

type declaredDecoderTimePayload struct {
    time.Time
    Name string `json:"name" validate:"notBlank"`
}

func (instance *declaredDecoderTimePayload) UnmarshalJSON(data []byte) error {
    decoded := struct {
        Name string `json:"name"`
    }{}

    if unmarshalErr := json.Unmarshal(data, &decoded); nil != unmarshalErr {
        return unmarshalErr
    }

    instance.Name = decoded.Name

    return nil
}

func TestValidateStruct_ADeclaredDecoderKeepsItsSiblingsValidated(t *testing.T) {
    payload := declaredDecoderTimePayload{}

    if unmarshalErr := json.Unmarshal([]byte(`{"name":"decoded"}`), &payload); nil != unmarshalErr {
        t.Fatalf("unexpected error: %v", unmarshalErr)
    }

    if "decoded" != payload.Name {
        t.Fatalf("expected the declared decoder to populate the sibling, got %q", payload.Name)
    }

    encoded, marshalErr := json.Marshal(payload)
    if nil != marshalErr {
        t.Fatalf("unexpected error: %v", marshalErr)
    }

    if `"0001-01-01T00:00:00Z"` != string(encoded) {
        t.Fatalf("expected the promoted codec to still own the wire form, got %s", encoded)
    }

    validatorInstance := NewValidator()

    requireNoValidationErrors(t, validatorInstance.Validate(payload))

    absentErrors := requireValidationErrors(t, validatorInstance.Validate(declaredDecoderTimePayload{}))
    if "name: this field is required" != absentErrors.Error() {
        t.Fatalf("expected the sibling constraint to be enforced, got %q", absentErrors.Error())
    }
}

type validationJsonOnlyDecoder struct {
    Grams int `json:"grams"`
}

func (instance *validationJsonOnlyDecoder) UnmarshalJSON(data []byte) error {
    instance.Grams = 1000

    return nil
}

type validationBothCodecDecoder struct {
    Grams int `json:"grams"`
}

func (instance *validationBothCodecDecoder) UnmarshalJSON(data []byte) error {
    instance.Grams = 1000

    return nil
}

func (instance *validationBothCodecDecoder) UnmarshalText(text []byte) error {
    instance.Grams = 1000

    return nil
}

type textDecoderTimePayload struct {
    time.Time
    validationJsonOnlyDecoder
    Name string `json:"name" validate:"notBlank"`
}

type ambiguousDecoderTimePayload struct {
    time.Time
    validationBothCodecDecoder
    Name string `json:"name" validate:"notBlank"`
}

func TestValidateStruct_APromotedTextDecoderLeavesNoSiblingToValidate(t *testing.T) {
    if true == reflect.PointerTo(reflect.TypeOf(textDecoderTimePayload{})).Implements(reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()) {
        t.Fatalf("expected two equal-depth json decoders to promote no unmarshaler")
    }

    if unmarshalErr := json.Unmarshal([]byte(`{"name":"decoded"}`), &textDecoderTimePayload{}); nil == unmarshalErr {
        t.Fatalf("expected the promoted text decoder to refuse an object body")
    }

    validatorInstance := NewValidator()

    requireNoValidationErrors(t, validatorInstance.Validate(textDecoderTimePayload{}))
}

func TestValidateStruct_AnAmbiguousDecoderKeepsItsSiblingsValidated(t *testing.T) {
    if false == reflect.TypeOf(ambiguousDecoderTimePayload{}).Implements(validationJsonMarshalerInterfaceType) {
        t.Fatalf("expected the time embed to promote its marshaler unopposed")
    }

    payload := ambiguousDecoderTimePayload{}

    if unmarshalErr := json.Unmarshal([]byte(`{"name":"decoded"}`), &payload); nil != unmarshalErr {
        t.Fatalf("unexpected error: %v", unmarshalErr)
    }

    if "decoded" != payload.Name {
        t.Fatalf("expected the object body to populate the sibling, got %q", payload.Name)
    }

    validatorInstance := NewValidator()

    absentErrors := requireValidationErrors(t, validatorInstance.Validate(ambiguousDecoderTimePayload{}))
    if "name: this field is required" != absentErrors.Error() {
        t.Fatalf("expected the sibling constraint to be enforced, got %q", absentErrors.Error())
    }
}

type validationCodecObject struct {
    Cents int `json:"cents"`
}

func (instance validationCodecObject) MarshalJSON() ([]byte, error) {
    return []byte(`{"cents":0}`), nil
}

type validationCodecScalar string

func (instance validationCodecScalar) MarshalJSON() ([]byte, error) {
    return []byte(`"1kg"`), nil
}

type validationTimeHolder struct {
    time.Time
}

type validationCodecCarrier struct {
    validationCodecObject
    Label string `json:"label"`
}

type timeShadowedByAnObjectCodecPayload struct {
    validationCodecObject
    validationTimeHolder
    Name string `json:"name" validate:"notBlank"`
}

type timeShadowedByAScalarCodecPayload struct {
    validationCodecScalar
    validationTimeHolder
    Name string `json:"name" validate:"notBlank"`
}

type timeOutrankingADeeperCodecPayload struct {
    time.Time
    validationCodecCarrier
    Name string `json:"name" validate:"notBlank"`
}

type timeTiedWithACodecPayload struct {
    time.Time
    validationCodecObject
    Name string `json:"name" validate:"notBlank"`
}

type timeCodecCyclePayload struct {
    *timeCodecCyclePayload
    time.Time
    Name string `json:"name" validate:"notBlank"`
}

func TestValidateStruct_AShallowerCodecEmbedShadowingATimeCodecKeepsItsSiblingsValidated(t *testing.T) {
    encoded, marshalErr := json.Marshal(timeShadowedByAnObjectCodecPayload{})
    if nil != marshalErr {
        t.Fatalf("unexpected error: %v", marshalErr)
    }

    if `{"cents":0}` != string(encoded) {
        t.Fatalf("expected the shallower codec to own the wire form, got %s", encoded)
    }

    validatorInstance := NewValidator()

    validationErrors := requireValidationErrors(t, validatorInstance.Validate(timeShadowedByAnObjectCodecPayload{}))
    if "name: this field is required" != validationErrors.Error() {
        t.Fatalf("expected the sibling constraint to be enforced, got %q", validationErrors.Error())
    }
}

func TestValidateStruct_ANonStructCodecEmbedShadowingATimeCodecKeepsItsSiblingsValidated(t *testing.T) {
    encoded, marshalErr := json.Marshal(timeShadowedByAScalarCodecPayload{})
    if nil != marshalErr {
        t.Fatalf("unexpected error: %v", marshalErr)
    }

    if `"1kg"` != string(encoded) {
        t.Fatalf("expected the shallower codec to own the wire form, got %s", encoded)
    }

    validatorInstance := NewValidator()

    validationErrors := requireValidationErrors(t, validatorInstance.Validate(timeShadowedByAScalarCodecPayload{}))
    if "name: this field is required" != validationErrors.Error() {
        t.Fatalf("expected the sibling constraint to be enforced, got %q", validationErrors.Error())
    }
}

func TestValidateStruct_ATimeCodecOutrankingADeeperCodecEmbedLeavesNoSiblingToValidate(t *testing.T) {
    encoded, marshalErr := json.Marshal(timeOutrankingADeeperCodecPayload{})
    if nil != marshalErr {
        t.Fatalf("unexpected error: %v", marshalErr)
    }

    if `"0001-01-01T00:00:00Z"` != string(encoded) {
        t.Fatalf("expected the shallower time codec to own the wire form, got %s", encoded)
    }

    if unmarshalErr := json.Unmarshal([]byte(`{"name":"decoded"}`), &timeOutrankingADeeperCodecPayload{}); nil == unmarshalErr {
        t.Fatalf("expected the promoted time decoder to refuse an object body")
    }

    validatorInstance := NewValidator()

    requireNoValidationErrors(t, validatorInstance.Validate(timeOutrankingADeeperCodecPayload{}))
}

func TestValidateStruct_ATimeCodecTiedWithACodecEmbedKeepsItsSiblingsValidated(t *testing.T) {
    if true == reflect.TypeOf(timeTiedWithACodecPayload{}).Implements(validationJsonMarshalerInterfaceType) {
        t.Fatalf("expected two equal-depth codec embeds to promote no marshaler")
    }

    validatorInstance := NewValidator()

    validationErrors := requireValidationErrors(t, validatorInstance.Validate(timeTiedWithACodecPayload{}))
    if "name: this field is required" != validationErrors.Error() {
        t.Fatalf("expected the sibling constraint to be enforced, got %q", validationErrors.Error())
    }
}

func TestValidateStruct_ATimeCodecBehindAnEmbedCycleKeepsItsSiblingsValidated(t *testing.T) {
    cycleType := reflect.TypeOf(timeCodecCyclePayload{})

    if false == cycleType.Implements(validationJsonMarshalerInterfaceType) {
        t.Fatalf("expected the time embed to promote its marshaler onto the enclosing value")
    }

    origin, depth, resolved := promotedValidationMarshalerOrigin(cycleType, map[reflect.Type]bool{})
    if true == resolved {
        t.Fatalf("expected an unresolvable promotion, got origin %v at depth %d", origin, depth)
    }

    validatorInstance := NewValidator()

    validationErrors := requireValidationErrors(t, validatorInstance.Validate(timeCodecCyclePayload{}))
    if "name: this field is required" != validationErrors.Error() {
        t.Fatalf("expected the sibling constraint to be enforced, got %q", validationErrors.Error())
    }
}

type validationPointerReceiverCodec struct {
    Grams int `json:"grams"`
}

func (instance *validationPointerReceiverCodec) MarshalJSON() ([]byte, error) {
    return []byte(`"1kg"`), nil
}

type pointerReceiverCodecHostPayload struct {
    *validationPointerReceiverCodec
    Name string `json:"name" validate:"notBlank"`
}

func TestPromotedValidationMarshalerOrigin_APointerEmbedWithAPointerReceiverCodecIsUnresolved(t *testing.T) {
    hostType := reflect.TypeOf(pointerReceiverCodecHostPayload{})

    if false == hostType.Implements(validationJsonMarshalerInterfaceType) {
        t.Fatalf("expected the pointer embed to promote its codec onto the enclosing value")
    }

    if true == reflect.TypeOf(validationPointerReceiverCodec{}).Implements(validationJsonMarshalerInterfaceType) {
        t.Fatalf("expected the dereferenced embed to keep its codec out of the value method set")
    }

    origin, depth, resolved := promotedValidationMarshalerOrigin(hostType, map[reflect.Type]bool{})
    if true == resolved {
        t.Fatalf("expected an unresolvable promotion, got origin %v at depth %d", origin, depth)
    }

    if nil != origin || 0 != depth {
        t.Fatalf("expected no origin and a zero depth for an unresolved promotion, got %v at depth %d", origin, depth)
    }

    validatorInstance := NewValidator()

    validationErrors := requireValidationErrors(t, validatorInstance.Validate(pointerReceiverCodecHostPayload{}))
    if "name: this field is required" != validationErrors.Error() {
        t.Fatalf("expected the sibling constraint to be enforced, got %q", validationErrors.Error())
    }
}

type sharedPointerItem struct {
    Name string `json:"name" validate:"notBlank"`
}

type sharedPointerBatch struct {
    Primary   *sharedPointerItem `json:"primary"`
    Secondary *sharedPointerItem `json:"secondary"`
}

/* @info the visited set is path-scoped: a shared, non-cyclic pointer is walked under every path that reaches it, so the same invalid value reported under primary is reported under secondary too — a whole-call set validated it once and silently skipped the second subtree */
func TestValidator_SharedPointerIsValidatedUnderEveryPath(t *testing.T) {
    validatorInstance := NewValidator()

    shared := &sharedPointerItem{Name: ""}

    validationErrors := requireValidationErrors(t, validatorInstance.Validate(sharedPointerBatch{Primary: shared, Secondary: shared}))

    if 2 != len(validationErrors) {
        t.Fatalf("expected the shared pointer to be validated under both paths, got %d errors: %v", len(validationErrors), validationErrors)
    }

    fields := map[string]bool{}
    for _, validationError := range validationErrors {
        fields[validationError.Field()] = true
    }

    if false == fields["primary.name"] || false == fields["secondary.name"] {
        t.Fatalf("expected errors under both primary.name and secondary.name, got %v", fields)
    }
}

type typedNilErrorConstraint struct{}

func (instance *typedNilErrorConstraint) Validate(value any, field string) validationcontract.ValidationError {
    var typedNil *ValidationError

    return typedNil
}

type typedNilErrorPayload struct {
    Value string `json:"value" validate:"typedNilError"`
}

/* @info a custom constraint written with a concrete error variable returns a typed nil on its success path; a plain nil comparison saw a non-nil interface and dereferenced it, panicking inside Validate on the request path */
func TestValidator_TypedNilConstraintErrorIsSuccess(t *testing.T) {
    validatorInstance := NewValidator()
    validatorInstance.RegisterConstraint("typedNilError", &typedNilErrorConstraint{})

    requireNoValidationErrors(t, validatorInstance.Validate(typedNilErrorPayload{Value: "anything"}))
}

/* @info the registry is append-only today, so the missing-name read is unreachable through Validate; the guard keeps a future removal path from handing constraint.Validate a nil interface, and this pins it at the only level it is observable */
func TestValidator_CreateConstraintWithParamsRefusesUnregisteredName(t *testing.T) {
    validatorInstance := NewValidator()

    constraint, ok, refusalCause := validatorInstance.createConstraintWithParams("neverRegistered", nil)

    if nil != constraint || true == ok {
        t.Fatalf("expected an unregistered name to fail closed, got constraint=%v ok=%v", constraint, ok)
    }

    if "" == refusalCause {
        t.Fatalf("expected a refusal cause for an unregistered name")
    }
}

type refusalCausePayload struct {
    Name string `json:"name" validate:"min(value=abc)"`
}

/* @info the constraint's own refusal reason travels into the error context: without it a rejected parameter value, a malformed tag and a constraint that takes no parameters all collapsed into one generic code with no way to tell them apart */
func TestValidator_ParameterRefusalCauseReachesTheErrorContext(t *testing.T) {
    validatorInstance := NewValidator()

    validationErrors := requireValidationErrors(t, validatorInstance.Validate(refusalCausePayload{Name: "anything"}))

    validationError, ok := validationErrors[0].(*ValidationError)
    if false == ok {
        t.Fatalf("expected *ValidationError, got %T", validationErrors[0])
    }

    cause, exists := validationError.Context()["cause"].(string)
    if false == exists || false == strings.Contains(cause, "invalid min length parameter") {
        t.Fatalf("expected the WithParams refusal reason in the error context, got %v", validationError.Context())
    }
}

var settlementProbeCalls atomic.Int64
var settlementProbeFirstEntered = make(chan struct{})
var settlementProbeFirstRelease = make(chan struct{})

type settlementProbeCodec struct {
    time.Time
    Label string `json:"label" validate:"notBlank"`
}

func (instance *settlementProbeCodec) UnmarshalJSON(data []byte) error {
    call := settlementProbeCalls.Add(1)

    if 1 == call {
        close(settlementProbeFirstEntered)
        <-settlementProbeFirstRelease

        return exception.NewError("refusing the empty object", nil, nil)
    }

    return nil
}

/* @info the codec memo settles on ONE verdict per type: the probe runs application code whose answer is not guaranteed stable, and with a plain Store the goroutine finishing LAST froze its verdict — here the first probe is held open, computes the opposite answer, and must still adopt the verdict the second probe already published */
func TestValidator_TimeCodecVerdictSettlesOnTheFirstStored(t *testing.T) {
    structType := reflect.TypeOf(settlementProbeCodec{})

    /* the memo is process-global, so a repeated run (-count) finds the verdict already settled and the probe choreography must not replay over closed channels */
    if cached, exists := validationTimeCodecCache.Load(structType); true == exists {
        if true == cached.(bool) {
            t.Fatalf("expected the settled verdict to remain the first stored one")
        }

        return
    }

    firstVerdict := make(chan bool)

    go func() {
        firstVerdict <- promotesValidationTimeCodec(structType)
    }()

    <-settlementProbeFirstEntered

    secondVerdict := promotesValidationTimeCodec(structType)
    if true == secondVerdict {
        t.Fatalf("expected the unblocked probe to conclude the type accepts an object body")
    }

    close(settlementProbeFirstRelease)

    if true == <-firstVerdict {
        t.Fatalf("expected the held-open probe to adopt the already-published verdict instead of overwriting it")
    }

    if cached, exists := validationTimeCodecCache.Load(structType); false == exists || true == cached.(bool) {
        t.Fatalf("expected the cached verdict to stay the first stored one, got %v (exists=%v)", cached, exists)
    }
}
