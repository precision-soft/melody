package internal

import (
    "net/netip"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
)

/* ValidateTrustedProxyList refuses a trusted-proxy entry that is neither a CIDR prefix nor a bare
   address, naming the entry and its position. Both readers of such a list — the forwarded client-ip
   resolver and the kernel's own trusted-peer check — skip an entry they cannot parse, so a typo
   (10.0.0.0/33, 10.0.0) narrowed the list in silence: the hop it named stopped being trusted,
   X-Forwarded-For from it stopped being believed, and every client behind that proxy collapsed onto
   the direct peer's single rate-limit bucket with no record anywhere. The list is configuration, so
   the typo is refused where it is ingested rather than discovered as a traffic anomaly.

   An empty entry is skipped rather than refused: a list assembled by splitting an environment
   variable carries a trailing empty field for a trailing separator, and both readers already treat
   it as absent. */
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
