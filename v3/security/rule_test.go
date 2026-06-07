package security

import (
    "testing"
)

func TestNewApiKeyHeaderRule_EmptyExpectedValuePanics(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic when the expected api key is empty (would fail open)")
        }
    }()

    _ = NewApiKeyHeaderRule(nil, "X-Api-Key", "")
}

func TestNewApiKeyHeaderRule_EmptyHeaderNamePanics(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic when the header name is empty")
        }
    }()

    _ = NewApiKeyHeaderRule(nil, "", "expected-secret")
}
