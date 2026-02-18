package validation

import (
	"reflect"
	"strings"
	"sync"

	"github.com/precision-soft/melody/exception"
	exceptioncontract "github.com/precision-soft/melody/exception/contract"
	"github.com/precision-soft/melody/internal"
	validationcontract "github.com/precision-soft/melody/validation/contract"
)

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
	var errors ValidationErrors

	if nil == data {
		return errors
	}

	value := reflect.ValueOf(data)
	if reflect.Ptr == value.Kind() {
		if true == value.IsNil() {
			return errors
		}

		value = value.Elem()
	}

	if reflect.Struct != value.Kind() {
		return errors
	}

	valueType := value.Type()

	for i := 0; i < value.NumField(); i++ {
		field := valueType.Field(i)
		fieldValue := value.Field(i)

		if false == field.IsExported() {
			continue
		}

		validateTag := field.Tag.Get("validate")
		if "" == validateTag || "-" == validateTag {
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

		rules, err := parseValidationTag(validateTag)
		if nil != err {
			errors = append(
				errors,
				NewValidationError(
					fieldName,
					"invalid validation tag syntax",
					ErrorInvalidRuleSyntax,
					map[string]any{
						"tag": validateTag,
					},
				),
			)

			continue
		}

		for _, rule := range rules {
			validationError := instance.validateRule(
				fieldValue.Interface(),
				fieldName,
				rule,
			)
			if nil != validationError {
				errors = append(errors, validationError)
			}
		}
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

	constraint := instance.createConstraintWithParams(rule.name, rule.params)

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

func (instance *Validator) createConstraintWithParams(name string, params map[string]string) validationcontract.Constraint {
	switch name {
	case ConstraintMinLength:
		if valueString, exists := params["value"]; true == exists {
			return NewMinLength(parseInt(valueString, 0))
		}
		return NewMinLength(1)

	case ConstraintMaxLength:
		if valueString, exists := params["value"]; true == exists {
			return NewMaxLength(parseInt(valueString, 100))
		}
		return NewMaxLength(100)

	case ConstraintRegex:
		if patternString, exists := params["pattern"]; true == exists {
			return NewRegex(patternString)
		}
		return NewRegex(".*")

	default:
		instance.mutex.RLock()
		constraint := instance.constraints[name]
		instance.mutex.RUnlock()

		return constraint
	}
}
