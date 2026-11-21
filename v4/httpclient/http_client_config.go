package httpclient

import (
    "net/url"
    "strings"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* HttpClientConfig is the configuration of one client. A field left out takes its default: no base url, a 30s timeout, no header, the default transport, and redirects followed up to net/http's cap of ten requests. */
type HttpClientConfig struct {
    /* BaseUrl is the url a relative target resolves against; its path ends with a slash, or the client is refused. A client with a base refuses a target that leaves its origin, and one without refuses a relative target. */
    BaseUrl string

    /* Timeout bounds one whole exchange: 30s by default, at least 1ms, unlimited admitted. */
    Timeout bound.Duration

    /* Headers are sent on every request under their canonical spelling; two spellings that collapse onto one header are refused. */
    Headers map[string]string

    Transport TransportConfig

    /* RedirectsDisabled makes the client answer a redirect as the response it is: the 3xx status and its Location reach the caller. A following client keeps net/http's rules, one of which turns a POST answered 301, 302 or 303 into a GET without its body. */
    RedirectsDisabled bool

    /* MaxRedirects bounds the requests one exchange makes while it follows redirects, the first included, as net/http counts its own cap: 10 by default, at least 1, so a cap of n follows n-1 redirects and refuses the next. It is refused beside RedirectsDisabled. */
    MaxRedirects bound.Count
}

var (
    timeoutRule = bound.Rule[time.Duration]{
        Name:              "HttpClientConfig.Timeout",
        Default:           30 * time.Second,
        Minimum:           bound.TimeoutMinimum,
        UnlimitedAdmitted: true,
    }

    maxRedirectsRule = bound.Rule[int]{
        Name:    "HttpClientConfig.MaxRedirects",
        Default: 10,
        Minimum: 1,
    }
)

/* refuseBaseUrlWithoutTrailingSlash refuses a base url whose escaped path does not end in a slash: RFC 3986 resolution merges a relative target over the last segment of the base path, so "https://host/v1" + "users" names "https://host/users". An empty path or an empty base url is legal; a base that does not parse is reported, sanitized, on the first request. */
func refuseBaseUrlWithoutTrailingSlash(baseUrl string) error {
    if "" == baseUrl {
        return nil
    }

    parsedBase, err := url.Parse(baseUrl)
    if nil != err {
        return nil
    }

    path := parsedBase.EscapedPath()
    if "" == path || true == strings.HasSuffix(path, "/") {
        return nil
    }

    return exception.NewError(
        "the base url path must end with a slash",
        exceptioncontract.Context{
            "baseUrl": sanitizeUrlForDiagnostics(baseUrl),
        },
        nil,
    )
}
