package validation

/* compareFloat64ToIntBound answers -1, 0 or 1 as the float stands to the integer bound, exactly at every magnitude. The plain spelling — comparing against float64(bound) — rounds a bound above 2^53 to a neighbour up to one ULP away before the comparison happens, so a float value adjacent to such a bound was misjudged against the declared number: a field holding 9007199254740996.0 was refused by greaterThan with bound 9007199254740995, which rounds to 9007199254740996.

   The arithmetic below is exact without big numbers: NaN never reaches it (both callers refuse NaN first); a float outside the int64 range is decided by sign, since every bound is an int; and inside the range the truncation int64(actual) is exact by definition, its fractional remainder is exact because a float with a fraction is smaller than 2^53 and a float of 2^53 or more has none, and the integer half then compares as integers. */
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
