package handler

import (
    "encoding/json"
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/.example/domain/service"
    "github.com/precision-soft/melody/.example/infra/http/page"
    "github.com/precision-soft/melody/.example/infra/http/presenter"
    "github.com/precision-soft/melody/.example/infra/http/route"
    "github.com/precision-soft/melody/.example/infra/security"
    melodyhttp "github.com/precision-soft/melody/http"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
    melodysessioncontract "github.com/precision-soft/melody/session/contract"
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

        passwordHash := security.Sha256Hex(password)

        userService := service.MustGetUserService(runtimeInstance.Container())

        user, authenticated, authenticationErr := userService.AuthenticateByUsernameAndPasswordHash(
            username,
            passwordHash,
        )
        if nil != authenticationErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "authentication failed", authenticationErr.Error()), nil
        }

        if false == authenticated {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "invalid credentials"), nil
        }

        sessionInstance := getSessionFromRequest(request)
        if nil == sessionInstance {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "session is not available"), nil
        }

        sessionInstance.Set(security.SessionKeySecurityUserId, user.Id)
        sessionInstance.Set(security.SessionKeySecurityRoles, user.Roles)

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
