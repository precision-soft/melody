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

/* WithoutRedirects returns the original 3xx response and Location rather than following it. Use it when the configured endpoint must be the only destination or when method-changing redirects are unacceptable. */
func (instance *HttpClientConfig) WithoutRedirects() *HttpClientConfig {
    instance.withoutRedirects = true

    return instance
}

/* FollowsRedirects is true unless WithoutRedirects is selected. */
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
