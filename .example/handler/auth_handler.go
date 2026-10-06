package handler

import (
    "encoding/json"
    "errors"
    "mime"
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/.example/page"
    "github.com/precision-soft/melody/.example/presenter"
    "github.com/precision-soft/melody/.example/route"
    "github.com/precision-soft/melody/.example/security"
    "github.com/precision-soft/melody/.example/service"
    melodyevent "github.com/precision-soft/melody/event"
    melodyhttp "github.com/precision-soft/melody/http"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/security"
    melodysecuritycontract "github.com/precision-soft/melody/security/contract"
    melodysessioncontract "github.com/precision-soft/melody/session/contract"
)

func LoginPageHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        return page.Html(runtimeInstance, request, nethttp.StatusOK, page.LoginHtml), nil
    }
}

/* LoginHandler signs an account in and admits the session through sessionIndex, which keeps the account under repository.UserSessionCap. It reads an application/json body alone and refuses any other with 415: a cross-site form cannot post json, so the door needs no anti-forgery token. */
func LoginHandler(sessionIndex security.SessionIndexLookup) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        type adminLoginRequest struct {
            Username string `json:"username"`
            Password string `json:"password"`
        }

        var dto adminLoginRequest

        httpRequest := request.HttpRequest()

        /* the sign-in reads json alone: a top-level cross-site form can post urlencoded, multipart or text/plain, never json, so a door that refuses every other body needs no anti-forgery token */
        mediaType, _, mediaTypeErr := mime.ParseMediaType(httpRequest.Header.Get("Content-Type"))
        if nil != mediaTypeErr || "application/json" != mediaType {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnsupportedMediaType, "the sign-in reads application/json"), nil
        }

        decoderErr := json.NewDecoder(httpRequest.Body).Decode(&dto)
        if nil != decoderErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "invalid json"), nil
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

        /* the id the rotation retires is read first, so its row leaves the index with it */
        previousSessionId := sessionInstance.Id()

        /* the session id is rotated before the authenticated identity is written, against session fixation: a pre-login id the client held must not survive into the authenticated session. RegenerateRequestSession republishes the rotated session on the request, so the identity lands on the id the response emits. */
        rotatedSession, regenerateErr := melodyhttp.RegenerateRequestSession(request)
        if nil != regenerateErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "session rotation failed", regenerateErr), nil
        }

        rotatedSession.Set(security.SessionKeySecurityUserId, user.Id)
        rotatedSession.Set(security.SessionKeySecurityRoles, append([]string{}, user.Roles...))
        rotatedSession.Set(security.SessionKeySecurityCredentialVersion, security.SessionCredentialVersion(user.Password))

        /* past the cap the account's oldest session ends here; a refused admission has cleared the rotated session, so the refusal opens nothing */
        if admitErr := security.AdmitSession(request, sessionIndex, user.Id, previousSessionId, rotatedSession); nil != admitErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "session admission failed", admitErr), nil
        }

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

/* LogoutHandler ends the session and takes its row out of sessionIndex, so the account's place is free for its next sign-in. */
func LogoutHandler(sessionIndex security.SessionIndexLookup) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        indexUrl := "/"

        sessionInstance := getSessionFromRequest(request)
        if nil == sessionInstance {
            return presenter.Redirect(runtimeInstance, request, indexUrl), nil
        }

        /* the whole session ends, not only the identity in it: an emptied session would be saved back under the same id with a re-issued cookie, while Clear routes the response path to DeleteSession and to the expired cookie */
        sessionInstance.Clear()

        /* the sign-out stands whatever the index answers */
        if releaseErr := security.ReleaseSession(request, sessionIndex, sessionInstance.Id()); nil != releaseErr {
            security.JournalUnreleasedSession(runtimeInstance, releaseErr)
        }

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
