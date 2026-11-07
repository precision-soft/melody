package http

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "io"
    "io/fs"
    "net"
    nethttp "net/http"
    "net/netip"
    stdpath "path"
    "reflect"
    "strings"
    "syscall"
    "unicode"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/internal"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    "github.com/precision-soft/melody/v2/session"
    sessioncontract "github.com/precision-soft/melody/v2/session/contract"
)

const sessionCookieSameSiteUnset = nethttp.SameSite(0)

func wrapControllerWithContainer(
    controller any,
) httpcontract.Handler {
    controllerValue := reflect.ValueOf(controller)
    controllerType := controllerValue.Type()

    if reflect.Func != controllerType.Kind() {
        exception.Panic(
            exception.NewError(
                "controller must be a function",
                exceptioncontract.Context{"type": controllerType.Kind().String()},
                nil,
            ),
        )
    }

    if controllerType.NumIn() < 1 {
        exception.Panic(
            exception.NewError(
                "controller must have at least one argument",
                exceptioncontract.Context{
                    "expected": "(*Request)",
                },
                nil,
            ),
        )
    }

    firstParamType := controllerType.In(0)
    requestContractType := reflect.TypeOf((*httpcontract.Request)(nil)).Elem()
    if false == firstParamType.Implements(requestContractType) {
        exception.Panic(
            exception.NewError(
                "first controller argument must implement http request contract",
                exceptioncontract.Context{
                    "type":     controllerType.Kind().String(),
                    "expected": requestContractType.String(),
                    "actual":   firstParamType.String(),
                },
                nil,
            ),
        )
    }

    if 2 != controllerType.NumOut() {
        exception.Panic(
            exception.NewError(
                "controller must return response",
                exceptioncontract.Context{
                    "expected": "(*Response, error)",
                },
                nil,
            ),
        )
    }

    firstReturnType := controllerType.Out(0)
    responseContractType := reflect.TypeOf((*httpcontract.Response)(nil)).Elem()
    if false == firstReturnType.Implements(responseContractType) {
        exception.Panic(
            exception.NewError(
                "controller must return response contract as first result",
                exceptioncontract.Context{
                    "controllerType": controllerType.String(),
                    "expected":       responseContractType.String(),
                    "actual":         firstReturnType.String(),
                },
                nil,
            ),
        )
    }

    errorInterfaceType := reflect.TypeOf((*error)(nil)).Elem()
    if controllerType.Out(1) != errorInterfaceType {
        exception.Panic(
            exception.NewError(
                "controller must return error as second result",
                exceptioncontract.Context{
                    "controllerType": controllerType.String(),
                },
                nil,
            ),
        )
    }

    return func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        if nil == runtimeInstance {
            return nil, exception.NewError(
                "runtime instance is nil in controller handler",
                nil,
                nil,
            )
        }

        arguments := make([]reflect.Value, controllerType.NumIn())
        arguments[0] = reflect.ValueOf(request)

        runtimeInterfaceType := reflect.TypeOf((*runtimecontract.Runtime)(nil)).Elem()

        for index := 1; index < controllerType.NumIn(); index++ {
            paramType := controllerType.In(index)

            if runtimeInterfaceType == paramType {
                arguments[index] = reflect.ValueOf(runtimeInstance)
                continue
            }

            dependency, err := runtimeInstance.Scope().GetByType(paramType)
            if nil != err {
                return nil, err
            }

            arguments[index] = reflect.ValueOf(dependency)
        }

        results := controllerValue.Call(arguments)

        responseValue := results[0]
        errorInterface := results[1].Interface()

        var response httpcontract.Response
        if true == internal.CanReflectValueBeNil(responseValue) {
            if false == responseValue.IsNil() {
                response = responseValue.Interface().(httpcontract.Response)
            }
        } else {
            response = responseValue.Interface().(httpcontract.Response)
        }

        var err error
        if nil != errorInterface {
            err = errorInterface.(error)
        }

        if nil == response {
            return nil, err
        }

        return response, err
    }
}

func wrapWithMiddlewares(handler httpcontract.Handler, middlewares []httpcontract.Middleware) httpcontract.Handler {
    return wrapWithMiddlewaresRecording(handler, middlewares, nil)
}

