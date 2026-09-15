package middleware

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
)

func forwardedRequest(remoteAddr string, forwardedFor string) httpcontract.Request {
    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    request.RemoteAddr = remoteAddr
    if "" != forwardedFor {
        request.Header.Set("X-Forwarded-For", forwardedFor)
    }

    return testhelper.NewHttpTestRequestFromHttpRequest(request)
}

func trustingPolicy(trustedProxies ...string) httpcontract.ForwardedHeadersPolicy {
    return httpcontract.ForwardedHeadersPolicy{
        TrustForwardedHeaders: true,
        TrustedProxyList:      trustedProxies,
    }
}

func TestForwardedClientIpResolver_ResolvesClientBehindTrustedProxy(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))

    ip := resolver(forwardedRequest("10.0.0.1:5555", "203.0.113.7"))
    if "203.0.113.7" != ip {
        t.Fatalf("expected the forwarded client, got: %s", ip)
    }
}

func TestForwardedClientIpResolver_SkipsTrustedInfixHops(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8", "192.168.0.10"))

    ip := resolver(forwardedRequest("10.0.0.1:5555", "203.0.113.7, 192.168.0.10, 10.0.0.9"))
    if "203.0.113.7" != ip {
        t.Fatalf("expected the first untrusted hop from the right, got: %s", ip)
    }
}

func TestForwardedClientIpResolver_DoesNotBelieveSpoofedExtraEntries(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))

    ip := resolver(forwardedRequest("10.0.0.1:5555", "198.51.100.99, 203.0.113.7"))
    if "203.0.113.7" != ip {
        t.Fatalf("expected the proxy-attested client, got: %s", ip)
    }
}

func TestForwardedClientIpResolver_FallsBackWhenPeerIsUntrusted(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))

    ip := resolver(forwardedRequest("203.0.113.50:5555", "198.51.100.99"))
    if "203.0.113.50" != ip {
        t.Fatalf("expected the direct peer, got: %s", ip)
    }
}

func TestForwardedClientIpResolver_FallsBackWhenHeadersNotTrusted(t *testing.T) {
    resolver := NewForwardedClientIpResolver(httpcontract.ForwardedHeadersPolicy{
        TrustForwardedHeaders: false,
        TrustedProxyList:      []string{"10.0.0.0/8"},
    })

    ip := resolver(forwardedRequest("10.0.0.1:5555", "203.0.113.7"))
    if "10.0.0.1" != ip {
        t.Fatalf("expected the direct peer when the policy does not trust forwarded headers, got: %s", ip)
    }
}

func TestForwardedClientIpResolver_FallsBackOnEmptyTrustedList(t *testing.T) {
    resolver := NewForwardedClientIpResolver(httpcontract.ForwardedHeadersPolicy{
        TrustForwardedHeaders: true,
        TrustedProxyList:      nil,
    })

    ip := resolver(forwardedRequest("10.0.0.1:5555", "203.0.113.7"))
    if "10.0.0.1" != ip {
        t.Fatalf("expected the direct peer with no trusted proxies, got: %s", ip)
    }
}

func TestForwardedClientIpResolver_FallsBackOnGarbageEntry(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))

    ip := resolver(forwardedRequest("10.0.0.1:5555", "not-an-address"))
    if "10.0.0.1" != ip {
        t.Fatalf("expected the direct peer on an unparseable chain, got: %s", ip)
    }
}

func TestForwardedClientIpResolver_FallsBackWhenChainIsAllTrusted(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))

    ip := resolver(forwardedRequest("10.0.0.1:5555", "10.0.0.7, 10.0.0.8"))
    if "10.0.0.1" != ip {
        t.Fatalf("expected the direct peer when every hop is trusted, got: %s", ip)
    }
}

func TestForwardedClientIpResolver_FallsBackWhenHeaderIsAbsent(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))

    ip := resolver(forwardedRequest("10.0.0.1:5555", ""))
    if "10.0.0.1" != ip {
        t.Fatalf("expected the direct peer without a forwarded header, got: %s", ip)
    }
}

func TestForwardedClientIpResolver_ResolvesIpv6Client(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))

    ip := resolver(forwardedRequest("10.0.0.1:5555", "2001:db8::1"))
    if "2001:db8::1" != ip {
        t.Fatalf("expected the ipv6 client, got: %s", ip)
    }
}

func TestForwardedClientIpResolver_MatchesCidrAndExactEntries(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("192.168.0.10", "2001:db8:aaaa::/48"))

    ip := resolver(forwardedRequest("192.168.0.10:5555", "203.0.113.7, 2001:db8:aaaa::5"))
    if "203.0.113.7" != ip {
        t.Fatalf("expected the untrusted hop past the ipv6 CIDR match, got: %s", ip)
    }
}

