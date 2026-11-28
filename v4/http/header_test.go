package http

import (
    "testing"
)

func TestSecurityHeaderNames_AnswersACopyOfTheEight(t *testing.T) {
    names := SecurityHeaderNames()
    if 8 != len(names) {
        t.Fatalf("expected the eight security headers, got %v", names)
    }

    names[0] = "changed"

    if HeaderContentTypeOptions != SecurityHeaderNames()[0] || HeaderPermissionsPolicy != SecurityHeaderNames()[7] {
        t.Fatalf("expected a copy of the caller's own, got %v", SecurityHeaderNames())
    }
}
