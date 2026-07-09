package middleware

import (
    "net"
    "net/netip"
    "strings"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

const forwardedForHeaderName = "X-Forwarded-For"

/* NewForwardedClientIpResolver returns a ClientIpResolver that walks X-Forwarded-For right-to-left, skipping addresses that match the trusted proxy list, and returns the first untrusted address — the real client as attested by the trusted edge. It reuses the same ForwardedHeadersPolicy handed to Kernel.SetForwardedHeadersPolicy, so there is a single trusted-proxy list to maintain. It falls back to DefaultClientIp — the direct peer — whenever the forwarded chain cannot be trusted: forwarded headers are not trusted by policy, the trusted list is empty, the direct peer is not a trusted proxy (the header is then attacker-controlled), the chain has no parseable untrusted address, or every entry is a trusted proxy. Plug it into a rate-limit config with SetClientIpResolver so per-IP limits behind a reverse proxy key on the client instead of the proxy. */
func NewForwardedClientIpResolver(policy httpcontract.ForwardedHeadersPolicy) ClientIpResolver {
    return func(request httpcontract.Request) string {
        if false == policy.TrustForwardedHeaders {
            return DefaultClientIp(request)
        }

        if 0 == len(policy.TrustedProxyList) {
            return DefaultClientIp(request)
        }

        directPeer := DefaultClientIp(request)
        if false == isTrustedProxyAddress(directPeer, policy.TrustedProxyList) {
            return DefaultClientIp(request)
        }

        forwardedChain := forwardedForAddresses(request)
        for index := len(forwardedChain) - 1; index >= 0; index-- {
            hop := forwardedChain[index]

            if true == isTrustedProxyAddress(hop, policy.TrustedProxyList) {
                continue
            }

            /* the first untrusted hop from the right is the client the nearest trusted proxy attested; anything further left is client-controlled and must not be believed */
            hopAddress, hopAddressErr := netip.ParseAddr(hop)
            if nil != hopAddressErr {
                /* an unparseable entry means the chain is garbage from here on; never return attacker-shaped input as a limiter key */
                return DefaultClientIp(request)
            }

            return hopAddress.String()
        }

        /* every hop was a trusted proxy: the client is the leftmost infrastructure address; fall back to the direct peer rather than guess */
        return DefaultClientIp(request)
    }
}

func forwardedForAddresses(request httpcontract.Request) []string {
    values := request.HttpRequest().Header.Values(forwardedForHeaderName)

    addresses := make([]string, 0, len(values))
    for _, value := range values {
        for _, entry := range strings.Split(value, ",") {
            trimmed := strings.TrimSpace(entry)
            if "" == trimmed {
                continue
            }

            addresses = append(addresses, trimmed)
        }
    }

    return addresses
}

/* isTrustedProxyAddress reports whether the host string matches the trusted proxy list of exact addresses and CIDR prefixes — the per-address form of the request-level check the http kernel applies for scheme detection. */
func isTrustedProxyAddress(hostString string, trustedProxyList []string) bool {
    trimmedHostString := strings.TrimSpace(hostString)
    if "" == trimmedHostString {
        return false
    }

    if hostFromSplit, _, splitErr := net.SplitHostPort(trimmedHostString); nil == splitErr && "" != strings.TrimSpace(hostFromSplit) {
        trimmedHostString = hostFromSplit
    }

    hostAddress, hostAddressErr := netip.ParseAddr(trimmedHostString)
    if nil != hostAddressErr {
        return false
    }

    for _, trustedProxyString := range trustedProxyList {
        trimmedTrustedProxyString := strings.TrimSpace(trustedProxyString)
        if "" == trimmedTrustedProxyString {
            continue
        }

        trustedPrefix, trustedPrefixErr := netip.ParsePrefix(trimmedTrustedProxyString)
        if nil == trustedPrefixErr {
            if true == trustedPrefix.Contains(hostAddress) {
                return true
            }

            continue
        }

        trustedAddress, trustedAddressErr := netip.ParseAddr(trimmedTrustedProxyString)
        if nil != trustedAddressErr {
            continue
        }

        if trustedAddress == hostAddress {
            return true
        }
    }

    return false
}
