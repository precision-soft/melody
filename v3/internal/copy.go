package internal

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
        switch typedValue := value.(type) {
        case map[string]any:
            copied[key] = CopyAnyMap(typedValue)
        case []any:
            copied[key] = CopyAnySlice(typedValue)
        default:
            copied[key] = value
        }
    }

    return copied
}

func CopyAnySlice(source []any) []any {
    if nil == source {
        return nil
    }

    copied := make([]any, len(source))
    for i, value := range source {
        switch typedValue := value.(type) {
        case map[string]any:
            copied[i] = CopyAnyMap(typedValue)
        case []any:
            copied[i] = CopyAnySlice(typedValue)
        default:
            copied[i] = value
        }
    }

    return copied
}
