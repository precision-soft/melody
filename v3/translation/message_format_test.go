package translation

import (
    "encoding/json"
    "testing"
)

func TestInterpolate_PoundStaysBoundThroughNestedSelect(t *testing.T) {
    pattern := "{count, plural, other {{gender, select, female {# guests (she)} other {# guests}}}}"

    result := formatMessage(pattern, map[string]any{"count": 3, "gender": "female"}, "en")
    if "3 guests (she)" != result {
        t.Fatalf("expected # to stay bound to the enclosing plural inside a nested select, got %q", result)
    }

    other := formatMessage(pattern, map[string]any{"count": 5, "gender": "robot"}, "en")
    if "5 guests" != other {
        t.Fatalf("expected the nested select other branch to substitute #, got %q", other)
    }
}

/** @info numeric */

func TestEvaluatePlural_PoundFloat32MatchesPlaceholder(t *testing.T) {
    pattern := "{value, plural, other {# km}}"

    result := formatMessage(pattern, map[string]any{"value": float32(0.1)}, "en")
    if "0.1 km" != result {
        t.Fatalf("expected the `#` substitution to render float32 with shortest representation like {value}, got %q", result)
    }
}

func TestEvaluatePlural_PoundPreservesLargeIntegerPrecision(t *testing.T) {
    pattern := "{count, plural, other {# items}}"

    cases := []struct {
        name     string
        value    any
        expected string
    }{
        {name: "int64 above 2^53", value: int64(9007199254740993), expected: "9007199254740993 items"},
        {name: "uint64 above 2^53", value: uint64(9007199254740993), expected: "9007199254740993 items"},
        {name: "json.Number above 2^53", value: json.Number("9007199254740993"), expected: "9007199254740993 items"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            result := formatMessage(pattern, map[string]any{"count": testCase.value}, "en")
            if testCase.expected != result {
                t.Fatalf("count=%v (%T): expected %q, got %q (the `#` substitution must render the exact integer, not the float64-rounded value)", testCase.value, testCase.value, testCase.expected, result)
            }
        })
    }
}

func TestEvaluatePlural_JsonNumberSelectsCategory(t *testing.T) {
    pattern := "{count, plural, one {# message} other {# messages}}"

    cases := []struct {
        name     string
        value    any
        expected string
    }{
        {name: "json.Number one", value: json.Number("1"), expected: "1 message"},
        {name: "json.Number other", value: json.Number("5"), expected: "5 messages"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            result := formatMessage(pattern, map[string]any{"count": testCase.value}, "en")
            if testCase.expected != result {
                t.Fatalf("count=%v (%T): expected %q, got %q (toFloat must accept json.Number, the type json.Decoder produces with UseNumber)", testCase.value, testCase.value, testCase.expected, result)
            }
        })
    }
}

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
