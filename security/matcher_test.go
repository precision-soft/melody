package security

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/http"
)

func TestPathPrefixMatcher_Matches(t *testing.T) {
    httpRequest, _ := nethttp.NewRequest("GET", "http://localhost/admin/products", nil)

    request := http.NewRequest(
        httpRequest,
        nil,
        nil,
        nil,
    )

    matcher := NewPathPrefixMatcher("/admin")

    if false == matcher.Matches(request) {
        t.Fatalf("expected matcher to match")
    }
}

func TestPathPrefixMatcher_DoesNotMatch(t *testing.T) {
    httpRequest, _ := nethttp.NewRequest("GET", "http://localhost/api/products", nil)

    request := http.NewRequest(
        httpRequest,
        nil,
        nil,
        nil,
    )

    matcher := NewPathPrefixMatcher("/admin")

    if true == matcher.Matches(request) {
        t.Fatalf("expected matcher to not match")
    }
}

func TestPathPrefixMatcher_NilRequestDoesNotMatch(t *testing.T) {
    matcher := NewPathPrefixMatcher("/admin")

    if true == matcher.Matches(nil) {
        t.Fatalf("expected matcher to not match a nil request")
    }
}

/* @info a request whose net/http request is missing matches nothing: the matcher reads the URL off it, and the guard is what keeps a firewall selection from panicking on a half-built request */
func TestPathPrefixMatcher_RequestWithoutAnHttpRequestDoesNotMatch(t *testing.T) {
    matcher := NewPathPrefixMatcher("/admin")

    if true == matcher.Matches(&requestWithoutHttpRequest{}) {
        t.Fatalf("expected a request without an http request to match nothing")
    }
}

/* @info an empty prefix matches every request that HAS a url: it is the catch-all a global firewall is written with, and it stays behind the three nil guards rather than in front of them */
func TestPathPrefixMatcher_EmptyPrefixMatchesEveryRequest(t *testing.T) {
    matcher := NewPathPrefixMatcher("")

    if false == matcher.Matches(newFirewallTestRequest("/anything")) {
        t.Fatalf("expected the empty prefix to match")
    }

    if true == matcher.Matches(nil) {
        t.Fatalf("expected the empty prefix to still refuse a nil request")
    }

    if true == matcher.Matches(&requestWithoutHttpRequest{}) {
        t.Fatalf("expected the empty prefix to still refuse a request without an http request")
    }
}
