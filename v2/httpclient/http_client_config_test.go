package httpclient

import (
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