/* wrapWithMiddlewaresRecording publishes each next() response, so the recovery defer can close it on an outer panic */
func wrapWithMiddlewaresRecording(
    handler httpcontract.Handler,
    middlewares []httpcontract.Middleware,
    recordReturnedResponse func(httpcontract.Response),
) httpcontract.Handler {
    wrapped := handler
    for index := len(middlewares) - 1; 0 <= index; index-- {
        if nil != recordReturnedResponse {
            wrapped = recordResponseAfterHandler(wrapped, recordReturnedResponse)
        }

        wrapped = middlewares[index](wrapped)
    }

    return wrapped
}

func recordResponseAfterHandler(
    handler httpcontract.Handler,
    recordReturnedResponse func(httpcontract.Response),
) httpcontract.Handler {
    return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        response, err := handler(runtimeInstance, writer, request)
        recordReturnedResponse(response)

        return response, err
    }
}

func splitPath(value string) []string {
    return splitNormalizedPath(strings.TrimSpace(value))
}

/* a request path is never trimmed: whitespace is significant to whatever sits in front of the application */
func splitRequestPath(value string) []string {
    return splitNormalizedPath(value)
}

/* requestPathCarriesEncodedSeparator reads URL.RawPath: URL.EscapedPath can re-escape the path and lose the separator */
func requestPathCarriesEncodedSeparator(rawPath string) bool {
    return true == strings.Contains(strings.ToLower(rawPath), "%2f")
}

func splitNormalizedPath(value string) []string {
    if "" == value {
        return []string{""}
    }

    normalizedPath := value
    if false == strings.HasPrefix(normalizedPath, "/") {
        normalizedPath = "/" + normalizedPath
    }

    if 1 < len(normalizedPath) {
        normalizedPath = strings.TrimRight(normalizedPath, "/")
        if "" == normalizedPath {
            normalizedPath = "/"
        }
    }

    return strings.Split(normalizedPath, "/")
}

/* requestPathIsCanonical keeps path consumers on one spelling: a fold could route to one handler, authorize on another */
func requestPathIsCanonical(path string) bool {
    /* refused ahead of the "/" test: the router routes " /public" as its own segment, the access-control matcher trims it */
    if "" != path && strings.TrimLeftFunc(path, unicode.IsSpace) != path {
        return false
    }

    if "" == path {
        return true
    }

    /* judged with "/" prepended, the spelling the router and matchers read: "admin/../public" routes and authorizes apart */
    if false == strings.HasPrefix(path, "/") {
        path = "/" + path
    }

    /* refused here because the router and the access-control matcher read whitespace apart; the trim stays in the matcher */
    if strings.TrimSpace(path) != path {
        return false
    }

    trimmedPath := path
    if 1 < len(trimmedPath) {
        trimmedPath = strings.TrimRight(trimmedPath, "/")
        if "" == trimmedPath {
            trimmedPath = "/"
        }
    }

    return stdpath.Clean(trimmedPath) == trimmedPath
}

