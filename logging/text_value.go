package logging

import (
    "fmt"
    "reflect"
)

/* textValueCycleMarker stands where a text rendering would have entered a container that is its own ancestor; it is the marker the json logger writes for the same shape, so one cycle reads the same in both journals */
const textValueCycleMarker = normalizeJsonContextCycleMarker

/* textVisitKey identifies a container on the current path of the walk: a map by its header, a slice by its backing pointer and length, each under its type, so two views of one array are two containers and a defined type over the same backing is its own */
type textVisitKey struct {
    pointer     uintptr
    length      int
    elementType reflect.Type
}

/* renderTextValue renders a value the way fmt's %v does, and renders a cycle as the marker instead of recursing into it: fmt has no cycle detection, so a map that holds itself — directly, through a slice, or through a struct field — recursed until the goroutine stack was gone, a fatal error no recover turns into a record. A value without a cycle is handed to fmt as it is, so every rendering this logger already produced stays the same. */
func renderTextValue(value any) string {
    if false == textValueHoldsACycle(reflect.ValueOf(value), 0, map[textVisitKey]struct{}{}) {
        return fmt.Sprintf("%v", value)
    }

    return fmt.Sprintf("%v", textSafeValue(reflect.ValueOf(value), 0, map[textVisitKey]struct{}{}))
}

/* textRendersThroughAMethod answers whether fmt's %v renders the value by calling a method of its own rather than by walking its shape — the Formatter, error and Stringer doors fmt consults at every depth for a value it can hand out — so the walk does not descend where fmt does not */
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

/* textValueHoldsACycle walks the value the way fmt prints it: maps, slices, arrays, structs and interfaces at every depth, and a pointer only at the top, where fmt prints &{…} — below it fmt prints the address and stops. The path holds the containers of the current branch only, so a map two keys share is not a cycle; it is one only when it is its own ancestor. */
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

/* textKindCanHoldACycle skips the elements of a scalar slice, which fmt prints one by one and which hold no reference to walk — a []byte of a megabyte is not a megabyte of calls */
func textKindCanHoldACycle(kind reflect.Kind) bool {
    switch kind {
    case reflect.Interface, reflect.Map, reflect.Slice, reflect.Array, reflect.Struct:
        return true
    }

    return false
}

/* textSafeValue rebuilds the part of a value that holds a cycle: a map as a map and a slice or an array as a slice, since %v prints them in the same shape whatever their element type, with the container that closes the cycle replaced by the marker. A struct that holds one cannot be rebuilt around a string, so it is rendered as the marker whole. Everything that holds no cycle is handed back as it is. */
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
        /* fmt prints the & of a top-level pointer only in front of a map, a slice, an array or a struct; the marker a cyclic struct became is a string, which behind a pointer would print as an address */
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
