package security

import (
    "context"
    "errors"
    "time"

    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodysession "github.com/precision-soft/melody/v4/session"
    melodysessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

/* SessionIndex is the index of the sessions each account holds (repository.UserSessionRepository). */
type SessionIndex interface {
    Admit(ctx context.Context, userId string, previousSessionId string, sessionId string, createdAt time.Time, sessionLive func(sessionId string) (bool, error), release func(sessionId string) error) error

    Release(ctx context.Context, sessionId string) error
}

type SessionIndexLookup func(request melodyhttpcontract.Request) (SessionIndex, error)

/* AdmitSession records the rotated session under its account and ends the account's oldest sessions past the cap; a refused admission clears the rotated session, so the sign-in fails rather than opening a session the cap does not count. */
func AdmitSession(
    request melodyhttpcontract.Request,
    sessionIndex SessionIndexLookup,
    userId string,
    previousSessionId string,
    rotatedSession melodysessioncontract.Session,
) error {
    admitErr := admitSession(request, sessionIndex, userId, previousSessionId, rotatedSession)
    if nil != admitErr {
        rotatedSession.Clear()
    }

    return admitErr
}

func admitSession(
    request melodyhttpcontract.Request,
    sessionIndex SessionIndexLookup,
    userId string,
    previousSessionId string,
    rotatedSession melodysessioncontract.Session,
) error {
    if nil == sessionIndex {
        return errors.New("the session index is not configured")
    }

    index, lookupErr := sessionIndex(request)
    if nil != lookupErr {
        return lookupErr
    }

    runtimeInstance := request.RuntimeInstance()
    if nil == runtimeInstance {
        return errors.New("the request carries no runtime to admit the session through")
    }

    sessionManager, sessionManagerErr := melodycontainer.FromResolver[melodysessioncontract.Manager](
        runtimeInstance.Context(),
        runtimeInstance.Container(),
        melodysession.ServiceSessionManager,
    )
    if nil != sessionManagerErr {
        return sessionManagerErr
    }

    clockInstance, clockErr := melodycontainer.FromResolver[melodyclockcontract.Clock](runtimeInstance.Context(), runtimeInstance.Container(), melodyclock.ServiceClock)
    if nil != clockErr {
        return clockErr
    }

    sessionStorage, sessionStorageErr := melodycontainer.FromResolver[melodysessioncontract.Storage](
        runtimeInstance.Context(),
        runtimeInstance.Container(),
        melodysession.ServiceSessionStorage,
    )
    if nil != sessionStorageErr {
        return sessionStorageErr
    }

    admissionContext := runtimeInstance.Context()

    return index.Admit(
        admissionContext,
        userId,
        previousSessionId,
        rotatedSession.Id(),
        clockInstance.Now(),
        func(sessionId string) (bool, error) {
            _, exists, loadErr := sessionStorage.Load(admissionContext, sessionId)

            return exists, loadErr
        },
        func(sessionId string) error {
            return sessionManager.DeleteSession(admissionContext, sessionId)
        },
    )
}

/* ReleaseSession removes the row of the session a sign-out ends; the sign-out ends the session whatever this answers. */
func ReleaseSession(request melodyhttpcontract.Request, sessionIndex SessionIndexLookup, sessionId string) error {
    if nil == sessionIndex {
        return errors.New("the session index is not configured")
    }

    index, lookupErr := sessionIndex(request)
    if nil != lookupErr {
        return lookupErr
    }

    runtimeInstance := request.RuntimeInstance()
    if nil == runtimeInstance {
        return errors.New("the request carries no runtime to release the session through")
    }

    return index.Release(runtimeInstance.Context(), sessionId)
}
