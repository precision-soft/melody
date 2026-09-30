package security

import (
    nethttp "net/http"
    "net/http/httptest"
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

/* the matcher reads the spelling the router reads: "/admin%2Fusers" is one segment the router never routes under "/admin", so a firewall written for "/admin/" does not claim it, while "/admin/caf%C3%A9" reads "/admin/café" and is claimed */
func TestPathPrefixMatcher_ReadsThePathTheRouterRoutes(t *testing.T) {
    matcher := NewPathPrefixMatcher("/admin/")

    for path, claimed := range map[string]bool{
        "/admin%2Fusers":    false,
        "/admin%2Fusers{":   false,
        "/admin/caf%C3%A9":  true,
        "/admin/users":      true,
    } {
        httpRequest, _ := nethttp.NewRequest("GET", "http://localhost"+path, nil)
        request := http.NewRequest(httpRequest, nil, nil, nil)

        if claimed != matcher.Matches(request) {
            t.Fatalf("expected the prefix to claim %q: %v", path, claimed)
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

/* an absolute-form target ("GET http://host") and an authority-form one ("CONNECT host:port") reach the handler with an empty path, which names the root: a firewall on "/" that did not claim it was skipped for the request, its access control with it */
func TestPathPrefixMatcher_AnEmptyPathIsTheRoot(t *testing.T) {
    for _, target := range []struct {
        method string
        target string
    }{
        {method: "GET", target: "http://localhost"},
        {method: "POST", target: "http://localhost"},
        {method: "CONNECT", target: "localhost:443"},
    } {
        httpRequest := httptest.NewRequest(target.method, target.target, nil)
        if "" != httpRequest.URL.Path {
            t.Fatalf("expected %s %s to carry an empty path, got %q", target.method, target.target, httpRequest.URL.Path)
        }

        request := http.NewRequest(
            httpRequest,
            nil,
            nil,
            nil,
        )

        if false == NewPathPrefixMatcher("/").Matches(request) {
            t.Fatalf("expected the root prefix to claim %s %s", target.method, target.target)
        }

        if true == NewPathPrefixMatcher("/admin").Matches(request) {
            t.Fatalf("expected %s %s to stay outside /admin", target.method, target.target)
        }
    }
}
