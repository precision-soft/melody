package internal

import (
    "math"
    "testing"
)

func TestParseQualityValue_EnforcesTheRfcGrammar(t *testing.T) {
    for _, validCase := range []struct {
        value    string
        expected float64
    }{
        {"0", 0},
        {"0.", 0},
        {"0.8", 0.8},
        {"0.825", 0.825},
        {"1", 1},
        {"1.", 1},
        {"1.0", 1},
        {"1.000", 1},
    } {
        parsedValue, valid := ParseQualityValue(validCase.value)
        if false == valid {
            t.Fatalf("q=%q: expected the value to be valid", validCase.value)
        }

        if 0.000001 < math.Abs(parsedValue-validCase.expected) {
            t.Fatalf("q=%q: expected %v, got %v", validCase.value, validCase.expected, parsedValue)
        }
    }

    for _, invalidValue := range []string{
        "", "abc", "NaN", "nan", "-1", "-0.5", "1e-1", "+.5", ".5", "0.1234", "1.5", "1.001", "2", "0x1p-1", "Inf", `"0.5"`,
    } {
        _, valid := ParseQualityValue(invalidValue)
        if true == valid {
            t.Fatalf("q=%q: expected the value to be invalid", invalidValue)
        }
    }
}

func TestParseQualityValue_RefusesEachShapeOutsideTheGrammar(t *testing.T) {
    for _, refused := range []string{"", "2", "-0.5", "1.5", "1.001", "0.1234", "1.0000", "0.5x", "0..5", "05", "1,0"} {
        parsed, valid := ParseQualityValue(refused)
        if true == valid {
            t.Fatalf("expected %q to be refused by the qvalue grammar, got %v", refused, parsed)
        }
    }

    for accepted, expected := range map[string]float64{
        "0":     0,
        "1":     1,
        "0.5":   0.5,
        "0.001": 0.001,
        "1.0":   1,
        "1.000": 1,
    } {
        parsed, valid := ParseQualityValue(accepted)
        if false == valid {
            t.Fatalf("expected %q to be accepted by the qvalue grammar", accepted)
        }

        if 0.0001 < parsed-expected || 0.0001 < expected-parsed {
            t.Fatalf("expected %q to parse to %v, got %v", accepted, expected, parsed)
        }
    }
}
