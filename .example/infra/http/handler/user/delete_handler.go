package user

import (
	nethttp "net/http"
	"strings"

	"github.com/precision-soft/melody/.example/domain/entity"
	"github.com/precision-soft/melody/.example/domain/service"
	"github.com/precision-soft/melody/.example/infra/http/presenter"
	melodyhttpcontract "github.com/precision-soft/melody/http/contract"
	melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
	melodysecurity "github.com/precision-soft/melody/security"
)

func ApiDeleteHandler() melodyhttpcontract.Handler {
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
				return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "cannot delete another admin"), nil
			}
		}

		deleted, deleteErr := userService.DeleteById(runtimeInstance, id)
		if nil != deleteErr {
			return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to delete user", deleteErr), nil
		}

		if false == deleted {
			return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "not found"), nil
		}

		return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{
			"deleted": true,
		}), nil
	}
}
