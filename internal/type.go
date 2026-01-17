package internal

import (
	"reflect"
)

func StringifyType(value any) string {
	if nil == value {
		return "nil"
	}

	return reflect.TypeOf(value).String()
}
