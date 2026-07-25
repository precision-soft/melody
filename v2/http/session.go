package http

import (
    "github.com/precision-soft/melody/v2/container"
    "github.com/precision-soft/melody/v2/exception"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/session"
    sessioncontract "github.com/precision-soft/melody/v2/session/contract"
)

/* RegenerateRequestSession rotates the id of the session the request carries and publishes the rotated session back on the request, which is the whole operation a login handler needs against session fixation: the response path saves that session and emits its cookie. Call it before writing the authenticated identity, and write the identity to the session it returns. The two steps are one call on purpose — rotating without republishing destroys the id the browser holds without ever telling it the new one. */
func RegenerateRequestSession(request httpcontract.Request) (sessioncontract.Session, error) {
    if nil == request {
        return nil, exception.NewError("request is nil in regenerate request session", nil, nil)
    }

    currentSession := sessionFromRequestAttribute(request)
    if nil == currentSession {
        return nil, exception.NewError("request carries no session in regenerate request session", nil, nil)
    }

    runtimeInstance := request.RuntimeInstance()
    if nil == runtimeInstance {
        return nil, exception.NewError("request has no runtime in regenerate request session", nil, nil)
    }

    sessionManager, sessionManagerErr := container.FromResolver[sessioncontract.Manager](
        runtimeInstance.Container(),
        session.ServiceSessionManager,
    )
    if nil != sessionManagerErr {
        return nil, sessionManagerErr
    }

    rotatedSession, regenerateErr := sessionManager.RegenerateSession(currentSession)
    if nil != regenerateErr {
        return nil, regenerateErr
    }

    request.Attributes().Set(RequestAttributeSession, rotatedSession)

    return rotatedSession, nil
}
