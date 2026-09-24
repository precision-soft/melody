package middleware

import (
    "net"
    "net/netip"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
)

const forwardedForHeaderName = "X-Forwarded-For"

/* NewForwardedClientIpResolver returns a ClientIpResolver that walks X-Forwarded-For right-to-left, skipping the trusted proxies of the policy handed to Kernel.SetForwardedHeadersPolicy, and answers the first untrusted address. It falls back to DefaultClientIp, the direct peer, whenever the chain cannot be trusted: forwarded headers not trusted, an empty trusted list, a direct peer that is not a trusted proxy, an unparseable entry or no untrusted one. */
func NewForwardedClientIpResolver(policy httpcontract.ForwardedHeadersPolicy) ClientIpResolver {
    /* an entry that parses as neither a prefix nor an address is refused, as the kernel refuses it */
    if validationErr := internal.ValidateTrustedProxyList(policy.TrustedProxyList); nil != validationErr {
        exception.Panic(validationErr)
    }

    /* the trusted list is copied, since the closure reads it on every request */
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

            /* unmapped, so ::ffff:1.2.3.4 and 1.2.3.4 key one bucket */
            return hopAddress.Unmap().String()
        }

        /* every hop was a trusted proxy, so the direct peer answers rather than a guess */
        return DefaultClientIp(request)
    }
}

/* bareAddressFromAuthority reduces a peer address or an X-Forwarded-For entry to a bare address literal from any of the four shapes an edge may write: bare, host:port, bracketed IPv6 with a port, and bracketed IPv6 without one. */
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

/* isTrustedProxyAddress reports whether the host matches the trusted list of exact addresses and CIDR prefixes, the per-address form of the kernel's scheme-detection check. */
func isTrustedProxyAddress(hostString string, trustedProxyList []string) bool {
    trimmedHostString := bareAddressFromAuthority(hostString)
    if "" == trimmedHostString {
        return false
    }

    hostAddress, hostAddressErr := netip.ParseAddr(trimmedHostString)
    if nil != hostAddressErr {
        return false
    }

    /* an IPv4-mapped IPv6 peer matches an IPv4 CIDR */
    hostAddress = hostAddress.Unmap()

    for _, trustedProxyString := range trustedProxyList {
        trimmedTrustedProxyString := strings.TrimSpace(trustedProxyString)
        if "" == trimmedTrustedProxyString {
            continue
        }

        trustedPrefix, trustedPrefixErr := netip.ParsePrefix(trimmedTrustedProxyString)
        if nil == trustedPrefixErr {
            /* a mapped prefix is unmapped so it contains the unmapped host */
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

        /* a mapped exact entry is unmapped so it equals the unmapped host */
        trustedAddress = trustedAddress.Unmap()

        if trustedAddress == hostAddress {
            return true
        }
    }

    return false
}
