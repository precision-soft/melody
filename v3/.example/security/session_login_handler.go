package security

import (
    "net/http"
    "fmt"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

func NewSessionLoginHandler(lookupUser SessionUserLookup) melodysecuritycontract.LoginHandler {
    return &sessionLoginHandler{lookupUser: lookupUser}
}

type sessionLoginHandler struct {
    lookupUser SessionUserLookup
}

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

    if nil == input.Token || false == input.Token.IsAuthenticated() {
        return nil, fmt.Errorf("authenticated user is required")
    }
    userIdentifier := input.Token.UserIdentifier()
    user, found, lookupErr := instance.lookupUser(request, userIdentifier)
    if nil != lookupErr {
        return nil, lookupErr
    }
    if false == found || nil == user || userIdentifier != user.Id || "" == user.Password || 0 == len(user.Roles) {
        return nil, fmt.Errorf("authenticated user is unavailable")
    }

    /* rotate the session id before the authenticated identity is written to it: a client that presents a session id chosen before authentication must not keep that id once it carries the identity, or an id an attacker seeded and planted in the victim's browser is authenticated as the victim. RegenerateRequestSession carries the values over under a fresh id and republishes it on the request, so the identity below is written to the id the response emits. */
    rotatedSession, regenerateErr := melodyhttp.RegenerateRequestSession(request)
    if nil != regenerateErr {
        return nil, regenerateErr
    }

    rotatedSession.Set(SessionKeySecurityUserId, userIdentifier)
    rotatedSession.Set(SessionKeySecurityRoles, append([]string{}, user.Roles...))
    rotatedSession.Set(SessionKeySecurityCredentialVersion, SessionCredentialVersion(user.Password))

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
