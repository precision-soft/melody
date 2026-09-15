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
        t.Fatalf("expected true for a case-insensitive html type on its own")
    }
}

func TestPrefersHtml_CaseInsensitiveAheadOfAnotherType(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "Text/HTML,Application/JSON")
    if false == PrefersHtml(request) {
        t.Fatalf("expected true for case-insensitive html before json")
    }
}

func TestPrefersHtml_ExactTypeQualityBeatsWildcard(t *testing.T) {
    cases := []struct {
        acceptHeader string
        expected     bool
    }{
        {"text/html;q=0, text/*", false},
        {"text/html;q=0, */*", false},
        {"text/html;q=0.1, text/*;q=0.9, application/json;q=0.5", false},
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

func TestPrefersHtml_ReturnsFalseForNeither(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "application/xml,text/plain")
    if true == PrefersHtml(request) {
        t.Fatalf("expected false when neither text/html nor application/json is present")
    }
}

func TestPrefersHtml_DropsAMemberWhoseQualityFallsOutsideTheGrammar(t *testing.T) {
    cases := []struct {
        acceptHeader string
        expected     bool
    }{
        {"text/html;q=Inf, application/json", false},
        {"text/html;q=NaN, application/json", false},
        {"text/html;q=5, application/json", false},
        {"text/html;q=-1, application/json", false},
        {"text/html;q=abc", false},
        {"text/html;q=0.9, application/json;q=Inf", true},
    }

    for _, testCase := range cases {
        request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", testCase.acceptHeader)

        actual := PrefersHtml(request)
        if testCase.expected != actual {
            t.Fatalf("Accept %q: expected prefersHtml=%v, got %v", testCase.acceptHeader, testCase.expected, actual)
        }
    }
}

func TestPrefersHtml_JoinsEveryLineOfARepeatedAcceptField(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "application/json;q=0.1")
    request.HttpRequest().Header.Add("Accept", "text/html")

    if false == PrefersHtml(request) {
        t.Fatalf("expected the html preference on the second header line to be read")
    }
}

func TestPrefersHtml_QuotedCommaKeepsTheHtmlRefusal(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(
        nethttp.MethodGet,
        "http://example.com/",
        `text/html;p="a,b";q=0, application/json`,
    )

    if true == PrefersHtml(request) {
        t.Fatalf("expected the quoted-comma header to keep the explicit html refusal")
    }
}

func TestAcceptQuality_QuotedSemicolonStaysOneParameter(t *testing.T) {
    quality, _ := acceptQuality(`text/html;p="x;q=0.9";q=0.5`, "text/html")
    if 0.5 != quality {
        t.Fatalf("expected the quoted semicolon to leave the q parameter readable, got %v", quality)
    }
}

func TestPrefersHtml_ATypedNilRequestIsNotHtml(t *testing.T) {
    var unassignedRequest *testhelper.HttpTestRequest

    if true == PrefersHtml(unassignedRequest) {
        t.Fatalf("expected a typed nil request to not prefer html")
    }
}

func TestPrefersHtml_ReadsAJsonPreferenceWrittenWithExcessTrailingZeros(t *testing.T) {
    htmlPreferred := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "text/html;q=0.9, application/json;q=1.0000")
    if true == PrefersHtml(htmlPreferred) {
        t.Fatal("expected the json member to keep its weight and win the negotiation")
    }

    stillRefused := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "text/html;q=0.9, application/json;q=1.0001")
    if false == PrefersHtml(stillRefused) {
        t.Fatal("expected a fourth digit outside the grammar to drop its member")
    }

    refusedWithZeros := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "text/html;q=0.9, application/json;q=0.0000")
    if false == PrefersHtml(refusedWithZeros) {
        t.Fatal("expected q=0.0000 to keep refusing json")
    }
}
