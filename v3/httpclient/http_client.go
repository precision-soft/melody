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
    if false == config.FollowsRedirects() {
        instance.client.CheckRedirect = answerRedirectAsTheResponse
    }

    return instance
}

/* answerRedirectAsTheResponse is the policy of a client built WithoutRedirects: net/http hands the redirect back unfollowed, body open, so the caller receives the 3xx. */
func answerRedirectAsTheResponse(request *nethttp.Request, via []*nethttp.Request) error {
    return nethttp.ErrUseLastResponse
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

/* sanitizeUrlForDiagnostics strips the userinfo and the query values, where a url carries a secret, and keeps the scheme, host, path and parameter names. A url that does not parse is cut textually. */
func sanitizeUrlForDiagnostics(urlString string) string {
    parsed, err := url.Parse(urlString)
    if nil != err {
        return sanitizeUrlTextually(urlString)
    }

    /* a password holding "/" or "?" ends the authority inside it, so net/url reads its head as the userinfo, or as a host and port when no "@" precedes the cut, and its tail as the path or a query name: such a url keeps only its scheme */
    if true == misreadCredentialTail(parsed) {
        return parsed.Scheme + "://" + internal.RedactedQueryValue
    }

    if nil != parsed.User {
        parsed.User = url.UserPassword(internal.RedactedQueryValue, internal.RedactedQueryValue)
    }

    if "" != parsed.Opaque {
        /* an opaque url keeps its reference in one unparsed span, so net/url finds no userinfo in "http:user:secret@host/path" and only a textual cut reaches the credential */
        parsed.Opaque = redactAuthorityUserinfo(parsed.Opaque, 0)
    }

    if "" != parsed.RawQuery {
        parsed.RawQuery = internal.RedactQueryValuesForDiagnostics(parsed.RawQuery)
    }

    parsed.Fragment = ""
    parsed.RawFragment = ""

    return parsed.String()
}

/* mergeQueryOptions sets the option pairs on a raw query: a pair of the url naming an option key is replaced, every other pair is kept byte for byte, since a round trip through url.Values drops a pair net/url refuses, a bare ";" or a bad escape, and re-encodes the rest */
func mergeQueryOptions(rawQuery string, query map[string]string) string {
    options := url.Values{}
    for key, value := range query {
        options.Set(key, value)
    }

    pairs := make([]string, 0)
    for _, pair := range strings.Split(rawQuery, "&") {
        if "" == pair {
            continue
        }

        name, _, _ := strings.Cut(pair, "=")
        if decodedName, decodeErr := url.QueryUnescape(name); nil == decodeErr {
            if _, replaced := options[decodedName]; true == replaced {
                continue
            }
        }

        pairs = append(pairs, pair)
    }

    pairs = append(pairs, options.Encode())

    return strings.Join(pairs, "&")
}

/* misreadCredentialTail reports an "@" in the path or in a query name of a url whose authority net/url split at a ":", a userinfo or a host and port. A query value may carry an "@" of its own; a url with a port and an "@" in its path loses its diagnostics with the rest, the price of failing closed. */
func misreadCredentialTail(parsed *url.URL) bool {
    if nil == parsed.User && false == strings.Contains(parsed.Host, ":") {
        return false
    }

    if true == strings.Contains(parsed.Path, "@") {
        return true
    }

    for _, pair := range strings.Split(parsed.RawQuery, "&") {
        name, _, _ := strings.Cut(pair, "=")
        if true == strings.Contains(name, "@") {
            return true
        }
    }

    return false
}

/* sanitizeUrlTextually removes the userinfo, the whole query and the fragment from a url net/url refused to parse. The userinfo is cut wherever the reference can carry one, since "//user:secret@host:notaport/path" reaches here with no "://". An "@" past the first "/", "?" or "#" of the authority leaves no trustworthy end to it, a password holding one of the three being the usual cause, so such a url keeps only what precedes its authority. */
func sanitizeUrlTextually(urlString string) string {
    authorityStart, hasAuthority := authorityStartIndex(urlString)
    if true == hasAuthority {
        authorityEnd := strings.IndexAny(urlString[authorityStart:], "/?#")
        if 0 <= authorityEnd && true == strings.Contains(urlString[authorityStart+authorityEnd:], "@") {
            return urlString[:authorityStart] + internal.RedactedQueryValue
        }
    }

    sanitized := urlString

    if referenceEnd := strings.IndexAny(sanitized, "?#"); 0 <= referenceEnd {
        if '?' == sanitized[referenceEnd] {
            sanitized = sanitized[:referenceEnd] + "?" + internal.RedactedQueryValue
        } else {
            sanitized = sanitized[:referenceEnd]
        }
    }

    if false == hasAuthority {
        return sanitized
    }

    return redactAuthorityUserinfo(sanitized, authorityStart)
}

/* authorityStartIndex reports where the region that can hold a userinfo begins: after the "://" of an absolute url, the leading "//" of a scheme-relative reference, or the ":" of an opaque one. The separator is the one that ends a scheme, so a "://" in a relative reference's query or path opens no authority. A relative path has no authority, and an "@" in it belongs to the path. */
func authorityStartIndex(value string) (int, bool) {
    if true == strings.HasPrefix(value, "//") {
        return len("//"), true
    }

    schemeEnd := schemeSeparatorIndex(value)
    if 0 > schemeEnd {
        return 0, false
    }

    if true == strings.HasPrefix(value[schemeEnd:], "://") {
        return schemeEnd + len("://"), true
    }

    return schemeEnd + len(":"), true
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
        internal.RedactedQueryValue + ":" + internal.RedactedQueryValue +
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

/* RequestStream hands the caller a response whose body is still on the wire, which the caller owns and closes on every path, since the streaming client carries no whole-request deadline; RequestStreamWithContext can bound it from outside. The response body cap bounds the stream as it bounds a buffered body, the inherited default included. */
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
    if true == internal.IsNilInterface(contextInstance) {
        return nil, exception.NewError("request context is nil", nil, nil)
    }

    requestConfig, err := applyRequestOptions(options)
    if nil != err {
        return nil, err
    }

    /* the cap is judged before anything is dialled, as on the buffered path */
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

    /* the cap binds every stream, the inherited default included */
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
            "type": internal.StringifyType(body),
        },
        nil,
    )
}

