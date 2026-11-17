package http

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
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
    "time"
    "unicode"

    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

const sessionCookieSameSiteUnset = nethttp.SameSite(0)

/* ControllerHandler adapts a controller function, its arguments resolved by type, to a Handler; another shape panics. */
func ControllerHandler(
    controller any,
) httpcontract.Handler {
    controllerValue := reflect.ValueOf(controller)
    if false == controllerValue.IsValid() {
        exception.Panic(
            exception.NewError(
                "controller must be a function",
                exceptioncontract.Context{"type": "nil"},
                nil,
            ),
        )
    }

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

    if true == controllerValue.IsNil() {
        exception.Panic(
            exception.NewError(
                "controller is a nil function",
                exceptioncontract.Context{"type": controllerType.String()},
                nil,
            ),
        )
    }

    if controllerType.NumIn() < 1 {
        exception.Panic(
            exception.NewError(
                "controller must have at least one argument",
                exceptioncontract.Context{
                    "type":          controllerType.String(),
                    "expected":      "(*Request)",
                    "argumentCount": controllerType.NumIn(),
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
                    "type":     controllerType.String(),
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
                "controller must return two results, a response and an error",
                exceptioncontract.Context{
                    "type":        controllerType.String(),
                    "expected":    "(Response, error)",
                    "resultCount": controllerType.NumOut(),
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
                    "type":           controllerType.String(),
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
                    "type":           controllerType.String(),
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

            dependency, err := runtimeInstance.Scope().GetByType(runtimeInstance.Context(), paramType)
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

/* never trimmed: whitespace is significant to a proxy rule in front of the application */
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

/* RequestPathAsRouted is the path as the router reads it, an encoded separator kept as %2F. */
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
    /* an empty path is the root, or the walk would miss a route registered as "/" */
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

func requestPathIsCanonical(path string) bool {
    /* refused before the "/" test below would pass it as not path-routed */
    if "" != path && strings.TrimLeftFunc(path, unicode.IsSpace) != path {
        return false
    }

    if "" == path {
        return true
    }

    /* judged as both matchers read it, with a "/" prepended and folded */
    if false == strings.HasPrefix(path, "/") {
        path = "/" + path
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

func writeResponse(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    writer nethttp.ResponseWriter,
    response httpcontract.Response,
    sessionManager sessioncontract.Manager,
    forwardedHeadersPolicy httpcontract.ForwardedHeadersPolicy,
    sessionCookiePolicy httpcontract.SessionCookiePolicy,
    unavailableRetryAfter time.Duration,
) httpcontract.Response {
    if true == internal.IsNilInterface(response) {
        response = &Response{
            statusCode: nethttp.StatusNoContent,
            headers:    make(nethttp.Header),
            bodyReader: nil,
        }
    }

    /* outside net/http's range or informational would panic or answer an implicit 200; zero means 200 */
    if statusCode := response.StatusCode(); 0 != statusCode && false == isFinalStatusCode(statusCode) {
        logger := logging.LoggerFromRuntime(runtimeInstance)
        if nil != logger {
            logger.Error(
                "response status code is not a final status; answering an internal server error",
                loggingcontract.Context{
                    "statusCode": statusCode,
                },
            )
        }

        closeDiscardedResponseBody(response, logger)

        response = renderErrorResponse(runtimeInstance, request, nethttp.StatusInternalServerError, "internal server error", nil)
    }

    /* at most once: the recovery path can re-enter after the first call, and SaveSession does not reset the modified flag */
    persistenceRecorder, isPersistenceRecorder := writer.(sessionPersistenceRecorder)
    sessionAlreadyPersisted := true == isPersistenceRecorder && true == persistenceRecorder.SessionPersisted()

    recorder, isRecorder := writer.(headerCommitRecorder)
    responseIsDiscarded := true == isRecorder && true == recorder.HeadersWritten()

    /* a request served the unavailable session leaves the storage and the cookie alone */
    sessionInstance := effectiveRequestSession(request)
    if _, isUnavailable := session.SessionUnavailableCause(sessionInstance); true == isUnavailable {
        sessionInstance = nil
    }

    if false == sessionAlreadyPersisted && false == internal.IsNilInterface(sessionManager) && false == internal.IsNilInterface(sessionInstance) {
        sessionPersistFailed := false

        /* released by the defer too, so a panicking foreign door leaves no timer armed */
        releasePersistenceContext := func() {}
        defer func() { releasePersistenceContext() }()

        openPersistenceContext := func() (context.Context, error) {
            persistenceContext, release, persistenceContextErr := sessionPersistenceContext(runtimeInstance, request, sessionManager)
            releasePersistenceContext = release

            return persistenceContext, persistenceContextErr
        }

        /* one snapshot decides both branches, so a concurrent Clear cannot land between the flag reads */
        _, sessionModified, sessionCleared := session.Snapshot(sessionInstance)

        if true == sessionCleared {
            /* a session the cookie did not name has no entry to remove and no copy to bury */
            if true == requestNamesSession(request, sessionInstance.Id()) {
                persistenceContext, err := openPersistenceContext()
                if nil == err {
                    err = sessionManager.DeleteSession(persistenceContext, sessionInstance.Id())
                }

                if nil != err {
                    /* a failed delete still expires the cookie, which can only end a session */
                    sessionPersistFailed = true

                    logSessionPersistenceEvent(runtimeInstance, loggingcontract.LevelError, "failed to delete session", err, sessionInstance.Id(), request)
                }
            }

            setSessionCookie(response, expiringSessionCookie(request, forwardedHeadersPolicy, sessionCookiePolicy))
        } else if true == sessionModified {
            /* a discarded response carries no Set-Cookie, so nothing could reference a session stored for it */
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
                persistenceContext, err := openPersistenceContext()
                if nil == err {
                    err = sessionManager.SaveSession(persistenceContext, sessionInstance)
                }

                if true == exception.ChainIs(err, session.ErrSessionRotated) {
                    /* the cookie is left alone: the rotating request hands the client the new id */
                    sessionPersistFailed = true

                    logSessionPersistenceEvent(runtimeInstance, loggingcontract.LevelWarning, "session was rotated away while the request was in flight", err, sessionInstance.Id(), request)
                } else if true == exception.ChainIs(err, session.ErrSessionDeleted) {
                    sessionPersistFailed = true

                    logSessionPersistenceEvent(runtimeInstance, loggingcontract.LevelWarning, "session was deleted while the request was in flight", err, sessionInstance.Id(), request)

                    setSessionCookie(response, expiringSessionCookie(request, forwardedHeadersPolicy, sessionCookiePolicy))
                } else if nil != err {
                    /* the response assumed the save would land, so a failure answers 503 unless committed */
                    sessionPersistFailed = true

                    if false == exception.AnswersUnavailable(err) {
                        err = exception.MarkInfrastructureFailure(err)
                    }

                    logSessionPersistenceEvent(runtimeInstance, loggingcontract.LevelError, "failed to save session", err, sessionInstance.Id(), request)

                    closeDiscardedResponseBody(response, logging.LoggerFromRuntime(runtimeInstance))

                    response = EmptyResponse(nethttp.StatusServiceUnavailable)

                    if 0 >= unavailableRetryAfter {
                        unavailableRetryAfter = config.DefaultUnavailableRetryAfter
                    }

                    SetRetryAfter(response, unavailableRetryAfter)
                } else {
                    setSessionCookie(response, sessionCookie(request, forwardedHeadersPolicy, sessionCookiePolicy, sessionInstance.Id()))
                }
            }
        }

        /* released before the body is written, so the persistence timer does not outlive the persistence */
        releasePersistenceContext()

        if true == isPersistenceRecorder && false == sessionPersistFailed {
            persistenceRecorder.MarkSessionPersisted()
        }
    }

    if true == responseIsDiscarded {
        closeDiscardedResponseBody(response, logging.LoggerFromRuntime(runtimeInstance))

        /* a hijacked connection records no status and keeps the substitute */
        if statusRecorder, isStatusRecorder := writer.(committedStatusRecorder); true == isStatusRecorder {
            if committedStatus := statusRecorder.CommittedStatusCode(); 0 < committedStatus && committedStatus != response.StatusCode() {
                return EmptyResponse(committedStatus)
            }
        }

        return response
    }

    err := WriteToHttpResponseWriter(runtimeInstance, request, writer, response)
    if nil != err {
        /* logged: a panic here would escape ServeHttp */
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
                    exception.LogContext(err, exception.LogContextConfig{Extra: writeLogContext}),
                )
            } else {
                logger.Error(
                    "failed to write response",
                    exception.LogContext(err, exception.LogContextConfig{Extra: writeLogContext}),
                )
            }
        }
    }

    return response
}

/* a second panic inside the kernel's recovery defer would reset the connection */
func closeResponseBodySafely(closer io.Closer) (closeErr error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        closeErr = exception.NewError(
            "response body close panicked",
            exceptioncontract.Context{
                "value": internal.DescribeRecoveredValue(recoveredValue),
            },
            nil,
        )
    }()

    return closer.Close()
}

func isClientAbortWriteError(request httpcontract.Request, err error) bool {
    if true == exception.ChainIs(err, syscall.EPIPE) || true == exception.ChainIs(err, syscall.ECONNRESET) {
        return true
    }

    if true == internal.IsNilInterface(request) || nil == request.HttpRequest() {
        return false
    }

    return context.Canceled == context.Cause(request.HttpRequest().Context())
}

/* the request's values without its cancellation, so a client that hung up still has its logout recorded */
func sessionPersistenceContext(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    sessionManager sessioncontract.Manager,
) (context.Context, context.CancelFunc, error) {
    timeout, timeoutErr := session.ResolvePersistenceTimeout(sessionManager)
    if nil != timeoutErr {
        return context.Background(), func() {}, timeoutErr
    }

    detached := context.WithoutCancel(requestContextOf(runtimeInstance, request))

    if true == timeout.Unlimited {
        return detached, func() {}, nil
    }

    persistenceContext, cancel := context.WithTimeout(detached, timeout.Value)

    return persistenceContext, cancel, nil
}

func requestContextOf(runtimeInstance runtimecontract.Runtime, request httpcontract.Request) context.Context {
    if false == internal.IsNilInterface(runtimeInstance) && nil != runtimeInstance.Context() {
        return runtimeInstance.Context()
    }

    if false == internal.IsNilInterface(request) && nil != request.HttpRequest() {
        return request.HttpRequest().Context()
    }

    return context.Background()
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

/* private, since a shared cache replaying it would hand one client's session to another */
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

/* the zero SameSite emits no attribute in net/http, so it falls back to the default; SameSiteDefaultMode asks for none on purpose */
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

    /* browsers drop a SameSite=None cookie that is not Secure, so it is Secure whatever the scheme */
    if nethttp.SameSiteNoneMode == resolveSessionCookieSameSite(sessionCookiePolicy) {
        return true
    }

    if true == internal.IsNilInterface(request) {
        return false
    }

    return "https" == detectSchemeWithForwardedHeadersPolicy(request.HttpRequest(), forwardedHeadersPolicy)
}

/* through the named level methods, which a substituted logger overrides */
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
        recordContext["method"] = internal.BoundDiagnosticText(request.HttpRequest().Method)
        recordContext["path"] = internal.BoundDiagnosticText(request.HttpRequest().URL.Path)
    }

    if loggingcontract.LevelWarning == level {
        logger.Warning(message, exception.LogContext(err, exception.LogContextConfig{Extra: recordContext}))

        return
    }

    logger.Error(message, exception.LogContext(err, exception.LogContextConfig{Extra: recordContext}))
}

func markResponsePrivateForSessionCookie(response httpcontract.Response) {
    if true == internal.IsNilInterface(response) {
        return
    }

    headers := response.Headers()
    if nil == headers {
        return
    }

    /* Cache-Control may span several field lines and the Set below replaces them all */
    existingLines := headers.Values("Cache-Control")
    if 0 == len(existingLines) {
        headers.Set("Cache-Control", "private")

        return
    }

    /* a value ending inside a quoted string would swallow an appended private, so it is replaced */
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
        /* not cut on a bare comma: a directive may carry a quoted field-name list */
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

/* CloseDiscardedResponseBody closes an unserved response's body, logging a failure to a logger that may be nil. */
func CloseDiscardedResponseBody(response httpcontract.Response, logger loggingcontract.Logger) {
    closeDiscardedResponseBody(response, logger)
}

func closeDiscardedResponseBody(response httpcontract.Response, logger loggingcontract.Logger) {
    /* read through, not compared: a typed nil dereferenced inside the recovery defer would panic past recover */
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

    /* the writer's deferred Close may have closed it already; a second close of an os.File is not logged */
    if true == exception.ChainIs(closeErr, fs.ErrClosed) {
        return
    }

    logger.Error(
        "failed to close discarded response body",
        exception.LogContext(closeErr, exception.LogContextConfig{}),
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

/* only a trusted proxy is believed and its leftmost entry read, so a trusted edge must overwrite the header */
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

    /* an IPv4-mapped IPv6 peer matches an IPv4 CIDR */
    remoteAddress = remoteAddress.Unmap()

    for _, trustedProxyString := range trustedProxyList {
        trimmedTrustedProxyString := strings.TrimSpace(trustedProxyString)
        if "" == trimmedTrustedProxyString {
            continue
        }

        trustedPrefix, trustedPrefixErr := netip.ParsePrefix(trimmedTrustedProxyString)
        if nil == trustedPrefixErr {
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

/* net.SplitHostPort, so the colons of a bracketed IPv6 literal are not a port */
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

func matchesLocale(locales []string, parameters map[string]string) bool {
    if 0 == len(locales) {
        return true
    }

    localeValue := ""
    if value, exists := parameters[RouteAttributeLocale]; true == exists {
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

/* only a separator is re-escaped, so "/files/a%2Fb/c" and "/files/a/b/c" bind different tails */
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
    parameters := make(map[string]string)

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
                    if regex, exists := routeDefinition.requirements[wildcardName]; true == exists {
                        if false == regex.MatchString(rest) {
                            return nil, false
                        }
                    }

                    parameters[wildcardName] = rest
                    if RouteAttributeLocale == wildcardName {
                        parameters[RouteAttributeLocale] = rest
                    }
                }

                return parameters, true
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

                parameters[wildcardName] = pathPart
                if RouteAttributeLocale == wildcardName {
                    parameters[RouteAttributeLocale] = pathPart
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
            /* an empty segment does not bind a named parameter, which a handler could not tell from a supplied one */
            if "" == pathPart {
                /* left unbound, since UrlGenerator mints exactly "/" for this shape */
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

            parameters[paramName] = pathPart

            if RouteAttributeLocale == paramName {
                parameters[RouteAttributeLocale] = pathPart
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

    return parameters, true
}

/* the asterisk-form of OPTIONS and an authority-form CONNECT are not path-routed */
func requestPathIsRoutable(path string) bool {
    if "" == path {
        return true
    }

    return strings.HasPrefix(path, "/")
}
