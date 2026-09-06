package config

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
)

/* a request as the compose load balancer delivers it: the peer is the proxy, inside the ranges this example
   trusts, and the client it forwarded is named in the header. */
func requestForwardedByTheProxy(t *testing.T) melodyhttpcontract.Request {
    t.Helper()

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/products/", nil)
    httpRequest.RemoteAddr = "172.18.0.9:41234"
    httpRequest.Header.Set("X-Forwarded-For", "203.0.113.7")

    return melodyhttp.NewRequest(httpRequest, nil, nil, melodyhttp.NewRequestContext("budget-test", time.Now()))
}

/* the budget is per client, which is what its own comment and the shipped .env both promise. Read from the
   peer address it is per PROXY: measured through the compose stack the key was the balancer's 172.18.0.9,
   so a single script could spend the hour of everyone behind it — and this listener runs ahead of
   authentication, so what it spends is everyone's ability to log in. */
func TestRequestBudgetConfig_ChargesTheForwardedClientRatherThanTheProxy(t *testing.T) {
    budgetConfig := requestBudgetConfig(100)

    resolver := budgetConfig.ClientIpResolver()
    if nil == resolver {
        t.Fatal("expected the request budget to resolve the client address rather than fall back to the peer")
    }

    if "203.0.113.7" != resolver(requestForwardedByTheProxy(t)) {
        t.Fatalf("expected the forwarded client to be charged, got %q", resolver(requestForwardedByTheProxy(t)))
    }
}

/* the sister case: the header is only believed from a peer this example trusts, so a client that sends one
   itself cannot pick which budget it spends. */
func TestRequestBudgetConfig_IgnoresAForwardedHeaderFromAnUntrustedPeer(t *testing.T) {
    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/products/", nil)
    httpRequest.RemoteAddr = "198.51.100.4:5555"
    httpRequest.Header.Set("X-Forwarded-For", "203.0.113.7")

    request := melodyhttp.NewRequest(httpRequest, nil, nil, melodyhttp.NewRequestContext("budget-test", time.Now()))

    resolver := requestBudgetConfig(100).ClientIpResolver()
    if "198.51.100.4" != resolver(request) {
        t.Fatalf("expected a header from an untrusted peer to be ignored, got %q", resolver(request))
    }
}