func writeResponse(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    writer nethttp.ResponseWriter,
    response httpcontract.Response,
    sessionManager sessioncontract.Manager,
    sessionInstance sessioncontract.Session,
    forwardedHeadersPolicy httpcontract.ForwardedHeadersPolicy,
    sessionCookiePolicy httpcontract.SessionCookiePolicy,
) httpcontract.Response {
    if true == internal.IsNilInterface(response) {
        response = &Response{
            statusCode: nethttp.StatusNoContent,
            headers:    make(nethttp.Header),
            bodyReader: nil,
        }
    }

    if statusCode := response.StatusCode(); 0 != statusCode && (100 > statusCode || 999 < statusCode) {
        logger := logging.LoggerFromRuntime(runtimeInstance)
        if nil != logger {
            logger.Error(
                "response status code is out of range; answering an internal server error",
                loggingcontract.Context{
                    "statusCode": statusCode,
                },
            )
        }

        /* the replaced response owns its body reader and nothing else reads it: a file response would leak its *os.File */
        closeDiscardedResponseBody(response, logger)

        response = renderErrorResponse(runtimeInstance, request, nethttp.StatusInternalServerError, "internal server error", nil)
    }

    /* persist at most once: panic recovery re-enters writeResponse; the header-commit flag cannot gate it, streams persist too */
    persistenceRecorder, isPersistenceRecorder := writer.(sessionPersistenceRecorder)
    sessionAlreadyPersisted := true == isPersistenceRecorder && true == persistenceRecorder.SessionPersisted()

    recorder, isRecorder := writer.(headerCommitRecorder)
    responseIsDiscarded := true == isRecorder && true == recorder.HeadersWritten()

    sessionInstance = republishedSession(request, sessionInstance)

    if false == sessionAlreadyPersisted && false == internal.IsNilInterface(sessionManager) && false == internal.IsNilInterface(sessionInstance) {
        sessionPersistFailed := false

        /* one snapshot decides both branches, so a concurrent Clear cannot make the cookie contradict the persistence branch */
        _, sessionModified, sessionCleared := sessionInstance.Snapshot()

        if true == sessionCleared {
            if true == requestNamesSession(request, sessionInstance.Id()) {
                if err := sessionManager.DeleteSession(sessionInstance.Id()); nil != err {
                    /* a failed delete still expires the browser cookie: that can only end a session, unlike a failed save, which suppresses it */
                    sessionPersistFailed = true

                    logSessionPersistenceEvent(runtimeInstance, loggingcontract.LevelError, "failed to delete session", err, sessionInstance.Id(), request)
                }
            }

            cookie := &nethttp.Cookie{
                Name:     session.SessionCookieName,
                Value:    "",
                Path:     resolveSessionCookiePath(sessionCookiePolicy),
                Domain:   sessionCookiePolicy.Domain,
                HttpOnly: true,
                SameSite: resolveSessionCookieSameSite(sessionCookiePolicy),
                Secure:   resolveSessionCookieSecure(request, forwardedHeadersPolicy, sessionCookiePolicy),
                MaxAge:   -1,
            }

            SetCookie(response, cookie)
        } else if true == sessionModified {
            /* a discarded response carries no Set-Cookie, so a session the client does not hold is not stored: it is unreachable */
            if true == responseIsDiscarded && false == requestNamesSession(request, sessionInstance.Id()) {
                sessionPersistFailed = true

                logger := logging.LoggerFromRuntime(runtimeInstance)
                if nil != logger {
                    logSessionPersistenceEvent(
                        runtimeInstance,
                        loggingcontract.LevelWarning,
                        "session not persisted: the response was already committed and the request does not name this session",
                        nil,
                        sessionInstance.Id(),
                        request,
                    )
                }
            } else {
                err := sessionManager.SaveSession(sessionInstance)
                if true == exception.ChainIs(err, session.ErrSessionDeleted) {
                    /* the session ended during this request: the write is refused so it cannot be re-created; the response is served unchanged */
                    sessionPersistFailed = true

                    logSessionPersistenceEvent(runtimeInstance, loggingcontract.LevelWarning, "session was deleted while the request was in flight", err, sessionInstance.Id(), request)

                    SetCookie(
                        response,
                        &nethttp.Cookie{
                            Name:     session.SessionCookieName,
                            Value:    "",
                            Path:     resolveSessionCookiePath(sessionCookiePolicy),
                            Domain:   sessionCookiePolicy.Domain,
                            HttpOnly: true,
                            SameSite: resolveSessionCookieSameSite(sessionCookiePolicy),
                            Secure:   resolveSessionCookieSecure(request, forwardedHeadersPolicy, sessionCookiePolicy),
                            MaxAge:   -1,
                        },
                    )
                } else if nil != err {
                    /* a save outage answers 500 unless headers are committed; the cookie is suppressed so no id points at nothing */
                    sessionPersistFailed = true

                    logSessionPersistenceEvent(runtimeInstance, loggingcontract.LevelError, "failed to save session", err, sessionInstance.Id(), request)

                    closeDiscardedResponseBody(response, logging.LoggerFromRuntime(runtimeInstance))

                    response = EmptyResponse(nethttp.StatusInternalServerError)
                } else {
                    cookie := &nethttp.Cookie{
                        Name:     session.SessionCookieName,
                        Value:    sessionInstance.Id(),
                        Path:     resolveSessionCookiePath(sessionCookiePolicy),
                        Domain:   sessionCookiePolicy.Domain,
                        HttpOnly: true,
                        SameSite: resolveSessionCookieSameSite(sessionCookiePolicy),
                        Secure:   resolveSessionCookieSecure(request, forwardedHeadersPolicy, sessionCookiePolicy),
                    }

                    SetCookie(response, cookie)
                    markResponsePrivateForSessionCookie(response)
                }
            }
        }

        if true == isPersistenceRecorder && false == sessionPersistFailed {
            persistenceRecorder.MarkSessionPersisted()
        }
    }

    if true == responseIsDiscarded {
        closeDiscardedResponseBody(response, logging.LoggerFromRuntime(runtimeInstance))

        if statusRecorder, isStatusRecorder := writer.(committedStatusRecorder); true == isStatusRecorder {
            if committedStatus := statusRecorder.CommittedStatusCode(); 0 < committedStatus && committedStatus != response.StatusCode() {
                return EmptyResponse(committedStatus)
            }
        }

        return response
    }

    err := WriteToHttpResponseWriter(runtimeInstance, request, writer, response)
    if nil != err {
        /* the headers may already be committed, and a panic would escape ServeHttp on the recovery path: log and return */
        logger := logging.LoggerFromRuntime(runtimeInstance)
        if nil != logger {
            writeLogContext := exceptioncontract.Context{}
            if false == internal.IsNilInterface(request) && nil != request.HttpRequest() {
                writeLogContext["method"] = internal.BoundDiagnosticText(request.HttpRequest().Method)
                writeLogContext["path"] = internal.BoundDiagnosticText(request.HttpRequest().URL.Path)
            }

            if true == isClientAbortWriteError(request, err) {
                logger.Warning(
                    "failed to write response; client disconnected",
                    exception.LogContext(err, writeLogContext),
                )
            } else {
                logger.Error(
                    "failed to write response",
                    exception.LogContext(err, writeLogContext),
                )
            }
        }
    }

    return response
}

