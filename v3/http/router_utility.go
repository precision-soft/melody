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

/* a request path is never trimmed: whitespace is significant to whatever sits in front of the application, so trimming it would let "/admin%20" reach the "/admin" handler through a proxy rule that never saw a match

   The path arrives in its ESCAPED spelling and is split on the separators the client actually sent, then each segment is unescaped on its own. Splitting the decoded path instead made "%2F" a segment separator: "/admin%2Fusers" is one segment naming a resource called "admin/users", but decoded first it became two and reached the "/admin/users" handler — while a proxy or WAF rule written against the raw request line saw neither. Per-segment unescaping keeps an encoded separator inside the value where the client put it, so a parameter can legitimately carry a slash and the route tree still sees exactly the segments the request line has. A segment whose escaping is malformed is left as sent rather than guessed at, so it matches only a route that literally spells it. */
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

func splitNormalizedPath(value string) []string {
    /* an empty path is the root, which is what every consumer of it means. Answered as [""] instead, the tree walk started with no segments to consume and reached only the tree root, where a route registered as "/" does not live — it lives under the empty static child — so a request whose target normalized to nothing 404'd even against an application that had registered "/". */
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

/* requestPathIsCanonical reports whether a request path is spelled the one way every path consumer reads the same. The router matches the path as sent — splitNormalizedPath drops trailing slashes but folds no dot or empty segment — while the access-control matcher folds "..", "." and "//" through path.Clean before it selects a rule, and the firewall matcher reads the raw path with neither fold. A path that folds to a different spelling therefore routes to one handler and is authorized against another rule, so a request that reaches a protected handler under its sent spelling can be granted the attributes of the folded spelling's rule — a different, possibly public one, or none at all. The static file server already refuses a non-canonical path for exactly this reason; refusing at the kernel keeps the router, the firewall matchers and the access control reading one spelling.

   A trailing slash is not a fold: the router and the matchers already agree "/admin/" names the route "/admin", so it is normalized away here before the comparison rather than refused. A target that does not begin with "/" — the asterisk-form of OPTIONS, an authority-form CONNECT — is not path-routed and is left to the router to answer. */
func requestPathIsCanonical(path string) bool {
    if false == strings.HasPrefix(path, "/") {
        return true
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
            /* a discarded response carries no Set-Cookie, so storing a session the client does not already hold would write a row nothing can ever reference: a first-time visitor on a streamed response (Server-Sent Events commit the headers before the handler runs) would leave one unreachable session behind per reconnect. A session the request already names is stored as before, since it needs no cookie to be reachable — and the clear path above still destroys a session whatever the response does with it. */
            if true == responseIsDiscarded && false == requestNamesSession(request, sessionInstance.Id()) {
                /* say so in the log. Suppressing the write is right — nothing could carry the id to the client — but a handler that rotated the session on a response it had already committed reaches here too, and for it the rotation has destroyed the previous entry while this drops the replacement: everything the session held is gone, from two calls that both reported success. Silence made that indistinguishable from the ordinary case this branch exists for, a first-time visitor on a stream who simply has no session worth storing. */
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
                if true == errors.Is(err, session.ErrSessionDeleted) {
                    /* the session ended while this request was running — another request logged it out, or rotated it away. That is not a failure of this request and it is not a storage outage: the write is refused so the deleted session cannot be re-created, the cookie is expired so the client stops presenting an id that no longer exists, and the handler's own response is served unchanged. */
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
                    /* a storage outage on the save path answers 500 rather than the response the handler produced. The handler wrote to the session and returned success on the assumption the write would land — a login answering "welcome" with the identity never stored, or an attempt counter that never grows while the backend is down — and the client cannot tell the difference. The headers are not committed at this point (the branch above holds the case where they are), so the response can still be replaced; the cookie is suppressed either way, so the browser is never pointed at an id nothing persisted. */
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
                    /* the id the cookie now carries names this one client, so the response must not be stored by a shared cache and replayed to another: a listener that modified the session on a response some layer marked Cache-Control: public — a static asset among them — would otherwise leak this session id to the next client the cache serves */
                    markResponsePrivateForSessionCookie(response)
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

        /* the returned response feeds the terminate event and the access log, and for a stream the truth lives on the connection, not in the synthetic response the kernel substituted: without this the journal recorded 204 for every streamed 200 and a rendered-but-never-written 500 for a panic mid-stream, so status-distribution queries over the access log were wrong for every streaming route. A hijacked connection records no status and keeps the substitute. */
        if statusRecorder, isStatusRecorder := writer.(committedStatusRecorder); true == isStatusRecorder {
            if committedStatus := statusRecorder.CommittedStatusCode(); 0 < committedStatus && committedStatus != response.StatusCode() {
                return EmptyResponse(committedStatus)
            }
        }

        return response
    }

    err := WriteToHttpResponseWriter(runtimeInstance, request, writer, response)
    if nil != err {
        /* the headers (and part of the body) may already be committed by the failed write, so a panic cannot produce a better response — and on the panic-recovery path it would escape ServeHttp and reset the connection; log and return instead. A client that walked away mid-write is the routine case and is recorded at warning: net/http suppresses this class entirely, and at error every impatient download paged the operator. The record carries the method and path either way, so it can name the route it happened on. */
        logger := logging.LoggerFromRuntime(runtimeInstance)
        if nil != logger {
            writeLogContext := exceptioncontract.Context{}
            if nil != request && nil != request.HttpRequest() {
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

/* closeResponseBodySafely contains a Close that panics: closeDiscardedResponseBody runs inside the kernel's recovery defer, where a body whose Close dereferences the state the panic invalidated would raise a second panic past the recovery and reset the connection with nothing served — the failure is worth a record, never the response. */
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

/* isClientAbortWriteError classifies a response-write failure the client caused rather than the server: the request context net/http cancels the moment the peer disconnects, and the broken-pipe and connection-reset errors a write to a gone peer answers with. Everything else stays a server-side write failure. */
func isClientAbortWriteError(request httpcontract.Request, err error) bool {
    if nil != request && nil != request.HttpRequest() && nil != request.HttpRequest().Context().Err() {
        return true
    }

    return true == errors.Is(err, syscall.EPIPE) || true == errors.Is(err, syscall.ECONNRESET)
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

/* logSessionPersistenceEvent records what the response path did with the session, at the level the event deserves and with the coordinates that let it be found again.

   The level is a parameter because one of the three is not a failure at all: a session another request ended while this one was running is the session ending — the contract says so in as many words, and so does the branch that answers it — while the other two are storage outages. Filed at error alongside them, a user who logged out in a second tab produced one indistinguishable "failed to save session" per concurrent request, against a perfectly healthy backend.

   The coordinates travel because only the middle record ever named the session, and that by accident: it carries sessionId through the cause's own context. None of the three named the route, so a record that did arrive could not be tied to the request that produced it. The level switch goes through the named methods rather than Log, because that is the door a substituted logger overrides. */
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
        /* the id is a live bearer credential: on a failed delete the store still holds the session, and on a failed save the id is one the client presented — logging it verbatim hands anyone with log-read access a cookie value they can present as that user. A one-way reference correlates the records of one session across a request without carrying the credential itself. */
        recordContext["sessionRef"] = sessionIdLogReference(sessionId)
    }

    if nil != request && nil != request.HttpRequest() {
        recordContext["method"] = request.HttpRequest().Method
        recordContext["path"] = request.HttpRequest().URL.Path
    }

    if loggingcontract.LevelWarning == level {
        logger.Warning(message, exception.LogContext(err, recordContext))

        return
    }

    logger.Error(message, exception.LogContext(err, recordContext))
}

/* markResponsePrivateForSessionCookie keeps a response that carries the session cookie out of a shared cache. The session cookie names one client, so a response stored under its URL alone by a shared cache and replayed to another client would hand that client the first one's session id — the leak a static or handler response marked Cache-Control: public invites the moment a listener or middleware modifies the session on it. The public token is dropped and private added, so a shared cache does not store it while a private browser cache still may; an already-private or no-store response is left as it is. */
func markResponsePrivateForSessionCookie(response httpcontract.Response) {
    if true == internal.IsNilInterface(response) {
        return
    }

    headers := response.Headers()
    if nil == headers {
        return
    }

    existing := headers.Get("Cache-Control")
    if "" == existing {
        headers.Set("Cache-Control", "private")

        return
    }

    rebuilt := make([]string, 0)
    hasPrivate := false
    hasNoStore := false

    for _, token := range strings.Split(existing, ",") {
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

    if false == hasPrivate && false == hasNoStore {
        rebuilt = append(rebuilt, "private")
    }

    headers.Set("Cache-Control", strings.Join(rebuilt, ", "))
}

/* sessionIdLogReference answers a short one-way reference to a session id for the log: a truncated SHA-256, enough to correlate the records of one session within a request but not to reconstruct the id, so a log reader cannot present it as a cookie. */
func sessionIdLogReference(sessionId string) string {
    digest := sha256.Sum256([]byte(sessionId))

    return hex.EncodeToString(digest[:])[:16]
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

    closeErr := closeResponseBodySafely(closer)
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

/* detectSchemeWithForwardedHeadersPolicy believes X-Forwarded-Proto only when the direct peer is a trusted proxy, and then reads the leftmost entry — the scheme the client-facing hop received. That convention is only as trustworthy as the edge's handling of the header: a trusted edge must SET X-Forwarded-Proto, overwriting whatever arrived, because an edge that merely appends leaves the client's own spelling leftmost and the client then chooses the scheme — the Secure attribute of every cookie this response sets included. The X-Forwarded-For reader can walk its chain right-to-left skipping trusted addresses; the proto entries carry no addresses to skip by, so overwrite-at-the-edge is the deployment's half of this contract. */
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

/* matchesHost compares host names the way the host header is defined rather than the way two strings compare. A host name is case-insensitive, so a client sending Example.com reaches a route bound to example.com; and the port is a discriminator only when the route asked for one, so a route bound to example.com matches example.com:8443 while a route bound to example.com:8443 still matches only that port. The exact comparison this replaced made a route bound to a host unreachable behind any non-default port — the whole of local development and every non-443 deployment — and the request fell through to a broader route on the same pattern, or to a 404, with nothing recorded. The sibling matchesScheme already folds case for the same reason.

   Whether the route named a port is asked of net.SplitHostPort and not of a colon scan: the colons inside a bracketed IPv6 literal are the address's own, so a route bound to [::1] was read as a route that had named a port and became unreachable behind every port — the same outage the exact comparison used to cause, kept alive for the one host family every developer machine answers on. */
func matchesHost(expectedHost string, actualHost string) bool {
    if "" == expectedHost {
        return true
    }

    if true == strings.EqualFold(expectedHost, actualHost) {
        return true
    }

    expectedHostWithoutPort, expectedNamedPort := splitHostAndPort(expectedHost)

    /* the route named a port, so the port is part of what it asked for and a request host carrying another one, or none, cannot satisfy it */
    if true == expectedNamedPort {
        return false
    }

    actualHostWithoutPort, _ := splitHostAndPort(actualHost)

    return strings.EqualFold(expectedHostWithoutPort, actualHostWithoutPort)
}

/* splitHostAndPort reduces a host value to the bare host name and reports whether it carried a port at all. The bracketed IPv6 form is the reason both answers come from one reader: net.SplitHostPort strips the brackets along with the port, so a value that carries no port has to be unbracketed by hand for the two sides of a comparison to be the same shape — [::1] against the ::1 the ported side yields. */
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

/* matchesLocale is the locale half of the match, in one place because two readers used to answer it: the matcher applied it and AllowedMethods did not, so the method list announced for a path named the methods of routes that path can never reach. A route declaring no locales accepts every one; a route declaring some accepts only a path that carries one of them, and a path carrying none is refused rather than admitted, since a locale-scoped route reached without a locale would serve one arbitrary language to everybody. */
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

/* requestPathIsRoutable reports whether a request target is a path at all. The origin-form is the only target a route table can answer: the asterisk-form of OPTIONS names the server rather than a resource, and net/http hands it through as the path "*", which the splitter then read as the single segment "*" — so a server-wide OPTIONS was offered to every "/:param" route in the application and bound the asterisk as that parameter's value. An authority-form CONNECT reaches the same door. Neither is path-routed; both are left to the kernel to answer as no route. */
func requestPathIsRoutable(path string) bool {
    if "" == path {
        return true
    }

    return strings.HasPrefix(path, "/")
}
