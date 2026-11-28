package http

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
)

const requestAttributeSessionPersistedCallbacks = "_session_persisted_callbacks"

/* SessionPersistedCallback runs once the response path ended the session's storage call: persistErr is that call's failure, which the response answers, and nil when it landed or none was made. A returned error replaces the response, raised as a handler's error, its Set-Cookie kept. */
type SessionPersistedCallback func(persistErr error) error

/* OnSessionPersisted registers a callback the response path runs once, after it persisted the request's session and before it writes the response; callbacks run in registration order. */
func OnSessionPersisted(request httpcontract.Request, callback SessionPersistedCallback) error {
    if true == internal.IsNilInterface(request) || true == internal.IsNilInterface(request.Attributes()) {
        return exception.NewError("request is nil in on session persisted", nil, nil)
    }

    if nil == callback {
        return exception.NewError("callback is nil in on session persisted", exceptioncontract.Context{"argument": "callback"}, nil)
    }

    callbacks := sessionPersistedCallbacks(request)

    request.Attributes().Set(requestAttributeSessionPersistedCallbacks, append(callbacks, callback))

    return nil
}

func sessionPersistedCallbacks(request httpcontract.Request) []SessionPersistedCallback {
    if true == internal.IsNilInterface(request) || true == internal.IsNilInterface(request.Attributes()) {
        return nil
    }

    value, exists := request.Attributes().Get(requestAttributeSessionPersistedCallbacks)
    if false == exists {
        return nil
    }

    callbacks, _ := value.([]SessionPersistedCallback)

    return callbacks
}

/* taken before the first runs, so a recovery that re-enters the response path runs none twice */
func runSessionPersistedCallbacks(
    runtimeInstance containercontract.Runtime,
    request httpcontract.Request,
    response httpcontract.Response,
    persistErr error,
    responseIsDiscarded bool,
    raiseCallbackError func(error) httpcontract.Response,
) httpcontract.Response {
    callbacks := sessionPersistedCallbacks(request)
    if 0 == len(callbacks) {
        return response
    }

    request.Attributes().Remove(requestAttributeSessionPersistedCallbacks)

    for _, callback := range callbacks {
        callbackErr := callback(persistErr)
        if nil == callbackErr {
            continue
        }

        logger := logging.LoggerFromRuntime(runtimeInstance)

        if true == responseIsDiscarded || nil == raiseCallbackError {
            if nil != logger {
                logger.Error(
                    "session persisted callback failed and its error cannot replace the response",
                    exception.LogContext(callbackErr, exception.LogContextOptions{}),
                )
            }

            continue
        }

        raised := raiseCallbackError(callbackErr)

        for _, cookieValue := range response.Headers().Values("Set-Cookie") {
            raised.Headers().Add("Set-Cookie", cookieValue)
        }

        if raised != response {
            closeDiscardedResponseBody(response, logger)
        }

        response = raised
    }

    return response
}
