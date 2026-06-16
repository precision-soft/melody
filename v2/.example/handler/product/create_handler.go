package product

import (
    "encoding/json"
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/v2/.example/entity"
    "github.com/precision-soft/melody/v2/.example/presenter"
    "github.com/precision-soft/melody/v2/.example/service"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v2/security"
    melodyvalidation "github.com/precision-soft/melody/v2/validation"
)

func ApiCreateHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        if false == melodysecurity.IsGranted(runtimeInstance, entity.RoleEditor) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "forbidden"), nil
        }

        var dto createRequest

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

        product, createErr := productService.Create(
            runtimeInstance,
            strings.TrimSpace(dto.Id),
            strings.TrimSpace(dto.Name),
            strings.TrimSpace(dto.Description),
            strings.TrimSpace(dto.CategoryId),
            dto.Price,
            strings.TrimSpace(dto.CurrencyId),
            dto.Stock,
        )
        if nil != createErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to create product", createErr), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusCreated, mapProduct(product)), nil
    }
}

type createRequest struct {
    Id          string  `json:"id" validate:"max=60"`
    Name        string  `json:"name" validate:"notBlank,min=2,max=120"`
    Description string  `json:"description" validate:"notBlank,min=1,max=40"`
    CategoryId  string  `json:"categoryId" validate:"notBlank"`
    Price       float64 `json:"price" validate:"greaterThan=0"`
    CurrencyId  string  `json:"currencyId" validate:"notBlank"`
    Stock       int64   `json:"stock" validate:"greaterThan=-1"`
}
