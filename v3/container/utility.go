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

/* typeIdentityKey is a map and stack key unique per type identity, unlike String(): the import path of the named type, reached through any wrapping, plus the full String(). */
func typeIdentityKey(targetType reflect.Type) string {
    if nil == targetType {
        return ""
    }

    return typeIdentityPath(targetType) + "\x00" + targetType.String()
}

/* typeIdentityPath is the import path a type's identity comes from: its own when named, its named element's through a pointer, slice, array or channel, a map's key and element paths joined. A type built of nothing named has no path; a colliding key is refused at registration by recordTypeIdentityKeyLocked. */
func typeIdentityPath(targetType reflect.Type) string {
    if "" != targetType.PkgPath() {
        return targetType.PkgPath()
    }

    switch targetType.Kind() {
    case reflect.Ptr, reflect.Slice, reflect.Array, reflect.Chan:
        return typeIdentityPath(targetType.Elem())
    case reflect.Map:
        return typeIdentityPath(targetType.Key()) + "|" + typeIdentityPath(targetType.Elem())
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
