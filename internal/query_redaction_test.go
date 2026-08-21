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

/* a query that does not parse cannot have its secret half told apart from its diagnosable half, so it
is redacted whole rather than passed through */
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
