package security

import (
    "errors"
    "net/http"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/presenter"
    "github.com/precision-soft/melody/v4/.example/route"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* NewSessionLoginHandler is what the firewall's sign-in door runs once it authenticated the credentials. It writes the session of the token from the account the lookup reads, not from the token: the session carries the account's roles and the credential version of its password hash, which SessionTokenResolver checks on every later request. */
func NewSessionLoginHandler(lookupUser SessionUserLookup, sessionIndex SessionIndexLookup) melodysecuritycontract.LoginHandler {
    return &sessionLoginHandler{
        lookupUser:   lookupUser,
        sessionIndex: sessionIndex,
    }
}

type sessionLoginHandler struct {
    lookupUser   SessionUserLookup
    sessionIndex SessionIndexLookup
}

func (instance *sessionLoginHandler) Login(
    runtimeInstance melodyruntimecontract.Runtime,
    request melodyhttpcontract.Request,
    input melodysecuritycontract.LoginInput,
) (*melodysecuritycontract.LoginResult, error) {
    sessionInstance, sessionErr := melodyhttp.RequestSession(request)
    if nil != sessionErr {
        return nil, sessionErr
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
        /* the cause is journaled with the token's user; this door is unauthenticated, so the public message says nothing of it */
        return nil, melodyexception.NewHttpError(
            http.StatusInternalServerError,
            melodyexception.HttpErrorConfig{
                Message: "authentication failed",
                Cause:   melodyexception.NewError("the authenticated account is not available", map[string]any{"tokenUser": userIdentifier}, nil),
            },
        )
    }

    /* the door rotated the id before this handler ran, against session fixation, and hands over the one it retired; the session on the request is the one the response emits */
    rotatedSession := sessionInstance

    rotatedSession.Set(SessionKeySecurityUserId, userIdentifier)
    rotatedSession.Set(SessionKeySecurityRoles, append([]string{}, user.Roles...))
    rotatedSession.Set(SessionKeySecurityCredentialVersion, SessionCredentialVersion(user.Password))

    /* past the cap the account's oldest session ends here; a refused admission has cleared the rotated session, so the refusal opens nothing */
    if admitErr := AdmitSession(request, instance.sessionIndex, userIdentifier, input.PreviousSessionId, rotatedSession); nil != admitErr {
        return nil, admitErr
    }

    redirectUrl, _ := melodyhttp.UrlGeneratorMustFromContainer(runtimeInstance.Context(), runtimeInstance.Container()).GeneratePath(LandingPageName(user.Roles), nil)

    response := presenter.ApiSuccess(
        runtimeInstance,
        request,
        http.StatusOK,
        map[string]any{
            "redirectUrl": redirectUrl,
        },
    )

    return &melodysecuritycontract.LoginResult{
        Token:    input.Token,
        Response: response,
    }, nil
}

/* LandingPageName answers the page a sign-in lands on: the product list for a role that edits the catalogue, and the read-only catalogue every account holding ROLE_USER opens otherwise, so an account whose role opens no editing page is not sent to a refusal */
func LandingPageName(roleList []string) string {
    if true == HoldsRole(NewRoleHierarchy().ExpandRoles(roleList), entity.RoleEditor) {
        return route.ProductsListPageName
    }

    return route.CataloguePageName
}

var _ melodysecuritycontract.LoginHandler = (*sessionLoginHandler)(nil)
