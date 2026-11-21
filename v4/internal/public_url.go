package internal

import (
    "net/url"
    "strings"
)

/* NormalizePublicUrl answers the public base an absolute url is generated behind: an http or https url with a host, no user info, no query and no fragment, its path kept without the trailing slash; the empty value is no base. The reason names what is refused. */
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

    if "" != parsed.Opaque || "" == parsed.Host {
        return "", "a public url names a host"
    }

    if nil != parsed.User {
        return "", "a public url carries no user info"
    }

    return parsed.Scheme + "://" + parsed.Host + strings.TrimRight(parsed.EscapedPath(), "/"), ""
}
