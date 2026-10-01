package security

import (
    nethttp "net/http"
    "net/http/httptest"
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

func TestPathPrefixMatcher_TheAsteriskFormIsTheRoot(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "*", nil)
    if "*" != httpRequest.URL.Path {
        t.Fatalf("expected GET * to carry the path \"*\", got %q", httpRequest.URL.Path)
    }

    request := http.NewRequest(
        httpRequest,
        nil,
        nil,
        nil,
    )

    if false == NewPathPrefixMatcher("/").Matches(request) {
        t.Fatalf("expected the root prefix to claim GET *")
    }

    if true == NewPathPrefixMatcher("/admin").Matches(request) {
        t.Fatalf("expected GET * to stay outside /admin")
    }
}

func TestPathPrefixMatcher_APathWithoutItsLeadingSlashIsReadWithIt(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/users", nil)
    httpRequest.URL.Path = "users"

    request := http.NewRequest(
        httpRequest,
        nil,
        nil,
        nil,
    )

    if false == NewPathPrefixMatcher("/users").Matches(request) {
        t.Fatalf("expected /users to claim the path users")
    }

    if false == NewPathPrefixMatcher("/").Matches(request) {
        t.Fatalf("expected the root prefix to claim the path users")
    }

    if true == NewPathPrefixMatcher("/admin").Matches(request) {
        t.Fatalf("expected the path users to stay outside /admin")
    }
}
