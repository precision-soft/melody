package bag

import (
	"strings"

	bagcontract "github.com/precision-soft/melody/v2/bag/contract"
	"github.com/precision-soft/melody/v2/internal"
)

func String(parameterBag bagcontract.ParameterBag, name string) (string, bool) {
	value, exists := parameterBag.Get(name)
	if false == exists {
		return "", false
	}

	if nil == value {
		return "", true
	}

	stringValue, isString := value.(string)
	if true == isString {
		return stringValue, true
	}

	return "", true
}

func StringOrDefault(parameterBag bagcontract.ParameterBag, name string, defaultValue string) string {
	value, exists := String(parameterBag, name)
	if false == exists {
		return defaultValue
	}

	return value
}

func HasNonEmptyString(parameterBag bagcontract.ParameterBag, name string) bool {
	value, exists := String(parameterBag, name)
	if false == exists {
		return false
	}

	if "" == strings.TrimSpace(value) {
		return false
	}

	return true
}

func Int(parameterBag bagcontract.ParameterBag, name string) (int64, bool, error) {
	value, exists := parameterBag.Get(name)
	if false == exists {
		return 0, false, nil
	}

	return internal.Int(value, name)
}

func Bool(parameterBag bagcontract.ParameterBag, name string) (bool, bool, error) {
	value, exists := parameterBag.Get(name)
	if false == exists {
		return false, false, nil
	}

	return internal.Bool(value, name)
}

func Float64(parameterBag bagcontract.ParameterBag, name string) (float64, bool, error) {
	value, exists := parameterBag.Get(name)
	if false == exists {
		return 0, false, nil
	}

	return internal.Float64(value, name)
}
