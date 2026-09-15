package validation

import (
    "encoding/json"
    "fmt"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    validationcontract "github.com/precision-soft/melody/v3/validation/contract"
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

/* MarshalJSON renders the validation collection as an array of structured field errors, preserving each element’s message, code and context. */
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

/* IsRuleWiringErrorCode identifies invalid rule declarations, including unknown rules, invalid parameters, malformed tags and uncompilable patterns. Validation fails closed with field errors; diagnostic details belong to operators rather than public responses. */
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

/* WithoutRuleWiringContext answers the collection as the client may see it: an entry that blames the declaration keeps its field, message and code and loses its context, which names the developer's typo, the refused parameters and the constraint's own reason. Every other entry is handed back untouched, so the bounds a numeric constraint reports — material the client needs to correct its request — still travel. The receiver is not modified: the record is rendered from the original and keeps everything. */
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
