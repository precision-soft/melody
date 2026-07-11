package validation

import (
    "fmt"
    "reflect"
    "strings"
    "sync"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/internal"
    validationcontract "github.com/precision-soft/melody/v2/validation/contract"
)

/* @important maxNestedValidationDepth bounds the recursive descent into nested struct/slice/map/embedded values so a self-referential or deeply cyclic payload cannot overflow the stack; the visited-pointer set below short-circuits genuine reference cycles, and this depth cap is the belt-and-suspenders backstop for value cycles the pointer set cannot observe. */
const maxNestedValidationDepth = 64

/* @important cyclicReference identifies an already-visited pointer/map header during the recursive descent so a reference cycle (a self-referential linked node, a slice/map that reaches back to an ancestor) is validated once and then short-circuited instead of recursing forever. */
type cyclicReference struct {
    pointer uintptr
    typ     reflect.Type
}

func NewValidator() *Validator {
    validator := &Validator{
        constraints: make(map[string]validationcontract.Constraint),
    }

    validator.RegisterConstraint(ConstraintNotBlank, &NotBlank{})
    validator.RegisterConstraint(ConstraintEmail, &Email{})
    validator.RegisterConstraint(ConstraintMinLength, NewMinLength(1))
    validator.RegisterConstraint(ConstraintMaxLength, NewMaxLength(100))
    validator.RegisterConstraint(ConstraintRegex, NewRegex(".*"))
    validator.RegisterConstraint(ConstraintNumeric, &Numeric{})
    validator.RegisterConstraint(ConstraintAlpha, &Alpha{})
    validator.RegisterConstraint(ConstraintAlphanumeric, &Alphanumeric{})
    validator.RegisterConstraint(ConstraintGreaterThan, NewGreaterThan(0))
    validator.RegisterConstraint(ConstraintLessThan, NewLessThan(0))
    validator.RegisterConstraint(ConstraintNotEmpty, NewNotEmpty())

    return validator
}

type Validator struct {
    mutex       sync.RWMutex
    constraints map[string]validationcontract.Constraint
}

func (instance *Validator) RegisterConstraint(name string, constraint validationcontract.Constraint) {
    if "" == name {
        exception.Panic(exception.NewError("constraint name is empty", nil, nil))
    }

    trimmedName := strings.TrimSpace(name)
    if name != trimmedName {
        exception.Panic(
            exception.NewError(
                "constraint name must not contain leading or trailing whitespace",
                exceptioncontract.Context{
                    "name": name,
                },
                nil,
            ),
        )
    }

    if true == internal.IsNilInterface(constraint) {
        exception.Panic(
            exception.NewError(
                "constraint instance is nil",
                exceptioncontract.Context{
                    "name": name,
                },
                nil,
            ),
        )
    }

    instance.mutex.Lock()

    _, exists := instance.constraints[name]
    if true == exists {
        instance.mutex.Unlock()

        exception.Panic(
            exception.NewError(
                "constraint already registered",
                exceptioncontract.Context{
                    "name": name,
                },
                nil,
            ),
        )
    }

    instance.constraints[name] = constraint

    instance.mutex.Unlock()
}

func (instance *Validator) Validate(data any) error {
    errors := instance.validateInternal(data)

    if 0 == len(errors) {
        return nil
    }

    return errors
}

func (instance *Validator) validateInternal(data any) ValidationErrors {
    if nil == data {
        return nil
    }

    return instance.validateReflected(reflect.ValueOf(data), "", 0, make(map[cyclicReference]bool))
}

/* @important validateReflected drives the recursive cascade: it unwraps pointers/interfaces (skipping nil and already-visited references), then dispatches structs, slices/arrays and maps to their per-kind walkers so that validate tags declared on nested fields are enforced with a path that identifies the offending nested field. Scalar leaves have no tags of their own to enforce here (their owning struct applies the tag) and fall through untouched, so a flat payload with no nested tags produces exactly the same result as before. */
func (instance *Validator) validateReflected(value reflect.Value, path string, depth int, visited map[cyclicReference]bool) ValidationErrors {
    var errors ValidationErrors

    if maxNestedValidationDepth < depth {
        return errors
    }

    if false == value.IsValid() {
        return errors
    }

    switch value.Kind() {
    case reflect.Interface:
        if true == value.IsNil() {
            return errors
        }

        return instance.validateReflected(value.Elem(), path, depth+1, visited)
    case reflect.Ptr:
        if true == value.IsNil() {
            return errors
        }

        reference := cyclicReference{pointer: value.Pointer(), typ: value.Type()}
        if true == visited[reference] {
            return errors
        }
        visited[reference] = true

        return instance.validateReflected(value.Elem(), path, depth+1, visited)
    case reflect.Struct:
        return instance.validateStruct(value, path, depth, visited)
    case reflect.Slice, reflect.Array:
        return instance.validateSequence(value, path, depth, visited)
    case reflect.Map:
        return instance.validateMap(value, path, depth, visited)
    default:
        return errors
    }
}

