package security

import (
    "context"
    "errors"
    "time"

    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodysession "github.com/precision-soft/melody/v3/session"
    melodysessioncontract "github.com/precision-soft/melody/v3/session/contract"
)

/* SessionIndex is the index of the sessions each account holds (repository.UserSessionRepository), named here by what the sign-in and sign-out doors call. */
type SessionIndex interface {
    Admit(ctx context.Context, userId string, previousSessionId string, sessionId string, createdAt time.Time, release func(sessionId string) error) error

    Release(ctx context.Context, sessionId string) error
}

/* SessionIndexLookup resolves the session index for a request, as SessionUserLookup resolves the account. */
type SessionIndexLookup func(request melodyhttpcontract.Request) (SessionIndex, error)

/* AdmitSession records the session a sign-in rotated to under its account and ends, through the session manager, the account's oldest sessions past the cap, so the storage never holds more of one account's sessions than the index admits. A refused admission clears the rotated session, so the response stores nothing and expires the cookie: the sign-in fails rather than opening a session the cap does not count. */
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
        runtimeInstance.Container(),
        melodysession.ServiceSessionManager,
    )
    if nil != sessionManagerErr {
        return sessionManagerErr
    }

    clockInstance, clockErr := melodycontainer.FromResolver[melodyclockcontract.Clock](runtimeInstance.Container(), melodyclock.ServiceClock)
    if nil != clockErr {
        return clockErr
    }

    return index.Admit(
        runtimeInstance.Context(),
        userId,
        previousSessionId,
        rotatedSession.Id(),
        clockInstance.Now(),
        sessionManager.DeleteSession,
    )
}

/* ReleaseSession removes the row of the session a sign-out ends. The sign-out ends the session whatever this answers: a row left behind holds one of the account's places until a later sign-in drops it as the oldest. */
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
