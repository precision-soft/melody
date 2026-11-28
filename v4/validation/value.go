package validation

import (
    "reflect"
)

func dereferenceValue(value any) (any, bool) {
    if nil == value {
        return nil, false
    }

    reflectedValue := reflect.ValueOf(value)

    for {
        kind := reflectedValue.Kind()

        if (reflect.Pointer == kind) || (reflect.Interface == kind) {
            if true == reflectedValue.IsNil() {
                return nil, false
            }

            reflectedValue = reflectedValue.Elem()

            continue
        }

        break
    }

    if reflect.Invalid == reflectedValue.Kind() {
        return nil, false
    }

    if reflect.String == reflectedValue.Kind() {
        return reflectedValue.String(), true
    }

    return reflectedValue.Interface(), true
}
