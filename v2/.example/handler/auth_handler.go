package handler

import (
    "encoding/json"
    "errors"
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/v2/.example/page"
    "github.com/precision-soft/melody/v2/.example/presenter"
    "github.com/precision-soft/melody/v2/.example/route"
    "github.com/precision-soft/melody/v2/.example/security"
    "github.com/precision-soft/melody/v2/.example/service"
    melodyevent "github.com/precision-soft/melody/v2/event"
    melodyhttp "github.com/precision-soft/melody/v2/http"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v2/security"
    melodysecuritycontract "github.com/precision-soft/melody/v2/security/contract"
    melodysessioncontract "github.com/precision-soft/melody/v2/session/contract"
)

func LoginPageHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        return page.Html(runtimeInstance, request, nethttp.StatusOK, page.LoginHtml), nil
    }
}

func LoginHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        type adminLoginRequest struct {
            Username string `json:"username"`
            Password string `json:"password"`
        }

        var dto adminLoginRequest

        httpRequest := request.HttpRequest()
        contentType := httpRequest.Header.Get("Content-Type")

        if true == strings.HasPrefix(contentType, "application/json") {
            decoderErr := json.NewDecoder(httpRequest.Body).Decode(&dto)
            if nil != decoderErr {
                return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "invalid json"), nil
            }
        } else {
            parseFormErr := httpRequest.ParseForm()
            if nil != parseFormErr {
                return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "invalid form"), nil
            }

            /* the credentials are read from the body alone: FormValue would also read the url query, which lands in every access log in front of the application */
            dto.Username = httpRequest.PostFormValue("username")
            dto.Password = httpRequest.PostFormValue("password")
        }

        username := strings.TrimSpace(dto.Username)
        password := strings.TrimSpace(dto.Password)

        if "" == username || "" == password {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "invalid credentials input"), nil
        }

        userService := service.MustGetUserService(runtimeInstance.Container())

        user, authenticated, authenticationErr := userService.AuthenticateByUsernameAndPassword(
            runtimeInstance.Context(),
            username,
            password,
        )
        if nil != authenticationErr {
            /* the cause names internals and this door is unauthenticated, so it stays out of the errors list; ApiErrorWithErr journals it and keeps it in the debug-gated context */
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "authentication failed", authenticationErr), nil
        }

        if false == authenticated {
            if dispatchErr := dispatchLoginFailure(runtimeInstance, request); nil != dispatchErr {
                /* the refusal keeps its status, as the framework's token source keeps it, and the dispatch failure is journaled here under the token source's message: ApiErrorWithErr journals a server-class status only, so a 401 would carry the cause nowhere outside the debug context */
                presenter.JournalRefusalCause(runtimeInstance, request, nethttp.StatusUnauthorized, "invalid credentials", "security login failure event dispatch failed", dispatchErr)

                return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusUnauthorized, "invalid credentials", dispatchErr), nil
            }

            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "invalid credentials"), nil
        }

        sessionInstance := getSessionFromRequest(request)
        if nil == sessionInstance {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "session is not available"), nil
        }

        /* the session id is rotated before the authenticated identity is written, against session fixation: a pre-login id the client held must not survive into the authenticated session. RegenerateRequestSession republishes the rotated session on the request, so the identity lands on the id the response emits. */
        rotatedSession, regenerateErr := melodyhttp.RegenerateRequestSession(request)
        if nil != regenerateErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "session rotation failed", regenerateErr), nil
        }

        rotatedSession.Set(security.SessionKeySecurityUserId, user.Id)
        rotatedSession.Set(security.SessionKeySecurityRoles, append([]string{}, user.Roles...))
        rotatedSession.Set(security.SessionKeySecurityCredentialVersion, security.SessionCredentialVersion(user.Password))

        redirectUrl, _ := melodyhttp.UrlGeneratorMustFromContainer(runtimeInstance.Container()).GeneratePath(route.ProductsListPageName, nil)

        return presenter.ApiSuccess(
            runtimeInstance,
            request,
            nethttp.StatusOK,
            map[string]any{
                "redirectUrl": redirectUrl,
            },
        ), nil
    }
}

/* errInvalidCredentials is the failure the login door reports on the security.login.failure event for a refused username or password. It names neither, so the journal records the refusal without the credentials that were tried. */
var errInvalidCredentials = errors.New("invalid credentials")

/* dispatchLoginFailure raises the login failure the firewall's own Login raises for a refused login. This door authenticates the credentials itself rather than through the firewall, so without it a refused password reached none of the security.login.failure listeners, the security journal's among them. */
func dispatchLoginFailure(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request) error {
    _, dispatchErr := melodyevent.EventDispatcherMustFromContainer(runtimeInstance.Container()).DispatchName(
        runtimeInstance,
        melodysecuritycontract.EventSecurityLoginFailure,
        melodysecurity.NewLoginFailureEvent(request, errInvalidCredentials),
    )

    return dispatchErr
}

func LogoutHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        indexUrl := "/"

        sessionInstance := getSessionFromRequest(request)
        if nil == sessionInstance {
            return presenter.Redirect(runtimeInstance, request, indexUrl), nil
        }

        /* the whole session ends, not only the identity in it: an emptied session would be saved back under the same id with a re-issued cookie, while Clear routes the response path to DeleteSession and to the expired cookie */
        sessionInstance.Clear()

        return presenter.Redirect(runtimeInstance, request, indexUrl), nil
    }
}

func getSessionFromRequest(request melodyhttpcontract.Request) melodysessioncontract.Session {
    if nil == request {
        return nil
    }

    attributes := request.Attributes()
    if nil == attributes {
        return nil
    }

    value, exists := attributes.Get(melodyhttp.RequestAttributeSession)
    if false == exists {
        return nil
    }

    sessionInstance, ok := value.(melodysessioncontract.Session)
    if false == ok {
        return nil
    }

    return sessionInstance
}
