package middleware

import (
    "net"
    "net/netip"
    "strings"

    httpcontract "github.com/precision-soft/melody/v2/http/contract"
)

const forwardedForHeaderName = "X-Forwarded-For"

/* NewForwardedClientIpResolver returns a ClientIpResolver that walks X-Forwarded-For right-to-left, skipping addresses that match the trusted proxy list, and returns the first untrusted address — the real client as attested by the trusted edge. It reuses the same ForwardedHeadersPolicy handed to Kernel.SetForwardedHeadersPolicy, so there is a single trusted-proxy list to maintain. It falls back to DefaultClientIp — the direct peer — whenever the forwarded chain cannot be trusted: forwarded headers are not trusted by policy, the trusted list is empty, the direct peer is not a trusted proxy (the header is then attacker-controlled), the chain has no parseable untrusted address, or every entry is a trusted proxy. Plug it into a rate-limit config with SetClientIpResolver so per-IP limits behind a reverse proxy key on the client instead of the proxy. */
func NewForwardedClientIpResolver(policy httpcontract.ForwardedHeadersPolicy) ClientIpResolver {
    /* the trusted list is copied at construction rather than captured live: the closure reads it on every request to pick the limiter key, and a caller reusing its slice after construction would rewrite the trust decision mid-serving as a data race — the same rule Kernel.SetForwardedHeadersPolicy applies to the same list. */
    copiedTrustedProxyList := make([]string, len(policy.TrustedProxyList))
    copy(copiedTrustedProxyList, policy.TrustedProxyList)
    policy.TrustedProxyList = copiedTrustedProxyList

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
            hopHostString := bareAddressFromAuthority(hop)

            hopAddress, hopAddressErr := netip.ParseAddr(hopHostString)
            if nil != hopAddressErr {
                /* an unparseable entry means the chain is garbage from here on; never return attacker-shaped input as a limiter key */
                return DefaultClientIp(request)
            }

            /* a proxy may write the same client as 1.2.3.4 or as ::ffff:1.2.3.4; unmapped, the two forms key one rate limit bucket instead of two */
            return hopAddress.Unmap().String()
        }

        /* every hop was a trusted proxy: the client is the leftmost infrastructure address; fall back to the direct peer rather than guess */
        return DefaultClientIp(request)
    }
}

/* bareAddressFromAuthority reduces a peer address or an X-Forwarded-For entry to the bare address literal netip.ParseAddr accepts. A trusted edge may write any of four shapes: a bare address, host:port, a bracketed IPv6 literal with a port, or a bracketed IPv6 literal without one. net.SplitHostPort covers only the two ported shapes, and netip.ParseAddr rejects the brackets the fourth still carries, so a bracketed literal without a port would be read as garbage — every IPv6 client behind such an edge would then collapse onto the proxy's own rate limit bucket while IPv4 clients kept theirs. Proxies such as IIS/ARR and Azure Application Gateway append the port, so strip it here too and the real client is not mistaken for garbage either way. */
func bareAddressFromAuthority(value string) string {
    trimmedValue := strings.TrimSpace(value)
    if "" == trimmedValue {
        return ""
    }

    if hostFromSplit, _, splitErr := net.SplitHostPort(trimmedValue); nil == splitErr && "" != strings.TrimSpace(hostFromSplit) {
        return strings.TrimSpace(hostFromSplit)
    }

    if true == strings.HasPrefix(trimmedValue, "[") && true == strings.HasSuffix(trimmedValue, "]") {
        return strings.TrimSpace(trimmedValue[1 : len(trimmedValue)-1])
    }

    return trimmedValue
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
    trimmedHostString := bareAddressFromAuthority(hostString)
    if "" == trimmedHostString {
        return false
    }

    hostAddress, hostAddressErr := netip.ParseAddr(trimmedHostString)
    if nil != hostAddressErr {
        return false
    }

    /* an IPv4-mapped IPv6 peer (::ffff:10.0.0.1) is the IPv4 address it names, so an IPv4 CIDR in the trusted proxy list must still match it */
    hostAddress = hostAddress.Unmap()

    for _, trustedProxyString := range trustedProxyList {
        trimmedTrustedProxyString := strings.TrimSpace(trustedProxyString)
        if "" == trimmedTrustedProxyString {
            continue
        }

        trustedPrefix, trustedPrefixErr := netip.ParsePrefix(trimmedTrustedProxyString)
        if nil == trustedPrefixErr {
            /* a mapped prefix (::ffff:10.0.0.0/104) names the IPv4 range it embeds; unmap it so it still Contains the unmapped host — netip treats the two address families as unequal otherwise */
            if true == trustedPrefix.Addr().Is4In6() && trustedPrefix.Bits() >= 96 {
                trustedPrefix = netip.PrefixFrom(trustedPrefix.Addr().Unmap(), trustedPrefix.Bits()-96)
            }

            if true == trustedPrefix.Contains(hostAddress) {
                return true
            }

            continue
        }

        trustedAddress, trustedAddressErr := netip.ParseAddr(trimmedTrustedProxyString)
        if nil != trustedAddressErr {
            continue
        }

        /* a mapped exact entry (::ffff:10.0.0.5) is the IPv4 address it names; unmap it so it still equals the unmapped host */
        trustedAddress = trustedAddress.Unmap()

        if trustedAddress == hostAddress {
            return true
        }
    }

    return false
}
