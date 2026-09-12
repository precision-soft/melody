package user

import (
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/presenter"
    "github.com/precision-soft/melody/v3/.example/security"
    "github.com/precision-soft/melody/v3/.example/service"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysessioncontract "github.com/precision-soft/melody/v3/session/contract"
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

/* normalizeRoles answers the roles in the order they were given, deduplicated. The order is part of the answer rather than an accident of it: the repository stores the list comma-joined into one column, so a set built by ranging a map is written back in a different spelling on roughly a quarter of the saves — measured at 131 of 1 000 for two roles and 226 of 1 000 for three — and the audit trail, which compares the stored values, then records a "roles changed" entry naming a change nobody asked for, with the same roles on both sides of it. */
func normalizeRoles(roles []string) []string {
    seen := map[string]struct{}{}
    result := make([]string, 0, len(roles))

    for _, role := range roles {
        normalized := strings.TrimSpace(role)
        if "" == normalized {
            continue
        }

        if _, exists := seen[normalized]; true == exists {
            continue
        }

        seen[normalized] = struct{}{}
        result = append(result, normalized)
    }

    if 0 == len(result) {
        return []string{entity.RoleUser}
    }

    return result
}

/* roleContainingComma reports the first role carrying a comma: the repository stores the role list comma-joined, so a role with one inside would come back as several roles on the next read — among them, possibly, an administrator nobody granted. */
func roleContainingComma(roles []string) (string, bool) {
    for _, role := range roles {
        if true == strings.Contains(role, ",") {
            return role, true
        }
    }

    return "", false
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

/* getStringSliceFromSession accepts the two spellings a role list has in a session: the []string the login handler writes, and the []any a file-backed storage answers after a restart — its snapshot round-trips through json, which keeps no element type. The second form is accepted only when EVERY element is a string; anything else stays a refusal. */
func getStringSliceFromSession(sessionInstance melodysessioncontract.Session, key string) ([]string, bool) {
    if false == sessionInstance.Has(key) {
        return nil, false
    }

    value := sessionInstance.Get(key)

    typed, ok := value.([]string)
    if true == ok {
        return typed, true
    }

    untyped, ok := value.([]any)
    if false == ok {
        return nil, false
    }

    restored := make([]string, 0, len(untyped))
    for _, element := range untyped {
        elementString, elementOk := element.(string)
        if false == elementOk {
            return nil, false
        }

        restored = append(restored, elementString)
    }

    return restored, true
}
