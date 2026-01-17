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
		context["context"] = instance.context
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
