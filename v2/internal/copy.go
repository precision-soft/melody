package internal

import "reflect"

/* maxCopyDepth bounds the deep-copy recursion as a safety net for genuinely deep (non-shared) data: a value that legitimately nests past this bound is returned as-is from there on rather than overflowing the goroutine stack — a fatal error that no deferred recover() can catch and which takes down the whole process. Cycles and shared substructure no longer reach this bound at all: the visited map closes a cycle onto its own copy and reuses the copy of a node reached through a second edge under the same static type — a node viewed under two defined types is two nodes in the copy, since the memo's copy carries one static type; what they hold beneath stays one node —, so the traversal is linear in the number of distinct nodes where the depth-only form was exponential — a 28-level value whose every node was reachable through two edges never finished copying, with the caller's lock held the whole time. */
const maxCopyDepth = 10000

/* visitedKey identifies a container node across the two edges that may reach it: maps are identified by their pointer, slices by their backing array pointer AND length, because two slices of one array with different lengths are two different nodes — memoizing on the pointer alone would hand the copy of one to a reader of the other. The static TYPE is part of the identity as well: a defined type over map[string]any or []any (a Meta, a claims map) shares its header with the plain value it was converted from, so the two spell the same pointer while being copied on different paths — the plain one on the typed fast path, the defined one through reflection — and the fast path's assertion on a memo the other path had written fell over, intermittently, on whichever key the map iteration reached second. */
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

/* CopyAnyValue copies one value at the same depth CopyAnyMap copies a whole map: maps and slices are descended into, everything else — a pointer, a struct, a channel — is returned as-is, which is the documented boundary of what a copied container may safely carry. */
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

/* at maxCopyDepth the value is returned as-is (a shallow alias) rather than copied further; realistic data (JSON-derived) is far shallower, and the shapes that used to ride the recursion to this bound — cycles, shared substructure — are resolved by the visited map before depth ever matters. */
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
