package http

import (
    "github.com/precision-soft/melody/v4/container"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

/* RegenerateRequestSession rotates the id of the request's session and publishes the rotated session on the request; write the authenticated identity to the session it returns. */
func RegenerateRequestSession(request httpcontract.Request) (sessioncontract.Session, error) {
    if true == internal.IsNilInterface(request) {
        return nil, exception.NewError("request is nil in regenerate request session", nil, nil)
    }

    currentSession, currentSessionErr := RequestSession(request)
    if nil != currentSessionErr {
        return nil, currentSessionErr
    }

    /* refused here, since a replacement manager may not refuse it */
    if cause, isUnavailable := session.SessionUnavailableCause(currentSession); true == isUnavailable {
        if false == exception.AnswersUnavailable(cause) {
            cause = exception.MarkInfrastructureFailure(cause)
        }

        if false == exception.ChainIs(cause, session.ErrSessionUnavailable) {
            cause = markSessionUnavailable(cause)
        }

        return nil, cause
    }

    runtimeInstance := request.RuntimeInstance()
    if nil == runtimeInstance {
        return nil, exception.NewError("request has no runtime in regenerate request session", nil, nil)
    }

    sessionManager, sessionManagerErr := container.FromResolver[sessioncontract.Manager](
        runtimeInstance.Context(),
        runtimeInstance.Container(),
        session.ServiceSessionManager,
    )
    if nil != sessionManagerErr {
        return nil, sessionManagerErr
    }

    rotationContext, releaseRotationContext, regenerateErr := storageCallContext(runtimeInstance.Context(), sessionManager)
    defer releaseRotationContext()

    if nil != regenerateErr {
        return nil, regenerateErr
    }

    rotatedSession, regenerateErr := sessionManager.RegenerateSession(rotationContext, currentSession)
    if nil != regenerateErr {
        return nil, markSessionUnavailable(namedStorageBound(runtimeInstance.Context(), rotationContext, regenerateErr))
    }

    if publishErr := SetRequestSession(request, rotatedSession); nil != publishErr {
        return nil, publishErr
    }

    return rotatedSession, nil
}
