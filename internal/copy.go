package internal

import "reflect"

/* maxCopyDepth bounds the recursion for genuinely deep data: past it a value is returned as-is rather than overflowing the goroutine stack. Cycles and shared substructure never reach it, because the visited map resolves them. */
const maxCopyDepth = 10000

/* visitedKey identifies a container node: a map by its pointer, a slice by its backing array and length, and both by static type, since a defined type over map[string]any shares its header with the plain value yet is copied on another path. */
type visitedKey struct {
    pointer   uintptr
    length    int
    valueType reflect.Type
}

const mapLength = -1

var plainMapType = reflect.TypeOf(map[string]any(nil))
var plainSliceType = reflect.TypeOf([]any(nil))

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
    return copyAnyMapAtDepth(source, 0, map[visitedKey]any{})
}

func CopyAnySlice(source []any) []any {
    return copyAnySliceAtDepth(source, 0, map[visitedKey]any{})
}

/* CopyAnyValue copies one value the way CopyAnyMap copies a map: maps and slices are descended into, everything else is returned as-is. */
func CopyAnyValue(value any) any {
    return copyAnyValueAtDepth(value, 0, map[visitedKey]any{})
}

func copyAnyMapAtDepth(source map[string]any, depth int, visited map[visitedKey]any) map[string]any {
    if nil == source {
        return map[string]any{}
    }

    key := visitedKey{pointer: reflect.ValueOf(source).Pointer(), length: mapLength, valueType: plainMapType}
    if existing, seen := visited[key]; true == seen {
        return existing.(map[string]any)
    }

    copied := make(map[string]any, len(source))
    /* registered before the descent, so a cycle closes onto the copy */
    visited[key] = copied

    for sourceKey, value := range source {
        copied[sourceKey] = copyAnyValueAtDepth(value, depth, visited)
    }

    return copied
}

func copyAnySliceAtDepth(source []any, depth int, visited map[visitedKey]any) []any {
    if nil == source {
        return nil
    }

    if 0 < len(source) {
        key := visitedKey{pointer: reflect.ValueOf(source).Pointer(), length: len(source), valueType: plainSliceType}
        if existing, seen := visited[key]; true == seen {
            return existing.([]any)
        }

        copied := make([]any, len(source))
        visited[key] = copied

        for index, value := range source {
            copied[index] = copyAnyValueAtDepth(value, depth, visited)
        }

        return copied
    }

    /* a zero-length slice carries nothing shareable and its backing pointer is not a stable identity, so it is copied without entering the visited map */
    return make([]any, 0)
}

/* at maxCopyDepth the value is returned as-is, a shallow alias, rather than copied further; realistic data is far shallower, and cycles and shared substructure are resolved by the visited map before depth matters. */
func copyAnyValueAtDepth(value any, depth int, visited map[visitedKey]any) any {
    if depth >= maxCopyDepth {
        return value
    }

    switch typedValue := value.(type) {
    case map[string]any:
        return copyAnyMapAtDepth(typedValue, depth+1, visited)
    case []any:
        return copyAnySliceAtDepth(typedValue, depth+1, visited)
    }

    reflectedValue := reflect.ValueOf(value)

    switch reflectedValue.Kind() {
    case reflect.Slice:
        if true == reflectedValue.IsNil() {
            return value
        }

        key := visitedKey{pointer: reflectedValue.Pointer(), length: reflectedValue.Len(), valueType: reflectedValue.Type()}
        if 0 < reflectedValue.Len() {
            if existing, seen := visited[key]; true == seen {
                return existing
            }
        }

        copiedSlice := reflect.MakeSlice(reflectedValue.Type(), reflectedValue.Len(), reflectedValue.Len())
        if 0 < reflectedValue.Len() {
            visited[key] = copiedSlice.Interface()
        }

        for index := 0; index < reflectedValue.Len(); index++ {
            copiedElement := copyAnyValueAtDepth(reflectedValue.Index(index).Interface(), depth+1, visited)
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

        key := visitedKey{pointer: reflectedValue.Pointer(), length: mapLength, valueType: reflectedValue.Type()}
        if existing, seen := visited[key]; true == seen {
            return existing
        }

        copiedMap := reflect.MakeMapWithSize(reflectedValue.Type(), reflectedValue.Len())
        visited[key] = copiedMap.Interface()

        iterator := reflectedValue.MapRange()
        for true == iterator.Next() {
            copiedElement := copyAnyValueAtDepth(iterator.Value().Interface(), depth+1, visited)
            if nil == copiedElement {
                copiedMap.SetMapIndex(iterator.Key(), reflect.Zero(reflectedValue.Type().Elem()))

                continue
            }

            copiedMap.SetMapIndex(iterator.Key(), reflect.ValueOf(copiedElement))
        }

        return copiedMap.Interface()
    }

    /* every other kind — a pointer, a struct or array holding one, a channel, a function — is returned as-is: the copy descends into maps and slices only, which is the documented boundary of what session data may safely carry (a pointer put into a session is shared state between the requests that loaded it) */
    return value
}
