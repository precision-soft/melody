package middleware

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
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

    /* client, then two trusted proxies appended by the infrastructure: walk right-to-left past both */
    ip := resolver(forwardedRequest("10.0.0.1:5555", "203.0.113.7, 192.168.0.10, 10.0.0.9"))
    if "203.0.113.7" != ip {
        t.Fatalf("expected the first untrusted hop from the right, got: %s", ip)
    }
}

func TestForwardedClientIpResolver_DoesNotBelieveSpoofedExtraEntries(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))

    /* the client sent its own X-Forwarded-For with a victim address; the proxy appended the real client — the rightmost untrusted entry wins, not the spoofed one */
    ip := resolver(forwardedRequest("10.0.0.1:5555", "198.51.100.99, 203.0.113.7"))
    if "203.0.113.7" != ip {
        t.Fatalf("expected the proxy-attested client, got: %s", ip)
    }
}

func TestForwardedClientIpResolver_FallsBackWhenPeerIsUntrusted(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))

    /* the direct peer is not a trusted proxy, so the header is attacker-controlled */
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

/* @info A proxy may write the same client as 1.2.3.4 or as ::ffff:1.2.3.4. Left mapped, the two forms key two different rate limit buckets, and an IPv4 CIDR in the trusted proxy list never matches a 4-in-6 peer. */
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

/* @info Proxies such as IIS/ARR and Azure Application Gateway append host:port to X-Forwarded-For. The untrusted client hop must be resolved to its bare address, not rejected as garbage — which would collapse every client into the proxy's own rate-limit bucket. */
func TestForwardedClientIpResolver_ResolvesPortedForwardedHop(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))

    ip := resolver(forwardedRequest("10.0.0.1:5555", "203.0.113.7:54321"))
    if "203.0.113.7" != ip {
        t.Fatalf("expected the ported forwarded client, got: %s", ip)
    }
}

/* @info A dual-stack proxy may write its hop as an IPv4-mapped address (::ffff:10.0.0.5) and the operator lists that literal in the trusted proxy list. The mapped trusted entry must still match the unmapped hop, otherwise the trusted hop is treated as the first untrusted client and the proxy's own address leaks as the limiter key. */
func TestForwardedClientIpResolver_MatchesMappedTrustedExactEntry(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8", "::ffff:192.168.1.1"))

    ip := resolver(forwardedRequest("10.0.0.1:5555", "203.0.113.7, ::ffff:192.168.1.1"))
    if "203.0.113.7" != ip {
        t.Fatalf("expected the client past the mapped trusted hop, got: %s", ip)
    }
}

/* @info A trusted proxy CIDR written in IPv4-mapped form (::ffff:192.168.0.0/120) must still contain the unmapped direct peer, otherwise the peer is rejected as untrusted and the forwarded chain is never walked. */
func TestForwardedClientIpResolver_MatchesMappedTrustedPrefix(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("::ffff:192.168.0.0/120"))

    ip := resolver(forwardedRequest("192.168.0.5:5555", "203.0.113.7"))
    if "203.0.113.7" != ip {
        t.Fatalf("expected the client behind the mapped trusted prefix, got: %s", ip)
    }
}

/* A trusted edge may write an IPv6 hop as a bracketed literal with no port. net.SplitHostPort rejects that shape and netip.ParseAddr rejects the brackets it still carries, so the hop read as garbage and the resolver fell back to the direct peer — every IPv6 client behind such an edge collapsed onto the proxy's single rate limit bucket while IPv4 clients kept their own. */
func TestForwardedClientIpResolver_ResolvesBracketedIpv6HopWithoutPort(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))

    ip := resolver(forwardedRequest("10.0.0.1:5555", "[2001:db8::1]"))
    if "2001:db8::1" != ip {
        t.Fatalf("expected the bracketed ipv6 client, got: %s", ip)
    }
}

/* The bracketed form must also be recognized as a trusted hop, otherwise a bracketed proxy is walked as if it were the client and the chain behind it is never reached. */
func TestForwardedClientIpResolver_TrustsBracketedIpv6Proxy(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8", "2001:db8:aaaa::/48"))

    ip := resolver(forwardedRequest("10.0.0.1:5555", "203.0.113.7, [2001:db8:aaaa::5]"))
    if "203.0.113.7" != ip {
        t.Fatalf("expected the client past the bracketed trusted hop, got: %s", ip)
    }
}

/* Bracketed and bare forms of one address name one client, so they must key one bucket. */
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
