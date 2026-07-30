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

    _ = NewApiKeyHeaderRule(NewPathPrefixMatcher("/"), "X-Api-Key", "")
}

func TestNewApiKeyHeaderRule_EmptyHeaderNamePanics(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic when the header name is empty")
        }
    }()

    _ = NewApiKeyHeaderRule(NewPathPrefixMatcher("/"), "", "expected-secret")
}

/* @info a nil matcher is refused at construction the way the other firewall dependencies are: leaving it unvalidated defers the failure to Applies on the request path, outside any recovery, turning a configuration mistake into a panic mid-request instead of a boot error */
func TestNewApiKeyHeaderRule_NilMatcherPanics(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic when the matcher is nil")
        }
    }()

    _ = NewApiKeyHeaderRule(nil, "X-Api-Key", "expected-secret")
}
