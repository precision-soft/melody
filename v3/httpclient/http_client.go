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
    "reflect"
    "strings"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    httpclientcontract "github.com/precision-soft/melody/v3/httpclient/contract"
    "github.com/precision-soft/melody/v3/internal"
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

/* HttpClient owns a reusable connection pool and synchronized process-wide defaults. Use request options for request-specific headers, credentials, and timeouts; setters change defaults shared by all callers. */
type HttpClient struct {
    client  *nethttp.Client
    mutex   sync.RWMutex
    baseUrl string
    headers map[string]string
    timeout time.Duration
}

/* NewHttpClient builds a client over its own net/http transport, which owns an idle connection pool: hold the client for as long as the application calls the service it points at, and close it when it is done, rather than building one per call. A nil configuration panics at the point the wiring mistake is made — NewDefaultHttpClient is the constructor that asks for the defaults. */
func NewHttpClient(config *HttpClientConfig) *HttpClient {
    if nil == config {
        exception.Panic(
            exception.NewError("http client configuration is nil", nil, nil),
        )
    }

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
        MaxIdleConnsPerHost:   transportConfig.MaxIdleConnsPerHost,
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
    if false == config.FollowsRedirects() {
        instance.client.CheckRedirect = answerRedirectAsTheResponse
    }

    return instance
}

func answerRedirectAsTheResponse(request *nethttp.Request, via []*nethttp.Request) error {
    return nethttp.ErrUseLastResponse
}

const defaultMaxRedirects = 10

type requestCredentialHeadersKeyType struct{}

var requestCredentialHeadersKey = requestCredentialHeadersKeyType{}

