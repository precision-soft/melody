package user

import (
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/v2/.example/domain/entity"
    "github.com/precision-soft/melody/v2/.example/domain/service"
    "github.com/precision-soft/melody/v2/.example/infra/http/presenter"
    "github.com/precision-soft/melody/v2/.example/infra/security"
    melodyhttp "github.com/precision-soft/melody/v2/http"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v2/security"
    melodysessioncontract "github.com/precision-soft/melody/v2/session/contract"
)

func ReadCurrentHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        userId := ""
        roles := []string{}

        sessionInstance := getSession(request)
        if nil != sessionInstance {
            userIdValue, ok := getStringFromSession(sessionInstance, security.SessionKeySecurityUserId)
            if true == ok {
                userId = userIdValue
            }

            rolesValue, ok := getStringSliceFromSession(sessionInstance, security.SessionKeySecurityRoles)
            if true == ok {
                roles = rolesValue
            }
        }

        if "" == userId {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "unauthenticated"), nil
        }

        return presenter.ApiSuccess(
            runtimeInstance,
            request,
            nethttp.StatusOK,
            userCurrentResponse{
                UserId: userId,
                Roles:  roles,
            },
        ), nil
    }
}

func ApiReadAllHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        if false == melodysecurity.IsGranted(runtimeInstance, entity.RoleAdmin) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "forbidden"), nil
        }

        userService := service.MustGetUserService(runtimeInstance.Container())

        users, listErr := userService.List()
        if nil != listErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to list users", listErr), nil
        }

        payload := make([]map[string]any, 0, len(users))
        for _, user := range users {
            if nil == user {
                continue
            }

            payload = append(
                payload,
                map[string]any{
                    "id":       user.Id,
                    "username": user.Username,
                    "roles":    append([]string{}, user.Roles...),
                },
            )
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, payload), nil
    }
}

func ApiReadHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        if false == melodysecurity.IsGranted(runtimeInstance, entity.RoleAdmin) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "forbidden"), nil
        }

        id, exists := request.Param("id")
        if false == exists {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "id is required"), nil
        }

        if "" == strings.TrimSpace(id) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "id is required"), nil
        }

        userService := service.MustGetUserService(runtimeInstance.Container())

        user, found, findErr := userService.FindById(id)
        if nil != findErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to load user", findErr), nil
        }
        if false == found {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "not found"), nil
        }

        payload := map[string]any{
            "id":       user.Id,
            "username": user.Username,
            "roles":    append([]string{}, user.Roles...),
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, payload), nil
    }
}

func normalizeRoles(roles []string) []string {
    unique := map[string]struct{}{}

    for _, role := range roles {
        normalized := strings.TrimSpace(role)
        if "" == normalized {
            continue
        }

        unique[normalized] = struct{}{}
    }

    result := make([]string, 0, len(unique))
    for role := range unique {
        result = append(result, role)
    }

    if 0 == len(result) {
        return []string{entity.RoleUser}
    }

    return result
}

func getSession(request melodyhttpcontract.Request) melodysessioncontract.Session {
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

func getStringFromSession(sessionInstance melodysessioncontract.Session, key string) (string, bool) {
    if false == sessionInstance.Has(key) {
        return "", false
    }

    value := sessionInstance.Get(key)

    typed, ok := value.(string)
    if false == ok {
        return "", false
    }

    return typed, true
}

func getStringSliceFromSession(sessionInstance melodysessioncontract.Session, key string) ([]string, bool) {
    if false == sessionInstance.Has(key) {
        return nil, false
    }

    value := sessionInstance.Get(key)

    typed, ok := value.([]string)
    if false == ok {
        return nil, false
    }

    return typed, true
}