/* closeResponseBodySafely contains a panicking Close: inside the recovery defer a second panic would reset the connection */
func closeResponseBodySafely(closer io.Closer) (closeErr error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        closeErr = exception.NewError(
            "response body close panicked",
            exceptioncontract.Context{
                "value": fmt.Sprintf("%v", recoveredValue),
            },
            nil,
        )
    }()

    return closer.Close()
}

/* a deadline or a cause-carrying cancellation the application set is not the client's doing: the failure stays server-side */
func isClientAbortWriteError(request httpcontract.Request, err error) bool {
    if true == exception.ChainIs(err, syscall.EPIPE) || true == exception.ChainIs(err, syscall.ECONNRESET) {
        return true
    }

    if true == internal.IsNilInterface(request) || nil == request.HttpRequest() {
        return false
    }

    return context.Canceled == context.Cause(request.HttpRequest().Context())
}

/* republishedSession prefers the handler's session, so the rotated id, the session-fixation defence, reaches the store */
func republishedSession(
    request httpcontract.Request,
    capturedSession sessioncontract.Session,
) sessioncontract.Session {
    publishedSession := sessionFromRequestAttribute(request)
    if nil == publishedSession {
        return capturedSession
    }

    return publishedSession
}

func sessionFromRequestAttribute(request httpcontract.Request) sessioncontract.Session {
    if true == internal.IsNilInterface(request) {
        return nil
    }

    attributes := request.Attributes()
    if true == internal.IsNilInterface(attributes) {
        return nil
    }

    attributeValue, exists := attributes.Get(RequestAttributeSession)
    if false == exists {
        return nil
    }

    publishedSession, isSession := attributeValue.(sessioncontract.Session)
    if false == isSession || true == internal.IsNilInterface(publishedSession) {
        return nil
    }

    return publishedSession
}

func requestNamesSession(request httpcontract.Request, sessionId string) bool {
    if true == internal.IsNilInterface(request) || "" == sessionId {
        return false
    }

    httpRequest := request.HttpRequest()
    if nil == httpRequest {
        return false
    }

    cookie, cookieErr := httpRequest.Cookie(session.SessionCookieName)
    if nil != cookieErr || nil == cookie {
        return false
    }

    return sessionId == cookie.Value
}

func resolveSessionCookiePath(sessionCookiePolicy httpcontract.SessionCookiePolicy) string {
    if "" == sessionCookiePolicy.Path {
        return "/"
    }

    return sessionCookiePolicy.Path
}

