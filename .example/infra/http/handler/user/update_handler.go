package user

import (
	"encoding/json"
	nethttp "net/http"
	"strings"

	"github.com/precision-soft/melody/.example/domain/entity"
	"github.com/precision-soft/melody/.example/domain/service"
	"github.com/precision-soft/melody/.example/infra/http/presenter"
	"github.com/precision-soft/melody/.example/infra/security"
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

		if true == hasRole(targetUser.Roles, entity.RoleAdmin) {
			if actorUserId != targetUser.Id {
				return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "cannot modify another admin"), nil
			}
		}

		normalizedUsername := strings.TrimSpace(dto.Username)
		if "" != normalizedUsername {
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
			targetUser.Password = security.Sha256Hex(normalizedPassword)
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

func hasRole(roles []string, role string) bool {
	for _, currentRole := range roles {
		if role == currentRole {
			return true
		}
	}

	return false
}
