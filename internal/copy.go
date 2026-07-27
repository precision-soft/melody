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
    }

    return value
}
