package internal

import "reflect"

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
    if nil == source {
        return map[string]any{}
    }

    copied := make(map[string]any, len(source))
    for key, value := range source {
        copied[key] = copyAnyValue(value)
    }

    return copied
}

func CopyAnySlice(source []any) []any {
    if nil == source {
        return nil
    }

    copied := make([]any, len(source))
    for index, value := range source {
        copied[index] = copyAnyValue(value)
    }

    return copied
}

func copyAnyValue(value any) any {
    switch typedValue := value.(type) {
    case map[string]any:
        return CopyAnyMap(typedValue)
    case []any:
        return CopyAnySlice(typedValue)
    }

    reflectedValue := reflect.ValueOf(value)

    switch reflectedValue.Kind() {
    case reflect.Slice:
        if true == reflectedValue.IsNil() {
            return value
        }

        copiedSlice := reflect.MakeSlice(reflectedValue.Type(), reflectedValue.Len(), reflectedValue.Len())
        for index := 0; index < reflectedValue.Len(); index++ {
            copiedElement := copyAnyValue(reflectedValue.Index(index).Interface())
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
            copiedElement := copyAnyValue(iterator.Value().Interface())
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
