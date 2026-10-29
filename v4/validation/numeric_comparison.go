package validation

/* compareFloat64ToIntBound answers -1, 0 or 1 as the float stands to the integer bound, exactly at every magnitude, where float64(bound) would round a bound above 2^53. NaN never reaches it; a float outside the int64 range is decided by sign, and inside it the truncation and its fractional remainder are exact. */
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
