package security

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v2/http"
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

func TestPathPrefixMatcher_RequestWithoutAnHttpRequestDoesNotMatch(t *testing.T) {
    matcher := NewPathPrefixMatcher("/admin")

    if true == matcher.Matches(&requestWithoutHttpRequest{}) {
        t.Fatalf("expected a request without an http request to match nothing")
    }
}

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

/* The request is an application-implementable contract, so a nil pointer of a request type reaches the
matcher as a non-nil interface and HttpRequest() below dereferences it. The untyped literal the sibling
probe passes is the only shape a bare comparison catches. */
func TestPathPrefixMatcher_ATypedNilRequestDoesNotMatch(t *testing.T) {
    matcher := NewPathPrefixMatcher("/admin")

    var unassignedRequest *http.Request

    if true == matcher.Matches(unassignedRequest) {
        t.Fatalf("expected matcher to not match a typed nil request")
    }
}
