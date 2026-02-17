package container

import (
	"reflect"
)

func canonicalServiceType(targetType reflect.Type) reflect.Type {
	if nil == targetType {
		return nil
	}

	if reflect.Interface == targetType.Kind() {
		return targetType
	}

	if reflect.Ptr != targetType.Kind() {
		return reflect.PointerTo(targetType)
	}

	return targetType
}

func defaultServiceNameForType(targetType reflect.Type) string {
	canonicalType := canonicalServiceType(targetType)

	if nil == canonicalType {
		return ""
	}

	return canonicalType.String()
}

func isAnyType(targetType reflect.Type) bool {
	if nil == targetType {
		return false
	}

	if reflect.Interface != targetType.Kind() {
		return false
	}

	return 0 == targetType.NumMethod()
}
