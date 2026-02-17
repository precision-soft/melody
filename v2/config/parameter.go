package config

import (
	"fmt"
	"strconv"
	"strings"

	configcontract "github.com/precision-soft/melody/v2/config/contract"
	"github.com/precision-soft/melody/v2/exception"
	"github.com/precision-soft/melody/v2/internal"
)

type ParameterMap map[string]*Parameter

func NewParameter(environmentKey string, environmentValue any, value any, isDefault bool) *Parameter {
	return &Parameter{
		environmentKey:   environmentKey,
		environmentValue: environmentValue,
		value:            value,
		isDefault:        isDefault,
	}
}

type Parameter struct {
	environmentKey   string
	environmentValue any
	value            any
	isDefault        bool
}

func (instance *Parameter) EnvironmentKey() string {
	return instance.environmentKey
}

func (instance *Parameter) EnvironmentValue() any {
	return instance.environmentValue
}

func (instance *Parameter) Value() any {
	return instance.value
}

func (instance *Parameter) IsDefault() bool {
	return instance.isDefault
}

func (instance *Parameter) String() string {
	stringValue, ok := instance.value.(string)
	if true == ok {
		return stringValue
	}

	return ""
}

func (instance *Parameter) MustString() string {
	stringValue, ok := instance.value.(string)
	if true == ok {
		return stringValue
	}

	exception.Panic(
		exception.NewError(
			"cannot convert parameter value to string",
			map[string]any{
				"environmentKey": instance.environmentKey,
				"valueType":      fmt.Sprintf("%T", instance.value),
			},
			nil,
		),
	)

	return ""
}

func (instance *Parameter) Bool() (bool, error) {
	boolValue, ok := instance.value.(bool)
	if true == ok {
		return boolValue, nil
	}

	stringValue, ok := instance.value.(string)
	if true == ok {
		parsedValue, boolFromStringErr := internal.BoolFromString(stringValue)
		if nil != boolFromStringErr {
			return false, exception.NewError(
				"cannot convert parameter value to bool",
				map[string]any{
					"environmentKey": instance.environmentKey,
				},
				boolFromStringErr,
			)
		}

		return parsedValue, nil
	}

	return false, exception.NewError(
		"cannot convert parameter value to bool",
		map[string]any{
			"environmentKey": instance.environmentKey,
		},
		nil,
	)
}

func (instance *Parameter) Int() (int, error) {
	intValue, ok := instance.value.(int)
	if true == ok {
		return intValue, nil
	}

	stringValue, ok := instance.value.(string)
	if true == ok {
		parsedValue, atoiErr := strconv.Atoi(strings.TrimSpace(stringValue))
		if nil != atoiErr {
			return 0, exception.NewError(
				"cannot convert parameter value to int",
				map[string]any{
					"environmentKey": instance.environmentKey,
				},
				atoiErr,
			)
		}

		return parsedValue, nil
	}

	return 0, exception.NewError(
		"cannot convert parameter value to int",
		map[string]any{
			"environmentKey": instance.environmentKey,
		},
		nil,
	)
}

var _ configcontract.Parameter = (*Parameter)(nil)
