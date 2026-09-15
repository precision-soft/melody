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

/* NewForwardedClientIpResolver returns a ClientIpResolver that walks X-Forwarded-For right-to-left, skipping addresses that match the trusted proxy list, and returns the first untrusted address — the real client as attested by the trusted edge. It reuses the same ForwardedHeadersPolicy handed to Kernel.SetForwardedHeadersPolicy, so there is a single trusted-proxy list to maintain. It falls back to DefaultClientIp — the direct peer — whenever the forwarded chain cannot be trusted: forwarded headers are not trusted by policy, the trusted list is empty, the direct peer is not a trusted proxy (the header is then attacker-controlled), the chain has no parseable untrusted address, or every entry is a trusted proxy. Plug it into a rate-limit config with SetClientIpResolver so per-IP limits behind a reverse proxy key on the client instead of the proxy. */
func NewForwardedClientIpResolver(policy httpcontract.ForwardedHeadersPolicy) ClientIpResolver {

    if validationErr := internal.ValidateTrustedProxyList(policy.TrustedProxyList); nil != validationErr {
        exception.Panic(validationErr)
    }

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

            hopHostString := bareAddressFromAuthority(hop)

            hopAddress, hopAddressErr := netip.ParseAddr(hopHostString)
            if nil != hopAddressErr {

                return DefaultClientIp(request)
            }

            return hopAddress.Unmap().String()
        }

        return DefaultClientIp(request)
    }
}

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

func isTrustedProxyAddress(hostString string, trustedProxyList []string) bool {
    trimmedHostString := bareAddressFromAuthority(hostString)
    if "" == trimmedHostString {
        return false
    }

    hostAddress, hostAddressErr := netip.ParseAddr(trimmedHostString)
    if nil != hostAddressErr {
        return false
    }

    hostAddress = hostAddress.Unmap()

    for _, trustedProxyString := range trustedProxyList {
        trimmedTrustedProxyString := strings.TrimSpace(trustedProxyString)
        if "" == trimmedTrustedProxyString {
            continue
        }

        trustedPrefix, trustedPrefixErr := netip.ParsePrefix(trimmedTrustedProxyString)
        if nil == trustedPrefixErr {

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

        trustedAddress = trustedAddress.Unmap()

        if trustedAddress == hostAddress {
            return true
        }
    }

    return false
}
