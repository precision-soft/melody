package security

import (
    "errors"
    "net/http"

    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* NewSessionLoginHandler writes the session of an authenticated token from the account the lookup reads, not from the token: the session carries the account's roles and the credential version of its password hash, which SessionTokenResolver checks on every later request. */
func NewSessionLoginHandler(lookupUser SessionUserLookup) melodysecuritycontract.LoginHandler {
    return &sessionLoginHandler{
        lookupUser: lookupUser,
    }
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
        return nil, errors.New("an authenticated token is required to open a session")
    }

    userIdentifier := input.Token.UserIdentifier()

    user, found, lookupErr := instance.lookupUser(request, userIdentifier)
    if nil != lookupErr {
        return nil, lookupErr
    }

    if false == found || nil == user || userIdentifier != user.Id || "" == user.Password || 0 == len(user.Roles) {
        return nil, errors.New("the authenticated account is not available")
    }

    /* the session id is rotated before the authenticated identity is written, against session fixation: an id chosen before authentication, possibly seeded by an attacker, must not carry the identity. RegenerateRequestSession carries the values over under a fresh id and republishes it on the request, so the identity lands on the id the response emits. */
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