/* the zero SameSite reads as unset and takes the framework default; SameSiteDefaultMode asks for no attribute on purpose */
func resolveSessionCookieSameSite(sessionCookiePolicy httpcontract.SessionCookiePolicy) nethttp.SameSite {
    if sessionCookieSameSiteUnset == sessionCookiePolicy.SameSite {
        return nethttp.SameSiteLaxMode
    }

    return sessionCookiePolicy.SameSite
}

func resolveSessionCookieSecure(
    request httpcontract.Request,
    forwardedHeadersPolicy httpcontract.ForwardedHeadersPolicy,
    sessionCookiePolicy httpcontract.SessionCookiePolicy,
) bool {
    if httpcontract.SessionCookieSecureAlways == sessionCookiePolicy.Secure {
        return true
    }

    if httpcontract.SessionCookieSecureNever == sessionCookiePolicy.Secure {
        return false
    }

    /* every current browser drops a SameSite=None cookie that is not Secure, so under FromScheme it is Secure on any scheme */
    if nethttp.SameSiteNoneMode == resolveSessionCookieSameSite(sessionCookiePolicy) {
        return true
    }

    if true == internal.IsNilInterface(request) {
        return false
    }

    return "https" == detectSchemeWithForwardedHeadersPolicy(request.HttpRequest(), forwardedHeadersPolicy)
}

func logSessionPersistenceEvent(
    runtimeInstance runtimecontract.Runtime,
    level loggingcontract.Level,
    message string,
    err error,
    sessionId string,
    request httpcontract.Request,
) {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    recordContext := exceptioncontract.Context{}

    if "" != sessionId {
        /* the id is a live bearer credential: never log it verbatim, only this one-way reference */
        recordContext["sessionRef"] = sessionIdLogReference(sessionId)
    }

    if false == internal.IsNilInterface(request) && nil != request.HttpRequest() {
        recordContext["method"] = internal.BoundDiagnosticText(request.HttpRequest().Method)
        recordContext["path"] = internal.BoundDiagnosticText(request.HttpRequest().URL.Path)
    }

    if loggingcontract.LevelWarning == level {
        logger.Warning(message, exception.LogContext(err, recordContext))

        return
    }

    logger.Error(message, exception.LogContext(err, recordContext))
}

/* markResponsePrivateForSessionCookie keeps the session id out of a shared cache; a private browser cache still may store */
func markResponsePrivateForSessionCookie(response httpcontract.Response) {
    if true == internal.IsNilInterface(response) {
        return
    }

    headers := response.Headers()
    if nil == headers {
        return
    }

    existingLines := headers.Values("Cache-Control")
    if 0 == len(existingLines) {
        headers.Set("Cache-Control", "private")

        return
    }

    /* a line ending inside a quoted string would swallow an appended "private", so the strictest answer replaces the header */
    for _, existing := range existingLines {
        if true == endsInsideQuotes(existing) {
            headers.Set("Cache-Control", "no-store, private")

            return
        }
    }

    rebuilt := make([]string, 0)
    hasPrivate := false
    hasNoStore := false

    for _, existing := range existingLines {
        /* a quoted field-name list, no-cache="X-One, Public, X-Two", must not be cut by a bare comma split */
        tokens, _ := internal.SplitOutsideQuotes(existing, ',')
        for _, token := range tokens {
            trimmed := strings.TrimSpace(token)
            if "" == trimmed {
                continue
            }

            lower := strings.ToLower(trimmed)
            if "public" == lower {
                continue
            }
            if "private" == lower {
                hasPrivate = true
            }
            if "no-store" == lower {
                hasNoStore = true
            }

            rebuilt = append(rebuilt, trimmed)
        }
    }

    if false == hasPrivate && false == hasNoStore {
        rebuilt = append(rebuilt, "private")
    }

    headers.Set("Cache-Control", strings.Join(rebuilt, ", "))
}

func endsInsideQuotes(value string) bool {
    insideQuotes := false
    escaped := false

    for index := 0; index < len(value); index++ {
        character := value[index]

        if true == escaped {
            escaped = false

            continue
        }

        if true == insideQuotes && '\\' == character {
            escaped = true

            continue
        }

        if '"' == character {
            insideQuotes = false == insideQuotes
        }
    }

    return insideQuotes
}

