package security

import (
    "testing"
)

func TestApiKeyRequestMatcherClaimsAKeyedRequestOnItsPrefix(t *testing.T) {
    matcher := NewApiKeyRequestMatcher("/products/api", ApiKeyHeaderName)

    request := requestAtPathWithHeader(t, "/products/api/read/", ApiKeyHeaderName, "some-key")

    if false == matcher.Matches(request) {
        t.Fatalf("a keyed request on the prefix was not claimed")
    }
}

/* The header condition is the guard that keeps the browser working: a cookie-carrying request without the header must fall through to the session firewall, never be claimed and answered anonymous here. */
func TestApiKeyRequestMatcherLeavesAKeylessRequestAlone(t *testing.T) {
    matcher := NewApiKeyRequestMatcher("/products/api", ApiKeyHeaderName)

    request := requestAtPathWithHeader(t, "/products/api/read/", ApiKeyHeaderName, "")

    if true == matcher.Matches(request) {
        t.Fatalf("a request without the header was claimed")
    }
}

func TestApiKeyRequestMatcherLeavesOtherPathsAlone(t *testing.T) {
    matcher := NewApiKeyRequestMatcher("/products/api", ApiKeyHeaderName)

    request := requestAtPathWithHeader(t, "/products/", ApiKeyHeaderName, "some-key")

    if true == matcher.Matches(request) {
        t.Fatalf("a keyed request off the prefix was claimed")
    }
}

func TestApiKeyRequestMatcherRefusesANilRequest(t *testing.T) {
    matcher := NewApiKeyRequestMatcher("/products/api", ApiKeyHeaderName)

    if true == matcher.Matches(nil) {
        t.Fatalf("a nil request was claimed")
    }
}
