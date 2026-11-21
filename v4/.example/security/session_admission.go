package security

import (
    "context"
    "errors"
    "time"

    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodysession "github.com/precision-soft/melody/v4/session"
    melodysessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

/* SessionIndex is the index of the sessions each account holds (repository.UserSessionRepository). */
type SessionIndex interface {
    Admit(ctx context.Context, userId string, previousSessionId string, sessionId string, createdAt time.Time, sessionLive func(sessionId string) (bool, error), release func(sessionId string) error) error

    Release(ctx context.Context, sessionId string) error
}

type SessionIndexLookup func(runtimeInstance melodycontainercontract.Runtime) (SessionIndex, error)

/* AdmitSession records the rotated session under its account and ends the account's oldest sessions past the cap; a refused admission clears the rotated session, so the sign-in fails rather than opening a session the cap does not count. */
func AdmitSession(
    runtimeInstance melodycontainercontract.Runtime,
    sessionIndex SessionIndexLookup,
    userId string,
    previousSessionId string,
    rotatedSession melodysessioncontract.Session,
) error {
    admitErr := admitSession(runtimeInstance, sessionIndex, userId, previousSessionId, rotatedSession)
    if nil != admitErr {
        rotatedSession.Clear()
    }

    return admitErr
}

func admitSession(
    runtimeInstance melodycontainercontract.Runtime,
    sessionIndex SessionIndexLookup,
    userId string,
    previousSessionId string,
    rotatedSession melodysessioncontract.Session,
) error {
    if nil == sessionIndex {
        return errors.New("the session index is not configured")
    }

    if refuseErr := melodycontainercontract.RefuseNilRuntime(runtimeInstance); nil != refuseErr {
        return refuseErr
    }

    index, lookupErr := sessionIndex(runtimeInstance)
    if nil != lookupErr {
        return lookupErr
    }

    sessionManager, sessionManagerErr := melodyruntime.FromRuntime[melodysessioncontract.Manager](
        runtimeInstance,
        melodysession.ServiceSessionManager,
    )
    if nil != sessionManagerErr {
        return sessionManagerErr
    }

    clockInstance, clockErr := melodyruntime.FromRuntime[melodyclockcontract.Clock](runtimeInstance, melodyclock.ServiceClock)
    if nil != clockErr {
        return clockErr
    }

    sessionStorage, sessionStorageErr := melodyruntime.FromRuntime[melodysessioncontract.Storage](
        runtimeInstance,
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
            _, exists, loadErr := sessionStorage.Load(runtimeInstance, sessionId)

            return exists, loadErr
        },
        func(sessionId string) error {
            return sessionManager.DeleteSession(runtimeInstance, sessionId)
        },
    )
}

/* ReleaseSession removes the row of the session a sign-out ends; the sign-out ends the session whatever this answers. */
func ReleaseSession(runtimeInstance melodycontainercontract.Runtime, sessionIndex SessionIndexLookup, sessionId string) error {
    if nil == sessionIndex {
        return errors.New("the session index is not configured")
    }

    if refuseErr := melodycontainercontract.RefuseNilRuntime(runtimeInstance); nil != refuseErr {
        return refuseErr
    }

    index, lookupErr := sessionIndex(runtimeInstance)
    if nil != lookupErr {
        return lookupErr
    }

    return index.Release(runtimeInstance.Context(), sessionId)
}
