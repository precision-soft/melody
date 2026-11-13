package logging

import (
    "fmt"
    "reflect"
)

/* textValueCycleMarker stands where a text rendering would enter a container that is its own ancestor; it is the marker the json logger writes for the same shape. */
const textValueCycleMarker = normalizeJsonContextCycleMarker

/* textVisitKey identifies a container on the current path of the walk: a map by its header, a slice by its backing pointer and length, each under its type, so two views of one array are two containers and a defined type over the same backing is its own */
type textVisitKey struct {
    pointer     uintptr
    length      int
    elementType reflect.Type
}

/* renderTextValue renders a value as fmt's %v does, with a cycle rendered as the marker: fmt has no cycle detection, and a map that holds itself would overflow the goroutine stack. A value without a cycle is handed to fmt unchanged. */
func renderTextValue(value any) string {
    if false == textValueHoldsACycle(reflect.ValueOf(value), 0, map[textVisitKey]struct{}{}) {
        return fmt.Sprintf("%v", value)
    }

    return fmt.Sprintf("%v", textSafeValue(reflect.ValueOf(value), 0, map[textVisitKey]struct{}{}))
}

/* textRendersThroughAMethod answers whether %v renders the value through its Formatter, error or Stringer method rather than its shape, so the walk does not descend where fmt does not. */
func textRendersThroughAMethod(value reflect.Value) bool {
    if false == value.CanInterface() {
        return false
    }

    switch value.Interface().(type) {
    case fmt.Formatter, error, fmt.Stringer:
        return true
    }

    return false
}

/* textContainerKey answers the identity of a map or slice for the path of the walk, and false for a nil one, which holds nothing and closes no cycle */
func textContainerKey(value reflect.Value) (textVisitKey, bool) {
    switch value.Kind() {
    case reflect.Map:
        if true == value.IsNil() {
            return textVisitKey{}, false
        }

        return textVisitKey{pointer: value.Pointer(), elementType: value.Type()}, true
    case reflect.Slice:
        if true == value.IsNil() {
            return textVisitKey{}, false
        }

        return textVisitKey{pointer: value.Pointer(), length: value.Len(), elementType: value.Type()}, true
    }

    return textVisitKey{}, false
}

/* textValueHoldsACycle walks the value as fmt prints it: maps, slices, arrays, structs and interfaces at every depth, and a pointer only at the top. The path holds the current branch only, so a map two keys share is not a cycle. */
func textValueHoldsACycle(value reflect.Value, depth int, path map[textVisitKey]struct{}) bool {
    if false == value.IsValid() {
        return false
    }

    if true == textRendersThroughAMethod(value) {
        return false
    }

    switch value.Kind() {
    case reflect.Interface:
        return textValueHoldsACycle(value.Elem(), depth+1, path)
    case reflect.Pointer:
        if 0 != depth || true == value.IsNil() {
            return false
        }

        switch value.Elem().Kind() {
        case reflect.Array, reflect.Slice, reflect.Struct, reflect.Map:
            return textValueHoldsACycle(value.Elem(), depth+1, path)
        }

        return false
    case reflect.Map:
        key, isContainer := textContainerKey(value)
        if false == isContainer {
            return false
        }
        if _, onPath := path[key]; true == onPath {
            return true
        }
        path[key] = struct{}{}
        defer delete(path, key)

        iterator := value.MapRange()
        for iterator.Next() {
            if true == textValueHoldsACycle(iterator.Key(), depth+1, path) || true == textValueHoldsACycle(iterator.Value(), depth+1, path) {
                return true
            }
        }

        return false
    case reflect.Slice:
        key, isContainer := textContainerKey(value)
        if false == isContainer {
            return false
        }
        if _, onPath := path[key]; true == onPath {
            return true
        }
        path[key] = struct{}{}
        defer delete(path, key)

        return textElementsHoldACycle(value, depth, path)
    case reflect.Array:
        return textElementsHoldACycle(value, depth, path)
    case reflect.Struct:
        for index := 0; index < value.NumField(); index++ {
            if true == textValueHoldsACycle(value.Field(index), depth+1, path) {
                return true
            }
        }

        return false
    }

    return false
}

func textElementsHoldACycle(value reflect.Value, depth int, path map[textVisitKey]struct{}) bool {
    if false == textKindCanHoldACycle(value.Type().Elem().Kind()) {
        return false
    }

    for index := 0; index < value.Len(); index++ {
        if true == textValueHoldsACycle(value.Index(index), depth+1, path) {
            return true
        }
    }

    return false
}

/* textKindCanHoldACycle skips the elements of a scalar slice, which hold no reference to walk. */
func textKindCanHoldACycle(kind reflect.Kind) bool {
    switch kind {
    case reflect.Interface, reflect.Map, reflect.Slice, reflect.Array, reflect.Struct:
        return true
    }

    return false
}

/* textSafeValue rebuilds the part of a value that holds a cycle, a map as a map and a slice or array as a slice, with the closing container replaced by the marker. A struct holding one is rendered as the marker whole. */
func textSafeValue(value reflect.Value, depth int, path map[textVisitKey]struct{}) any {
    if false == value.IsValid() {
        return nil
    }

    if false == textValueHoldsACycle(value, depth, path) {
        if true == value.CanInterface() {
            return value.Interface()
        }

        return fmt.Sprintf("%v", value)
    }

    switch value.Kind() {
    case reflect.Interface:
        return textSafeValue(value.Elem(), depth+1, path)
    case reflect.Pointer:
        rebuilt := textSafeValue(value.Elem(), depth+1, path)
        /* fmt prints the & of a top-level pointer only before a map, a slice, an array or a struct, and the marker is a string */
        if rebuiltKind := reflect.ValueOf(rebuilt).Kind(); reflect.Map != rebuiltKind && reflect.Slice != rebuiltKind {
            return rebuilt
        }

        pointer := reflect.New(reflect.TypeOf(rebuilt))
        pointer.Elem().Set(reflect.ValueOf(rebuilt))

        return pointer.Interface()
    case reflect.Map:
        key, _ := textContainerKey(value)
        if _, onPath := path[key]; true == onPath {
            return textValueCycleMarker
        }
        path[key] = struct{}{}
        defer delete(path, key)

        rebuilt := make(map[any]any, value.Len())
        iterator := value.MapRange()
        for iterator.Next() {
            rebuilt[iterator.Key().Interface()] = textSafeValue(iterator.Value(), depth+1, path)
        }

        return rebuilt
    case reflect.Slice, reflect.Array:
        if reflect.Slice == value.Kind() {
            key, _ := textContainerKey(value)
            if _, onPath := path[key]; true == onPath {
                return textValueCycleMarker
            }
            path[key] = struct{}{}
            defer delete(path, key)
        }

        rebuilt := make([]any, value.Len())
        for index := 0; index < value.Len(); index++ {
            rebuilt[index] = textSafeValue(value.Index(index), depth+1, path)
        }

        return rebuilt
    }

    return textValueCycleMarker
}
