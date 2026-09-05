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
    baseUrl   string
    timeout   time.Duration
    headers   map[string]string
    transport *TransportConfig
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
