package httpclient

import (
    "bytes"
    "context"
    "encoding/json"
    "io"
    "math"
    "net"
    nethttp "net/http"
    "net/url"
    "strings"
    "sync"
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
    mutex   sync.RWMutex
    baseUrl string
    headers map[string]string
    timeout time.Duration
}

func NewHttpClient(config *HttpClientConfig) *HttpClient {
    timeout := config.Timeout()
    if 0 >= timeout {
        timeout = 30 * time.Second
    }

    headers := config.Headers()
    if nil == headers {
        headers = make(map[string]string)
    }

    transportConfig := resolveTransportConfig(config.Transport())

    transport := &nethttp.Transport{
        Proxy:                 nethttp.ProxyFromEnvironment,
        DialContext:           (&net.Dialer{Timeout: transportConfig.DialTimeout, KeepAlive: transportConfig.KeepAlive}).DialContext,
        ForceAttemptHTTP2:     true,
        MaxIdleConns:          transportConfig.MaxIdleConns,
        IdleConnTimeout:       transportConfig.IdleConnTimeout,
        TLSHandshakeTimeout:   transportConfig.TlsHandshakeTimeout,
        ExpectContinueTimeout: transportConfig.ExpectContinueTimeout,
        ResponseHeaderTimeout: transportConfig.ResponseHeaderTimeout,
    }

    instance := &HttpClient{
        client: &nethttp.Client{
            Timeout:   timeout,
            Transport: transport,
        },
        baseUrl: config.BaseUrl(),
        headers: headers,
        timeout: timeout,
    }

    instance.client.CheckRedirect = instance.credentialStrippingRedirectPolicy

    return instance
}

/* defaultMaxRedirects mirrors net/http's own cap; it is stated here because the client installs its own policy. */
const defaultMaxRedirects = 10

/* requestCredentialHeadersKeyType keys the per-request credential header names on the request context. The redirect policy only knows the header names the client was CONFIGURED with; the ones a caller attaches to a single request through WithHeader/WithHeaders are just as secret, and the context is the only channel that reaches a redirect the client itself creates. */
type requestCredentialHeadersKeyType struct{}

var requestCredentialHeadersKey = requestCredentialHeadersKeyType{}

/* credentialStrippingRedirectPolicy keeps net/http's ten-redirect cap but removes every credential the client attaches to each request once the redirect leaves the original origin. net/http strips only Authorization, WWW-Authenticate and Cookie, and only across domains — a client configured with an api-key header (X-Api-Key, X-Internal-Token, ...) would otherwise hand that secret to whatever host the first server points it at, which the operator of that server chooses. A scheme downgrade counts as leaving the origin: https -> http would put the credential on the wire in the clear. It is a method rather than a closure over the header map because net/http runs it on the request goroutine while SetHeader may be writing that very map. */
func (instance *HttpClient) credentialStrippingRedirectPolicy(request *nethttp.Request, via []*nethttp.Request) error {
    if defaultMaxRedirects <= len(via) {
        return exception.NewError(
            "stopped after too many redirects",
            exceptioncontract.Context{"redirects": len(via), "url": request.URL.String()},
            nil,
        )
    }

    if true == isSameOrigin(via[0].URL, request.URL) {
        return nil
    }

    instance.mutex.RLock()
    for headerName := range instance.headers {
        request.Header.Del(headerName)
    }
    instance.mutex.RUnlock()

    if requestHeaderNames, ok := request.Context().Value(requestCredentialHeadersKey).([]string); true == ok {
        for _, headerName := range requestHeaderNames {
            request.Header.Del(headerName)
        }
    }

    request.Header.Del("Authorization")
    request.Header.Del("Cookie")
    request.Header.Del("Proxy-Authorization")

    /* net/http auto-populates Referer with the full previous url, query string included; on a non-downgrade cross-origin hop it does not strip it, so a secret placed in the url (WithQuery) would reach the redirect target the first server chose. */
    request.Header.Del("Referer")

    return nil
}

/* withRequestCredentialHeaders carries the caller's per-request header names to the redirect policy, which net/http hands a request derived from this one. */
func withRequestCredentialHeaders(request *nethttp.Request, headers map[string]string) *nethttp.Request {
    if 0 == len(headers) {
        return request
    }

    headerNames := make([]string, 0, len(headers))
    for headerName := range headers {
        headerNames = append(headerNames, headerName)
    }

    return request.WithContext(
        context.WithValue(request.Context(), requestCredentialHeadersKey, headerNames),
    )
}

/* isSameOrigin compares the scheme, the host and the EFFECTIVE port: "https://host" and "https://host:443" name one origin, and hosts are case-insensitive, so neither spelling may be read as a credential boundary the caller never crossed. */
func isSameOrigin(origin *url.URL, target *url.URL) bool {
    if nil == origin || nil == target {
        return false
    }

    if false == strings.EqualFold(origin.Scheme, target.Scheme) {
        return false
    }

    if false == strings.EqualFold(origin.Hostname(), target.Hostname()) {
        return false
    }

    return effectivePort(origin) == effectivePort(target)
}

