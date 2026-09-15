package internal

import "reflect"

const maxCopyDepth = 10000

type visitedKey struct {
    pointer uintptr
    length  int
}

const mapLength = -1

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

/* CopyAnyValue copies one value at the same depth CopyAnyMap copies a whole map: maps and slices are descended into, everything else — a pointer, a struct, a channel — is returned as-is, which is the documented boundary of what a copied container may safely carry. */
func CopyAnyValue(value any) any {
    return copyAnyValueAtDepth(value, 0, map[visitedKey]any{})
}

func copyAnyMapAtDepth(source map[string]any, depth int, visited map[visitedKey]any) map[string]any {
    if nil == source {
        return map[string]any{}
    }

    key := visitedKey{pointer: reflect.ValueOf(source).Pointer(), length: mapLength}
    if existing, seen := visited[key]; true == seen {
        return existing.(map[string]any)
    }

    copied := make(map[string]any, len(source))

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
        key := visitedKey{pointer: reflect.ValueOf(source).Pointer(), length: len(source)}
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

    return make([]any, 0)
}

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

        key := visitedKey{pointer: reflectedValue.Pointer(), length: reflectedValue.Len()}
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

        key := visitedKey{pointer: reflectedValue.Pointer(), length: mapLength}
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

    return value
}