func (instance *HttpClient) credentialStrippingRedirectPolicy(request *nethttp.Request, via []*nethttp.Request) error {
    if defaultMaxRedirects <= len(via) {
        return exception.NewError(
            "stopped after too many redirects",
            exceptioncontract.Context{
                "redirects": len(via),
                "url":       sanitizeUrlForDiagnostics(request.URL.String()),
            },
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

    request.Header.Del("Referer")

    return nil
}

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

func sanitizeUrlForDiagnostics(urlString string) string {
    parsed, err := url.Parse(urlString)
    if nil != err {
        return sanitizeUrlTextually(urlString)
    }

    if nil != parsed.User {
        parsed.User = url.UserPassword(redactedValue, redactedValue)
    }

    if "" != parsed.Opaque {

        parsed.Opaque = redactAuthorityUserinfo(parsed.Opaque, 0)
    }

    if "" != parsed.RawQuery {
        queryValues := parsed.Query()
        for key := range queryValues {
            queryValues.Set(key, redactedValue)
        }

        parsed.RawQuery = queryValues.Encode()
    }

    parsed.Fragment = ""
    parsed.RawFragment = ""

    return parsed.String()
}

const redactedValue = "xxxxx"

func sanitizeUrlTextually(urlString string) string {
    sanitized := urlString

    if queryStart := strings.Index(sanitized, "?"); 0 <= queryStart {
        sanitized = sanitized[:queryStart] + "?" + redactedValue
    }

    authorityStart, hasAuthority := authorityStartIndex(sanitized)
    if false == hasAuthority {
        return sanitized
    }

    return redactAuthorityUserinfo(sanitized, authorityStart)
}

func authorityStartIndex(value string) (int, bool) {
    if schemeEnd := strings.Index(value, "://"); 0 <= schemeEnd {
        return schemeEnd + len("://"), true
    }

    if true == strings.HasPrefix(value, "//") {
        return len("//"), true
    }

    if schemeEnd := schemeSeparatorIndex(value); 0 <= schemeEnd {
        return schemeEnd + len(":"), true
    }

    return 0, false
}

func schemeSeparatorIndex(value string) int {
    for index := 0; index < len(value); index++ {
        currentByte := value[index]

        switch {
        case ':' == currentByte:
            if 0 == index {
                return -1
            }

            return index
        case ('a' <= currentByte && 'z' >= currentByte) || ('A' <= currentByte && 'Z' >= currentByte):
            continue
        case 0 == index:
            return -1
        case ('0' <= currentByte && '9' >= currentByte) || '+' == currentByte || '-' == currentByte || '.' == currentByte:
            continue
        default:
            return -1
        }
    }

    return -1
}

func redactAuthorityUserinfo(value string, authorityStart int) string {
    authorityEnd := strings.Index(value[authorityStart:], "/")
    if 0 > authorityEnd {
        authorityEnd = len(value)
    } else {
        authorityEnd += authorityStart
    }

    userinfoEnd := strings.LastIndex(value[authorityStart:authorityEnd], "@")
    if 0 > userinfoEnd {
        return value
    }

    return value[:authorityStart] +
        redactedValue + ":" + redactedValue +
        value[authorityStart+userinfoEnd:]
}

func (instance *HttpClient) Get(urlString string, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {
    return instance.Request(nethttp.MethodGet, urlString, options...)
}

func (instance *HttpClient) Post(urlString string, body any, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {

    options = append(options[:len(options):len(options)], WithJson(body))

    return instance.Request(nethttp.MethodPost, urlString, options...)
}

func (instance *HttpClient) Put(urlString string, body any, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {

    options = append(options[:len(options):len(options)], WithJson(body))

    return instance.Request(nethttp.MethodPut, urlString, options...)
}

func (instance *HttpClient) Patch(urlString string, body any, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {

    options = append(options[:len(options):len(options)], WithJson(body))

    return instance.Request(nethttp.MethodPatch, urlString, options...)
}

func (instance *HttpClient) Delete(urlString string, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {
    return instance.Request(nethttp.MethodDelete, urlString, options...)
}

func (instance *HttpClient) Request(method string, urlString string, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {
    requestConfig, err := applyRequestOptions(options)
    if nil != err {
        return nil, err
    }

    maxResponseBodyBytes := requestConfig.MaxResponseBodyBytes()
    if 0 >= maxResponseBodyBytes {

        return nil, exception.NewError(
            "invalid max response body bytes",
            exceptioncontract.Context{
                "maxResponseBodyBytes": maxResponseBodyBytes,
                "method":               method,
                "url":                  sanitizeUrlForDiagnostics(urlString),
            },
            nil,
        )
    }

    request, err := instance.buildRequest(context.Background(), method, urlString, requestConfig)
    if nil != err {
        return nil, err
    }

    client := instance.clientForRequest(requestConfig.Timeout())

    response, err := client.Do(request)
    if nil != err {
        return nil, newRequestFailedError(method, request.URL, err)
    }
    defer response.Body.Close()

    readLimit := int64(maxResponseBodyBytes)
    if math.MaxInt64 > readLimit {
        readLimit++
    }

    limitedReader := io.LimitReader(response.Body, readLimit)

    body, err := io.ReadAll(limitedReader)
    if nil != err {
        return nil, exception.NewError(
            "failed to read response body",
            exceptioncontract.Context{
                "method":     method,
                "url":        sanitizeUrlForDiagnostics(request.URL.String()),
                "statusCode": response.StatusCode,
            },
            err,
        )
    }

    if maxResponseBodyBytes < len(body) {
        return nil, exception.NewError(
            "response body exceeded max size",
            exceptioncontract.Context{
                "maxResponseBodyBytes": maxResponseBodyBytes,
                "method":               method,
                "url":                  sanitizeUrlForDiagnostics(request.URL.String()),
                "statusCode":           response.StatusCode,
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

/* RequestStream hands the caller a response whose body is still on the wire. The caller OWNS it and must Close it on every path, including the ones that never read it — a status it does not like, an error it returns instead — because the streaming client carries no whole-request deadline: an unclosed stream pins its connection and its descriptor for as long as the process lives. RequestStreamWithContext is the variant that can bound one from the outside. The response body cap bounds the stream exactly as it bounds a buffered body, the inherited default included; a caller streaming more than the default names its own cap through WithMaxResponseBodyBytes. */
func (instance *HttpClient) RequestStream(
    method string,
    urlString string,
    options ...httpclientcontract.RequestOption,
) (httpclientcontract.StreamResponse, error) {
    return instance.RequestStreamWithContext(context.Background(), method, urlString, options...)
}

/* RequestStreamWithContext is RequestStream bound to a context: cancelling it ends the request and the body read, which is the only remedy for a stream a server never ends. The close obligation described on RequestStream is unchanged — cancelling releases the connection, it does not close the StreamResponse for the caller. */
func (instance *HttpClient) RequestStreamWithContext(
    contextInstance context.Context,
    method string,
    urlString string,
    options ...httpclientcontract.RequestOption,
) (httpclientcontract.StreamResponse, error) {
    if nil == contextInstance {
        return nil, exception.NewError("request context is nil", nil, nil)
    }

    requestConfig, err := applyRequestOptions(options)
    if nil != err {
        return nil, err
    }

    if 0 >= requestConfig.MaxResponseBodyBytes() {
        return nil, exception.NewError(
            "invalid max response body bytes",
            exceptioncontract.Context{
                "maxResponseBodyBytes": requestConfig.MaxResponseBodyBytes(),
                "method":               method,
                "url":                  sanitizeUrlForDiagnostics(urlString),
            },
            nil,
        )
    }

    requestInstance, err := instance.buildRequest(contextInstance, method, urlString, requestConfig)
    if nil != err {
        return nil, err
    }

    clientInstance := instance.streamClientForRequest(requestConfig.Timeout())

    response, err := clientInstance.Do(requestInstance)
    if nil != err {
        return nil, newRequestFailedError(method, requestInstance.URL, err)
    }

    body := newLimitedStreamBody(
        response.Body,
        requestConfig.MaxResponseBodyBytes(),
        method,
        sanitizeUrlForDiagnostics(requestInstance.URL.String()),
    )

    return NewStreamResponse(
        response.StatusCode,
        response.Header.Clone(),
        body,
    ), nil
}

func applyRequestOptions(options []httpclientcontract.RequestOption) (*RequestOptions, error) {
    requestConfig := NewRequestOptions()

    for index, applyOption := range options {
        if nil == applyOption {
            return nil, exception.NewError(
                "nil request option",
                exceptioncontract.Context{
                    "index": index,
                },
                nil,
            )
        }

        applyOption(requestConfig)
    }

    return requestConfig, nil
}

func newRequestFailedError(method string, requestUrl *url.URL, err error) error {
    urlForDiagnostics := ""
    if nil != requestUrl {
        urlForDiagnostics = sanitizeUrlForDiagnostics(requestUrl.String())
    }

    cause := err
    if urlErr, ok := err.(*url.Error); true == ok && nil != urlErr.Err {
        cause = urlErr.Err
    }

    return exception.NewError(
        "request failed",
        exceptioncontract.Context{
            "method": method,
            "url":    urlForDiagnostics,
        },
        cause,
    )
}

func (instance *HttpClient) buildRequest(
    contextInstance context.Context,
    method string,
    urlString string,
    requestConfig *RequestOptions,
) (*nethttp.Request, error) {
    fullUrl, err := instance.buildUrl(urlString, requestConfig.Query())
    if nil != err {
        return nil, err
    }

    bodyReader, err := buildRequestBodyReader(requestConfig)
    if nil != err {
        return nil, err
    }

    request, err := nethttp.NewRequestWithContext(contextInstance, method, fullUrl, bodyReader)
    if nil != err {
        return nil, exception.NewError(
            "failed to create request",
            exceptioncontract.Context{
                "method": method,
                "url":    sanitizeUrlForDiagnostics(fullUrl),
            },

            exception.NewError(sanitizeUrlParseError(err), nil, nil),
        )
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

    applyAuthorization(request, requestConfig.Authorization())

    return request, nil
}

func applyAuthorization(request *nethttp.Request, authorization httpclientcontract.AuthorizationOptions) {
    if true == internal.IsNilInterface(authorization) {
        return
    }

    bearer := authorization.Bearer()
    if "" != bearer {
        request.Header.Set("Authorization", "Bearer "+bearer)

        return
    }

    basicAuthorization := authorization.Basic()
    if true == internal.IsNilInterface(basicAuthorization) {
        return
    }

    request.SetBasicAuth(
        basicAuthorization.Username(),
        basicAuthorization.Password(),
    )
}

func buildRequestBodyReader(requestConfig *RequestOptions) (io.Reader, error) {
    body := requestConfig.Body()
    if nil == body {
        return nil, nil
    }

    if "application/json" == requestConfig.ContentType() {
        jsonData, err := json.Marshal(body)
        if nil != err {
            return nil, exception.NewError("failed to marshal json body", nil, err)
        }

        return bytes.NewReader(jsonData), nil
    }

    if stringValue, ok := body.(string); true == ok {
        return strings.NewReader(stringValue), nil
    }

    if data, ok := body.([]byte); true == ok {
        copied := make([]byte, len(data))
        copy(copied, data)

        return bytes.NewReader(copied), nil
    }

    return nil, exception.NewError(
        "unsupported body type",
        exceptioncontract.Context{
            "type": typeNameOf(body),
        },
        nil,
    )
}

func typeNameOf(value any) string {
    reflectedType := reflect.TypeOf(value)
    if nil == reflectedType {
        return "nil"
    }

    return reflectedType.String()
}

/* SetBaseUrl refuses a base whose path lacks its trailing slash, the rule the constructor states: RFC 3986 resolution merges a relative target over the last segment of the base path, so the missing slash silently cuts the segment the caller meant to keep. */
func (instance *HttpClient) SetBaseUrl(baseUrl string) {
    refuseBaseUrlWithoutTrailingSlash(baseUrl)

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.baseUrl = baseUrl
}

/* SetHeader changes a shared default using the canonical header name. Per-request headers belong in request options. */
func (instance *HttpClient) SetHeader(key string, value string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.headers[canonicalHeaderKey(key)] = value
}

func (instance *HttpClient) SetTimeout(timeout time.Duration) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.timeout = timeout
}

/* Close releases the idle connections the client's own transport is holding. Every client builds its own pool — a hundred connections per host by default, kept for ninety seconds — and dropping the last reference to the client releases none of them, because each parked connection has a read loop of its own keeping the transport reachable. Close does not abort requests in flight, and the client stays usable afterwards: it dials again. */
func (instance *HttpClient) Close() error {
    transport, ok := instance.client.Transport.(*nethttp.Transport)
    if false == ok {
        return nil
    }

    transport.CloseIdleConnections()

    return nil
}

func (instance *HttpClient) buildUrl(urlString string, query map[string]string) (string, error) {
    instance.mutex.RLock()
    baseUrl := instance.baseUrl
    instance.mutex.RUnlock()

    parsedTarget, err := url.Parse(urlString)
    if nil != err {
        return "", exception.NewError(
            "failed to parse request url",
            exceptioncontract.Context{
                "url": sanitizeUrlForDiagnostics(urlString),
            },
            exception.NewError(sanitizeUrlParseError(err), nil, nil),
        )
    }

    resolvedUrl := parsedTarget

    if "" != baseUrl {
        parsedBase, baseErr := url.Parse(baseUrl)
        if nil != baseErr {
            return "", exception.NewError(
                "failed to parse the base url",
                exceptioncontract.Context{
                    "baseUrl": sanitizeUrlForDiagnostics(baseUrl),
                },
                exception.NewError(sanitizeUrlParseError(baseErr), nil, nil),
            )
        }

        resolvedUrl = parsedBase.ResolveReference(parsedTarget)

        if false == isSameOrigin(parsedBase, resolvedUrl) {
            return "", exception.NewError(
                "the request url leaves the origin of the configured base url",
                exceptioncontract.Context{
                    "baseUrl": sanitizeUrlForDiagnostics(baseUrl),
                    "url":     sanitizeUrlForDiagnostics(urlString),
                },
                nil,
            )
        }
    } else if false == resolvedUrl.IsAbs() {
        return "", exception.NewError(
            "the request url is relative and the client has no base url",
            exceptioncontract.Context{
                "url": sanitizeUrlForDiagnostics(urlString),
            },
            nil,
        )
    }

    if 0 < len(query) {
        queryValues := resolvedUrl.Query()
        for key, value := range query {
            queryValues.Set(key, value)
        }

        resolvedUrl.RawQuery = queryValues.Encode()
    }

    return resolvedUrl.String(), nil
}

func sanitizeUrlParseError(err error) string {
    if urlErr, ok := err.(*url.Error); true == ok && nil != urlErr.Err {
        return urlErr.Err.Error()
    }

    return err.Error()
}

func (instance *HttpClient) streamClientForRequest(timeout time.Duration) *nethttp.Client {
    if 0 < timeout {
        return instance.clientForRequest(timeout)
    }

    if 0 > timeout {
        return instance.clientForRequest(0)
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
