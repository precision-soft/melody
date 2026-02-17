package bag

import (
	"github.com/precision-soft/melody/v2/exception"
	exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"

	bagcontract "github.com/precision-soft/melody/v2/bag/contract"
)

func StringSlice(parameterBag bagcontract.ParameterBag, name string) ([]string, bool) {
	value, exists := parameterBag.Get(name)
	if false == exists {
		return nil, false
	}

	if nil == value {
		return nil, true
	}

	switch typedValue := value.(type) {
	case []string:
		copied := make([]string, len(typedValue))
		copy(copied, typedValue)

		return copied, true
	case string:
		return []string{typedValue}, true
	default:
		return nil, true
	}
}

func StringAt(parameterBag bagcontract.ParameterBag, name string, index int) (string, bool, error) {
	values, exists := StringSlice(parameterBag, name)
	if false == exists {
		return "", false, nil
	}

	if 0 > index {
		return "", true, exception.NewError(
			"index must be non negative",
			exceptioncontract.Context{
				"parameterName": name,
				"index":         index,
			},
			nil,
		)
	}

	if index >= len(values) {
		return "", true, exception.NewError(
			"index is out of range",
			exceptioncontract.Context{
				"parameterName": name,
				"index":         index,
				"length":        len(values),
			},
			nil,
		)
	}

	return values[index], true, nil
}

func AppendString(parameterBag bagcontract.ParameterBag, name string, value string) error {
	currentValue, exists := parameterBag.Get(name)
	if false == exists {
		parameterBag.Set(name, []string{value})
		return nil
	}

	if nil == currentValue {
		parameterBag.Set(name, []string{value})
		return nil
	}

	switch typedValue := currentValue.(type) {
	case []string:
		newValues := make([]string, 0, len(typedValue)+1)
		newValues = append(newValues, typedValue...)
		newValues = append(newValues, value)

		parameterBag.Set(name, newValues)

		return nil
	case string:
		parameterBag.Set(name, []string{typedValue, value})

		return nil
	default:
		return exception.NewError(
			"parameter is not a string or string slice",
			exceptioncontract.Context{
				"parameterName": name,
			},
			nil,
		)
	}
}

func AppendStringSlice(parameterBag bagcontract.ParameterBag, name string, values []string) error {
	for _, currentValue := range values {
		appendStringErr := AppendString(parameterBag, name, currentValue)
		if nil != appendStringErr {
			return appendStringErr
		}
	}

	return nil
}
