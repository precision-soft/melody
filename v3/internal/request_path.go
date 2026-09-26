package internal

import (
    "net/url"
    "strings"
)

/* RequestPathAsSent answers the escaped path as the client spelled it: URL.RawPath while it still unescapes to URL.Path, else URL.EscapedPath. URL.EscapedPath alone re-escapes the decoded path whenever the raw spelling carries a byte it does not keep, such as "{" or raw UTF-8, and an encoded "/" becomes a separator there. */
func RequestPathAsSent(requestUrl *url.URL) string {
    if "" != requestUrl.RawPath && false == RequestRawPathIsStale(requestUrl) {
        return requestUrl.RawPath
    }

    return requestUrl.EscapedPath()
}

/* RequestRawPathIsStale reports whether URL.RawPath is set and does not unescape to URL.Path, the state a handler in front of the kernel leaves when it rewrites Path alone: the spelling the client sent cannot then be read, so the kernel refuses the request rather than route it on the re-escaped decoded path. A cleared RawPath is indistinguishable from a request that carried no escape. */
func RequestRawPathIsStale(requestUrl *url.URL) bool {
    if "" == requestUrl.RawPath {
        return false
    }

    unescapedPath, unescapeErr := url.PathUnescape(requestUrl.RawPath)

    return nil != unescapeErr || unescapedPath != requestUrl.Path
}

/* RequestPathCarriesLiteralEncodedSeparator reports whether a segment of the path as sent decodes to a literal "%2F": such a segment ("%252F") shares its routed spelling with a separator encoded inside a segment, which RequestPathAsRouted always spells "%2F", so a binding compared on the routed spelling cannot tell the two requests apart. */
func RequestPathCarriesLiteralEncodedSeparator(requestUrl *url.URL) bool {
    for _, segment := range strings.Split(RequestPathAsSent(requestUrl), "/") {
        unescapedSegment, _ := url.PathUnescape(segment)

        if true == strings.Contains(unescapedSegment, "%2F") {
            return true
        }
    }

    return false
}
