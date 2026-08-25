package security

import (
    "testing"

    "github.com/precision-soft/melody/v3/internal/testhelper"
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

func TestNewApiKeyHeaderRule_NilMatcherPanics(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewApiKeyHeaderRule(nil, "X-Api-Key", "secret")
    }, "api key header rule matcher is nil")
}

func TestNewApiKeyHeaderRule_TypedNilMatcherPanics(t *testing.T) {
    var typedNilMatcher *PathPrefixMatcher

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewApiKeyHeaderRule(typedNilMatcher, "X-Api-Key", "secret")
    }, "api key header rule matcher is nil")
}
