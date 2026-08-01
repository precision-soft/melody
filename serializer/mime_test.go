package serializer

import (
    "math"
    "testing"
)

func TestNormalizeMime_StripsParametersAndLowercases(t *testing.T) {
    if "application/json" != normalizeMime("Application/Json; charset=utf-8") {
        t.Fatalf("unexpected mime")
    }
}

func TestParseAcceptHeader_SortsByQualityDescending(t *testing.T) {
    values := parseAcceptHeader("text/plain;q=0.2, application/json;q=0.9")

    if 2 != len(values) {
        t.Fatalf("unexpected length")
    }

    if "application/json" != values[0].mime {
        t.Fatalf("expected json first")
    }
    if "text/plain" != values[1].mime {
        t.Fatalf("expected text second")
    }
}

/* @info the qvalue grammar of RFC 7231 is enforced as written — a zero with up to three decimal digits or a one with up to three zero decimals; every other spelling is invalid, where the previous leniency scored q=abc as full acceptance, clamped q=-1 into a refusal and let q=NaN through as a weight no comparison could select or refuse */
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
        parsedValue, valid := parseQualityValue(validCase.value)
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
        _, valid := parseQualityValue(invalidValue)
        if true == valid {
            t.Fatalf("q=%q: expected the value to be invalid", invalidValue)
        }
    }
}

func TestParseAcceptHeader_DropsAMemberWithAMalformedQuality(t *testing.T) {
    values := parseAcceptHeader("application/json;q=abc, text/plain;q=0.5")

    if 1 != len(values) {
        t.Fatalf("expected the malformed member to be dropped, got %d members", len(values))
    }

    if "text/plain" != values[0].mime {
        t.Fatalf("expected the valid member to survive, got %s", values[0].mime)
    }
}

/* @info a separator inside a quoted parameter value belongs to that value: without quote awareness the media range text/plain;version="1,2";q=0 split into two members and the refusal it carried travelled with the junk half */
func TestSplitOutsideQuotes_HonoursQuotedSections(t *testing.T) {
    parts := splitOutsideQuotes(`text/plain;version="1,2";q=0, application/json`, ',')
    if 2 != len(parts) {
        t.Fatalf("expected 2 members, got %d: %q", len(parts), parts)
    }

    parameters := splitOutsideQuotes(`version="a;b";q=1`, ';')
    if 2 != len(parameters) {
        t.Fatalf("expected 2 parameters, got %d: %q", len(parameters), parameters)
    }

    escaped := splitOutsideQuotes(`p="a\",b";q=1`, ',')
    if 1 != len(escaped) {
        t.Fatalf("expected the escaped quote to keep the member whole, got %d: %q", len(escaped), escaped)
    }
}

func TestWildcardSubtypeMatching(t *testing.T) {
    if false == isWildcardSubtype("application/*") {
        t.Fatalf("expected wildcard subtype")
    }

    if false == matchWildcardSubtype("application/*", "application/json") {
        t.Fatalf("expected match")
    }

    if true == matchWildcardSubtype("application/*", "text/plain") {
        t.Fatalf("expected no match")
    }
}
