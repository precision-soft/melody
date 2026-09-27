package container

import (
    "reflect"
    "strings"
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

/* typeIdentityKey is a map and stack key unique per type identity, unlike String(): the import path of the named type, reached through any wrapping, plus the full String(). */
func typeIdentityKey(targetType reflect.Type) string {
    if nil == targetType {
        return ""
    }

    return typeIdentityPath(targetType) + "\x00" + targetType.String()
}

/* typeIdentityPath is the import-path material String() leaves out of a type's identity: its own path when named — and nothing more, since a package declares one type per name and String() spells the rest, type arguments with their full paths included — and, for an unnamed composite, the paths of what it is built from, in the order String() spells them: the element of a pointer, slice, array or channel; a map's key and element; a func's parameters then results; a struct's fields, each with the package of an unexported field; an interface's methods, each with the package of an unexported method. Two types of one String() have one shape, so the joined paths compare position by position. Recursion stops at every named type, and an unnamed type cannot contain itself, so it terminates. What it cannot tell apart is two function-local types of one name in one package; recordTypeIdentityKeyLocked refuses that pair. */
func typeIdentityPath(targetType reflect.Type) string {
    if "" != targetType.PkgPath() {
        return targetType.PkgPath()
    }

    switch targetType.Kind() {
    case reflect.Ptr, reflect.Slice, reflect.Array, reflect.Chan:
        return typeIdentityPath(targetType.Elem())
    case reflect.Map:
        return typeIdentityPath(targetType.Key()) + "|" + typeIdentityPath(targetType.Elem())
    case reflect.Func:
        parts := make([]string, 0, targetType.NumIn()+targetType.NumOut())
        for index := 0; index < targetType.NumIn(); index++ {
            parts = append(parts, typeIdentityPath(targetType.In(index)))
        }
        for index := 0; index < targetType.NumOut(); index++ {
            parts = append(parts, typeIdentityPath(targetType.Out(index)))
        }
        return strings.Join(parts, "|")
    case reflect.Struct:
        parts := make([]string, 0, targetType.NumField())
        for index := 0; index < targetType.NumField(); index++ {
            field := targetType.Field(index)
            parts = append(parts, field.PkgPath+":"+typeIdentityPath(field.Type))
        }
        return strings.Join(parts, "|")
    case reflect.Interface:
        parts := make([]string, 0, targetType.NumMethod())
        for index := 0; index < targetType.NumMethod(); index++ {
            method := targetType.Method(index)
            parts = append(parts, method.PkgPath+":"+typeIdentityPath(method.Type))
        }
        return strings.Join(parts, "|")
    }

    return ""
}

func defaultServiceNameForType(targetType reflect.Type) string {
    canonicalType := canonicalServiceType(targetType)

    if nil == canonicalType {
        return ""
    }

    return uniqueTypeName(canonicalType)
}

/* uniqueTypeName is the service name a type registration derives, qualified with the import path so same-named types of different packages get distinct names. An unnamed or builtin type keeps its String(). */
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

/* overrideValueFitsRegisteredType decides whether an override value may sit under a registered type: by assignability for interface registrations, and by canonical identity for value-typed ones, whose canonical key holds raw values. */
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
