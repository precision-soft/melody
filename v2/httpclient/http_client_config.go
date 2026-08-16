package httpclient

import (
    "time"
)

func NewHttpClientConfig(
    baseUrl string,
    timeout time.Duration,
    headers map[string]string,
) *HttpClientConfig {
    return &HttpClientConfig{
        baseUrl: baseUrl,
        timeout: timeout,
        headers: canonicalHeaderMap(headers),
    }
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
