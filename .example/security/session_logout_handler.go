package security

import (
    "net/http"

    melodyhttp "github.com/precision-soft/melody/http"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/security/contract"
)

func NewSessionLogoutHandler() melodysecuritycontract.LogoutHandler {
    return &sessionLogoutHandler{}
}

type sessionLogoutHandler struct{}

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

    /* the whole session ends here, rather than only the identity in it: deleting the two keys leaves the entry MODIFIED, so the response path saves it back under the same id and re-issues the cookie — the storage keeps an emptied record for the whole session lifetime and the client carries a live session id across its own logout. Clear marks the session cleared, which is what routes the response path to DeleteSession and to the expired cookie. */
    sessionInstance.Clear()

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
