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

/* refuseBaseUrlWithoutTrailingSlash rejects a base url whose path does not end in a slash, at the door that stores it. RFC 3986 reference resolution — which buildUrl implements — merges a relative target over the LAST SEGMENT of the base path, so "https://host/v1" + "users" names "https://host/users": the "/v1" the caller thought of as a prefix is silently cut, and every request answers 404 in production. Refusing at construction makes the mistake fall at wiring instead. The path is judged in its escaped form, the one the merge operates on. A base with an empty path ("https://host") is legal — there is no segment to cut — and an empty base url means no base at all. A base that does not parse cannot be judged here; buildUrl reports the parse failure on the first request, sanitized. */
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

/* WithoutRedirects makes the client answer a redirect as the response it is instead of following it: the 3xx status and its Location reach the caller. A client that follows keeps net/http's rules, and one of them turns a POST answered 301, 302 or 303 into a GET without its body — so a caller that posts to a sink and reads the success of what came back has read the success of a page the sink pointed at, not of what the sink stored. A caller whose target must be where it was configured names that here and judges the status itself. */
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