func TestForwardedClientIpResolver_UnmapsIpv4MappedAddresses(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))

    mapped := resolver(forwardedRequest("[::ffff:10.1.2.3]:4567", "::ffff:203.0.113.7"))
    plain := resolver(forwardedRequest("10.1.2.3:4567", "203.0.113.7"))

    if "203.0.113.7" != plain {
        t.Fatalf("expected the plain client address, got %q", plain)
    }
    if mapped != plain {
        t.Fatalf("an ipv4-mapped chain must key the same bucket as its plain form: %q vs %q", mapped, plain)
    }
}

func TestForwardedClientIpResolver_ResolvesPortedForwardedHop(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))

    ip := resolver(forwardedRequest("10.0.0.1:5555", "203.0.113.7:54321"))
    if "203.0.113.7" != ip {
        t.Fatalf("expected the ported forwarded client, got: %s", ip)
    }
}

func TestForwardedClientIpResolver_MatchesMappedTrustedExactEntry(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8", "::ffff:192.168.1.1"))

    ip := resolver(forwardedRequest("10.0.0.1:5555", "203.0.113.7, ::ffff:192.168.1.1"))
    if "203.0.113.7" != ip {
        t.Fatalf("expected the client past the mapped trusted hop, got: %s", ip)
    }
}

func TestForwardedClientIpResolver_MatchesMappedTrustedPrefix(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("::ffff:192.168.0.0/120"))

    ip := resolver(forwardedRequest("192.168.0.5:5555", "203.0.113.7"))
    if "203.0.113.7" != ip {
        t.Fatalf("expected the client behind the mapped trusted prefix, got: %s", ip)
    }
}

func TestForwardedClientIpResolver_ResolvesBracketedIpv6HopWithoutPort(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))

    ip := resolver(forwardedRequest("10.0.0.1:5555", "[2001:db8::1]"))
    if "2001:db8::1" != ip {
        t.Fatalf("expected the bracketed ipv6 client, got: %s", ip)
    }
}

func TestForwardedClientIpResolver_TrustsBracketedIpv6Proxy(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8", "2001:db8:aaaa::/48"))

    ip := resolver(forwardedRequest("10.0.0.1:5555", "203.0.113.7, [2001:db8:aaaa::5]"))
    if "203.0.113.7" != ip {
        t.Fatalf("expected the client past the bracketed trusted hop, got: %s", ip)
    }
}

func TestForwardedClientIpResolver_BracketedAndBareIpv6KeyTheSameClient(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))

    bracketed := resolver(forwardedRequest("10.0.0.1:5555", "[2001:db8::1]"))
    bare := resolver(forwardedRequest("10.0.0.1:5555", "2001:db8::1"))
    ported := resolver(forwardedRequest("10.0.0.1:5555", "[2001:db8::1]:54321"))

    if bracketed != bare {
        t.Fatalf("a bracketed ipv6 hop must key the same bucket as its bare form: %q vs %q", bracketed, bare)
    }
    if ported != bare {
        t.Fatalf("a ported ipv6 hop must key the same bucket as its bare form: %q vs %q", ported, bare)
    }
}

func TestForwardedClientIpResolver_CopiesTheTrustedProxyListAtConstruction(t *testing.T) {
    trustedProxyList := []string{"10.0.0.0/8"}
    resolver := NewForwardedClientIpResolver(httpcontract.ForwardedHeadersPolicy{
        TrustForwardedHeaders: true,
        TrustedProxyList:      trustedProxyList,
    })

    trustedProxyList[0] = "192.0.2.0/24"

    clientIp := resolver(forwardedRequest("10.0.0.1:4711", "203.0.113.9"))
    if "203.0.113.9" != clientIp {
        t.Fatalf("expected the construction-time trusted list to keep deciding, got %q", clientIp)
    }
}

func TestNewForwardedClientIpResolver_RefusesAMalformedTrustedProxyEntry(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewForwardedClientIpResolver(httpcontract.ForwardedHeadersPolicy{
                TrustForwardedHeaders: true,
                TrustedProxyList:      []string{"10.0.0.0/8", "10.0.0.0/33"},
            })
        },
        "trusted proxy entry is neither a CIDR prefix nor an address",
    )
}

func TestNewForwardedClientIpResolver_KeepsAcceptingAnEmptyEntry(t *testing.T) {
    resolver := NewForwardedClientIpResolver(httpcontract.ForwardedHeadersPolicy{
        TrustForwardedHeaders: true,
        TrustedProxyList:      []string{"10.0.0.0/8", ""},
    })

    if nil == resolver {
        t.Fatalf("expected a resolver")
    }
}

func TestForwardedClientIpResolver_ReadsEveryForwardedForLine(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    request.RemoteAddr = "10.0.0.1:5555"
    request.Header.Add("X-Forwarded-For", "198.51.100.99")
    request.Header.Add("X-Forwarded-For", "203.0.113.7")

    ip := resolver(testhelper.NewHttpTestRequestFromHttpRequest(request))
    if "203.0.113.7" != ip {
        t.Fatalf("expected the client the trusted edge attested on the second line, got: %s", ip)
    }
}
