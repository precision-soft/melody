package internal

import (
    "strings"
    "testing"
)

func TestRedactQueryValuesForDiagnostics_KeepsNamesAndRedactsValues(t *testing.T) {
    redacted := RedactQueryValuesForDiagnostics("apiKey=live-credential&page=2")

    if true == strings.Contains(redacted, "live-credential") {
        t.Fatalf("the credential survived redaction: %q", redacted)
    }

    for _, parameterName := range []string{"apiKey", "page"} {
        if false == strings.Contains(redacted, parameterName) {
            t.Fatalf("expected the parameter name %q to survive, got %q", parameterName, redacted)
        }
    }
}

/* a query that does not parse cannot have its secret half told apart from its diagnosable half, so it is redacted whole rather than passed through */
func TestRedactQueryValuesForDiagnostics_RedactsAnUnparseableQueryWhole(t *testing.T) {
    redacted := RedactQueryValuesForDiagnostics("%zz=secret-value")

    if true == strings.Contains(redacted, "secret-value") {
        t.Fatalf("an unparseable query leaked its values: %q", redacted)
    }

    if RedactedQueryValue != redacted {
        t.Fatalf("expected an unparseable query to be redacted whole, got %q", redacted)
    }
}

func TestRedactQueryValuesForDiagnostics_LeavesAnEmptyQueryEmpty(t *testing.T) {
    if "" != RedactQueryValuesForDiagnostics("") {
        t.Fatalf("expected an absent query to stay absent rather than become a redaction marker")
    }
}

/* the internal-auth refusal renders the signed and the request query side by side and fires only when they differ byte for byte, so a redaction that re-encodes through url.Values — sorted names, a repeated name collapsed — rendered the reordered, the duplicated and the differently repeated query as two identical strings */
func TestRedactQueryValuesForDiagnostics_KeepsOrderAndMultiplicity(t *testing.T) {
    cases := []struct {
        signed  string
        request string
    }{
        {"b=2&a=1", "a=1&b=2"},
        {"a=1", "a=1&a=2"},
        {"a=1&a=2&a=3", "a=9"},
    }

    for _, testCase := range cases {
        signed := RedactQueryValuesForDiagnostics(testCase.signed)
        request := RedactQueryValuesForDiagnostics(testCase.request)

        if signed == request {
            t.Fatalf("expected %q and %q to render differently, both rendered %q", testCase.signed, testCase.request, signed)
        }

        for _, rendered := range []string{signed, request} {
            if true == strings.Contains(rendered, "1") || true == strings.Contains(rendered, "2") || true == strings.Contains(rendered, "9") {
                t.Fatalf("a value survived the redaction: %q", rendered)
            }
        }
    }

    if "b=xxxxx&a=xxxxx" != RedactQueryValuesForDiagnostics("b=2&a=1") {
        t.Fatalf("expected the names in their written order, got %q", RedactQueryValuesForDiagnostics("b=2&a=1"))
    }

    if "a=xxxxx&a=xxxxx" != RedactQueryValuesForDiagnostics("a=1&a=2") {
        t.Fatalf("expected every occurrence of a repeated name kept, got %q", RedactQueryValuesForDiagnostics("a=1&a=2"))
    }

    /* a pair with no "=" is the shape of a capability token — "?9f8a7b3c" — so it is redacted whole rather than kept as a name that names no value */
    if "xxxxx&a=xxxxx" != RedactQueryValuesForDiagnostics("9f8a7b3c&a=1") {
        t.Fatalf("expected a pair with no equals redacted whole, got %q", RedactQueryValuesForDiagnostics("9f8a7b3c&a=1"))
    }

    if "a=xxxxx" != RedactQueryValuesForDiagnostics("a=1;b=2") {
        t.Fatalf("expected the semicolon-joined tail redacted as the value it is, got %q", RedactQueryValuesForDiagnostics("a=1;b=2"))
    }
}
