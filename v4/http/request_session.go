package http

import (
    "context"
    "sync"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

/* sessionUnavailableError puts session.ErrSessionUnavailable in the chain, so a firewall door tells a session outage from a credential one */
type sessionUnavailableError struct {
    cause error
}

func (instance *sessionUnavailableError) Error() string {
    return instance.cause.Error()
}

func (instance *sessionUnavailableError) Is(target error) bool {
    return session.ErrSessionUnavailable == target
}

func (instance *sessionUnavailableError) Unwrap() error {
    return instance.cause
}

func markSessionUnavailable(err error) error {
    if false == exception.AnswersUnavailable(err) {
        return err
    }

    return &sessionUnavailableError{cause: err}
}

/* the request's runtime bounded by StorageTimeout, on the request's own context, so a client that left still stops the call */
func storageCallRuntime(requestRuntime containercontract.Runtime, manager sessioncontract.Manager) (containercontract.Runtime, context.CancelFunc, error) {
    if nilErr := containercontract.RefuseNilRuntime(requestRuntime); nil != nilErr {
        return requestRuntime, func() {}, nilErr
    }

    timeout, timeoutErr := session.ResolveStorageTimeout(manager)
    if nil != timeoutErr {
        return requestRuntime, func() {}, timeoutErr
    }

    if true == timeout.Unlimited {
        return requestRuntime, func() {}, nil
    }

    callContext, cancel := context.WithTimeout(requestRuntime.Context(), timeout.Value)

    return runtime.WithContext(requestRuntime, callContext), cancel, nil
}

/* an absent runtime reads as a context that never ends, so the classification of its refusal names no bound */
func contextOfRuntime(runtimeInstance containercontract.Runtime) context.Context {
    if true == internal.IsNilInterface(runtimeInstance) || true == internal.IsNilInterface(runtimeInstance.Context()) {
        return context.Background()
    }

    return runtimeInstance.Context()
}

/* a call the bound cut while the request was live is an outage naming the bound; the request's own end stays the manager's answer */
func namedStorageBound(requestContext context.Context, callContext context.Context, err error) error {
    if true == storageBoundExpired(requestContext) || false == storageBoundExpired(callContext) {
        return err
    }

    return exception.NewError(
        "the session storage call was cut by its bound",
        exceptioncontract.Context{"bound": session.StorageTimeoutRule.Name},
        exception.MarkInfrastructureFailure(err),
    )
}

/* loaded at most once: a failed load answers the same filed error to every later read */
type requestSession struct {
    mutex          sync.Mutex
    requestRuntime containercontract.Runtime
    manager        sessioncontract.Manager
    request        httpcontract.Request
    logger         loggingcontract.Logger
    cookieValue    string
    cookiePresent  bool
    policy         httpcontract.SessionPolicy
    loaded         bool
    session        sessioncontract.Session
    loadErr        error
}

func newRequestSession(
    requestRuntime containercontract.Runtime,
    manager sessioncontract.Manager,
    request httpcontract.Request,
    logger loggingcontract.Logger,
    cookieValue string,
    cookiePresent bool,
    policy httpcontract.SessionPolicy,
) *requestSession {
    return &requestSession{
        requestRuntime: requestRuntime,
        manager:        manager,
        request:        request,
        logger:         logger,
        cookieValue:    cookieValue,
        cookiePresent:  cookiePresent,
        policy:         policy,
    }
}

func (instance *requestSession) load() (sessioncontract.Session, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.loaded {
        return instance.session, instance.loadErr
    }

    var sessionInstance sessioncontract.Session

    loadRuntime, releaseLoadRuntime, loadErr := storageCallRuntime(instance.requestRuntime, instance.manager)
    defer releaseLoadRuntime()

    if nil == loadErr && true == instance.cookiePresent {
        sessionInstance, loadErr = instance.manager.Session(loadRuntime, instance.cookieValue)
    }

    /* a replaceable manager may answer a typed-nil session or a typed-nil error, which a bare comparison misreads */
    if true == internal.IsNilInterface(loadErr) && true == internal.IsNilInterface(sessionInstance) {
        sessionInstance, loadErr = instance.manager.NewSession(loadRuntime)
    }

    if false == internal.IsNilInterface(loadErr) {
        loadErr = namedStorageBound(contextOfRuntime(instance.requestRuntime), contextOfRuntime(loadRuntime), loadErr)
    }

    if false == internal.IsNilInterface(loadErr) {
        /* a session answered beside an error is half made and neither served nor persisted */
        sessionInstance = nil

        servedEmpty := true == instance.policy.EmptyOnOutage && true == exception.AnswersUnavailable(loadErr)

        /* without a logger the error is not marked logged, so its answerer files it */
        if false == internal.IsNilInterface(instance.logger) {
            if true == servedEmpty {
                loadErr = recordOccurrence(
                    instance.logger,
                    instance.request,
                    loadErr,
                    "session storage could not answer; readers of the session get an empty session that is not saved",
                    occurrenceRecordConfig{Warning: true, Extra: instance.sessionReferenceContext()},
                )
            } else {
                loadErr = recordOccurrence(
                    instance.logger,
                    instance.request,
                    loadErr,
                    "session could not be loaded",
                    occurrenceRecordConfig{Warning: true == internal.IsClientHangup(contextOfRuntime(instance.requestRuntime), loadErr)},
                )
            }
        }

        loadErr = markSessionUnavailable(loadErr)

        if true == servedEmpty {
            sessionInstance = session.NewUnavailableSession(loadErr)
            loadErr = nil
        }
    } else {
        loadErr = nil

        if true == internal.IsNilInterface(sessionInstance) {
            loadErr = exception.NewError("the session manager answered no session and no error", nil, nil)
            sessionInstance = nil
        }
    }

    instance.loaded = true
    instance.session = sessionInstance
    instance.loadErr = loadErr

    return sessionInstance, loadErr
}

func (instance *requestSession) sessionReferenceContext() exceptioncontract.Context {
    if false == instance.cookiePresent {
        return nil
    }

    return exceptioncontract.Context{"sessionRef": sessionIdLogReference(instance.cookieValue)}
}

func (instance *requestSession) loadedSession() sessioncontract.Session {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.session
}

/* RequestSession answers the request's session, loaded once at the first call: the stored one the cookie names or a new one; a storage outage answers an error marked as an infrastructure failure carrying session.ErrSessionUnavailable. */
func RequestSession(request httpcontract.Request) (sessioncontract.Session, error) {
    value, exists := requestSessionAttribute(request)
    if false == exists {
        return nil, exception.NewError("the request carries no session", nil, nil)
    }

    switch typed := value.(type) {
    case *requestSession:
        if nil == typed {
            break
        }

        return typed.load()
    case sessioncontract.Session:
        if false == internal.IsNilInterface(typed) {
            return typed, nil
        }
    }

    return nil, exception.NewError("the request carries no session", nil, nil)
}

/* SetRequestSession publishes a session on the request, saved and given its cookie by the response path; a nil session is refused. */
func SetRequestSession(request httpcontract.Request, sessionInstance sessioncontract.Session) error {
    if true == internal.IsNilInterface(request) || true == internal.IsNilInterface(request.Attributes()) {
        return exception.NewError("request is nil in set request session", nil, nil)
    }

    if true == internal.IsNilInterface(sessionInstance) {
        return exception.NewError("session is nil in set request session", map[string]any{"argument": "sessionInstance"}, nil)
    }

    request.Attributes().Set(requestAttributeSession, sessionInstance)

    return nil
}

func effectiveRequestSession(request httpcontract.Request) sessioncontract.Session {
    value, exists := requestSessionAttribute(request)
    if false == exists {
        return nil
    }

    switch typed := value.(type) {
    case *requestSession:
        if nil == typed {
            return nil
        }

        return typed.loadedSession()
    case sessioncontract.Session:
        if false == internal.IsNilInterface(typed) {
            return typed
        }
    }

    return nil
}

func requestSessionAttribute(request httpcontract.Request) (any, bool) {
    if true == internal.IsNilInterface(request) {
        return nil, false
    }

    attributes := request.Attributes()
    if true == internal.IsNilInterface(attributes) {
        return nil, false
    }

    return attributes.Get(requestAttributeSession)
}
