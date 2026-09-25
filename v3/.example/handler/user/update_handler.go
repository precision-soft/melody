package user

import (
    "encoding/json"
    "errors"
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/presenter"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/security"
    "github.com/precision-soft/melody/v3/.example/service"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
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
            return presenter.ApiRefusal(runtimeInstance, request, nethttp.StatusBadRequest, "invalid json", decodeErr), nil
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
            /* the username becomes a cache key component and a 255-byte column, so a longer spelling is turned away before the row lands */
            if false == service.CacheSafeIdentifier(repository.NormalizedUsername(normalizedUsername)) {
                return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "username must stay within 255 bytes"), nil
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
            /* bcrypt reads at most 72 bytes of the plaintext, so a longer password is refused as the caller's mistake */
            if security.PasswordMaximumBytes < len(normalizedPassword) {
                return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, security.PasswordTooLongMessage), nil
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

        if unknownRole, hasUnknownRole := roleOutsideTheVocabulary(dto.Roles); true == hasUnknownRole {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "role "+unknownRole+" is not one this application knows ("+strings.Join(entity.KnownRoleList(), ", ")+")"), nil
        }

        targetUser.Roles = rolesForUpdate(dto.Roles, targetUser.Roles)

        updatedUser, updated, updateErr := userService.Update(
            runtimeInstance,
            targetUser.Id,
            targetUser.Username,
            targetUser.Password,
            targetUser.Roles,
        )
        if nil != updateErr {
            /* the read above is a check and the unique index is the guard: a rename the index refuses is the caller's 400 */
            if true == errors.Is(updateErr, repository.ErrUsernameAlreadyExists) {
                return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "username already exists"), nil
            }

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
    token, exists := security.TokenFromRuntime(runtimeInstance)
    if false == exists {
        return "", []string{}
    }

    return token.UserIdentifier(), token.Roles()
}

type adminUserUpdateRequest struct {
    Username string   `json:"username"`
    Password string   `json:"password"`
    Roles    []string `json:"roles"`
}

/* rolesForUpdate answers the roles an update stores: the ones the body named, normalised, or the target's own when the body named none, as an omitted username or password is kept. An explicitly empty list falls back to the base role, the rule normalizeRoles carries; the decoder leaves the field nil only when the caller never named it. */
func rolesForUpdate(requested []string, current []string) []string {
    if nil == requested {
        return current
    }

    return normalizeRoles(requested)
}

/* protectsAnotherAdmin answers whether the change would touch an administrator who is not the actor. An administrator may edit and delete their own account and everyone below, never a peer; the update and the delete door both ask it. */
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
