package http

import (
    "sync"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

/* sessionUnavailableError puts session.ErrSessionUnavailable in the chain of a session the storage could not load or rotate, so a firewall door tells a session outage from a credential one; its message is the cause's */
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

/* requestSession is the session of one request, loaded from the storage at the first read and at most once: a request that never reads it costs the storage nothing, and a load that failed answers the same filed error to every later read, so one outage is one record. The load holds the handle's lock across the manager and the record, so a manager or a logger that reads the session of the same request blocks on it. */
type requestSession struct {
    mutex         sync.Mutex
    manager       sessioncontract.Manager
    request       httpcontract.Request
    logger        loggingcontract.Logger
    cookieValue   string
    cookiePresent bool
    policy        httpcontract.SessionPolicy
    loaded        bool
    session       sessioncontract.Session
    loadErr       error
}

func newRequestSession(
    manager sessioncontract.Manager,
    request httpcontract.Request,
    logger loggingcontract.Logger,
    cookieValue string,
    cookiePresent bool,
    policy httpcontract.SessionPolicy,
) *requestSession {
    return &requestSession{
        manager:       manager,
        request:       request,
        logger:        logger,
        cookieValue:   cookieValue,
        cookiePresent: cookiePresent,
        policy:        policy,
    }
}

func (instance *requestSession) load() (sessioncontract.Session, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.loaded {
        return instance.session, instance.loadErr
    }

    var sessionInstance sessioncontract.Session
    var loadErr error

    if true == instance.cookiePresent {
        sessionInstance, loadErr = instance.manager.Session(instance.cookieValue)
    }

    /* IsNilInterface on both, since a replaceable manager may answer "not found" with a typed-nil session, which a bare comparison takes for a live one, and "no error" with a typed-nil error, which it takes for an error */
    if true == internal.IsNilInterface(loadErr) && true == internal.IsNilInterface(sessionInstance) {
        sessionInstance, loadErr = instance.manager.NewSession()
    }

    if false == internal.IsNilInterface(loadErr) {
        /* a session a manager answered beside its error is half made and is neither served nor persisted */
        sessionInstance = nil

        /* only an outage is served degraded under the policy; a manager's own fault stays an error */
        servedEmpty := true == instance.policy.EmptyOnOutage && true == exception.AnswersUnavailable(loadErr)

        /* without a logger nothing is filed, so the error is not marked logged and its answerer files it */
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
                loadErr = recordErrorOccurrence(instance.logger, instance.request, loadErr, "session could not be loaded")
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

/* sessionReferenceContext names the session the cookie points at by its one-way reference, nothing without a cookie */
func (instance *requestSession) sessionReferenceContext() exceptioncontract.Context {
    if false == instance.cookiePresent {
        return nil
    }

    return exceptioncontract.Context{"sessionRef": sessionIdLogReference(instance.cookieValue)}
}

/* loadedSession answers the session a reader loaded, never loading it */
func (instance *requestSession) loadedSession() sessioncontract.Session {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.session
}

/* RequestSession answers the session of the request, loading it from the storage at the first call: the stored session the cookie names, a new one when the cookie names none, and an error when it could not: for a storage that could not answer, an error marked as an infrastructure failure with session.ErrSessionUnavailable in its chain, which a handler returns to be answered 503 with Retry-After, and for a manager's own fault or a request the kernel did not serve, an unmarked one. The load happens once per request, so a later call answers the same session or the same error. A session published with SetRequestSession or rotated with RegenerateRequestSession is answered as published. */
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

/* SetRequestSession publishes a session on the request: the response path saves it and emits its cookie in place of the one a read loaded. A nil session is refused. */
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

/* effectiveRequestSession is the session the response path persists, read without loading: the published one, else the one a reader loaded, else none */
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
