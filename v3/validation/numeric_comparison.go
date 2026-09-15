package validation

func compareFloat64ToIntBound(actual float64, bound int) int {
    if actual < -9223372036854775808.0 {
        return -1
    }

    if actual >= 9223372036854775808.0 {
        return 1
    }

    truncated := int64(actual)
    boundValue := int64(bound)

    if truncated > boundValue {
        return 1
    }

    if truncated < boundValue {
        return -1
    }

    fraction := actual - float64(truncated)
    if 0 < fraction {
        return 1
    }
    if 0 > fraction {
        return -1
    }

    return 0
}
