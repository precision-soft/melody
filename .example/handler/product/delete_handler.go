package product

import (
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/.example/entity"
    "github.com/precision-soft/melody/.example/presenter"
    "github.com/precision-soft/melody/.example/service"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/security"
)

func ApiDeleteHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        if false == melodysecurity.IsGranted(runtimeInstance, entity.RoleEditor) {
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

        productService := service.MustGetProductService(runtimeInstance.Container())

        deleted, deleteErr := productService.DeleteById(
            runtimeInstance,
            id,
        )
        if nil != deleteErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to delete product", deleteErr), nil
        }

        if false == deleted {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "not found"), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{
            "deleted": true,
        }), nil
    }
}
