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

func ApiCreateHandler() melodyhttpcontract.Handler {
	return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
		if false == melodysecurity.IsGranted(runtimeInstance, entity.RoleAdmin) {
			return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "forbidden"), nil
		}

		var dto adminUserCreateRequest
		decodeErr := json.NewDecoder(request.HttpRequest().Body).Decode(&dto)
		if nil != decodeErr {
			return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "invalid json"), nil
		}

		normalizedUsername := strings.TrimSpace(dto.Username)
		if "" == normalizedUsername {
			return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "username is required"), nil
		}

		normalizedPassword := strings.TrimSpace(dto.Password)
		if "" == normalizedPassword {
			return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "password is required"), nil
		}

		userService := service.MustGetUserService(runtimeInstance.Container())

		_, exists, findErr := userService.FindByUsername(normalizedUsername)
		if nil != findErr {
			return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to check username", findErr), nil
		}
		if true == exists {
			return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "username already exists"), nil
		}

		user, createErr := userService.Create(
			runtimeInstance,
			"",
			normalizedUsername,
			security.Sha256Hex(normalizedPassword),
			normalizeRoles(dto.Roles),
		)
		if nil != createErr {
			return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to create user", createErr), nil
		}

		return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusCreated, map[string]any{
			"id":       user.Id,
			"username": user.Username,
			"roles":    append([]string{}, user.Roles...),
		}), nil
	}
}
