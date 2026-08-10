package http

import (
    "errors"
    "io"
    "io/fs"
    "net"
    nethttp "net/http"
    "net/netip"
    "reflect"
    "strings"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/precision-soft/melody/session"
    sessioncontract "github.com/precision-soft/melody/session/contract"
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

/* wrapWithMiddlewaresRecording interleaves a recording shim under every middleware layer: the response each layer's next() returns is published to the recorder as the stack unwinds, so when an outer middleware panics after its next() returned, the kernel's recovery defer can still see — and close — the response that was in flight. Without the shim nothing holds that response: the kernel assigns it only after the whole chain has returned. A nil recorder builds the plain chain. */
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

/* a request path is never trimmed: whitespace is significant to whatever sits in front of the application, so trimming it would let "/admin%20" reach the "/admin" handler through a proxy rule that never saw a match */
func splitRequestPath(value string) []string {
    return splitNormalizedPath(value)
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

/* writeResponse persists the session, emits the session cookie and writes the response, and returns the response it actually wrote: a session-storage outage on the save path replaces the caller's response with an empty 500, and the caller publishes the returned value — the terminate event and the access log then report the status the client received rather than the response the handler produced. The replacement is a fresh response, so headers a kernel.response listener set on the original do not survive onto it, and the session cookie is suppressed deliberately. */
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

    /* a status code outside net/http's [100, 999] is refused by name here and answered as the rendered 500, because handing it to WriteHeader panics inside the delegate — and that panic reached the client as an implicit empty 200: the recovery re-entered writeResponse, read the header-commit flag the failed WriteHeader had already raised, classified the response as a committed stream and skipped the write. Zero stays the write path's own "unset means 200". */
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

        response = renderErrorResponse(runtimeInstance, request, nethttp.StatusInternalServerError, "internal server error", nil)
    }

    /* persist the session at most once per request: the panic-recovery path can re-enter writeResponse after the first call already committed the session but then failed while writing the body, and SaveSession does not reset the modified flag, so without this guard the session store would be written twice. The header-commit flag cannot gate this — a handler that streamed its own response still needs its session persisted on that first (already-committed) call. */
    persistenceRecorder, isPersistenceRecorder := writer.(sessionPersistenceRecorder)
    sessionAlreadyPersisted := true == isPersistenceRecorder && true == persistenceRecorder.SessionPersisted()

    recorder, isRecorder := writer.(headerCommitRecorder)
    responseIsDiscarded := true == isRecorder && true == recorder.HeadersWritten()

    sessionInstance = republishedSession(request, sessionInstance)

    if false == sessionAlreadyPersisted && false == internal.IsNilInterface(sessionManager) && false == internal.IsNilInterface(sessionInstance) {
        sessionPersistFailed := false

        /* one snapshot decides both branches: reading the two flags through the individual accessors let a concurrent Clear land between them, and the cookie decision below then contradicted the persistence branch taken above it. */
        _, sessionModified, sessionCleared := sessionInstance.Snapshot()

        if true == sessionCleared {
            if err := sessionManager.DeleteSession(sessionInstance.Id()); nil != err {
                /* a session-backend outage on logout must degrade to a logged error but STILL expire the browser cookie: clearing the cookie is independent of and strictly safer than the backend delete (it can only end a session, never resurrect an unpersisted one), so a failed DeleteSession must not leave the client holding a live session cookie while it is told it was logged out. Mark the persistence failed so MarkSessionPersisted is skipped, but emit the clearing cookie below regardless. (This differs from the save path, where a failed SaveSession MUST suppress the cookie so the browser is not pointed at a never-persisted session id.) */
                sessionPersistFailed = true

                logSessionPersistenceError(runtimeInstance, "failed to delete session", err)
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
            /* a discarded response carries no Set-Cookie, so storing a session the client does not already hold would write a row nothing can ever reference: a first-time visitor on a streamed response (Server-Sent Events commit the headers before the handler runs) would leave one unreachable session behind per reconnect. A session the request already names is stored as before, since it needs no cookie to be reachable — and the clear path above still destroys a session whatever the response does with it. */
            if true == responseIsDiscarded && false == requestNamesSession(request, sessionInstance.Id()) {
                /* say so in the log. Suppressing the write is right — nothing could carry the id to the client — but a handler that rotated the session on a response it had already committed reaches here too, and for it the rotation has destroyed the previous entry while this drops the replacement: everything the session held is gone, from two calls that both reported success. Silence made that indistinguishable from the ordinary case this branch exists for, a first-time visitor on a stream who simply has no session worth storing. */
                sessionPersistFailed = true

                logger := logging.LoggerFromRuntime(runtimeInstance)
                if nil != logger {
                    logger.Warning(
                        "session not persisted: the response was already committed and the request does not name this session",
                        loggingcontract.Context{
                            "sessionId": sessionInstance.Id(),
                        },
                    )
                }
            } else {
                err := sessionManager.SaveSession(sessionInstance)
                if true == errors.Is(err, session.ErrSessionDeleted) {
                    /* the session ended while this request was running — another request logged it out, or rotated it away. That is not a failure of this request and it is not a storage outage: the write is refused so the deleted session cannot be re-created, the cookie is expired so the client stops presenting an id that no longer exists, and the handler's own response is served unchanged. */
                    sessionPersistFailed = true

                    logSessionPersistenceError(runtimeInstance, "session was deleted while the request was in flight", err)

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
                    /* a storage outage on the save path answers 500 rather than the response the handler produced. The handler wrote to the session and returned success on the assumption the write would land — a login answering "welcome" with the identity never stored, or an attempt counter that never grows while the backend is down — and the client cannot tell the difference. The headers are not committed at this point (the branch above holds the case where they are), so the response can still be replaced; the cookie is suppressed either way, so the browser is never pointed at an id nothing persisted. */
                    sessionPersistFailed = true

                    logSessionPersistenceError(runtimeInstance, "failed to save session", err)

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
                }
            }
        }

        if true == isPersistenceRecorder && false == sessionPersistFailed {
            persistenceRecorder.MarkSessionPersisted()
        }
    }

    /* a handler that streamed its own response (for example Server-Sent Events) has already committed the headers; whether it then returned no response or failed after committing, skip writing so we do not emit a superfluous WriteHeader over the in-flight stream. */
    if true == responseIsDiscarded {
        closeDiscardedResponseBody(response, logging.LoggerFromRuntime(runtimeInstance))
        return response
    }

    err := WriteToHttpResponseWriter(runtimeInstance, request, writer, response)
    if nil != err {
        /* the headers (and part of the body) may already be committed by the failed write, so a panic cannot produce a better response — and on the panic-recovery path it would escape ServeHttp and reset the connection; log and return instead */
        logger := logging.LoggerFromRuntime(runtimeInstance)
        if nil != logger {
            logger.Error(
                "failed to write response",
                exception.LogContext(err),
            )
        }
    }

    return response
}

/* republishedSession prefers the session a handler published on the request over the one the kernel captured before routing, so rotating the session id (the session-fixation defence) reaches the store and the Set-Cookie. */
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
    if nil == request {
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
    if nil == request || "" == sessionId {
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

/* net/http has no name for the zero SameSite, and it emits no attribute for it — which is what an operator who only set Path or Domain would silently get. Treat it as unset and fall back to the framework default; SameSiteDefaultMode remains the way to ask for no attribute on purpose. */
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

    if nil == request {
        return false
    }

    return "https" == detectSchemeWithForwardedHeadersPolicy(request.HttpRequest(), forwardedHeadersPolicy)
}

func logSessionPersistenceError(
    runtimeInstance runtimecontract.Runtime,
    message string,
    err error,
) {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    logger.Error(
        message,
        exception.LogContext(err),
    )
}

func closeDiscardedResponseBody(response httpcontract.Response, logger loggingcontract.Logger) {
    /* the interface is read through, not compared: this runs inside the kernel's recovery defer, where a typed nil dereferenced on BodyReader below is a second panic after recover has already run and ServeHttp answers nothing at all */
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

    closeErr := closer.Close()
    if nil == closeErr || nil == logger {
        return
    }

    /* the response may already have been closed by the writer's own deferred Close, when the panic that discarded it unwound from inside WriteToHttpResponseWriter. Closing an os.File twice is safe and reports this; it is not a failure worth an error line on a path that is already reporting a panic. */
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
        /* a chain of proxies appends rather than replaces, so the header arrives as "https, http": the client-facing hop is the leftmost entry. Returning the whole list yields a scheme equal to neither "http" nor "https", and every downstream equality test — the Secure attribute on the cookies this response sets, above all — then reads the request as plaintext. */
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

    /* an IPv4-mapped IPv6 peer (::ffff:10.0.0.1) is the IPv4 address it names, so an IPv4 CIDR in the trusted proxy list must still match it — mirrors the per-address check in http/middleware/client_ip.go */
    remoteAddress = remoteAddress.Unmap()

    for _, trustedProxyString := range trustedProxyList {
        trimmedTrustedProxyString := strings.TrimSpace(trustedProxyString)
        if "" == trimmedTrustedProxyString {
            continue
        }

        trustedPrefix, trustedPrefixErr := netip.ParsePrefix(trimmedTrustedProxyString)
        if nil == trustedPrefixErr {
            /* a mapped prefix (::ffff:10.0.0.0/104) names the IPv4 range it embeds; unmap it so it still Contains the unmapped host — netip treats the two address families as unequal otherwise, mirroring http/middleware/client_ip.go */
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
                    /* a requirement on a catch-all is a whitelist like any other; skipping it here would let the wildcard swallow anything while the single-segment and named-parameter branches below enforce theirs */
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
                /* a trailing optional reached through the root is omitted rather than refused: "/" is the one path whose trailing slash cannot be trimmed away, so it splits into two empty segments and the optional lands on the second one. UrlGenerator mints exactly "/" for this shape and the openapi document advertises it, so refusing it would 404 a url the framework itself produced. The parameter is left unbound, which is what an omitted optional means everywhere else. */
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
