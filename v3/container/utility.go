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

/* typeIdentityKey is a stable map/stack key that is unique per type identity, unlike String(), which two same-named types from different packages share. A package cannot declare two types of the same name, so the named type's import path — reached through any pointer, slice, array, channel or map wrapping — plus the full String() distinguishes them without a registry or a lock. Reached through pointers alone, the key of an unnamed composite type carried no path at all, so a slice of one package's Bus and a slice of another package's Bus of the same short name were one key: a declaration on the first passed as registered through the second, and the plan wrote an edge onto a service nothing had named. */
func typeIdentityKey(targetType reflect.Type) string {
    if nil == targetType {
        return ""
    }

    return typeIdentityPath(targetType) + "\x00" + targetType.String()
}

/* typeIdentityPath is the import path a type's identity comes from: its own when it is named, and its named element's when it is a pointer, slice, array or channel of one — a map's is the paths of its key and its element, joined. A type built of nothing named — a function, an unnamed struct, an unnamed interface — has no path, and String() alone is what tells two of them apart. */
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

/* uniqueTypeName is the service name a type registration derives from the type. It qualifies the type with its import path, so two same-named types from different packages — which share a String() built from the short package name — get distinct names and can both be type-registered. An unnamed or builtin type, never a real service type, keeps its String(). */
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

/* overrideValueFitsRegisteredType decides whether an override value may sit under a registered type, judging it the way the readers will: raw assignability covers the interface registrations, whose stored value is asserted against the interface at resolution; the canonical-identity arm covers the value-typed registrations, whose canonical key already holds raw values built by the provider itself — a string service is registered under *string, so a string override occupies exactly the slot a built string occupies, and raw assignability alone would refuse what the registration's own creations serve. */
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