/* SetBaseUrl refuses a base whose path lacks its trailing slash, the rule the constructor states. */
func (instance *HttpClient) SetBaseUrl(baseUrl string) {
    refuseBaseUrlWithoutTrailingSlash(baseUrl)

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

/* buildUrl resolves the target against the base url by RFC 3986 reference resolution: an absolute-path target replaces the base path, a relative one merges over its last segment, an empty one names the base itself, and "//host/x" takes the base scheme. With a base url, a target whose resolved url leaves the base origin is refused, so the configured credentials never travel to a host the target chose; without one, a relative target is refused. */
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
        resolvedUrl.RawQuery = mergeQueryOptions(resolvedUrl.RawQuery, query)
    }

    return resolvedUrl.String(), nil
}

/* sanitizeUrlParseError keeps what net/url says about a refused url and drops the url itself, which its message quotes with userinfo and query. The message also quotes the span it refused, a port or an escape, which can be the head of a password net/url read as a port, an "@" written "%40" included, so every quoted span is redacted. */
func sanitizeUrlParseError(err error) string {
    if urlErr, ok := err.(*url.Error); true == ok && nil != urlErr.Err {
        return redactQuotedSpans(urlErr.Err.Error())
    }

    return err.Error()
}

/* redactQuotedSpans replaces the text between each pair of double quotes, and an unclosed quote's tail, with the redaction */
func redactQuotedSpans(text string) string {
    var redacted strings.Builder

    for {
        quoteStart := strings.Index(text, "\"")
        if 0 > quoteStart {
            redacted.WriteString(text)

            return redacted.String()
        }

        redacted.WriteString(text[:quoteStart+1])
        redacted.WriteString(internal.RedactedQueryValue)

        rest := text[quoteStart+1:]
        quoteEnd := strings.Index(rest, "\"")
        if 0 > quoteEnd {
            return redacted.String()
        }

        redacted.WriteString("\"")
        text = rest[quoteEnd+1:]
    }
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
