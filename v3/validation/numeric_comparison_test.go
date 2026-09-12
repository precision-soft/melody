package validation

import (
    "math"
    "testing"
)

func TestCompareFloat64ToIntBound_IsExactAtEveryMagnitude(t *testing.T) {
    cases := []struct {
        name     string
        actual   float64
        bound    int
        expected int
    }{
        {name: "plain below", actual: 4.5, bound: 5, expected: -1},
        {name: "plain equal", actual: 5, bound: 5, expected: 0},
        {name: "plain above", actual: 5.5, bound: 5, expected: 1},
        {name: "negative fraction below", actual: -2.5, bound: -2, expected: -1},
        {name: "negative fraction above", actual: -2.5, bound: -3, expected: 1},
        /* the ULP case the plain spelling misjudged: 9007199254740995 is not representable and float64(bound) rounds it to 9007199254740996, so the value equal to the ROUNDED bound read as equal to the declared one */
        {name: "value adjacent to an unrepresentable bound is above it", actual: 9007199254740996.0, bound: 9007199254740995, expected: 1},
        {name: "value below an unrepresentable bound", actual: 9007199254740992.0, bound: 9007199254740993, expected: -1},
        {name: "positive infinity beats every bound", actual: math.Inf(1), bound: math.MaxInt64, expected: 1},
        {name: "negative infinity loses to every bound", actual: math.Inf(-1), bound: math.MinInt64, expected: -1},
        {name: "the exact minimum of int64", actual: -9223372036854775808.0, bound: math.MinInt64, expected: 0},
        {name: "two to the sixty-third exceeds every int bound", actual: 9223372036854775808.0, bound: math.MaxInt64, expected: 1},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            actual := compareFloat64ToIntBound(testCase.actual, testCase.bound)
            if testCase.expected != actual {
                t.Fatalf("expected %d comparing %v to %d, got %d", testCase.expected, testCase.actual, testCase.bound, actual)
            }
        })
    }
}

/* the end-to-end halves of the ULP case: each constraint judges the adjacent value against the DECLARED bound, where the float64 conversion judged it against the rounded neighbour */
func TestGreaterThan_JudgesAFloatAgainstTheDeclaredBoundNotTheRoundedOne(t *testing.T) {
    constraint := NewGreaterThan(9007199254740995)

    validationError := constraint.Validate(9007199254740996.0, "field")
    if nil != validationError {
        t.Fatalf("expected 9007199254740996.0 to pass greaterThan 9007199254740995, got %s", validationError.Error())
    }
}

func TestLessThan_JudgesAFloatAgainstTheDeclaredBoundNotTheRoundedOne(t *testing.T) {
    constraint := NewLessThan(9007199254740993)

    validationError := constraint.Validate(9007199254740992.0, "field")
    if nil != validationError {
        t.Fatalf("expected 9007199254740992.0 to pass lessThan 9007199254740993, got %s", validationError.Error())
    }
}
