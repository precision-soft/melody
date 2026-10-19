package validation

import (
    "encoding/json"
    "fmt"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    validationcontract "github.com/precision-soft/melody/v2/validation/contract"
)

func NewValidationError(field string, message string, code string, context map[string]any) *ValidationError {
    var copiedContext map[string]any
    if nil != context {
        copiedContext = make(map[string]any, len(context))
        for key, value := range context {
            copiedContext[key] = value
        }
    }

    return &ValidationError{
        field:   field,
        message: message,
        code:    code,
        context: copiedContext,
    }
}

type ValidationError struct {
    field   string
    message string
    code    string
    context map[string]any
}

func (instance *ValidationError) Field() string {
    return instance.field
}

func (instance *ValidationError) Message() string {
    return instance.message
}

func (instance *ValidationError) Code() string {
    return instance.code
}

func (instance *ValidationError) Context() map[string]any {
    if nil == instance.context {
        return nil
    }

    copied := make(map[string]any, len(instance.context))
    for key, value := range instance.context {
        copied[key] = value
    }

    return copied
}

func (instance *ValidationError) Error() string {
    return fmt.Sprintf("%s: %s", instance.field, instance.message)
}

func (instance *ValidationError) ToExceptionError() error {
    context := exceptioncontract.Context{
        "field": instance.field,
        "code":  instance.code,
    }

    if nil != instance.context {
        /* the copying getter keeps the map private, so no consumer of the exception mutates the validation error through it */
        context["context"] = instance.Context()
    }

    return exception.NewError(
        fmt.Sprintf("%s: %s", instance.field, instance.message),
        context,
        nil,
    )
}

func (instance *ValidationError) MarshalJSON() ([]byte, error) {
    type validationErrorJson struct {
        Field   string         `json:"field"`
        Message string         `json:"message"`
        Code    string         `json:"code"`
        Context map[string]any `json:"context,omitempty"`
    }

    return json.Marshal(validationErrorJson{
        Field:   instance.field,
        Message: instance.message,
        Code:    instance.code,
        Context: instance.context,
    })
}

var _ validationcontract.ValidationError = (*ValidationError)(nil)

type ValidationErrors []validationcontract.ValidationError

/* MarshalJSON renders the collection as an array of its elements, each through its own marshaler, so a log record carries the same per-field structure the http response body does. */
func (instance ValidationErrors) MarshalJSON() ([]byte, error) {
    return json.Marshal([]validationcontract.ValidationError(instance))
}

func (instance ValidationErrors) Error() string {
    if 0 == len(instance) {
        return ""
    }

    messages := make([]string, len(instance))
    for i, err := range instance {
        messages[i] = err.Error()
    }

    sort.Strings(messages)

    return strings.Join(messages, "; ")
}

func (instance ValidationErrors) HasErrors() bool {
    return 0 < len(instance)
}

/* IsRuleWiringErrorCode reports whether a code names a mistake in the declaration rather than the value: an unknown rule, a refused parameter set, an unreadable tag, or a pattern that does not compile. Such a record belongs at error, and its context names the developer's typo, material for the operator rather than the client. The refusal itself stays a field error, since the validator fails closed on a rule it cannot honour. */
func IsRuleWiringErrorCode(code string) bool {
    switch code {
    case ErrorUnknownRule, ErrorInvalidRuleSyntax, ConstraintRegexErrorInvalidPattern:
        return true
    default:
        return false
    }
}

/* HasRuleWiringError reports whether any member of the collection blames the declaration rather than the value. */
func (instance ValidationErrors) HasRuleWiringError() bool {
    for _, validationError := range instance {
        if nil == validationError {
            continue
        }

        if true == IsRuleWiringErrorCode(validationError.Code()) {
            return true
        }
    }

    return false
}

/* WithoutRuleWiringContext answers the collection as the client may see it: an entry blaming the declaration keeps its field, message and code and loses its context, while every other entry, bounds included, is handed back untouched. The receiver is not modified. */
func (instance ValidationErrors) WithoutRuleWiringContext() ValidationErrors {
    projected := make(ValidationErrors, 0, len(instance))

    for _, validationError := range instance {
        if nil == validationError || false == IsRuleWiringErrorCode(validationError.Code()) {
            projected = append(projected, validationError)

            continue
        }

        projected = append(
            projected,
            NewValidationError(
                validationError.Field(),
                validationError.Message(),
                validationError.Code(),
                nil,
            ),
        )
    }

    return projected
}
