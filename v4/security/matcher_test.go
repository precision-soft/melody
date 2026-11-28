package security

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    "github.com/precision-soft/melody/v4/http"
)

func TestPathPrefixMatcher_Matches(t *testing.T) {
    httpRequest, _ := nethttp.NewRequest("GET", "http://localhost/admin/products", nil)

    request := http.NewRequest(
        httpRequest,
        nil,
        http.RequestConfig{},
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
        http.RequestConfig{},
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

func TestPathPrefixMatcher_ATrailingSlashPrefixClaimsTheBareSpelling(t *testing.T) {
    matcher := NewPathPrefixMatcher("/admin/")

    matching := []string{"/admin", "/admin/", "/admin/products"}
    for _, path := range matching {
        httpRequest, _ := nethttp.NewRequest("GET", "http://localhost"+path, nil)
        request := http.NewRequest(httpRequest, nil, http.RequestConfig{})

        if false == matcher.Matches(request) {
            t.Fatalf("expected the trailing-slash prefix to match %q", path)
        }
    }

    notMatching := []string{"/administrator", "/admi", "/"}
    for _, path := range notMatching {
        httpRequest, _ := nethttp.NewRequest("GET", "http://localhost"+path, nil)
        request := http.NewRequest(httpRequest, nil, http.RequestConfig{})

        if true == matcher.Matches(request) {
            t.Fatalf("expected the trailing-slash prefix not to match %q", path)
        }
    }
}

func TestPathPrefixMatcher_ReadsThePathTheRouterRoutes(t *testing.T) {
    matcher := NewPathPrefixMatcher("/admin/")

    for path, claimed := range map[string]bool{
        "/admin%2Fusers":   false,
        "/admin%2Fusers{":  false,
        "/admin/caf%C3%A9": true,
        "/admin/users":     true,
    } {
        httpRequest, _ := nethttp.NewRequest("GET", "http://localhost"+path, nil)
        request := http.NewRequest(httpRequest, nil, http.RequestConfig{})

        if claimed != matcher.Matches(request) {
            t.Fatalf("expected the prefix to claim %q: %v", path, claimed)
        }
    }
}

func TestPathPrefixMatcher_ATypedNilRequestDoesNotMatch(t *testing.T) {
    matcher := NewPathPrefixMatcher("/admin")

    var unassignedRequest *http.Request

    if true == matcher.Matches(unassignedRequest) {
        t.Fatalf("expected matcher to not match a typed nil request")
    }
}

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
            http.RequestConfig{},
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
        http.RequestConfig{},
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
        http.RequestConfig{},
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

func TestNewPathPrefixMatcher_RefusesAPrefixNoRequestPathCanCarry(t *testing.T) {
    for _, prefix := range []string{"api", " /api", "/api//users", "/api/../users", "/api/./users", "/api/./", "/billing//", "/billing///", "//", "///"} {
        expectPanicNamingField(t, "NewPathPrefixMatcher.prefix", func() { NewPathPrefixMatcher(prefix) })
    }

    for _, prefix := range []string{"/reports", "/billing/", "/", "", "/api/.", "/files/.."} {
        if prefix != NewPathPrefixMatcher(prefix).Prefix() {
            t.Fatalf("expected %q admitted as declared", prefix)
        }
    }
}

func segmentPrefixMatches(matcher *SegmentPrefixMatcher, path string) bool {
    httpRequest := httptest.NewRequest(nethttp.MethodGet, "http://localhost"+path, nil)

    return matcher.Matches(http.NewRequest(httpRequest, nil, http.RequestConfig{}))
}

func TestSegmentPrefixMatcher_ClaimsItsTreeAtASegmentBoundary(t *testing.T) {
    for _, declared := range []string{"/api", "/api/", " /api "} {
        matcher := NewSegmentPrefixMatcher(declared)
        if "/api" != matcher.Prefix() || `segment prefix "/api"` != matcher.String() {
            t.Fatalf("expected %q folded to /api, got %q / %q", declared, matcher.Prefix(), matcher.String())
        }

        for _, path := range []string{"/api", "/api/", "/api/users", "/api/users/1"} {
            if false == segmentPrefixMatches(matcher, path) {
                t.Fatalf("expected %q to claim %q", declared, path)
            }
        }

        for _, path := range []string{"/api-admin", "/apiary", "/ap", "/", "/other/api", "/api%2Fusers"} {
            if true == segmentPrefixMatches(matcher, path) {
                t.Fatalf("expected %q not to claim %q", declared, path)
            }
        }
    }

    root := NewSegmentPrefixMatcher("/")
    for _, path := range []string{"/", "/api", "/anything/deeper"} {
        if false == segmentPrefixMatches(root, path) {
            t.Fatalf("expected / to claim %q", path)
        }
    }

    if true == NewSegmentPrefixMatcher("/api").Matches(nil) {
        t.Fatalf("expected a nil request not matched")
    }
}

func TestNewSegmentPrefixMatcher_RefusesAPrefixItCannotBound(t *testing.T) {
    for _, prefix := range []string{"", "   ", "//", "///", "api", "/api//users", "/api/../users", "/api/./users", "/api/.", "/files/..", "/api//"} {
        expectPanicNamingField(t, "NewSegmentPrefixMatcher.prefix", func() { NewSegmentPrefixMatcher(prefix) })
    }
}
