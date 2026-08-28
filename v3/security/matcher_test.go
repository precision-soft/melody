package security

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v3/http"
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

/* the router reads "/admin/" and "/admin" as the same route, so a prefix written with the trailing slash must claim the bare spelling too — without it, the unwritten spelling escaped the firewall that named the other. The negative half pins the surgical scope: only the exact bare spelling is added, never a wider segment. */
func TestPathPrefixMatcher_ATrailingSlashPrefixClaimsTheBareSpelling(t *testing.T) {
    matcher := NewPathPrefixMatcher("/admin/")

    matching := []string{"/admin", "/admin/", "/admin/products"}
    for _, path := range matching {
        httpRequest, _ := nethttp.NewRequest("GET", "http://localhost"+path, nil)
        request := http.NewRequest(httpRequest, nil, nil, nil)

        if false == matcher.Matches(request) {
            t.Fatalf("expected the trailing-slash prefix to match %q", path)
        }
    }

    notMatching := []string{"/administrator", "/admi", "/"}
    for _, path := range notMatching {
        httpRequest, _ := nethttp.NewRequest("GET", "http://localhost"+path, nil)
        request := http.NewRequest(httpRequest, nil, nil, nil)

        if true == matcher.Matches(request) {
            t.Fatalf("expected the trailing-slash prefix not to match %q", path)
        }
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
