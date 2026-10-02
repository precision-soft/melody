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

        /* the change carries only what the body names: an omitted username, password or role list is never written, so the directory keeps whatever it holds when the locked write runs */
        change := repository.UserChange{}

        normalizedUsername := strings.TrimSpace(dto.Username)
        if "" != normalizedUsername {
            /* the username becomes a cache key component and a 255-byte column, so a longer spelling is turned away before the row lands */
            if false == service.CacheSafeIdentifier(repository.NormalizedUsername(normalizedUsername)) {
                return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "username must stay within 255 bytes"), nil
            }

            change.Username = &normalizedUsername
        }

        normalizedPassword := strings.TrimSpace(dto.Password)
        if "" != normalizedPassword {
            /* bcrypt reads at most 72 bytes of the plaintext, so a longer password is refused as the caller's mistake */
            if security.PasswordMaximumBytes < len(normalizedPassword) {
                return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, security.PasswordTooLongMessage), nil
            }

            /* hashed before the write door is entered, so the row's lock is never held across bcrypt */
            passwordHash, hashErr := security.HashPassword(normalizedPassword)
            if nil != hashErr {
                return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to hash password", hashErr), nil
            }

            change.PasswordHash = &passwordHash
        }

        if commaRole, hasCommaRole := roleContainingComma(dto.Roles); true == hasCommaRole {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "role "+commaRole+" must not contain commas"), nil
        }

        if unknownRole, hasUnknownRole := roleOutsideTheVocabulary(dto.Roles); true == hasUnknownRole {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "role "+unknownRole+" is not one this application knows ("+strings.Join(entity.KnownRoleList(), ", ")+")"), nil
        }

        change.Roles = rolesForUpdate(dto.Roles)

        actorUserId, _ := Actor(runtimeInstance)

        updatedUser, updated, updateErr := service.MustGetUserService(runtimeInstance.Container()).Update(
            runtimeInstance,
            id,
            change,
            refusingAnotherAdmin(actorUserId),
        )
        if nil != updateErr {
            if true == errors.Is(updateErr, errAnotherAdmin) {
                return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "cannot modify another admin"), nil
            }

            /* the locked read and the unique index both answer this refusal: a rename onto a taken name is the caller's 400 */
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

/* rolesForUpdate answers the roles an update writes: the ones the body named, normalised, or nil when the body named none, which the change reads as "leave the stored roles". An explicitly empty list falls back to the base role, the rule normalizeRoles carries; the decoder leaves the field nil only when the caller never named it. */
func rolesForUpdate(requested []string) []string {
    if nil == requested {
        return nil
    }

    return normalizeRoles(requested)
}

/* errAnotherAdmin is the guard's refusal, mapped by both doors onto their 403 */
var errAnotherAdmin = errors.New("the account is another administrator")

/* refusingAnotherAdmin is the guard both write doors hand the repository, so the peer-administrator decision is taken on the account as the locked read holds it, never on a cached copy */
func refusingAnotherAdmin(actorUserId string) repository.UserGuard {
    return func(current *entity.User) error {
        if true == protectsAnotherAdmin(actorUserId, current) {
            return errAnotherAdmin
        }

        return nil
    }
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
