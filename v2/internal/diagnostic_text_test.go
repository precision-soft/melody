package internal

import (
    "strings"
    "testing"
)

func TestBoundDiagnosticText_AValueAtTheBoundIsKeptWhole(t *testing.T) {
    value := strings.Repeat("a", maxDiagnosticTextLength)

    if value != BoundDiagnosticText(value) {
        t.Fatalf("expected a value at the bound kept whole")
    }
}

func TestBoundDiagnosticText_AValuePastTheBoundIsCutAndNamesTheDroppedBytes(t *testing.T) {
    bounded := BoundDiagnosticText(strings.Repeat("a", maxDiagnosticTextLength+100))

    if strings.Repeat("a", maxDiagnosticTextLength)+"...(truncated 100 bytes)" != bounded {
        t.Fatalf("unexpected bounded text: %q", bounded)
    }
}

func TestBoundDiagnosticText_TheCutKeepsARuneWhole(t *testing.T) {
    bounded := BoundDiagnosticText(strings.Repeat("a", maxDiagnosticTextLength-1) + strings.Repeat("é", 10))

    if strings.Repeat("a", maxDiagnosticTextLength-1)+"...(truncated 20 bytes)" != bounded {
        t.Fatalf("expected the cut before the split rune, got %q", bounded)
    }
}
