package internal

import "reflect"

/* @important maxCopyDepth bounds the deep-copy recursion so a cyclic value stored in session data (for example a map that contains itself, reachable through the public Session.Set/Save API which take any) cannot recurse until the goroutine stack overflows — a fatal error that no deferred recover() can catch and which takes down the whole process. Realistic session data (JSON-derived) is far shallower than this bound. */
const maxCopyDepth = 10000

func CopyStringMap[T any](input map[string]T) map[string]T {
    if nil == input {
        return make(map[string]T)
    }

    copied := make(map[string]T, len(input))

    for key, value := range input {
        copied[key] = value
    }

    return copied
}

func CopyAnyMap(source map[string]any) map[string]any {
    return copyAnyMapAtDepth(source, 0)
}

func CopyAnySlice(source []any) []any {
    return copyAnySliceAtDepth(source, 0)
}

func copyAnyMapAtDepth(source map[string]any, depth int) map[string]any {
    if nil == source {
        return map[string]any{}
    }

    copied := make(map[string]any, len(source))
    for key, value := range source {
        copied[key] = copyAnyValueAtDepth(value, depth)
    }

    return copied
}

func copyAnySliceAtDepth(source []any, depth int) []any {
    if nil == source {
        return nil
    }

    copied := make([]any, len(source))
    for index, value := range source {
        copied[index] = copyAnyValueAtDepth(value, depth)
    }

    return copied
}

/* @important at maxCopyDepth the value is returned as-is (a shallow alias) rather than copied further, which both halts a cyclic structure before it overflows the stack and leaves legitimate (far shallower) data fully deep-copied. */
func copyAnyValueAtDepth(value any, depth int) any {
    if depth >= maxCopyDepth {
        return value
    }

    switch typedValue := value.(type) {
    case map[string]any:
        return copyAnyMapAtDepth(typedValue, depth+1)
    case []any:
        return copyAnySliceAtDepth(typedValue, depth+1)
    }

    reflectedValue := reflect.ValueOf(value)

    switch reflectedValue.Kind() {
    case reflect.Slice:
        if true == reflectedValue.IsNil() {
            return value
        }

        copiedSlice := reflect.MakeSlice(reflectedValue.Type(), reflectedValue.Len(), reflectedValue.Len())
        for index := 0; index < reflectedValue.Len(); index++ {
            copiedElement := copyAnyValueAtDepth(reflectedValue.Index(index).Interface(), depth+1)
            if nil == copiedElement {
                copiedSlice.Index(index).Set(reflect.Zero(reflectedValue.Type().Elem()))

                continue
            }

            copiedSlice.Index(index).Set(reflect.ValueOf(copiedElement))
        }

        return copiedSlice.Interface()
    case reflect.Map:
        if true == reflectedValue.IsNil() {
            return value
        }

        copiedMap := reflect.MakeMapWithSize(reflectedValue.Type(), reflectedValue.Len())
        iterator := reflectedValue.MapRange()
        for true == iterator.Next() {
            copiedElement := copyAnyValueAtDepth(iterator.Value().Interface(), depth+1)
            if nil == copiedElement {
                copiedMap.SetMapIndex(iterator.Key(), reflect.Zero(reflectedValue.Type().Elem()))

                continue
            }

            copiedMap.SetMapIndex(iterator.Key(), reflect.ValueOf(copiedElement))
        }

        return copiedMap.Interface()
    case reflect.Pointer:
        return copyPointerAtDepth(reflectedValue, value, depth)
    case reflect.Struct:
        return copyStructAtDepth(reflectedValue, value, depth)
    case reflect.Array:
        return copyArrayAtDepth(reflectedValue, value, depth)
    }

    return value
}

/* a pointer handed through a copy addresses the very value the source keeps reading, so the copy isolates nothing: a consumer that writes through it writes into the original. The pointee is copied into a fresh allocation and the returned pointer addresses that. Identity is deliberately not preserved — two pointers that addressed one value in the source address two values in the copy — because a copy whose holder can still reach the source is not a copy. */
func copyPointerAtDepth(reflectedValue reflect.Value, value any, depth int) any {
    if true == reflectedValue.IsNil() {
        return value
    }

    copiedPointer := reflect.New(reflectedValue.Type().Elem())

    copiedElement := copyAnyValueAtDepth(reflectedValue.Elem().Interface(), depth+1)
    if nil != copiedElement {
        copiedPointer.Elem().Set(reflect.ValueOf(copiedElement))
    }

    return copiedPointer.Interface()
}

/* a struct travels inside an interface as a copy of its own bytes, so its scalar fields are isolated already; its slice, map and pointer fields are not, because those fields are headers that keep addressing the source's memory. That is how a consumer which only reads the copy still writes into the original. Every exported field that can carry such a reference is copied.

Unexported fields cannot be read or written through reflection without unsafe, so they stay as the whole-struct assignment left them: shallow. That limit is also what keeps the handle types usable through a copy — an *os.File, a *sql.DB, the location inside a time.Time are reached through unexported fields, and duplicating them would produce a structurally valid value that no longer names the thing it stood for.

A struct with no exported field that can alias is returned as it arrived, so the common case — a time, a decoded number, a plain record of scalars — allocates nothing. */
func copyStructAtDepth(reflectedValue reflect.Value, value any, depth int) any {
    structType := reflectedValue.Type()

    var copiedStruct reflect.Value

    for index := 0; index < structType.NumField(); index++ {
        field := structType.Field(index)

        if false == field.IsExported() {
            continue
        }

        if false == kindMayAliasSource(field.Type.Kind()) {
            continue
        }

        if false == copiedStruct.IsValid() {
            copiedStruct = reflect.New(structType).Elem()
            copiedStruct.Set(reflectedValue)
        }

        copiedField := copyAnyValueAtDepth(reflectedValue.Field(index).Interface(), depth+1)
        if nil == copiedField {
            copiedStruct.Field(index).Set(reflect.Zero(field.Type))

            continue
        }

        copiedStruct.Field(index).Set(reflect.ValueOf(copiedField))
    }

    if false == copiedStruct.IsValid() {
        return value
    }

    return copiedStruct.Interface()
}

/* an array is copied whole when it is assigned, so only its elements can still address the source. An array of scalars is returned as it arrived rather than walked, which is what keeps a checksum or an identifier free of any cost. */
func copyArrayAtDepth(reflectedValue reflect.Value, value any, depth int) any {
    arrayType := reflectedValue.Type()

    if false == kindMayAliasSource(arrayType.Elem().Kind()) {
        return value
    }

    copiedArray := reflect.New(arrayType).Elem()
    copiedArray.Set(reflectedValue)

    for index := 0; index < reflectedValue.Len(); index++ {
        copiedElement := copyAnyValueAtDepth(reflectedValue.Index(index).Interface(), depth+1)
        if nil == copiedElement {
            copiedArray.Index(index).Set(reflect.Zero(arrayType.Elem()))

            continue
        }

        copiedArray.Index(index).Set(reflect.ValueOf(copiedElement))
    }

    return copiedArray.Interface()
}

/* the kinds whose value still names memory the source keeps using: a slice or map header, a pointer, an interface holding any of those, and the two composites that can contain them. Every other kind is duplicated by the assignment itself, so walking it would allocate for nothing. Chan and Func are absent on purpose — each names a single shared thing that has no copy, so aliasing is the only behaviour available for them. */
func kindMayAliasSource(kind reflect.Kind) bool {
    switch kind {
    case reflect.Slice, reflect.Map, reflect.Pointer, reflect.Interface, reflect.Struct, reflect.Array:
        return true
    }

    return false
}
