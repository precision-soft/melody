package product

import (
    "encoding/json"
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/v2/.example/domain/entity"
    "github.com/precision-soft/melody/v2/.example/domain/service"
    "github.com/precision-soft/melody/v2/.example/infra/http/presenter"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v2/security"
    melodyvalidation "github.com/precision-soft/melody/v2/validation"
)

func ApiUpdateHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        if false == melodysecurity.IsGranted(runtimeInstance, entity.RoleEditor) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "forbidden"), nil
        }

        id, exists := request.Param("id")
        if false == exists {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "id is required"), nil
        }

        if "" == strings.TrimSpace(id) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "id is required"), nil
        }

        var dto updateRequest

        decoderErr := json.NewDecoder(request.HttpRequest().Body).Decode(&dto)
        if nil != decoderErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "invalid json"), nil
        }

        validatorInstance := melodyvalidation.ValidatorMustFromContainer(runtimeInstance.Container())

        validationErrors := validatorInstance.Validate(dto)
        if nil != validationErrors {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, validationErrors.Error()), nil
        }

        productService := service.MustGetProductService(runtimeInstance.Container())

        product, found, updateErr := productService.Update(
            runtimeInstance,
            id,
            strings.TrimSpace(dto.Name),
            strings.TrimSpace(dto.Description),
            strings.TrimSpace(dto.CategoryId),
            dto.Price,
            strings.TrimSpace(dto.CurrencyId),
            dto.Stock,
        )
        if nil != updateErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to update product"), nil
        }

        if false == found {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "not found"), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, mapProduct(product)), nil
    }
}

type updateRequest struct {
    Name        string  `json:"name" validation:"required,min=2,max=120"`
    Description string  `json:"description" validation:"required,min=1,max=40"`
    CategoryId  string  `json:"categoryId" validation:"required"`
    Price       float64 `json:"price" validation:"required,min=0"`
    CurrencyId  string  `json:"currencyId" validation:"required"`
    Stock       int64   `json:"stock" validation:"required,min=0"`
}
