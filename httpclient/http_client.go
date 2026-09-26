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

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    httpclientcontract "github.com/precision-soft/melody/httpclient/contract"
    "github.com/precision-soft/melody/internal"
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

/* NewHttpClient builds a client over its own net/http transport and idle connection pool: hold the client while the application calls the service, and close it when done, rather than building one per call. A nil configuration panics; NewDefaultHttpClient asks for the defaults. */
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

    return instance
}

/* defaultMaxRedirects mirrors net/http's own cap; it is stated here because the client installs its own policy. */
const defaultMaxRedirects = 10

/* requestCredentialHeadersKeyType keys the per-request credential header names on the request context, the only channel that reaches a redirect the client itself creates. */
type requestCredentialHeadersKeyType struct{}

var requestCredentialHeadersKey = requestCredentialHeadersKeyType{}

/* credentialStrippingRedirectPolicy keeps net/http's ten-redirect cap and removes every credential the client attaches once the redirect leaves the original origin, a scheme downgrade included; net/http strips only Authorization, WWW-Authenticate and Cookie, and only across domains. It is a method, not a closure over the header map, since net/http runs it on the request goroutine while SetHeader may be writing that map. */
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

    /* net/http fills Referer with the full previous url, query included, and keeps it on a cross-origin hop, so a secret passed through WithQuery would reach the redirect target */
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

/* isSameOrigin compares the scheme, the case-insensitive host and the effective port, so "https://host" and "https://host:443" are one origin. */
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

/* hasAbsoluteUrlScheme reports whether the target names its own host. Schemes are case-insensitive, so an "HTTP://" target is an absolute reference as "http://" is and is never hung under the base url. */
func hasAbsoluteUrlScheme(urlString string) bool {
    lowerCased := strings.ToLower(urlString)

    return true == strings.HasPrefix(lowerCased, "http://") ||
        true == strings.HasPrefix(lowerCased, "https://")
}

