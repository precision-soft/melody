package httpclient

import (
    "time"
)

func NewHttpClientConfig(
    baseUrl string,
    timeout time.Duration,
    headers map[string]string,
) *HttpClientConfig {
    copiedHeaders := map[string]string{}

    if nil != headers {
        copiedHeaders = make(map[string]string, len(headers))
        for key, value := range headers {
            copiedHeaders[key] = value
        }
    }

    return &HttpClientConfig{
        baseUrl: baseUrl,
        timeout: timeout,
        headers: copiedHeaders,
    }
}

type HttpClientConfig struct {
    baseUrl string
    timeout time.Duration
    headers map[string]string
}

func (instance *HttpClientConfig) BaseUrl() string {
    return instance.baseUrl
}

func (instance *HttpClientConfig) Timeout() time.Duration {
    return instance.timeout
}

func (instance *HttpClientConfig) Headers() map[string]string {
    return instance.headers
}
