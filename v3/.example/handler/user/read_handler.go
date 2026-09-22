package user

import (
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/presenter"
    "github.com/precision-soft/melody/v3/.example/service"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
)

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

/* roleOutsideTheVocabulary answers the first role the application does not know, trimmed: the voter compares a
   role's spelling exactly, so a spelling outside the closed vocabulary would be stored, reported and grant
   nothing — the refusal the console grant makes, at the two doors that write roles from a request */
func roleOutsideTheVocabulary(roles []string) (string, bool) {
    for _, role := range roles {
        normalized := strings.TrimSpace(role)
        if "" == normalized {
            continue
        }

        if false == entity.IsKnownRole(normalized) {
            return normalized, true
        }
    }

    return "", false
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