/* sanitizeUrlForDiagnostics strips the userinfo and the query values, where a url carries a secret, and keeps the scheme, host, path and parameter names. A url that does not parse is cut textually. */
func sanitizeUrlForDiagnostics(urlString string) string {
    parsed, err := url.Parse(urlString)
    if nil != err {
        return sanitizeUrlTextually(urlString)
    }

    if nil != parsed.User {
        parsed.User = url.UserPassword(redactedValue, redactedValue)
    }

    if "" != parsed.Opaque {
        /* an opaque url keeps its reference in one unparsed span, so net/url finds no userinfo in "http:user:secret@host/path" and only a textual cut reaches the credential */
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

/* sanitizeUrlTextually removes the userinfo and the whole query from a url net/url refused to parse. The userinfo is cut wherever the reference can carry one, since "//user:secret@host:notaport/path" reaches here with no "://". */
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

/* authorityStartIndex reports where the region that can hold a userinfo begins: after the "://" of an absolute url, the leading "//" of a scheme-relative reference, or the ":" of an opaque one. A relative path has no authority, and an "@" in it belongs to the path. */
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

/* schemeSeparatorIndex reports the index of the ":" closing a scheme at the head of the value, or -1, under net/url's grammar: a letter, then letters, digits, "+", "-" and ".". */
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

/* redactAuthorityUserinfo replaces the userinfo of the authority at authorityStart with the redacted pair. The authority ends at the first path separator, so an "@" in the path is left alone. */
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
    requestConfig, err := applyRequestOptions(options)
    if nil != err {
        return nil, err
    }

    maxResponseBodyBytes := requestConfig.MaxResponseBodyBytes()
    if 0 >= maxResponseBodyBytes {
        /* the cap is judged before anything is dialled, so a refused body never follows a committed side effect a caller would retry */
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

    /* the +1 lets ReadAll see one byte past the cap; it saturates, since int64(math.MaxInt)+1 is negative and LimitReader would then read nothing */
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

/* RequestStream hands the caller a response whose body is still on the wire, which the caller owns and closes on every path, since the streaming client carries no whole-request deadline; RequestStreamWithContext can bound it from outside. The response body cap applies only when the caller named one, since the default is sized for Request, which holds the whole body in memory. */
func (instance *HttpClient) RequestStream(
    method string,
    urlString string,
    options ...httpclientcontract.RequestOption,
) (httpclientcontract.StreamResponse, error) {
    return instance.RequestStreamWithContext(context.Background(), method, urlString, options...)
}

/* RequestStreamWithContext is RequestStream bound to a context: cancelling it ends the request and the body read. The caller still closes the StreamResponse. */
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

    /* the cap is judged before anything is dialled, as on the buffered path */
    if true == requestConfig.hasExplicitMaxResponseBodyBytes() && 0 >= requestConfig.MaxResponseBodyBytes() {
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

    body := response.Body

    if true == requestConfig.hasExplicitMaxResponseBodyBytes() {
        maxResponseBodyBytes := requestConfig.MaxResponseBodyBytes()

        body = newLimitedStreamBody(
            response.Body,
            maxResponseBodyBytes,
            method,
            sanitizeUrlForDiagnostics(requestInstance.URL.String()),
        )
    }

    return NewStreamResponse(
        response.StatusCode,
        response.Header.Clone(),
        body,
    ), nil
}

/* applyRequestOptions folds the caller's options onto a fresh option set. A nil option is refused, since calling it would panic on the request path; a refusal an option kept, such as SetHeaders on a colliding map, fails the request under the index of that option. */
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

        if refusal := requestConfig.refusal; nil != refusal {
            return nil, exception.NewError(
                "request option refused",
                exceptioncontract.Context{
                    "index": index,
                },
                refusal,
            )
        }
    }

    return requestConfig, nil
}

/* newRequestFailedError reports a failed exchange without the url net/http embeds in its error, which carries the query and userinfo into the logged cause chain. The *url.Error stays in the chain for errors.As, carrying the sanitized url, and the sanitized url sits in the context too. */
func newRequestFailedError(method string, requestUrl *url.URL, err error) error {
    urlForDiagnostics := ""
    if nil != requestUrl {
        urlForDiagnostics = sanitizeUrlForDiagnostics(requestUrl.String())
    }

    /* the link is rebuilt whatever its Err holds, so no shape of *url.Error reaches the record unsanitized */
    cause := err
    if urlErr, ok := err.(*url.Error); true == ok {
        cause = &url.Error{Op: urlErr.Op, URL: sanitizeUrlForDiagnostics(urlErr.URL), Err: urlErr.Err}
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

/* buildRequest turns the caller's options into a net/http request. The caller's context is bound here, since the per-request credential header names are planted in the request context and a later WithContext would drop them from the redirect policy's reach. */
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
            /* net/url's parse error quotes the url, userinfo included, so it cannot travel as the cause */
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

/* applyAuthorization writes the credential the caller asked for: a bearer token wins over basic, and basic travels whenever asked for, empty halves included. It runs after every header door, so a typed credential wins over an Authorization header from a header map. */
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

/* buildRequestBodyReader wraps the caller's body. A []byte is copied, since net/http may still read the body on its own goroutine after Client.Do returns, and a pooled buffer reused then would be a data race. */
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

/* typeNameOf names the type a value carries, so a body the client cannot encode names its type. */
func typeNameOf(value any) string {
    reflectedType := reflect.TypeOf(value)
    if nil == reflectedType {
        return "nil"
    }

    return reflectedType.String()
}

func (instance *HttpClient) SetBaseUrl(baseUrl string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.baseUrl = baseUrl
}

/* SetHeader stores the header under its canonical spelling, as the constructor does, so a rotated credential overwrites the entry it means to. */
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

/* Close releases the idle connections of the client's own transport, which dropping the last reference does not. It does not abort requests in flight, and the client stays usable afterwards. */
func (instance *HttpClient) Close() error {
    transport, ok := instance.client.Transport.(*nethttp.Transport)
    if false == ok {
        return nil
    }

    transport.CloseIdleConnections()

    return nil
}

/* buildUrl resolves the target against the base url as a PREFIX, unlike RFC 3986 resolution: a base of "https://host/v1" and a target of "/users" name "https://host/v1/users", and an empty target names the base itself. An absolute target names its own host; with a base url, one leaving the base origin is refused, so the configured credentials never travel to a host the target chose. A caller talking to more than one origin builds a client without a base url. */
func (instance *HttpClient) buildUrl(urlString string, query map[string]string) (string, error) {
    instance.mutex.RLock()
    baseUrl := instance.baseUrl
    instance.mutex.RUnlock()

    if "" != baseUrl {
        if true == hasAbsoluteUrlScheme(urlString) {
            if err := refuseForeignOrigin(baseUrl, urlString); nil != err {
                return "", err
            }
        } else if "" == urlString {
            urlString = strings.TrimSuffix(baseUrl, "/")
        } else {
            urlString = strings.TrimSuffix(baseUrl, "/") + "/" + strings.TrimPrefix(urlString, "/")
        }
    }

    if 0 == len(query) {
        return urlString, nil
    }

    parsedUrl, err := url.Parse(urlString)
    if nil != err {
        return "", exception.NewError(
            "failed to parse request url",
            exceptioncontract.Context{
                "url": sanitizeUrlForDiagnostics(urlString),
            },
            exception.NewError(sanitizeUrlParseError(err), nil, nil),
        )
    }

    queryValues := parsedUrl.Query()
    for key, value := range query {
        queryValues.Set(key, value)
    }

    parsedUrl.RawQuery = queryValues.Encode()

    return parsedUrl.String(), nil
}

/* refuseForeignOrigin rejects an absolute target that leaves the origin of the configured base url. */
func refuseForeignOrigin(baseUrl string, urlString string) error {
    parsedBase, baseErr := url.Parse(baseUrl)
    if nil != baseErr {
        return exception.NewError(
            "failed to parse the base url",
            exceptioncontract.Context{
                "baseUrl": sanitizeUrlForDiagnostics(baseUrl),
            },
            exception.NewError(sanitizeUrlParseError(baseErr), nil, nil),
        )
    }

    parsedTarget, targetErr := url.Parse(urlString)
    if nil != targetErr {
        return exception.NewError(
            "failed to parse request url",
            exceptioncontract.Context{
                "url": sanitizeUrlForDiagnostics(urlString),
            },
            exception.NewError(sanitizeUrlParseError(targetErr), nil, nil),
        )
    }

    if true == isSameOrigin(parsedBase, parsedTarget) {
        return nil
    }

    return exception.NewError(
        "the absolute url leaves the origin of the configured base url",
        exceptioncontract.Context{
            "baseUrl": sanitizeUrlForDiagnostics(baseUrl),
            "url":     sanitizeUrlForDiagnostics(urlString),
        },
        nil,
    )
}

/* sanitizeUrlParseError keeps what net/url says about a refused url and drops the url itself, which its message quotes with userinfo and query. */
func sanitizeUrlParseError(err error) string {
    if urlErr, ok := err.(*url.Error); true == ok && nil != urlErr.Err {
        return urlErr.Err.Error()
    }

    return err.Error()
}

/* streamClientForRequest drops the whole-request Timeout for the streaming path, since it would force-close a long-lived body mid-read. The header phase stays bounded by the transport, the body belongs to the caller or its context, and an explicit per-request timeout is still honored. */
func (instance *HttpClient) streamClientForRequest(timeout time.Duration) *nethttp.Client {
    if 0 < timeout {
        return instance.clientForRequest(timeout)
    }

    /* a negative timeout reads as unset, as everywhere in this package, so an exhausted budget cannot yield an unbounded stream */
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