func sessionIdLogReference(sessionId string) string {
    digest := sha256.Sum256([]byte(sessionId))

    return hex.EncodeToString(digest[:])[:16]
}

func closeDiscardedResponseBody(response httpcontract.Response, logger loggingcontract.Logger) {
    /* read through, not compared: in the recovery defer a typed nil dereferenced below is a second panic after recover */
    if true == internal.IsNilInterface(response) {
        return
    }

    bodyReader := response.BodyReader()
    if nil == bodyReader {
        return
    }

    closer, ok := bodyReader.(io.Closer)
    if false == ok {
        return
    }

    closeErr := closeResponseBodySafely(closer)
    if nil == closeErr || nil == logger {
        return
    }

    if true == exception.ChainIs(closeErr, fs.ErrClosed) {
        return
    }

    logger.Error(
        "failed to close discarded response body",
        exception.LogContext(closeErr),
    )
}

func detectScheme(request *nethttp.Request) string {
    return detectSchemeWithForwardedHeadersPolicy(
        request,
        httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      nil,
        },
    )
}

/* a trusted edge must SET X-Forwarded-Proto: if it appends, the client's own spelling is leftmost and picks the scheme */
func detectSchemeWithForwardedHeadersPolicy(request *nethttp.Request, policy httpcontract.ForwardedHeadersPolicy) string {
    if nil == request {
        return "http"
    }

    if nil != request.TLS {
        return "https"
    }

    if false == policy.TrustForwardedHeaders {
        return "http"
    }

    if 0 == len(policy.TrustedProxyList) {
        return "http"
    }

    if false == isRequestFromTrustedProxy(request, policy.TrustedProxyList) {
        return "http"
    }

    forwardedProto := request.Header.Get("X-Forwarded-Proto")
    if "" != forwardedProto {
        /* a chain of proxies arrives as "https, http": the client-facing hop is the leftmost entry */
        if commaIndex := strings.IndexByte(forwardedProto, ','); -1 != commaIndex {
            forwardedProto = forwardedProto[:commaIndex]
        }

        return strings.ToLower(strings.TrimSpace(forwardedProto))
    }

    return "http"
}

func isRequestFromTrustedProxy(request *nethttp.Request, trustedProxyList []string) bool {
    if nil == request {
        return false
    }

    remoteAddressString := strings.TrimSpace(request.RemoteAddr)
    if "" == remoteAddressString {
        return false
    }

    remoteHostString := remoteAddressString
    hostFromSplit, _, splitErr := net.SplitHostPort(remoteAddressString)
    if nil == splitErr && "" != strings.TrimSpace(hostFromSplit) {
        remoteHostString = hostFromSplit
    }

    remoteAddress, remoteAddressErr := netip.ParseAddr(remoteHostString)
    if nil != remoteAddressErr {
        return false
    }

    remoteAddress = remoteAddress.Unmap()

    for _, trustedProxyString := range trustedProxyList {
        trimmedTrustedProxyString := strings.TrimSpace(trustedProxyString)
        if "" == trimmedTrustedProxyString {
            continue
        }

        trustedPrefix, trustedPrefixErr := netip.ParsePrefix(trimmedTrustedProxyString)
        if nil == trustedPrefixErr {
            /* a mapped prefix names the IPv4 range it embeds: unmapped, since netip treats the two address families as unequal */
            if true == trustedPrefix.Addr().Is4In6() && trustedPrefix.Bits() >= 96 {
                trustedPrefix = netip.PrefixFrom(trustedPrefix.Addr().Unmap(), trustedPrefix.Bits()-96)
            }

            if true == trustedPrefix.Contains(remoteAddress) {
                return true
            }

            continue
        }

        trustedAddress, trustedAddressErr := netip.ParseAddr(trimmedTrustedProxyString)
        if nil != trustedAddressErr {
            continue
        }

        if trustedAddress.Unmap() == remoteAddress {
            return true
        }
    }

    return false
}

func matchesMethod(methods []string, method string) bool {
    if 0 == len(methods) {
        return true
    }

    normalizedMethod := strings.ToUpper(strings.TrimSpace(method))

    for _, allowedMethod := range methods {
        normalizedAllowedMethod := strings.ToUpper(strings.TrimSpace(allowedMethod))

        if normalizedAllowedMethod == normalizedMethod {
            return true
        }
    }

    return false
}

