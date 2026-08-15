package security

import (
    "net/http"

    melodyhttp "github.com/precision-soft/melody/http"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/security/contract"
)

func NewSessionLoginHandler() melodysecuritycontract.LoginHandler {
    return &sessionLoginHandler{}
}

type sessionLoginHandler struct{}

func (instance *sessionLoginHandler) Login(
    runtimeInstance melodyruntimecontract.Runtime,
    request melodyhttpcontract.Request,
    input melodysecuritycontract.LoginInput,
) (*melodysecuritycontract.LoginResult, error) {
    sessionInstance := getSession(request)
    if nil == sessionInstance {
        response := melodyhttp.JsonErrorResponse(http.StatusInternalServerError, "session is not available")

        return &melodysecuritycontract.LoginResult{
            Token:    input.Token,
            Response: response,
        }, nil
    }

    userIdentifier := ""
    var roles []string
    if nil != input.Token {
        userIdentifier = input.Token.UserIdentifier()
        roles = input.Token.Roles()
    }

    /* rotate the session id before the authenticated identity is written to it: a client that presents a session id chosen before authentication must not keep that id once it carries the identity, or an id an attacker seeded and planted in the victim's browser is authenticated as the victim. RegenerateRequestSession carries the values over under a fresh id and republishes it on the request, so the identity below is written to the id the response emits. */
    rotatedSession, regenerateErr := melodyhttp.RegenerateRequestSession(request)
    if nil != regenerateErr {
        return nil, regenerateErr
    }

    rotatedSession.Set(SessionKeySecurityUserId, userIdentifier)
    rotatedSession.Set(SessionKeySecurityRoles, roles)

    response, err := melodyhttp.JsonResponse(http.StatusOK, map[string]any{
        "success": true,
        "userId":  userIdentifier,
    })
    if nil != err {
        return nil, err
    }

    return &melodysecuritycontract.LoginResult{
        Token:    input.Token,
        Response: response,
    }, nil
}

var _ melodysecuritycontract.LoginHandler = (*sessionLoginHandler)(nil)
