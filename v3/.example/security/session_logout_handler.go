package security

import (
    "net/http"

    examplejournal "github.com/precision-soft/melody/v3/.example/journal"
    melodyexception "github.com/precision-soft/melody/v3/exception"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

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
        response := melodyhttp.JsonErrorResponse(http.StatusInternalServerError, "session is not available")

        return &melodysecuritycontract.LogoutResult{
            Response: response,
        }, nil
    }

    /* the whole session ends, not only the identity in it: an emptied session would be saved back under the same id with a re-issued cookie, while Clear routes the response path to DeleteSession and to the expired cookie */
    sessionInstance.Clear()

    /* the sign-out stands whatever the index answers, as at the sign-out door */
    if releaseErr := ReleaseSession(request, instance.sessionIndex, sessionInstance.Id()); nil != releaseErr {
        examplejournal.LoggerOr(runtimeInstance, melodylogging.EmergencyLogger()).Warning(
            "the signed-out session stays in the session index",
            melodyexception.LogContext(releaseErr),
        )
    }

    response, err := melodyhttp.JsonResponse(http.StatusOK, map[string]any{
        "success": true,
    })
    if nil != err {
        return nil, err
    }

    return &melodysecuritycontract.LogoutResult{
        Response: response,
    }, nil
}

var _ melodysecuritycontract.LogoutHandler = (*sessionLogoutHandler)(nil)
