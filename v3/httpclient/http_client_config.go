package httpclient

import (
    "net/url"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

func NewHttpClientConfig(
    baseUrl string,
    timeout time.Duration,
    headers map[string]string,
) *HttpClientConfig {
    refuseBaseUrlWithoutTrailingSlash(baseUrl)

    return &HttpClientConfig{
        baseUrl: baseUrl,
        timeout: timeout,
        headers: canonicalHeaderMap(headers),
    }
}

/* refuseBaseUrlWithoutTrailingSlash rejects a base url whose escaped path does not end in a slash: RFC 3986 resolution merges a relative target over the last segment of the base path, so "https://host/v1" + "users" names "https://host/users". An empty path or an empty base url is legal; a base that does not parse is reported, sanitized, on the first request. */
func refuseBaseUrlWithoutTrailingSlash(baseUrl string) {
    if "" == baseUrl {
        return
    }

    parsedBase, err := url.Parse(baseUrl)
    if nil != err {
        return
    }

    path := parsedBase.EscapedPath()
    if "" == path || true == strings.HasSuffix(path, "/") {
        return
    }

    exception.Panic(
        exception.NewError(
            "the base url path must end with a slash",
            exceptioncontract.Context{
                "baseUrl": sanitizeUrlForDiagnostics(baseUrl),
            },
            nil,
        ),
    )
}

type HttpClientConfig struct {
    baseUrl          string
    timeout          time.Duration
    headers          map[string]string
    transport        *TransportConfig
    withoutRedirects bool
}

/* WithoutRedirects makes the client answer a redirect as the response it is: the 3xx status and its Location reach the caller. A following client keeps net/http's rules, one of which turns a POST answered 301, 302 or 303 into a GET without its body. */
func (instance *HttpClientConfig) WithoutRedirects() *HttpClientConfig {
    instance.withoutRedirects = true

    return instance
}

/* FollowsRedirects answers whether the client follows a redirect, which it does unless WithoutRedirects was named. */
func (instance *HttpClientConfig) FollowsRedirects() bool {
    return false == instance.withoutRedirects
}

func (instance *HttpClientConfig) WithTransport(transport *TransportConfig) *HttpClientConfig {
    instance.transport = transport

    return instance
}

func (instance *HttpClientConfig) Transport() *TransportConfig {
    return instance.transport
}

func (instance *HttpClientConfig) BaseUrl() string {
    return instance.baseUrl
}

func (instance *HttpClientConfig) Timeout() time.Duration {
    return instance.timeout
}

func (instance *HttpClientConfig) Headers() map[string]string {
    copied := make(map[string]string, len(instance.headers))
    for key, value := range instance.headers {
        copied[key] = value
    }

    return copied
}
