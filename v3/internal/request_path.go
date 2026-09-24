package internal

import (
    "net/url"
)

/* RequestPathAsSent answers the escaped path as the client spelled it: URL.RawPath while it still unescapes to URL.Path, else URL.EscapedPath. URL.EscapedPath alone re-escapes the decoded path whenever the raw spelling carries a byte it does not keep, such as "{" or raw UTF-8, and an encoded "/" becomes a separator there. */
func RequestPathAsSent(requestUrl *url.URL) string {
    if "" != requestUrl.RawPath {
        unescapedPath, unescapeErr := url.PathUnescape(requestUrl.RawPath)
        if nil == unescapeErr && unescapedPath == requestUrl.Path {
            return requestUrl.RawPath
        }
    }

    return requestUrl.EscapedPath()
}
