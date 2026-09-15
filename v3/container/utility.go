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

func typeIdentityKey(targetType reflect.Type) string {
    if nil == targetType {
        return ""
    }

    named := targetType
    for reflect.Ptr == named.Kind() {
        named = named.Elem()
    }

    return named.PkgPath() + "\x00" + targetType.String()
}

func defaultServiceNameForType(targetType reflect.Type) string {
    canonicalType := canonicalServiceType(targetType)

    if nil == canonicalType {
        return ""
    }

    return uniqueTypeName(canonicalType)
}

func uniqueTypeName(targetType reflect.Type) string {
    pointerPrefix := ""
    named := targetType
    for reflect.Ptr == named.Kind() {
        pointerPrefix = pointerPrefix + "*"
        named = named.Elem()
    }

    if "" == named.PkgPath() {
        return targetType.String()
    }

    return pointerPrefix + named.PkgPath() + "." + named.Name()
}

func overrideValueFitsRegisteredType(valueType reflect.Type, registeredType reflect.Type) bool {
    if true == valueType.AssignableTo(registeredType) {
        return true
    }

    return canonicalServiceType(valueType) == registeredType
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
