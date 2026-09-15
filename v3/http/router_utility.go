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

/* RequestPathAsRouted decodes each escaped segment while retaining encoded separators as %2F. Non-path targets are unchanged. This spelling intentionally aliases a literal %2F from %252F with an encoded separator from %2F, although the router distinguishes those segment identities. Rules containing %2F therefore match both representations. */
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
    if path != strings.TrimSpace(path) {
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

        closeDiscardedResponseBody(response, logger)

        response = renderErrorResponse(runtimeInstance, request, nethttp.StatusInternalServerError, "internal server error", nil)
    }

    persistenceRecorder, isPersistenceRecorder := writer.(sessionPersistenceRecorder)
    sessionAlreadyPersisted := true == isPersistenceRecorder && true == persistenceRecorder.SessionPersisted()

    recorder, isRecorder := writer.(headerCommitRecorder)
    responseIsDiscarded := true == isRecorder && true == recorder.HeadersWritten()

    sessionInstance = republishedSession(request, sessionInstance)

    if false == sessionAlreadyPersisted && false == internal.IsNilInterface(sessionManager) && false == internal.IsNilInterface(sessionInstance) {
        sessionPersistFailed := false

        _, sessionModified, sessionCleared := sessionInstance.Snapshot()

        if true == sessionCleared {
            if err := sessionManager.DeleteSession(sessionInstance.Id()); nil != err {

                sessionPersistFailed = true

                logSessionPersistenceEvent(runtimeInstance, loggingcontract.LevelError, "failed to delete session", err, sessionInstance.Id(), request)
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
                if true == errors.Is(err, session.ErrSessionRotated) {

                    sessionPersistFailed = true

                    logSessionPersistenceEvent(runtimeInstance, loggingcontract.LevelWarning, "session was rotated away while the request was in flight", err, sessionInstance.Id(), request)
                } else if true == errors.Is(err, session.ErrSessionDeleted) {

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

    existingLines := headers.Values("Cache-Control")
    if 0 == len(existingLines) {
        headers.Set("Cache-Control", "private")

        return
    }

    rebuilt := make([]string, 0)
    hasPrivate := false
    hasNoStore := false

    for _, existing := range existingLines {

        for _, token := range internal.SplitOutsideQuotes(existing, ',') {
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

func sessionIdLogReference(sessionId string) string {
    digest := sha256.Sum256([]byte(sessionId))

    return hex.EncodeToString(digest[:])[:16]
}

func closeDiscardedResponseBody(response httpcontract.Response, logger loggingcontract.Logger) {

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

func joinCatchAllSegments(pathSegments []string) string {
    escapedSegments := make([]string, 0, len(pathSegments))

    for _, segment := range pathSegments {
        escapedSegments = append(escapedSegments, strings.ReplaceAll(strings.ReplaceAll(segment, "%", "%25"), "/", "%2F"))
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

func requestPathIsRoutable(path string) bool {
    if "" == path {
        return true
    }

    return strings.HasPrefix(path, "/")
}
