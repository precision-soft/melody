package internal

import (
    "net/netip"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
)

/* ValidateTrustedProxyList refuses a trusted-proxy entry that is neither a CIDR prefix nor a bare address, naming the entry and its position, since both readers of the list skip an entry they cannot parse and a typo would narrow the list in silence. An empty entry, the trailing field of a split environment variable, is skipped. */
func ValidateTrustedProxyList(trustedProxyList []string) *exception.Error {
    for index, trustedProxyString := range trustedProxyList {
        trimmedTrustedProxyString := strings.TrimSpace(trustedProxyString)
        if "" == trimmedTrustedProxyString {
            continue
        }

        if _, prefixErr := netip.ParsePrefix(trimmedTrustedProxyString); nil == prefixErr {
            continue
        }

        if _, addressErr := netip.ParseAddr(trimmedTrustedProxyString); nil == addressErr {
            continue
        }

        return exception.NewError(
            "trusted proxy entry is neither a CIDR prefix nor an address",
            map[string]any{
                "index": index,
                "entry": trustedProxyString,
            },
            nil,
        )
    }

    return nil
}
