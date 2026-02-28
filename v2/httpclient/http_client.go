package httpclient

import (
    "bytes"
    "encoding/json"
    "io"
    "net"
    nethttp "net/http"
    "net/url"
    "strings"
    "time"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    httpclientcontract "github.com/precision-soft/melody/v2/httpclient/contract"
)

func NewDefaultHttpClient() *HttpClient {
    return NewHttpClient(
        NewHttpClientConfig(
            "",
            30*time.Second,
            make(map[string]string),
        ),
    )
}

type HttpClient struct {
    client  *nethttp.Client
    baseUrl string
    headers map[string]string
    timeout time.Duration
}

func NewHttpClient(config *HttpClientConfig) *HttpClient {
    timeout := config.Timeout()
    if 0 == timeout {
        timeout = 30 * time.Second
    }

    headers := config.Headers()
    if nil == headers {
        headers = make(map[string]string)
    }

    transport := &nethttp.Transport{
        Proxy:                 nethttp.ProxyFromEnvironment,
        DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
        ForceAttemptHTTP2:     true,
        MaxIdleConns:          100,
        IdleConnTimeout:       90 * time.Second,
        TLSHandshakeTimeout:   10 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
        ResponseHeaderTimeout: 15 * time.Second,
    }

    return &HttpClient{
        client: &nethttp.Client{
            Timeout:   timeout,
            Transport: transport,
        },
        baseUrl: config.BaseUrl(),
        headers: headers,
        timeout: timeout,
    }
}

