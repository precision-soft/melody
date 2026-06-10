package translation

import (
    "testing"
)

func TestEvaluatePlural_NonStandardNumericTypesSelectCategory(t *testing.T) {
    pattern := "{count, plural, one {# message} other {# messages}}"

    cases := []struct {
        name     string
        value    any
        expected string
    }{
        {name: "int8", value: int8(1), expected: "1 message"},
        {name: "int16", value: int16(1), expected: "1 message"},
        {name: "int32", value: int32(1), expected: "1 message"},
        {name: "uint", value: uint(1), expected: "1 message"},
        {name: "uint32", value: uint32(1), expected: "1 message"},
        {name: "float32", value: float32(1), expected: "1 message"},
        {name: "int32 plural", value: int32(5), expected: "5 messages"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            result := formatMessage(pattern, map[string]any{"count": testCase.value}, "en")
            if testCase.expected != result {
                t.Fatalf("count=%v (%T): expected %q, got %q (toFloat must accept the same numeric kinds the placeholder renderer does)", testCase.value, testCase.value, testCase.expected, result)
            }
        })
    }
}
