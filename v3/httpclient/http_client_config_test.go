package httpclient

import (
    "strings"
    "testing"
)

func TestHttpClientConfigHeaders_ReturnsDefensiveCopy(t *testing.T) {
    config := NewHttpClientConfig(
        "",
        0,
        map[string]string{
            "X-Test": "original",
        },
    )

    first := config.Headers()
    first["X-Test"] = "mutated"
    first["X-New"] = "added"

    second := config.Headers()
    if "original" != second["X-Test"] {
        t.Fatalf("expected defensive copy, got %q", second["X-Test"])
    }
    if _, exists := second["X-New"]; true == exists {
        t.Fatalf("expected no new key leaked into config")
    }
}

/* the constructor is one of the four doors that write into a header map applied with Set; storing the raw spelling there left the canonicalizing setters guarding a map that was already ambiguous. */
func TestNewHttpClientConfig_HeadersAreStoredCanonicalized(t *testing.T) {
    config := NewHttpClientConfig("", 0, map[string]string{"x-api-key": "secret"})

    if "secret" != config.Headers()["X-Api-Key"] {
        t.Fatalf("expected the configured header stored under its canonical spelling, got %#v", config.Headers())
    }
}

/* RFC 3986 resolution merges a relative target over the LAST SEGMENT of the base path: a base spelled without its trailing slash loses that segment on every request, as a 404 in production. The refusal moves the mistake to the wiring site. */
func TestNewHttpClientConfig_ABaseUrlPathWithoutATrailingSlashIsRefused(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected a base url path without a trailing slash to be refused")
        }

        err, ok := recovered.(error)
        if false == ok {
            t.Fatalf("expected the refusal to travel as an error, got %T", recovered)
        }
        if false == strings.Contains(err.Error(), "must end with a slash") {
            t.Fatalf("expected the refusal to name the missing slash, got %q", err.Error())
        }
    }()

    NewHttpClientConfig("https://api.example.com/v1", 0, nil)
}

/* a base with an empty path has no segment for the merge to cut, and an empty base url means no base at all; neither is a mistake. A base that does not parse cannot be judged at this door — buildUrl reports it on the first request. */
func TestNewHttpClientConfig_ABaseWithAnEmptyPathOrNoBaseAtAllIsLegal(t *testing.T) {
    for _, legalBaseUrl := range []string{"", "https://api.example.com", "https://api.example.com/", "https://api.example.com/v1/", ":"} {
        config := NewHttpClientConfig(legalBaseUrl, 0, nil)

        if legalBaseUrl != config.BaseUrl() {
            t.Fatalf("expected the base url stored as given, got %q for %q", config.BaseUrl(), legalBaseUrl)
        }
    }
}
