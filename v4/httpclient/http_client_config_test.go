package httpclient

import (
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

func TestHttpClientConfigHeaders_TheClientHoldsItsOwnCopy(t *testing.T) {
    headers := map[string]string{
        "X-Test": "original",
    }

    client := newTestHttpClient(t, HttpClientConfig{Headers: headers})

    headers["X-Test"] = "mutated"
    headers["X-New"] = "added"

    if "original" != client.headers["X-Test"] {
        t.Fatalf("expected the client to hold its own copy, got %q", client.headers["X-Test"])
    }
    if _, exists := client.headers["X-New"]; true == exists {
        t.Fatalf("expected no new key leaked into the client")
    }
}

func TestHttpClientConfigHeaders_AreStoredCanonicalized(t *testing.T) {
    client := newTestHttpClient(t, HttpClientConfig{Headers: map[string]string{"x-api-key": "secret"}})

    if "secret" != client.headers["X-Api-Key"] {
        t.Fatalf("expected the configured header stored under its canonical spelling, got %#v", client.headers)
    }
}

func TestHttpClientConfigBaseUrl_APathWithoutATrailingSlashIsRefused(t *testing.T) {
    client, err := NewHttpClient(HttpClientConfig{BaseUrl: "https://user:SECRET@api.example.com/v1"})
    if nil != client {
        t.Fatalf("expected no client beside the refusal")
    }

    if nil == err || false == strings.Contains(err.Error(), "must end with a slash") {
        t.Fatalf("expected the refusal to name the missing slash, got %v", err)
    }

    if true == strings.Contains(renderErrorForLog(t, err), "SECRET") {
        t.Fatalf("the base url credential reached the refusal: %s", renderErrorForLog(t, err))
    }
}

func TestHttpClientConfigBaseUrl_ABaseWithAnEmptyPathOrNoBaseAtAllIsLegal(t *testing.T) {
    for _, legalBaseUrl := range []string{"", "https://api.example.com", "https://api.example.com/", "https://api.example.com/v1/", ":"} {
        client := newTestHttpClient(t, HttpClientConfig{BaseUrl: legalBaseUrl})

        if legalBaseUrl != client.baseUrl {
            t.Fatalf("expected the base url stored as given, got %q for %q", client.baseUrl, legalBaseUrl)
        }
    }
}

func TestHttpClientConfigTimeout_AbsentIsThirtySeconds(t *testing.T) {
    client := newTestHttpClient(t, HttpClientConfig{})

    if 30*time.Second != client.client.Timeout {
        t.Fatalf("expected the 30s default, got %v", client.client.Timeout)
    }
}

func TestHttpClientConfigTimeout_ATimeoutBelowTheMinimumIsRefusedByName(t *testing.T) {
    for _, timeout := range []time.Duration{0, -1 * time.Second, time.Millisecond - 1} {
        client, err := NewHttpClient(HttpClientConfig{Timeout: bound.Of(timeout)})
        if nil != client {
            t.Fatalf("expected no client beside the refusal of %v", timeout)
        }

        assertRefusedField(t, err, "HttpClientConfig.Timeout", "the limit is below its minimum")
    }
}

func TestHttpClientConfigTimeout_AnUnlimitedTimeoutLeavesTheClientWithoutADeadline(t *testing.T) {
    client := newTestHttpClient(t, HttpClientConfig{Timeout: bound.UnlimitedDuration()})

    if 0 != client.client.Timeout {
        t.Fatalf("expected net/http's zero timeout, no deadline, got %v", client.client.Timeout)
    }

    bounded := newTestHttpClient(t, HttpClientConfig{Timeout: bound.Of(2 * time.Second)})

    if 2*time.Second != bounded.client.Timeout {
        t.Fatalf("expected the set timeout, got %v", bounded.client.Timeout)
    }
}