func (instance *Validator) validateStruct(value reflect.Value, path string, depth int, visited map[cyclicReference]bool) ValidationErrors {
    var errors ValidationErrors

    valueType := value.Type()

    for i := 0; i < value.NumField(); i++ {
        field := valueType.Field(i)
        fieldValue := value.Field(i)

        if false == field.IsExported() {
            continue
        }

        fieldName := field.Name
        jsonTag := field.Tag.Get("json")
        if "" != jsonTag && "-" != jsonTag {
            parts := strings.Split(jsonTag, ",")
            if "" != parts[0] {
                fieldName = parts[0]
            }
        }

        fieldPath := fieldName
        if "" != path {
            fieldPath = path + "." + fieldName
        }

        validateTag := field.Tag.Get("validate")
        if "" != validateTag && "-" != validateTag {
            rules, err := parseValidationTag(validateTag)
            if nil != err {
                errors = append(
                    errors,
                    NewValidationError(
                        fieldPath,
                        "invalid validation tag syntax",
                        ErrorInvalidRuleSyntax,
                        map[string]any{
                            "tag": validateTag,
                        },
                    ),
                )
            } else {
                for _, rule := range rules {
                    validationError := instance.validateRule(
                        fieldValue.Interface(),
                        fieldPath,
                        rule,
                    )
                    if nil != validationError {
                        errors = append(errors, validationError)
                    }
                }
            }
        }

        /* @important an embedded (anonymous) struct is promoted onto its parent, mirroring how the openapi schema mirror flattens embeds, so its nested fields keep the parent's path prefix instead of gaining the embed type name as a segment. */
        recursionPath := fieldPath
        if true == field.Anonymous {
            recursionPath = path
        }

        errors = append(errors, instance.validateReflected(fieldValue, recursionPath, depth+1, visited)...)
    }

    return errors
}

func (instance *Validator) validateSequence(value reflect.Value, path string, depth int, visited map[cyclicReference]bool) ValidationErrors {
    var errors ValidationErrors

    if reflect.Slice == value.Kind() {
        if reflect.Uint8 == value.Type().Elem().Kind() {
            /* @important a byte slice is a scalar payload (the openapi mirror emits it as a string/byte), never a sequence of validatable elements, so it carries no nested tags to enforce. */
            return errors
        }

        if true == value.IsNil() {
            return errors
        }
    }

    for i := 0; i < value.Len(); i++ {
        elementPath := fmt.Sprintf("%s[%d]", path, i)

        errors = append(errors, instance.validateReflected(value.Index(i), elementPath, depth+1, visited)...)
    }

    return errors
}

func (instance *Validator) validateMap(value reflect.Value, path string, depth int, visited map[cyclicReference]bool) ValidationErrors {
    var errors ValidationErrors

    if true == value.IsNil() {
        return errors
    }

    iterator := value.MapRange()
    for true == iterator.Next() {
        elementPath := fmt.Sprintf("%s[%v]", path, iterator.Key().Interface())

        errors = append(errors, instance.validateReflected(iterator.Value(), elementPath, depth+1, visited)...)
    }

    return errors
}

func (instance *Validator) validateRule(value any, fieldName string, rule validationRule) validationcontract.ValidationError {
    instance.mutex.RLock()
    _, exists := instance.constraints[rule.name]
    instance.mutex.RUnlock()

    if false == exists {
        return NewValidationError(
            fieldName,
            "unknown validation rule",
            ErrorUnknownRule,
            map[string]any{
                "rule": rule.name,
            },
        )
    }

    constraint, paramsOk := instance.createConstraintWithParams(rule.name, rule.params)
    if false == paramsOk {
        return NewValidationError(
            fieldName,
            "invalid validation rule parameter",
            ErrorInvalidRuleSyntax,
            map[string]any{
                "rule":   rule.name,
                "params": rule.params,
            },
        )
    }

    err := constraint.Validate(value, fieldName)
    if nil == err {
        return nil
    }

    if "" != err.Field() {
        return err
    }

    return NewValidationError(
        fieldName,
        err.Message(),
        err.Code(),
        err.Context(),
    )
}

func (instance *Validator) createConstraintWithParams(name string, params map[string]string) (validationcontract.Constraint, bool) {
    instance.mutex.RLock()
    constraint := instance.constraints[name]
    instance.mutex.RUnlock()

    if 0 == len(params) {
        return constraint, true
    }

    parameterized, ok := constraint.(validationcontract.ParameterizedConstraint)
    if false == ok {
        /* @important fail closed when the tag carries parameters the registered constraint cannot consume: validating with the unparameterized singleton would silently enforce a different configuration than the tag declares (a custom `between(min=1,max=5)` would validate with whatever the singleton was built with) */
        return nil, false
    }

    configured, withParamsErr := parameterized.WithParams(params)
    if nil != withParamsErr {
        return nil, false
    }

    if true == internal.IsNilInterface(configured) {
        return nil, false
    }

    return configured, true
}
