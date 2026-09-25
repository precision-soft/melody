package security

import (
    "net/http"

    melodyhttp "github.com/precision-soft/melody/v2/http"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v2/security/contract"
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

    /* the session id is rotated before the authenticated identity is written, against session fixation: an id chosen before authentication, possibly seeded by an attacker, must not carry the identity. RegenerateRequestSession carries the values over under a fresh id and republishes it on the request, so the identity lands on the id the response emits. */
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
