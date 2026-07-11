package http

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v3/internal/testhelper"
)

func TestPrefersHtml_ReturnsFalseWhenNoAccept(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "")
    if true == PrefersHtml(request) {
        t.Fatalf("expected false")
    }
}

func TestPrefersHtml_ReturnsTrueForHtmlWithoutJson(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "text/html,application/xhtml+xml")
    if false == PrefersHtml(request) {
        t.Fatalf("expected true")
    }
}

func TestPrefersHtml_ReturnsTrueWhenHtmlBeforeJson(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "text/html,application/json")
    if false == PrefersHtml(request) {
        t.Fatalf("expected true when text/html appears before application/json")
    }
}

func TestPrefersHtml_ReturnsFalseWhenJsonBeforeHtml(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "application/json,text/html")
    if true == PrefersHtml(request) {
        t.Fatalf("expected false when application/json appears before text/html")
    }
}

func TestPrefersHtml_ReturnsFalseWhenNilRequest(t *testing.T) {
    if true == PrefersHtml(nil) {
        t.Fatalf("expected false for nil request")
    }
}

func TestPrefersHtml_ReturnsFalseWhenOnlyJson(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "application/json")
    if true == PrefersHtml(request) {
        t.Fatalf("expected false when only application/json is present")
    }
}

func TestPrefersHtml_ReturnsTrueForTextHtmlOnly(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "text/html")
    if false == PrefersHtml(request) {
        t.Fatalf("expected true when only text/html is present")
    }
}

func TestPrefersHtml_ReturnsFalseForWildcard(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "*/*")
    if true == PrefersHtml(request) {
        t.Fatalf("expected false for wildcard accept header without text/html")
    }
}

func TestPrefersHtml_CaseInsensitive(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "Text/HTML")
    if false == PrefersHtml(request) {
        t.Fatalf("expected true for case-insensitive text/html match")
    }
}

/** @info A wildcard range must never override the exact type's weight (RFC 7231 5.3.2 gives the weight to the most specific matching range). A trailing type wildcard or catch-all range with a higher q must not mask an explicit "text/html;q=0" refusal or a low exact q, which would serve html against the client's stated preference. */
func TestPrefersHtml_ExactTypeQualityBeatsWildcard(t *testing.T) {
    cases := []struct {
        acceptHeader string
        expected     bool
    }{
        /* explicit q=0 refusal on the exact type must survive a higher wildcard q */
        {"text/html;q=0, text/*", false},
        {"text/html;q=0, */*", false},
        /* the exact-type weights (html 0.1 < json 0.5) decide, not the higher text/* q */
        {"text/html;q=0.1, text/*;q=0.9, application/json;q=0.5", false},
        /* a bare wildcard still supplies the weight when the exact type is absent */
        {"text/*", true},
    }

    for _, testCase := range cases {
        request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", testCase.acceptHeader)

        actual := PrefersHtml(request)
        if testCase.expected != actual {
            t.Fatalf("Accept %q: expected prefersHtml=%v, got %v", testCase.acceptHeader, testCase.expected, actual)
        }
    }
}

/** @info The q parameter is how a client ranks alternatives: "text/html;q=0.1, application/json" asks for json, and q=0 refuses a type outright. Reading the header by substring position alone served the representation the client down-weighted, or one it had explicitly rejected. */
func TestPrefersHtml_HonoursQualityValues(t *testing.T) {
    cases := []struct {
        acceptHeader string
        expected     bool
    }{
        {"text/html;q=0, application/json", false},
        {"text/html;q=0.1, application/json", false},
        {"text/html;q=0.9, application/json;q=0.8", true},
        {"application/json, text/html", false},
        {"text/html, application/json", true},
        {"text/html", true},
        {"application/json", false},
        {"text/html;q=0.5, application/json;q=0.5", true},
    }

    for _, testCase := range cases {
        request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", testCase.acceptHeader)

        actual := PrefersHtml(request)
        if testCase.expected != actual {
            t.Fatalf("Accept %q: expected prefersHtml=%v, got %v", testCase.acceptHeader, testCase.expected, actual)
        }
    }
}
