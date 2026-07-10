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
    wrapped := handler
    for index := len(middlewares) - 1; 0 <= index; index-- {
        wrapped = middlewares[index](wrapped)
    }

    return wrapped
}

func splitPath(value string) []string {
    trimmedPath := strings.TrimSpace(value)
    if "" == trimmedPath {
        return []string{""}
    }

    if false == strings.HasPrefix(trimmedPath, "/") {
        trimmedPath = "/" + trimmedPath
    }

    if 1 < len(trimmedPath) {
        trimmedPath = strings.TrimRight(trimmedPath, "/")
        if "" == trimmedPath {
            trimmedPath = "/"
        }
    }

    return strings.Split(trimmedPath, "/")
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
) {
    if nil == response {
        response = &Response{
            statusCode: nethttp.StatusNoContent,
            headers:    make(nethttp.Header),
            bodyReader: nil,
        }
    }

    /* @info persist the session at most once per request: the panic-recovery path can re-enter writeResponse after the first call already committed the session but then failed while writing the body, and SaveSession does not reset the modified flag, so without this guard the session store would be written twice. The header-commit flag cannot gate this — a handler that streamed its own response still needs its session persisted on that first (already-committed) call. */
    persistenceRecorder, isPersistenceRecorder := writer.(sessionPersistenceRecorder)
    sessionAlreadyPersisted := true == isPersistenceRecorder && true == persistenceRecorder.SessionPersisted()

    if false == sessionAlreadyPersisted && nil != sessionManager && nil != sessionInstance {
        sessionPersistFailed := false

        if true == sessionInstance.IsCleared() {
            if err := sessionManager.DeleteSession(sessionInstance.Id()); nil != err {
                /* @important a session-backend outage on logout must degrade to a logged error but STILL expire the browser cookie: clearing the cookie is independent of and strictly safer than the backend delete (it can only end a session, never resurrect an unpersisted one), so a failed DeleteSession must not leave the client holding a live session cookie while it is told it was logged out. Mark the persistence failed so MarkSessionPersisted is skipped, but emit the clearing cookie below regardless. (This differs from the save path, where a failed SaveSession MUST suppress the cookie so the browser is not pointed at a never-persisted session id.) */
                sessionPersistFailed = true

                logSessionPersistenceError(runtimeInstance, "failed to delete session", err)
            }

            cookiePath := sessionCookiePolicy.Path
            if "" == cookiePath {
                cookiePath = "/"
            }

            cookie := &nethttp.Cookie{
                Name:     session.SessionCookieName,
                Value:    "",
                Path:     cookiePath,
                Domain:   sessionCookiePolicy.Domain,
                HttpOnly: true,
                SameSite: sessionCookiePolicy.SameSite,
                Secure:   "https" == detectSchemeWithForwardedHeadersPolicy(request.HttpRequest(), forwardedHeadersPolicy),
                MaxAge:   -1,
            }

            SetCookie(response, cookie)
        } else if true == sessionInstance.IsModified() {
            err := sessionManager.SaveSession(sessionInstance)
            if nil != err {
                /* @important same degradation as the delete path: log once and send the response without the session cookie; the session is intentionally not marked persisted so a later successful write could still commit it */
                sessionPersistFailed = true

                logSessionPersistenceError(runtimeInstance, "failed to save session", err)
            } else {
                cookiePath := sessionCookiePolicy.Path
                if "" == cookiePath {
                    cookiePath = "/"
                }

                cookie := &nethttp.Cookie{
                    Name:     session.SessionCookieName,
                    Value:    sessionInstance.Id(),
                    Path:     cookiePath,
                    Domain:   sessionCookiePolicy.Domain,
                    HttpOnly: true,
                    SameSite: sessionCookiePolicy.SameSite,
                    Secure:   "https" == detectSchemeWithForwardedHeadersPolicy(request.HttpRequest(), forwardedHeadersPolicy),
                }

                SetCookie(response, cookie)
            }
        }

        if true == isPersistenceRecorder && false == sessionPersistFailed {
            persistenceRecorder.MarkSessionPersisted()
        }
    }

    /* @info a handler that streamed its own response (for example Server-Sent Events) has already committed the headers; whether it then returned no response or failed after committing, skip writing so we do not emit a superfluous WriteHeader over the in-flight stream. */
    recorder, isRecorder := writer.(headerCommitRecorder)
    if true == isRecorder && true == recorder.HeadersWritten() {
        closeDiscardedResponseBody(response, logging.LoggerFromRuntime(runtimeInstance))
        return
    }

    err := WriteToHttpResponseWriter(runtimeInstance, request, writer, response)
    if nil != err {
        /* @important the headers (and part of the body) may already be committed by the failed write, so a panic cannot produce a better response — and on the panic-recovery path it would escape ServeHttp and reset the connection; log and return instead */
        logger := logging.LoggerFromRuntime(runtimeInstance)
        if nil != logger {
            logger.Error(
                "failed to write response",
                exception.LogContext(err),
            )
        }
    }
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
    if nil == response {
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

    for _, trustedProxyString := range trustedProxyList {
        trimmedTrustedProxyString := strings.TrimSpace(trustedProxyString)
        if "" == trimmedTrustedProxyString {
            continue
        }

        trustedPrefix, trustedPrefixErr := netip.ParsePrefix(trimmedTrustedProxyString)
        if nil == trustedPrefixErr {
            if true == trustedPrefix.Contains(remoteAddress) {
                return true
            }

            continue
        }

        trustedAddress, trustedAddressErr := netip.ParseAddr(trimmedTrustedProxyString)
        if nil != trustedAddressErr {
            continue
        }

        if trustedAddress == remoteAddress {
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
