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

func TestNewHttpClientConfig_HeadersAreStoredCanonicalized(t *testing.T) {
    config := NewHttpClientConfig("", 0, map[string]string{"x-api-key": "secret"})

    if "secret" != config.Headers()["X-Api-Key"] {
        t.Fatalf("expected the configured header stored under its canonical spelling, got %#v", config.Headers())
    }
}

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

func TestNewHttpClientConfig_ABaseWithAnEmptyPathOrNoBaseAtAllIsLegal(t *testing.T) {
    for _, legalBaseUrl := range []string{"", "https://api.example.com", "https://api.example.com/", "https://api.example.com/v1/", ":"} {
        config := NewHttpClientConfig(legalBaseUrl, 0, nil)

        if legalBaseUrl != config.BaseUrl() {
            t.Fatalf("expected the base url stored as given, got %q for %q", config.BaseUrl(), legalBaseUrl)
        }
    }
}
