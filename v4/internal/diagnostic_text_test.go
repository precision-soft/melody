package internal

import (
    "strings"
    "testing"
)

func TestBoundDiagnosticText_AValueAtTheBoundIsKeptWhole(t *testing.T) {
    value := strings.Repeat("a", DiagnosticTextMaxLength)

    if value != BoundDiagnosticText(value) {
        t.Fatalf("expected a value at the bound kept whole")
    }
}

func TestBoundDiagnosticText_AValuePastTheBoundIsCutAndNamesTheDroppedBytes(t *testing.T) {
    bounded := BoundDiagnosticText(strings.Repeat("a", DiagnosticTextMaxLength+100))

    if strings.Repeat("a", DiagnosticTextMaxLength)+"...(truncated 100 bytes)" != bounded {
        t.Fatalf("unexpected bounded text: %q", bounded)
    }
}

func TestBoundDiagnosticText_TheCutKeepsARuneWhole(t *testing.T) {
    bounded := BoundDiagnosticText(strings.Repeat("a", DiagnosticTextMaxLength-1) + strings.Repeat("é", 10))

    if strings.Repeat("a", DiagnosticTextMaxLength-1)+"...(truncated 20 bytes)" != bounded {
        t.Fatalf("expected the cut before the split rune, got %q", bounded)
    }
}

func TestBoundDiagnosticTextWithin_CutsAtTheCallersLengthOnARuneBoundary(t *testing.T) {
    if "abc" != BoundDiagnosticTextWithin("abc", 3) {
        t.Fatalf("expected a value at the cut kept whole")
    }

    if "ab...(truncated 1 bytes)" != BoundDiagnosticTextWithin("abc", 2) {
        t.Fatalf("expected the value cut at 2, got %q", BoundDiagnosticTextWithin("abc", 2))
    }

    if "a...(truncated 2 bytes)" != BoundDiagnosticTextWithin("aé", 2) {
        t.Fatalf("expected the cut moved back to a rune start, got %q", BoundDiagnosticTextWithin("aé", 2))
    }

    for _, maxLength := range []int{0, -1} {
        if "...(truncated 3 bytes)" != BoundDiagnosticTextWithin("abc", maxLength) {
            t.Fatalf("expected a cut of %d to keep only the count, got %q", maxLength, BoundDiagnosticTextWithin("abc", maxLength))
        }
    }
}
