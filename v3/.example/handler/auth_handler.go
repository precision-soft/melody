package handler

import (
    "encoding/json"
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/v3/.example/page"
    "github.com/precision-soft/melody/v3/.example/presenter"
    "github.com/precision-soft/melody/v3/.example/route"
    "github.com/precision-soft/melody/v3/.example/security"
    "github.com/precision-soft/melody/v3/.example/service"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysessioncontract "github.com/precision-soft/melody/v3/session/contract"
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

            dto.Username = httpRequest.FormValue("username")
            dto.Password = httpRequest.FormValue("password")
        }

        username := strings.TrimSpace(dto.Username)
        password := strings.TrimSpace(dto.Password)

        if "" == username || "" == password {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "invalid credentials input"), nil
        }

        userService := service.MustGetUserService(runtimeInstance.Container())

        user, authenticated, authenticationErr := userService.AuthenticateByUsernameAndPassword(
            username,
            password,
        )
        if nil != authenticationErr {
            /* the cause stays out of the errors list on purpose: it names internals — a cache refusal, a store address — and this is an unauthenticated door; ApiErrorWithErr keeps it in the debug-gated context instead */
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "authentication failed", authenticationErr), nil
        }

        if false == authenticated {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "invalid credentials"), nil
        }

        sessionInstance := getSessionFromRequest(request)
        if nil == sessionInstance {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "session is not available"), nil
        }

        /* rotate the session id before writing the authenticated identity, the defence against session fixation: a pre-login id the client already held — one an attacker could have seeded and planted — must not survive into the authenticated session. RegenerateRequestSession republishes the rotated session on the request, so the identity is written to the id the response emits. */
        rotatedSession, regenerateErr := melodyhttp.RegenerateRequestSession(request)
        if nil != regenerateErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "session rotation failed", regenerateErr), nil
        }

        rotatedSession.Set(security.SessionKeySecurityUserId, user.Id)
        rotatedSession.Set(security.SessionKeySecurityRoles, user.Roles)

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

func LogoutHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        indexUrl := "/"

        sessionInstance := getSessionFromRequest(request)
        if nil == sessionInstance {
            return presenter.Redirect(runtimeInstance, request, indexUrl), nil
        }

        sessionInstance.Delete(security.SessionKeySecurityUserId)
        sessionInstance.Delete(security.SessionKeySecurityRoles)

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
