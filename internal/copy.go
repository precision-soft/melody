package internal

import "reflect"

/* @important maxCopyDepth bounds the deep-copy recursion as a safety net for genuinely deep (non-shared) data: a value that legitimately nests past this bound is returned as-is from there on rather than overflowing the goroutine stack — a fatal error that no deferred recover() can catch and which takes down the whole process. Cycles and shared substructure no longer reach this bound at all: the visited map closes a cycle onto its own copy and reuses the copy of a node reached through a second edge, so the traversal is linear in the number of distinct nodes where the depth-only form was exponential — a 28-level value whose every node was reachable through two edges never finished copying, with the caller's lock held the whole time. */
const maxCopyDepth = 10000

/* visitedKey identifies a container node across the two edges that may reach it: maps are identified by their pointer alone, slices by their backing array pointer AND length, because two slices of one array with different lengths are two different nodes — memoizing on the pointer alone would hand the copy of one to a reader of the other. */
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

func copyAnyMapAtDepth(source map[string]any, depth int, visited map[visitedKey]any) map[string]any {
    if nil == source {
        return map[string]any{}
    }

    key := visitedKey{pointer: reflect.ValueOf(source).Pointer(), length: mapLength}
    if existing, seen := visited[key]; true == seen {
        return existing.(map[string]any)
    }

    copied := make(map[string]any, len(source))
    /* registered before the descent: a cycle that returns to this node closes onto the copy, not onto the live original */
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

    /* a zero-length slice carries nothing shareable and its backing pointer is not a stable identity, so it is copied without entering the visited map */
    return make([]any, 0)
}

/* @important at maxCopyDepth the value is returned as-is (a shallow alias) rather than copied further; realistic data (JSON-derived) is far shallower, and the shapes that used to ride the recursion to this bound — cycles, shared substructure — are resolved by the visited map before depth ever matters. */
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

    /* every other kind — a pointer, a struct or array holding one, a channel, a function — is returned as-is: the copy descends into maps and slices only, which is the documented boundary of what session data may safely carry (a pointer put into a session is shared state between the requests that loaded it) */
    return value
}