func matchesHost(expectedHost string, actualHost string) bool {
    if "" == expectedHost {
        return true
    }

    return expectedHost == actualHost
}

func matchesScheme(schemes []string, scheme string) bool {
    if 0 == len(schemes) {
        return true
    }

    for _, allowedScheme := range schemes {
        if strings.EqualFold(allowedScheme, scheme) {
            return true
        }
    }

    return false
}

func matchesLocale(locales []string, params map[string]string) bool {
    if 0 == len(locales) {
        return true
    }

    localeValue := ""
    if value, exists := params[RouteAttributeLocale]; true == exists {
        localeValue = value
    }

    if "" == localeValue {
        return false
    }

    for _, allowedLocale := range locales {
        if allowedLocale == localeValue {
            return true
        }
    }

    return false
}

func matchPath(
    routeDefinition route,
    pathSegments []string,
) (map[string]string, bool) {
    patternSegments := routeDefinition.parts
    params := make(map[string]string)

    pathIndex := 0
    patternIndex := 0

    for patternIndex < len(patternSegments) {
        routePart := patternSegments[patternIndex]
        isLastPattern := patternIndex == len(patternSegments)-1

        if true == strings.HasPrefix(routePart, "*") {
            wildcardName := strings.TrimPrefix(routePart, "*")
            isCatchAll := false
            if true == strings.HasSuffix(wildcardName, "...") {
                isCatchAll = true
                wildcardName = strings.TrimSuffix(wildcardName, "...")
            }

            if true == isLastPattern {
                isCatchAll = true
            }

            if true == isCatchAll {
                rest := ""
                if len(pathSegments) > pathIndex {
                    rest = strings.Join(pathSegments[pathIndex:], "/")
                }

                if "" != wildcardName {
                    if regex, exists := routeDefinition.requirements[wildcardName]; true == exists {
                        if false == regex.MatchString(rest) {
                            return nil, false
                        }
                    }

                    params[wildcardName] = rest
                    if RouteAttributeLocale == wildcardName {
                        params[RouteAttributeLocale] = rest
                    }
                }

                return params, true
            }

            if pathIndex >= len(pathSegments) {
                return nil, false
            }

            pathPart := pathSegments[pathIndex]
            if "" != wildcardName {
                if regex, exists := routeDefinition.requirements[wildcardName]; true == exists {
                    if false == regex.MatchString(pathPart) {
                        return nil, false
                    }
                }

                params[wildcardName] = pathPart
                if RouteAttributeLocale == wildcardName {
                    params[RouteAttributeLocale] = pathPart
                }
            }

            pathIndex++
            patternIndex++

            continue
        }

        if pathIndex >= len(pathSegments) {
            if true == strings.HasPrefix(routePart, ":") {
                paramName := strings.TrimPrefix(routePart, ":")
                if true == strings.HasSuffix(paramName, "?") {
                    paramName = strings.TrimSuffix(paramName, "?")

                    patternIndex++

                    continue
                }
            }

            return nil, false
        }

        pathPart := pathSegments[pathIndex]

        if true == strings.HasPrefix(routePart, ":") {
            if "" == pathPart {
                /* "/" splits into two empty segments: refusing this trailing optional would 404 a url UrlGenerator mints */
                if true == isLastPattern && pathIndex == len(pathSegments)-1 && true == strings.HasSuffix(routePart, "?") {
                    pathIndex++
                    patternIndex++

                    continue
                }

                return nil, false
            }

            paramName := strings.TrimPrefix(routePart, ":")
            if true == strings.HasSuffix(paramName, "?") {
                paramName = strings.TrimSuffix(paramName, "?")
            }

            if regex, exists := routeDefinition.requirements[paramName]; true == exists {
                if false == regex.MatchString(pathPart) {
                    return nil, false
                }
            }

            params[paramName] = pathPart

            if RouteAttributeLocale == paramName {
                params[RouteAttributeLocale] = pathPart
            }

            pathIndex++
            patternIndex++

            continue
        }

        if routePart != pathPart {
            return nil, false
        }

        pathIndex++
        patternIndex++
    }

    if pathIndex != len(pathSegments) {
        return nil, false
    }

    return params, true
}
