package bag

import (
	bagcontract "github.com/precision-soft/melody/v2/bag/contract"
	"github.com/precision-soft/melody/v2/internal"
)

func StringStrict(parameterBag bagcontract.ParameterBag, name string) (string, bool, error) {
	value, exists := parameterBag.Get(name)
	if false == exists {
		return "", false, nil
	}

	if nil == value {
		return "", true, nil
	}

	stringValue, isString := value.(string)
	if true == isString {
		return stringValue, true, nil
	}

	return "", true, internal.ParseError(name, "string", value, nil)
}
