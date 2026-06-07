package opentelemetry

import (
    "testing"
)

func TestNormalizedMethod(t *testing.T) {
    standard := []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "CONNECT", "OPTIONS", "TRACE"}
    for _, method := range standard {
        if normalized := normalizedMethod(method); method != normalized {
            t.Fatalf("expected standard method %q to be preserved, got %q", method, normalized)
        }
    }

    for _, method := range []string{"BREW", "XYZZY", "M0001", "get", ""} {
        if normalized := normalizedMethod(method); "_OTHER" != normalized {
            t.Fatalf("expected non-standard method %q to normalize to _OTHER, got %q", method, normalized)
        }
    }
}
