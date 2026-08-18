package user

import (
    "encoding/json"
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/.example/entity"
    "github.com/precision-soft/melody/.example/presenter"
    "github.com/precision-soft/melody/.example/repository"
    "github.com/precision-soft/melody/.example/security"
    "github.com/precision-soft/melody/.example/service"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/security"
)

func ApiUpdateHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        if false == melodysecurity.IsGranted(runtimeInstance, entity.RoleAdmin) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "forbidden"), nil
        }

        id, exists := request.Param("id")
        if false == exists {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "id is required"), nil
        }

        id = strings.TrimSpace(id)
        if "" == id {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "id is required"), nil
        }

        var dto adminUserUpdateRequest
        decodeErr := json.NewDecoder(request.HttpRequest().Body).Decode(&dto)
        if nil != decodeErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "invalid json"), nil
        }

        userService := service.MustGetUserService(runtimeInstance.Container())

        targetUser, found, findErr := userService.FindById(id)
        if nil != findErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to load user", findErr), nil
        }
        if false == found {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "not found"), nil
        }

        actorUserId, _ := Actor(runtimeInstance)

        if true == protectsAnotherAdmin(actorUserId, targetUser) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "cannot modify another admin"), nil
        }

        normalizedUsername := strings.TrimSpace(dto.Username)
        if "" != normalizedUsername {
            /* the same spelling door the create handler holds: the username becomes a cache key component */
            if false == service.CacheSafeIdentifier(repository.NormalizedUsername(normalizedUsername)) {
                return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "username must not contain spaces or newlines and must stay within 255 bytes"), nil
            }

            if normalizedUsername != targetUser.Username {
                otherUser, otherExists, otherFindErr := userService.FindByUsername(normalizedUsername)
                if nil != otherFindErr {
                    return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to check username", otherFindErr), nil
                }

                if true == otherExists {
                    if nil != otherUser {
                        if otherUser.Id != targetUser.Id {
                            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "username already exists"), nil
                        }
                    }
                }
            }

            targetUser.Username = normalizedUsername
        }

        normalizedPassword := strings.TrimSpace(dto.Password)
        if "" != normalizedPassword {
            if security.PasswordMaximumBytes < len(normalizedPassword) {
                return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "password must not exceed 72 bytes"), nil
            }

            passwordHash, hashErr := security.HashPassword(normalizedPassword)
            if nil != hashErr {
                return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to hash password", hashErr), nil
            }

            targetUser.Password = passwordHash
        }

        if commaRole, hasCommaRole := roleContainingComma(dto.Roles); true == hasCommaRole {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "role "+commaRole+" must not contain commas"), nil
        }

        targetUser.Roles = normalizeRoles(dto.Roles)

        updatedUser, updated, updateErr := userService.Update(
            runtimeInstance,
            targetUser.Id,
            targetUser.Username,
            targetUser.Password,
            targetUser.Roles,
        )
        if nil != updateErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to update user", updateErr), nil
        }

        if false == updated {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "not found"), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{
            "id":       updatedUser.Id,
            "username": updatedUser.Username,
            "roles":    append([]string{}, updatedUser.Roles...),
        }), nil
    }
}

func Actor(runtimeInstance melodyruntimecontract.Runtime) (string, []string) {
    securityContext, exists := melodysecurity.SecurityContextFromRuntime(runtimeInstance)
    if false == exists {
        return "", []string{}
    }

    token := securityContext.Token()
    if nil == token {
        return "", []string{}
    }

    return token.UserIdentifier(), token.Roles()
}

type adminUserUpdateRequest struct {
    Username string   `json:"username"`
    Password string   `json:"password"`
    Roles    []string `json:"roles"`
}

/* protectsAnotherAdmin answers whether the change the actor is asking for would touch an administrator who is not the actor. An administrator may edit and delete their own account and everyone below them, and may not reach a peer: an account that can grant roles is the one account whose holder must not be able to lock a colleague out or take their place quietly. Both the update and the delete door ask the same question, so the two cannot drift apart on who is protected — only on the words they refuse with. */
func protectsAnotherAdmin(actorUserId string, targetUser *entity.User) bool {
    if nil == targetUser {
        return false
    }

    if false == hasRole(targetUser.Roles, entity.RoleAdmin) {
        return false
    }

    return actorUserId != targetUser.Id
}

func hasRole(roles []string, role string) bool {
    for _, currentRole := range roles {
        if role == currentRole {
            return true
        }
    }

    return false
}
