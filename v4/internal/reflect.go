package internal

import "reflect"

func CanReflectValueBeNil(value reflect.Value) bool {
    switch value.Kind() {
    case reflect.Chan:
        return true
    case reflect.Func:
        return true
    case reflect.Interface:
        return true
    case reflect.Map:
        return true
    case reflect.Pointer:
        return true
    case reflect.Slice:
        return true
    default:
        return false
    }
}