func (instance *HttpClient) Get(urlString string, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {
    return instance.Request(nethttp.MethodGet, urlString, options...)
}

func (instance *HttpClient) Post(urlString string, body any, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {
    options = append(options, WithJson(body))

    return instance.Request(nethttp.MethodPost, urlString, options...)
}

func (instance *HttpClient) Put(urlString string, body any, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {
    options = append(options, WithJson(body))

    return instance.Request(nethttp.MethodPut, urlString, options...)
}

func (instance *HttpClient) Patch(urlString string, body any, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {
    options = append(options, WithJson(body))

    return instance.Request(nethttp.MethodPatch, urlString, options...)
}

func (instance *HttpClient) Delete(urlString string, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {
    return instance.Request(nethttp.MethodDelete, urlString, options...)
}

func (instance *HttpClient) Request(method string, urlString string, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {
    requestConfig := NewRequestOptions()

    for _, applyOption := range options {
        applyOption(requestConfig)
    }

    fullUrl, err := instance.buildUrl(urlString, requestConfig.Query())
    if nil != err {
        return nil, err
    }

    var bodyReader io.Reader
    if nil != requestConfig.Body() {
        if "application/json" == requestConfig.ContentType() {
            jsonData, err := json.Marshal(requestConfig.Body())
            if nil != err {
                return nil, exception.NewError("failed to marshal json body", nil, err)
            }

            bodyReader = bytes.NewReader(jsonData)
        } else if stringValue, ok := requestConfig.Body().(string); ok {
            bodyReader = strings.NewReader(stringValue)
        } else if data, ok := requestConfig.Body().([]byte); ok {
            bodyReader = bytes.NewReader(data)
        } else {
            return nil, exception.NewError("unsupported body type", nil, nil)
        }
    }

    request, err := nethttp.NewRequest(method, fullUrl, bodyReader)
    if nil != err {
        return nil, exception.NewError("failed to create request", nil, err)
    }

    for key, value := range instance.headers {
        request.Header.Set(key, value)
    }

    for key, value := range requestConfig.Headers() {
        request.Header.Set(key, value)
    }

    if "" != requestConfig.ContentType() {
        request.Header.Set("Content-Type", requestConfig.ContentType())
    }

    authorization := requestConfig.Authorization()
    if nil != authorization {
        bearer := authorization.Bearer()
        if "" != bearer {
            request.Header.Set("Authorization", "Bearer "+bearer)
        } else {
            basicAuthorization := authorization.Basic()
            if nil != basicAuthorization {
                username := basicAuthorization.Username()
                if "" != username {
                    request.SetBasicAuth(
                        username,
                        basicAuthorization.Password(),
                    )
                }
            }
        }
    }

    client := instance.clientForRequest(requestConfig.Timeout())

    response, err := client.Do(request)
    if nil != err {
        return nil, exception.NewError("request failed", nil, err)
    }
    defer response.Body.Close()

    maxResponseBodyBytes := requestConfig.MaxResponseBodyBytes()
    if 0 >= maxResponseBodyBytes {
        return nil, exception.NewError("invalid max response body bytes", nil, nil)
    }

    limitedReader := io.LimitReader(response.Body, int64(maxResponseBodyBytes)+1)

    body, err := io.ReadAll(limitedReader)
    if nil != err {
        return nil, exception.NewError("failed to read response body", nil, err)
    }

    if maxResponseBodyBytes < len(body) {
        return nil, exception.NewError(
            "response body exceeded max size",
            exceptioncontract.Context{
                "maxResponseBodyBytes": maxResponseBodyBytes,
            },
            nil,
        )
    }

    return NewResponse(
        response.StatusCode,
        response.Status,
        response.Header,
        body,
        request,
    ), nil
}

func (instance *HttpClient) RequestStream(
    method string,
    urlString string,
    options ...httpclientcontract.RequestOption,
) (httpclientcontract.StreamResponse, error) {
    requestConfig := NewRequestOptions()

    for _, applyOption := range options {
        applyOption(requestConfig)
    }

    fullUrl, err := instance.buildUrl(urlString, requestConfig.Query())
    if nil != err {
        return nil, err
    }

    var bodyReader io.Reader
    if nil != requestConfig.Body() {
        if "application/json" == requestConfig.ContentType() {
            jsonData, err := json.Marshal(requestConfig.Body())
            if nil != err {
                return nil, exception.NewError("failed to marshal json body", nil, err)
            }

            bodyReader = bytes.NewReader(jsonData)
        } else if stringValue, ok := requestConfig.Body().(string); ok {
            bodyReader = strings.NewReader(stringValue)
        } else if data, ok := requestConfig.Body().([]byte); ok {
            bodyReader = bytes.NewReader(data)
        } else {
            return nil, exception.NewError("unsupported body type", nil, nil)
        }
    }

    requestInstance, err := nethttp.NewRequest(method, fullUrl, bodyReader)
    if nil != err {
        return nil, exception.NewError("failed to create request", nil, err)
    }

    for key, value := range instance.headers {
        requestInstance.Header.Set(key, value)
    }

    for key, value := range requestConfig.Headers() {
        requestInstance.Header.Set(key, value)
    }

    if "" != requestConfig.ContentType() {
        requestInstance.Header.Set("Content-Type", requestConfig.ContentType())
    }

    authorization := requestConfig.Authorization()
    if nil != authorization {
        bearer := authorization.Bearer()
        if "" != bearer {
            requestInstance.Header.Set("Authorization", "Bearer "+bearer)
        } else {
            basicAuthorization := authorization.Basic()
            if nil != basicAuthorization {
                username := basicAuthorization.Username()
                if "" != username {
                    requestInstance.SetBasicAuth(
                        username,
                        basicAuthorization.Password(),
                    )
                }
            }
        }
    }

    clientInstance := instance.clientForRequest(requestConfig.Timeout())

    response, err := clientInstance.Do(requestInstance)
    if nil != err {
        return nil, exception.NewError("request failed", nil, err)
    }

    return NewStreamResponse(
        response.StatusCode,
        response.Header.Clone(),
        response.Body,
    ), nil
}

func (instance *HttpClient) buildUrl(urlString string, query map[string]string) (string, error) {
    if "" != instance.baseUrl &&
        false == strings.HasPrefix(urlString, "http://") &&
        false == strings.HasPrefix(urlString, "https://") {
        urlString = strings.TrimSuffix(instance.baseUrl, "/") + "/" + strings.TrimPrefix(urlString, "/")
    }

    if 0 == len(query) {
        return urlString, nil
    }

    parsedUrl, err := url.Parse(urlString)
    if nil != err {
        return "", exception.NewError(
            "failed to parse request url",
            exceptioncontract.Context{
                "url": urlString,
            },
            err,
        )
    }

    queryValues := parsedUrl.Query()
    for key, value := range query {
        queryValues.Set(key, value)
    }

    parsedUrl.RawQuery = queryValues.Encode()
    return parsedUrl.String(), nil
}

func (instance *HttpClient) SetBaseUrl(baseUrl string) {
    instance.baseUrl = baseUrl
}

func (instance *HttpClient) SetHeader(key string, value string) {
    instance.headers[key] = value
}

func (instance *HttpClient) SetTimeout(timeout time.Duration) {
    instance.timeout = timeout
    instance.client.Timeout = timeout
}

func (instance *HttpClient) clientForRequest(timeout time.Duration) *nethttp.Client {
    if 0 >= timeout {
        return instance.client
    }

    return &nethttp.Client{
        Transport:     instance.client.Transport,
        CheckRedirect: instance.client.CheckRedirect,
        Jar:           instance.client.Jar,
        Timeout:       timeout,
    }
}

var _ httpclientcontract.Client = (*HttpClient)(nil)