/* effectivePort resolves the port a url reaches, spelled out or implied by its scheme. */
func effectivePort(value *url.URL) string {
    if port := value.Port(); "" != port {
        return port
    }

    switch strings.ToLower(value.Scheme) {
    case "https":
        return "443"
    case "http":
        return "80"
    }

    return ""
}

func (instance *HttpClient) Get(urlString string, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {
    return instance.Request(nethttp.MethodGet, urlString, options...)
}

func (instance *HttpClient) Post(urlString string, body any, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {
    /* clamp capacity so appending WithJson never writes into a spare slot of the caller's slice, which a concurrent Post/Put/Patch may share. */
    options = append(options[:len(options):len(options)], WithJson(body))

    return instance.Request(nethttp.MethodPost, urlString, options...)
}

func (instance *HttpClient) Put(urlString string, body any, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {
    /* clamp capacity so appending WithJson never writes into a spare slot of the caller's slice, which a concurrent Post/Put/Patch may share. */
    options = append(options[:len(options):len(options)], WithJson(body))

    return instance.Request(nethttp.MethodPut, urlString, options...)
}

func (instance *HttpClient) Patch(urlString string, body any, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {
    /* clamp capacity so appending WithJson never writes into a spare slot of the caller's slice, which a concurrent Post/Put/Patch may share. */
    options = append(options[:len(options):len(options)], WithJson(body))

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

    request, err := instance.buildRequest(method, urlString, requestConfig)
    if nil != err {
        return nil, err
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

    /* the +1 lets ReadAll observe one byte past the cap so an over-long body is detected; saturate instead of wrapping, because int64(math.MaxInt)+1 is negative and LimitReader would then read nothing and return an empty body with no error */
    readLimit := int64(maxResponseBodyBytes)
    if math.MaxInt64 > readLimit {
        readLimit++
    }

    limitedReader := io.LimitReader(response.Body, readLimit)

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

    requestInstance, err := instance.buildRequest(method, urlString, requestConfig)
    if nil != err {
        return nil, err
    }

    clientInstance := instance.streamClientForRequest(requestConfig.Timeout())

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

func (instance *HttpClient) SetBaseUrl(baseUrl string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.baseUrl = baseUrl
}

func (instance *HttpClient) SetHeader(key string, value string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.headers[key] = value
}

func (instance *HttpClient) SetTimeout(timeout time.Duration) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.timeout = timeout
}

func (instance *HttpClient) buildRequest(method string, urlString string, requestConfig *RequestOptions) (*nethttp.Request, error) {
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

    instance.mutex.RLock()
    for key, value := range instance.headers {
        request.Header.Set(key, value)
    }
    instance.mutex.RUnlock()

    for key, value := range requestConfig.Headers() {
        request.Header.Set(key, value)
    }

    request = withRequestCredentialHeaders(request, requestConfig.Headers())

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

    return request, nil
}

func (instance *HttpClient) buildUrl(urlString string, query map[string]string) (string, error) {
    instance.mutex.RLock()
    baseUrl := instance.baseUrl
    instance.mutex.RUnlock()

    if "" != baseUrl &&
        false == strings.HasPrefix(urlString, "http://") &&
        false == strings.HasPrefix(urlString, "https://") {
        urlString = strings.TrimSuffix(baseUrl, "/") + "/" + strings.TrimPrefix(urlString, "/")
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

/* streamClientForRequest drops the whole-request Timeout for the streaming path. nethttp.Client.Timeout bounds everything up to and including the body read, so a long-lived stream (server-sent events, a log tail, a large download) is force-closed mid-read the moment the client timeout elapses — the streaming API is unusable beyond it. The header phase stays bounded by the transport (DialTimeout, TLSHandshakeTimeout, ResponseHeaderTimeout); the body's lifetime belongs to the caller, who closes it, or to a context the caller attaches to the request. An explicit per-request timeout is still honored, because a caller that asks for one on a stream is asking to bound the stream. */
func (instance *HttpClient) streamClientForRequest(timeout time.Duration) *nethttp.Client {
    if 0 < timeout {
        return instance.clientForRequest(timeout)
    }

    return &nethttp.Client{
        Transport:     instance.client.Transport,
        CheckRedirect: instance.client.CheckRedirect,
        Jar:           instance.client.Jar,
        Timeout:       0,
    }
}

func (instance *HttpClient) clientForRequest(timeout time.Duration) *nethttp.Client {
    if 0 >= timeout {
        instance.mutex.RLock()
        timeout = instance.timeout
        instance.mutex.RUnlock()
    }

    if 0 >= timeout || instance.client.Timeout == timeout {
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
