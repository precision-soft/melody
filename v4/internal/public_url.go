package internal

import (
    "net/url"
    "strings"
)

/* NormalizePublicUrl answers the public base an absolute url is generated behind: an http or https url with a host name, no user info, no query, no fragment and no empty, "." or ".." path segment, its path kept without the trailing slash and the whole written by url.URL, so the base it answers parses back to itself; the empty value is no base. The reason names what is refused. */
func NormalizePublicUrl(value string) (string, string) {
    if "" == value {
        return "", ""
    }

    if true == strings.ContainsAny(value, "?#") {
        return "", "a public url carries no query and no fragment"
    }

    parsed, parseErr := url.Parse(value)
    if nil != parseErr {
        return "", "a public url must parse as an url"
    }

    if "http" != parsed.Scheme && "https" != parsed.Scheme {
        return "", "a public url is http or https"
    }

    if "" != parsed.Opaque || "" == parsed.Hostname() {
        return "", "a public url names a host"
    }

    if nil != parsed.User {
        return "", "a public url carries no user info"
    }

    escapedPath := strings.TrimRight(parsed.EscapedPath(), "/")
    if "" != escapedPath {
        for _, escapedSegment := range strings.Split(escapedPath, "/")[1:] {
            segment, _ := url.PathUnescape(escapedSegment)
            if "" == segment || "." == segment || ".." == segment {
                return "", "a public url's path carries no empty, \".\" or \"..\" segment"
            }
        }
    }

    normalized := url.URL{Scheme: parsed.Scheme, Host: parsed.Host, RawPath: escapedPath}
    normalized.Path, _ = url.PathUnescape(escapedPath)

    return normalized.String(), ""
}
