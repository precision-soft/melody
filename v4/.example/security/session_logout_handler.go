package security

import (
    examplejournal "github.com/precision-soft/melody/v4/.example/journal"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyruntimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* NewSessionLogoutHandler gives the session's place in the index back; the firewall's sign-out door ends the session itself and redirects to its target path. */
func NewSessionLogoutHandler(sessionIndex SessionIndexLookup) melodysecuritycontract.LogoutHandler {
    return &sessionLogoutHandler{
        sessionIndex: sessionIndex,
    }
}

type sessionLogoutHandler struct {
    sessionIndex SessionIndexLookup
}

func (instance *sessionLogoutHandler) Logout(
    runtimeInstance melodyruntimecontract.Runtime,
    request melodyhttpcontract.Request,
    input melodysecuritycontract.LogoutInput,
) (*melodysecuritycontract.LogoutResult, error) {
    sessionInstance := getSession(request)
    if nil == sessionInstance {
        return &melodysecuritycontract.LogoutResult{}, nil
    }

    /* the sign-out stands whatever the index answers: the door ends the session once this returned, and a row that could not be removed is journaled and holds its place until a later sign-in drops it as the oldest */
    if releaseErr := ReleaseSession(request, instance.sessionIndex, sessionInstance.Id()); nil != releaseErr {
        examplejournal.LoggerOr(runtimeInstance, melodylogging.EmergencyLogger()).Warning(
            "the signed-out session stays in the session index",
            melodyexception.LogContext(releaseErr, melodyexception.LogContextConfig{}),
        )
    }

    return &melodysecuritycontract.LogoutResult{}, nil
}

var _ melodysecuritycontract.LogoutHandler = (*sessionLogoutHandler)(nil)
