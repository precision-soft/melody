package validation

import (
    "encoding/json"
    "fmt"
    "sort"
    "strings"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    validationcontract "github.com/precision-soft/melody/validation/contract"
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
        /* the copying getter, not the field: the constructor copies on the way in and Context copies on the way out precisely to keep this map private, and handing the live map to the exception would let any consumer mutate the validation error through it */
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

/* MarshalJSON renders the collection as the array it is, each element through its own marshaler, so a log record carrying it says the same thing the http response body says: the flattened Error() string was the only form the log ever saw, and the per-field structure — field, message, code, context — survived in one place and not the other. The json logger hands an error implementing json.Marshaler to the encoder for exactly this opt-in. */
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

/* IsRuleWiringErrorCode reports whether a code names a mistake in the DECLARATION rather than in the value: a rule the registry does not know, a parameter set the constraint refuses, a tag the parser cannot read, or a pattern that does not compile. None of them can be produced by any input a client sends — the tag is what is wrong, and it is wrong for every request that route will ever serve.

The distinction has two consumers. A record filed for one of these is a program failure and belongs at error, not at the warning a deliberate 4xx earns, where a permanently broken route was indistinguishable from a user mistyping an address. And the context these errors carry names the developer's own typo, the parameters that were refused and the constraint's reason — the operator's material, not the client's.

The refusal itself stays a field error by design: the validator fails closed on a rule it cannot honour rather than passing the value, and VALIDATION.md documents the codes as validation codes with full rights. */
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
