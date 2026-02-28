package validation

import (
    "reflect"

    validationcontract "github.com/precision-soft/melody/validation/contract"
)

const (
    ConstraintNotEmpty           = "notEmpty"
    ConstraintNotEmptyErrorEmpty = "empty"
)

func NewNotEmpty() *NotEmpty {
    return &NotEmpty{}
}

type NotEmpty struct{}

func (instance *NotEmpty) Validate(value any, field string) validationcontract.ValidationError {
    if nil == value {
        return NewValidationError(field, "value must not be empty", ConstraintNotEmptyErrorEmpty, nil)
    }

    reflectedValue := reflect.ValueOf(value)

    for {
        if reflect.Invalid == reflectedValue.Kind() {
            return NewValidationError(field, "value is invalid", ConstraintNotEmptyErrorEmpty, nil)
        }

        if (reflect.Pointer == reflectedValue.Kind()) || (reflect.Interface == reflectedValue.Kind()) {
            if true == reflectedValue.IsNil() {
                return NewValidationError(field, "value must not be empty", ConstraintNotEmptyErrorEmpty, nil)
            }
            reflectedValue = reflectedValue.Elem()
            continue
        }

        break
    }

    switch reflectedValue.Kind() {
    case reflect.String, reflect.Array, reflect.Slice, reflect.Map:
        if 0 == reflectedValue.Len() {
            return NewValidationError(field, "value must not be empty", ConstraintNotEmptyErrorEmpty, nil)
        }
        return nil
    default:
        return NewValidationError(field, "value must be a string/array/slice/map", ConstraintNotEmptyErrorEmpty, nil)
    }
}

var _ validationcontract.Constraint = (*NotEmpty)(nil)
