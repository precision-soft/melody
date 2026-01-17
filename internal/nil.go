package internal

import (
	"reflect"
)

func IsNilInterface(value any) bool {
	if nil == value {
		return true
	}

	reflectedValue := reflect.ValueOf(value)

	switch reflectedValue.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return true == reflectedValue.IsNil()
	default:
		return false
	}
}
