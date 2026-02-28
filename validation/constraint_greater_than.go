package validation

import (
    "fmt"
    "reflect"

    validationcontract "github.com/precision-soft/melody/validation/contract"
)

const (
    ConstraintGreaterThan                 = "greaterThan"
    ConstraintGreaterThanErrorSmallerThan = "smallerThan"
)

func NewGreaterThan(min int) *GreaterThan {
    return &GreaterThan{
        min: min,
    }
}

type GreaterThan struct {
    min int
}

func (instance *GreaterThan) Validate(value any, field string) validationcontract.ValidationError {
    if nil == value {
        return nil
    }

    reflectedValue := reflect.ValueOf(value)
    for {
        if reflect.Invalid == reflectedValue.Kind() {
            return NewValidationError(field, "value is invalid", ConstraintGreaterThanErrorSmallerThan, nil)
        }

        if (reflect.Pointer == reflectedValue.Kind()) || (reflect.Interface == reflectedValue.Kind()) {
            if true == reflectedValue.IsNil() {
                return NewValidationError(
                    field,
                    fmt.Sprintf("value must be greater than %d", instance.min),
                    ConstraintGreaterThanErrorSmallerThan,
                    map[string]any{
                        "min": instance.min,
                    },
                )
            }

            reflectedValue = reflectedValue.Elem()
            continue
        }

        break
    }

    switch reflectedValue.Kind() {
    case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
        actual := reflectedValue.Int()
        if actual <= int64(instance.min) {
            return NewValidationError(
                field,
                fmt.Sprintf("value must be greater than %d", instance.min),
                ConstraintGreaterThanErrorSmallerThan,
                map[string]any{
                    "min":    instance.min,
                    "actual": actual,
                },
            )
        }

        return nil

    case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
        actual := reflectedValue.Uint()

        if 0 > instance.min {
            return nil
        }

        if actual <= uint64(instance.min) {
            return NewValidationError(
                field,
                fmt.Sprintf("value must be greater than %d", instance.min),
                ConstraintGreaterThanErrorSmallerThan,
                map[string]any{
                    "min":    instance.min,
                    "actual": actual,
                },
            )
        }

        return nil

    default:
        return NewValidationError(field, "value must be an integer", ConstraintGreaterThanErrorSmallerThan, nil)
    }
}

func (instance *GreaterThan) Min() int {
    return instance.min
}

var _ validationcontract.Constraint = (*GreaterThan)(nil)
