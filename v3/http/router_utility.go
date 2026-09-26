package http

import (
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "fmt"
    "io"
    "io/fs"
    "net"
    nethttp "net/http"
    "net/netip"
    "net/url"
    stdpath "path"
    "reflect"
    "strings"
    "syscall"
    "unicode"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/session"
    sessioncontract "github.com/precision-soft/melody/v3/session/contract"
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

/* wrapWithMiddlewaresRecording publishes the response each middleware's next() returns to the recorder as the stack unwinds, so the kernel's recovery can close a response in flight when an outer middleware panics after next() returned. A nil recorder builds the plain chain. */
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

/* a request path is never trimmed, since whitespace is significant to a proxy rule in front of the application. It is split on the separators the client sent and each segment is unescaped on its own, so an encoded separator stays inside its segment; a segment whose escaping is malformed is kept as sent. */
func splitRequestPath(value string) []string {
    segments := splitNormalizedPath(value)

    for index, segment := range segments {
        unescapedSegment, unescapeErr := url.PathUnescape(segment)
        if nil != unescapeErr {
            continue
        }

        segments[index] = unescapedSegment
    }

    return segments
}

/* RequestPathAsRouted answers the request path spelled the way the router reads it, for every other consumer of the path to read the same: each segment unescaped on its own, with a separator encoded inside a segment kept as "%2F". A segment carrying "%252F" and one carrying "%2F" share that spelling, so a rule written with "%2F" names both. A target that does not begin with "/" is returned as it came. */
func RequestPathAsRouted(escapedPath string) string {
    if false == strings.HasPrefix(escapedPath, "/") {
        return escapedPath
    }

    segments := strings.Split(escapedPath, "/")
    for index, segment := range segments {
        unescapedSegment, unescapeErr := url.PathUnescape(segment)
        if nil != unescapeErr {
            continue
        }

        segments[index] = strings.ReplaceAll(unescapedSegment, "/", "%2F")
    }

    return strings.Join(segments, "/")
}

func splitNormalizedPath(value string) []string {
    /* an empty path is the root; answered as no segment at all, the walk would miss a route registered as "/" */
    if "" == value {
        value = "/"
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

/* requestPathIsCanonical reports whether a request path is spelled the one way the router, the firewall matcher and the access-control matcher read alike. A path that folds through "..", "." or "//", or that carries leading or trailing whitespace, would route to one handler and be authorized against another rule, so it is refused. A trailing slash is normalized away rather than refused, and a target that does not begin with "/" is left to the router unless it begins with whitespace. */
func requestPathIsCanonical(path string) bool {
    /* a path that begins with whitespace is refused before the "/" test below would pass it as not path-routed */
    if "" != path && strings.TrimLeftFunc(path, unicode.IsSpace) != path {
        return false
    }

    if false == strings.HasPrefix(path, "/") {
        return true
    }

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

/* writeResponse persists the session, emits the session cookie and writes the response, and returns the response it actually wrote, which the caller publishes. A session-storage outage on save replaces the response with a fresh empty 500 that carries neither the original headers nor the session cookie. */
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

    /* a status outside net/http's [100, 999] would panic inside WriteHeader after the commit flag is raised, so it is refused here and answered as the rendered 500; zero means 200 */
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

        /* the replaced response owns its body reader and nothing downstream reads it, so it is closed here */
        closeDiscardedResponseBody(response, logger)

        response = renderErrorResponse(runtimeInstance, request, nethttp.StatusInternalServerError, "internal server error", nil)
    }

    /* the session is persisted at most once per request: the recovery path can re-enter writeResponse after the first call committed it, and SaveSession does not reset the modified flag */
    persistenceRecorder, isPersistenceRecorder := writer.(sessionPersistenceRecorder)
    sessionAlreadyPersisted := true == isPersistenceRecorder && true == persistenceRecorder.SessionPersisted()

    recorder, isRecorder := writer.(headerCommitRecorder)
    responseIsDiscarded := true == isRecorder && true == recorder.HeadersWritten()

    sessionInstance = republishedSession(request, sessionInstance)

    if false == sessionAlreadyPersisted && false == internal.IsNilInterface(sessionManager) && false == internal.IsNilInterface(sessionInstance) {
        sessionPersistFailed := false

        /* one snapshot decides both branches, so a concurrent Clear cannot land between the two flag reads */
        _, sessionModified, sessionCleared := sessionInstance.Snapshot()

        if true == sessionCleared {
            if err := sessionManager.DeleteSession(sessionInstance.Id()); nil != err {
                /* a failed delete on logout is logged and still expires the cookie, which can only end a session; a failed save, by contrast, suppresses the cookie */
                sessionPersistFailed = true

                logSessionPersistenceEvent(runtimeInstance, loggingcontract.LevelError, "failed to delete session", err, sessionInstance.Id(), request)
            }

            setSessionCookie(response, expiringSessionCookie(request, forwardedHeadersPolicy, sessionCookiePolicy))
        } else if true == sessionModified {
            /* a discarded response carries no Set-Cookie, so a session the client does not already hold is not stored: nothing could reference it */
            if true == responseIsDiscarded && false == requestNamesSession(request, sessionInstance.Id()) {
                /* the drop is logged, since a handler that rotated the session on a committed response loses the previous entry and the replacement here */
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
                if true == errors.Is(err, session.ErrSessionRotated) {
                    /* another request rotated this id away: the write is refused so the retired id is not re-created, and the cookie is left alone, since the rotating request hands the client the new id */
                    sessionPersistFailed = true

                    logSessionPersistenceEvent(runtimeInstance, loggingcontract.LevelWarning, "session was rotated away while the request was in flight", err, sessionInstance.Id(), request)
                } else if true == errors.Is(err, session.ErrSessionDeleted) {
                    /* another request ended the session: the write is refused so it is not re-created, the cookie is expired, and the handler's response is served unchanged */
                    sessionPersistFailed = true

                    logSessionPersistenceEvent(runtimeInstance, loggingcontract.LevelWarning, "session was deleted while the request was in flight", err, sessionInstance.Id(), request)

                    setSessionCookie(response, expiringSessionCookie(request, forwardedHeadersPolicy, sessionCookiePolicy))
                } else if nil != err {
                    /* a storage outage on save answers 500 in place of the handler's response, which assumed the write would land; the cookie is suppressed so the browser never holds an id nothing persisted */
                    sessionPersistFailed = true

                    logSessionPersistenceEvent(runtimeInstance, loggingcontract.LevelError, "failed to save session", err, sessionInstance.Id(), request)

                    closeDiscardedResponseBody(response, logging.LoggerFromRuntime(runtimeInstance))

                    response = EmptyResponse(nethttp.StatusInternalServerError)
                } else {
                    setSessionCookie(response, sessionCookie(request, forwardedHeadersPolicy, sessionCookiePolicy, sessionInstance.Id()))
                }
            }
        }

        if true == isPersistenceRecorder && false == sessionPersistFailed {
            persistenceRecorder.MarkSessionPersisted()
        }
    }

    /* a handler that committed its own response, a stream, is not written over */
    if true == responseIsDiscarded {
        closeDiscardedResponseBody(response, logging.LoggerFromRuntime(runtimeInstance))

        /* for a stream the status on the connection is the one reported to the terminate event and the access log; a hijacked connection records none and keeps the substitute */
        if statusRecorder, isStatusRecorder := writer.(committedStatusRecorder); true == isStatusRecorder {
            if committedStatus := statusRecorder.CommittedStatusCode(); 0 < committedStatus && committedStatus != response.StatusCode() {
                return EmptyResponse(committedStatus)
            }
        }

        return response
    }

    err := WriteToHttpResponseWriter(runtimeInstance, request, writer, response)
    if nil != err {
        /* a failed write cannot produce a better response and a panic here would escape ServeHttp, so it is logged: a client abort at warning, anything else at error */
        logger := logging.LoggerFromRuntime(runtimeInstance)
        if nil != logger {
            writeLogContext := exceptioncontract.Context{}
            if false == internal.IsNilInterface(request) && nil != request.HttpRequest() {
                writeLogContext["method"] = request.HttpRequest().Method
                writeLogContext["path"] = request.HttpRequest().URL.Path
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

/* closeResponseBodySafely contains a Close that panics, since it runs inside the kernel's recovery defer where a second panic would reset the connection. */
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

func isClientAbortWriteError(request httpcontract.Request, err error) bool {
    if false == internal.IsNilInterface(request) && nil != request.HttpRequest() && nil != request.HttpRequest().Context().Err() {
        return true
    }

    return true == errors.Is(err, syscall.EPIPE) || true == errors.Is(err, syscall.ECONNRESET)
}

/* republishedSession prefers the session a handler published on the request over the one captured before routing, so a rotated id reaches the store and the Set-Cookie. */
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

func sessionCookie(
    request httpcontract.Request,
    forwardedHeadersPolicy httpcontract.ForwardedHeadersPolicy,
    sessionCookiePolicy httpcontract.SessionCookiePolicy,
    sessionId string,
) *nethttp.Cookie {
    return &nethttp.Cookie{
        Name:     session.SessionCookieName,
        Value:    sessionId,
        Path:     resolveSessionCookiePath(sessionCookiePolicy),
        Domain:   sessionCookiePolicy.Domain,
        HttpOnly: true,
        SameSite: resolveSessionCookieSameSite(sessionCookiePolicy),
        Secure:   resolveSessionCookieSecure(request, forwardedHeadersPolicy, sessionCookiePolicy),
    }
}

func expiringSessionCookie(
    request httpcontract.Request,
    forwardedHeadersPolicy httpcontract.ForwardedHeadersPolicy,
    sessionCookiePolicy httpcontract.SessionCookiePolicy,
) *nethttp.Cookie {
    cookie := sessionCookie(request, forwardedHeadersPolicy, sessionCookiePolicy, "")
    cookie.MaxAge = -1

    return cookie
}

/* setSessionCookie writes the session cookie and marks the response private, since a shared cache replaying it would hand one client's session, or its ending, to another. */
func setSessionCookie(response httpcontract.Response, cookie *nethttp.Cookie) {
    SetCookie(response, cookie)
    markResponsePrivateForSessionCookie(response)
}

func resolveSessionCookiePath(sessionCookiePolicy httpcontract.SessionCookiePolicy) string {
    if "" == sessionCookiePolicy.Path {
        return "/"
    }

    return sessionCookiePolicy.Path
}

/* the zero SameSite emits no attribute in net/http, so it reads as unset and falls back to the framework default; SameSiteDefaultMode asks for no attribute on purpose. */
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

    if true == internal.IsNilInterface(request) {
        return false
    }

    return "https" == detectSchemeWithForwardedHeadersPolicy(request.HttpRequest(), forwardedHeadersPolicy)
}

/* logSessionPersistenceEvent logs through the named level methods, the door a substituted logger overrides. A session another request ended is not a failure and is logged at the level the caller passes, below the storage outages. */
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
        /* the id is a live bearer credential, so only a one-way reference is logged */
        recordContext["sessionRef"] = sessionIdLogReference(sessionId)
    }

    if false == internal.IsNilInterface(request) && nil != request.HttpRequest() {
        recordContext["method"] = request.HttpRequest().Method
        recordContext["path"] = request.HttpRequest().URL.Path
    }

    if loggingcontract.LevelWarning == level {
        logger.Warning(message, exception.LogContext(err, recordContext))

        return
    }

    logger.Error(message, exception.LogContext(err, recordContext))
}

func markResponsePrivateForSessionCookie(response httpcontract.Response) {
    if true == internal.IsNilInterface(response) {
        return
    }

    headers := response.Headers()
    if nil == headers {
        return
    }

    /* Cache-Control may span several field lines and the Set below replaces them all, so every line is read */
    existingLines := headers.Values("Cache-Control")
    if 0 == len(existingLines) {
        headers.Set("Cache-Control", "private")

        return
    }

    rebuilt := make([]string, 0)
    hasPrivate := false
    hasNoStore := false

    for _, existing := range existingLines {
        /* a directive may carry a quoted field-name list, so the value is not cut on a bare comma; the cut past the member cap is not read, since the value merged is the application's own Cache-Control, not a client's negotiation */
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

/* sessionIdLogReference answers a truncated SHA-256 of a session id: enough to correlate one session's records, not enough to present as a cookie. */
func sessionIdLogReference(sessionId string) string {
    digest := sha256.Sum256([]byte(sessionId))

    return hex.EncodeToString(digest[:])[:16]
}

func closeDiscardedResponseBody(response httpcontract.Response, logger loggingcontract.Logger) {
    /* the interface is read through, not compared: a typed nil dereferenced here, inside the recovery defer, would panic past recover */
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

    /* the writer's own deferred Close may have closed the body already; a second close of an os.File reports it and is not logged */
    if true == errors.Is(closeErr, fs.ErrClosed) {
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

/* detectSchemeWithForwardedHeadersPolicy believes X-Forwarded-Proto only from a trusted proxy and reads its leftmost entry, so a trusted edge must overwrite the header rather than append to it. */
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
        /* proxies append, so the client-facing hop is the leftmost entry */
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

    /* an IPv4-mapped IPv6 peer matches an IPv4 CIDR, as in http/middleware/client_ip.go */
    remoteAddress = remoteAddress.Unmap()

    for _, trustedProxyString := range trustedProxyList {
        trimmedTrustedProxyString := strings.TrimSpace(trustedProxyString)
        if "" == trimmedTrustedProxyString {
            continue
        }

        trustedPrefix, trustedPrefixErr := netip.ParsePrefix(trimmedTrustedProxyString)
        if nil == trustedPrefixErr {
            /* a mapped prefix is unmapped so it contains the unmapped host, as in http/middleware/client_ip.go */
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

/* matchesHost compares host names case-insensitively, and the port only when the route named one. Whether it did is read with net.SplitHostPort, so the colons of a bracketed IPv6 literal are not a port. */
func matchesHost(expectedHost string, actualHost string) bool {
    if "" == expectedHost {
        return true
    }

    if true == strings.EqualFold(expectedHost, actualHost) {
        return true
    }

    expectedHostWithoutPort, expectedNamedPort := splitHostAndPort(expectedHost)

    if true == expectedNamedPort {
        return false
    }

    actualHostWithoutPort, _ := splitHostAndPort(actualHost)

    return strings.EqualFold(expectedHostWithoutPort, actualHostWithoutPort)
}

/* splitHostAndPort answers the bare host, unbracketed, and whether a port was present, so both sides of a comparison have one shape. */
func splitHostAndPort(hostValue string) (string, bool) {
    hostWithoutPort, _, splitErr := net.SplitHostPort(hostValue)
    if nil == splitErr {
        return hostWithoutPort, true
    }

    if true == strings.HasPrefix(hostValue, "[") && true == strings.HasSuffix(hostValue, "]") {
        return hostValue[1 : len(hostValue)-1], false
    }

    return hostValue, false
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

/* matchesLocale is read by the matcher and by AllowedMethods alike. A route declaring no locales accepts every one; a route declaring some refuses a path carrying none. */
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

/* joinCatchAllSegments re-escapes a separator inside a segment, so "/files/a%2Fb/c" and "/files/a/b/c" bind different tails; every other escape stays decoded, so a literal "%2F" sent as "%252F" binds the same tail as an encoded separator, the non-injective spelling RequestPathAsRouted shares. */
func joinCatchAllSegments(pathSegments []string) string {
    escapedSegments := make([]string, 0, len(pathSegments))

    for _, segment := range pathSegments {
        escapedSegments = append(escapedSegments, strings.ReplaceAll(segment, "/", "%2F"))
    }

    return strings.Join(escapedSegments, "/")
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
                    rest = joinCatchAllSegments(pathSegments[pathIndex:])
                }

                if "" != wildcardName {
                    /* a requirement on a catch-all is enforced like the other branches enforce theirs */
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
            /* an empty segment does not satisfy a named parameter: "/users//profile" would otherwise bind an empty id that a handler cannot tell from a supplied one */
            if "" == pathPart {
                /* an optional reached through the root lands on the second of the two empty segments "/" splits into and is left unbound, since UrlGenerator mints exactly "/" for this shape */
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

/* requestPathIsRoutable reports whether the target is origin-form: the asterisk-form of OPTIONS and an authority-form CONNECT are not path-routed, so "*" never binds a parameter. */
func requestPathIsRoutable(path string) bool {
    if "" == path {
        return true
    }

    return strings.HasPrefix(path, "/")
}
