package validation

import (
    "fmt"
    "math"
    "reflect"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    validationcontract "github.com/precision-soft/melody/validation/contract"
)

const (
    ConstraintLessThan                 = "lessThan"
    ConstraintLessThanErrorGreaterThan = "greaterThanMax"
)

func NewLessThan(max int) *LessThan {
    return &LessThan{
        max: max,
    }
}

type LessThan struct {
    max int
}

func (instance *LessThan) Validate(value any, field string) validationcontract.ValidationError {
    if nil == value {
        return nil
    }

    reflectedValue := reflect.ValueOf(value)
    for {
        if reflect.Invalid == reflectedValue.Kind() {
            return NewValidationError(field, "value is invalid", ConstraintLessThanErrorGreaterThan, nil)
        }

        if (reflect.Pointer == reflectedValue.Kind()) || (reflect.Interface == reflectedValue.Kind()) {
            if true == reflectedValue.IsNil() {
                return NewValidationError(
                    field,
                    fmt.Sprintf("value must be less than %d", instance.max),
                    ConstraintLessThanErrorGreaterThan,
                    map[string]any{
                        "max": instance.max,
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
        if actual >= int64(instance.max) {
            return NewValidationError(
                field,
                fmt.Sprintf("value must be less than %d", instance.max),
                ConstraintLessThanErrorGreaterThan,
                map[string]any{
                    "max":    instance.max,
                    "actual": actual,
                },
            )
        }

        return nil

    case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
        actual := reflectedValue.Uint()

        if 0 > instance.max {
            return NewValidationError(
                field,
                fmt.Sprintf("value must be less than %d", instance.max),
                ConstraintLessThanErrorGreaterThan,
                map[string]any{
                    "max":    instance.max,
                    "actual": actual,
                },
            )
        }

        if actual >= uint64(instance.max) {
            return NewValidationError(
                field,
                fmt.Sprintf("value must be less than %d", instance.max),
                ConstraintLessThanErrorGreaterThan,
                map[string]any{
                    "max":    instance.max,
                    "actual": actual,
                },
            )
        }

        return nil

    case reflect.Float32, reflect.Float64:
        actual := reflectedValue.Float()
        if true == math.IsNaN(actual) || actual >= float64(instance.max) {
            return NewValidationError(
                field,
                fmt.Sprintf("value must be less than %d", instance.max),
                ConstraintLessThanErrorGreaterThan,
                map[string]any{
                    "max":    instance.max,
                    "actual": actual,
                },
            )
        }

        return nil

    default:
        return NewValidationError(field, "value must be numeric", ConstraintLessThanErrorGreaterThan, nil)
    }
}

func (instance *LessThan) Max() int {
    return instance.max
}

func (instance *LessThan) WithParams(params map[string]string) (validationcontract.Constraint, error) {
    valueString, exists := params["value"]
    if false == exists {
        return NewLessThan(instance.max), nil
    }

    parsed, ok := parseIntStrict(valueString)
    if false == ok {
        return nil, exception.NewError(
            "invalid less than parameter",
            exceptioncontract.Context{
                "value": valueString,
            },
            nil,
        )
    }

    return NewLessThan(parsed), nil
}

var _ validationcontract.Constraint = (*LessThan)(nil)
var _ validationcontract.ParameterizedConstraint = (*LessThan)(nil)
