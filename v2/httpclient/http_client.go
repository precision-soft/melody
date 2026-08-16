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

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    httpclientcontract "github.com/precision-soft/melody/v2/httpclient/contract"
    "github.com/precision-soft/melody/v2/internal"
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

/* hasAbsoluteUrlScheme reports whether the target names its own host. Schemes are case-insensitive, so an "HTTP://" spelling — which a url harvested from a document or a Location header legitimately carries — names the same absolute reference as "http://" and must not be read as a path to hang under the base url. */
func hasAbsoluteUrlScheme(urlString string) bool {
    lowerCased := strings.ToLower(urlString)

    return true == strings.HasPrefix(lowerCased, "http://") ||
        true == strings.HasPrefix(lowerCased, "https://")
}

/* sanitizeUrlForDiagnostics strips the two places a url carries a secret — the userinfo and the query values — while keeping everything that makes a failure diagnosable: the scheme, the host, the path and the parameter names. A url a caller built by hand may not parse at all, which is exactly the failure being reported, so the textual fallback cuts the same two regions without a parser. */
func sanitizeUrlForDiagnostics(urlString string) string {
    parsed, err := url.Parse(urlString)
    if nil != err {
        return sanitizeUrlTextually(urlString)
    }

    if nil != parsed.User {
        parsed.User = url.UserPassword(redactedValue, redactedValue)
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

/* sanitizeUrlTextually removes the userinfo and the whole query from a url net/url refused to parse. */
func sanitizeUrlTextually(urlString string) string {
    sanitized := urlString

    if queryStart := strings.Index(sanitized, "?"); 0 <= queryStart {
        sanitized = sanitized[:queryStart] + "?" + redactedValue
    }

    schemeEnd := strings.Index(sanitized, "://")
    if 0 > schemeEnd {
        return sanitized
    }

    authorityStart := schemeEnd + len("://")

    authorityEnd := strings.Index(sanitized[authorityStart:], "/")
    if 0 > authorityEnd {
        authorityEnd = len(sanitized)
    } else {
        authorityEnd += authorityStart
    }

    userinfoEnd := strings.LastIndex(sanitized[authorityStart:authorityEnd], "@")
    if 0 > userinfoEnd {
        return sanitized
    }

    return sanitized[:authorityStart] +
        redactedValue + ":" + redactedValue +
        sanitized[authorityStart+userinfoEnd:]
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
        /* the cap is known before anything is dialled, and it used to be read after the exchange: a POST that had already committed its side effect answered with an error phrased as though nothing had been sent, and a caller retrying on it duplicated the operation. */
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

    /* the +1 lets ReadAll observe one byte past the cap so an over-long body is detected; saturate instead of wrapping, because int64(math.MaxInt)+1 is negative and LimitReader would then read nothing and return an empty body with no error */
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

/* RequestStream hands the caller a response whose body is still on the wire. The caller OWNS it and must Close it on every path, including the ones that never read it — a status it does not like, an error it returns instead — because the streaming client carries no whole-request deadline: an unclosed stream pins its connection and its descriptor for as long as the process lives. RequestStreamWithContext is the variant that can bound one from the outside. The response body cap applies only when the caller asked for one; the default belongs to Request, which holds the whole body in memory. */
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

    /* the explicit cap is judged before anything is dialled, the rule the buffered path states: read after the exchange, a POST that had already committed its side effect answered with an error phrased as though nothing had been sent, and a caller retrying on it duplicated the operation */
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

/* applyRequestOptions folds the caller's options onto a fresh option set. A nil option is refused rather than skipped: an option chosen by a condition whose other branch produced nothing is a wiring mistake, and calling it would be a nil function call on the request path, outside any recovery this package owns. */
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

/* newRequestFailedError reports a failed exchange without the url net/http embeds in its own error text. A *url.Error carries the whole request url — query string included — and the cause chain is rendered into the log record, so a token passed through WithQuery, or a password spelled in the userinfo, would be written out by the most ordinary failure there is: a refused connection. The inner error keeps the diagnosis; the sanitized url sits beside it in the context. */
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

/* buildRequest turns the caller's options into a net/http request: the url, the body, the headers of the client and of the request, and the authorization. The caller's context is bound here rather than by the caller afterwards, because the per-request credential header names are planted in the request's context and a WithContext applied later replaces the whole context, taking the plant with it — the redirect policy would then find nothing to strip and a per-request credential would follow a cross-origin redirect. */
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
            /* net/url's parse error quotes the url it was handed, userinfo included, so it cannot travel as the cause; what it adds beyond the message is which character it refused. */
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

/* applyAuthorization writes the credential the caller asked for. A bearer token wins over a basic credential when both are set — the two cannot share one Authorization header. Basic travels whenever it was asked for, empty halves included: an api key spelled as the password of an empty user is the ordinary shape of "-u :key", and dropping it silently sent the request unauthenticated with nothing to say so. */
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

/* buildRequestBodyReader wraps the caller's body. A []byte is copied because net/http writes the request body on its own goroutine and Client.Do returns as soon as the response headers arrive: a server that answers without draining the body leaves the transport reading the caller's slice after the call returned, so a pooled buffer reused right after a request is a data race and torn bytes on the wire. */
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

/* typeNameOf names the type a value carries, so a body the client cannot encode says which type it was handed. */
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

/* SetHeader stores the header under its canonical spelling, the one the constructor stores under: the map is applied to every request with Set, which canonicalizes, so rotating a credential under a different spelling than the one it was configured with used to leave two live entries whose survivor was chosen by map iteration order — a different credential per request. Canonicalizing here makes the collision structurally impossible, and a rotation overwrites the entry it means to. */
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

/* buildUrl resolves the caller's target against the configured base url. The base url is a PREFIX here, deliberately unlike RFC 3986 reference resolution — which Symfony and Guzzle implement, and where an absolute path replaces the base path entirely, so a base of "https://host/v1" and a target of "/users" would name "https://host/users". An empty target names the base resource itself, with nothing appended.

An absolute target names its own host, so the base url does not apply to it. When the client HAS a base url, a target leaving that origin is refused instead: the headers and the authorization this client was configured with would otherwise travel to a host the target string chose — the very leak the redirect policy exists to stop, one hop earlier. A caller that talks to more than one origin builds a client without a base url. */
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

/* sanitizeUrlParseError keeps what net/url says about a url it refused while dropping the url itself, which its message quotes in full — userinfo and query included. */
func sanitizeUrlParseError(err error) string {
    if urlErr, ok := err.(*url.Error); true == ok && nil != urlErr.Err {
        return urlErr.Err.Error()
    }

    return err.Error()
}

/* streamClientForRequest drops the whole-request Timeout for the streaming path. nethttp.Client.Timeout bounds everything up to and including the body read, so a long-lived stream (server-sent events, a log tail, a large download) is force-closed mid-read the moment the client timeout elapses — the streaming API is unusable beyond it. The header phase stays bounded by the transport (DialTimeout, TLSHandshakeTimeout, ResponseHeaderTimeout); the body's lifetime belongs to the caller, who closes it, or to a context the caller attaches to the request. An explicit per-request timeout is still honored, because a caller that asks for one on a stream is asking to bound the stream. */
func (instance *HttpClient) streamClientForRequest(timeout time.Duration) *nethttp.Client {
    if 0 < timeout {
        return instance.clientForRequest(timeout)
    }

    /* a negative timeout is not a request to run forever: every other guard in this package reads a non-positive duration as unset, and a caller computing what is left of a deadline that has already passed would otherwise get an unbounded stream out of an exhausted budget. */
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
