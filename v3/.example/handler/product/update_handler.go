package product

import (
    "errors"
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/presenter"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/service"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
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

        updateProduct := melodyhttp.JsonHandler(
            func(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request, dto updateRequest) (melodyhttpcontract.Response, error) {
                dto = dto.trimmed()
                if refusal := presenter.ApiRefusalOfInvalidBody(runtimeInstance, request, dto); nil != refusal {
                    return refusal, nil
                }

                productService := service.MustGetProductService(runtimeInstance.Container())

                product, found, updateErr := productService.Update(
                    runtimeInstance,
                    id,
                    dto.Name,
                    dto.Description,
                    dto.CategoryId,
                    dto.Price,
                    dto.CurrencyId,
                    dto.Stock,
                )
                if true == errors.Is(updateErr, repository.ErrUnknownCategory) || true == errors.Is(updateErr, repository.ErrUnknownCurrency) {
                    status, message := createRefusalStatus(updateErr)

                    return presenter.ApiError(runtimeInstance, request, status, message), nil
                }

                if nil != updateErr {
                    return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to update product", updateErr), nil
                }

                if false == found {
                    return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "not found"), nil
                }

                return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, mapProduct(product)), nil
            },
            melodyhttp.WithJsonHandlerErrorResponder(apiJsonErrorResponder),
        )

        return updateProduct(runtimeInstance, writer, request)
    }
}

type updateRequest struct {
    Name        string  `json:"name" validate:"notBlank,min=2,max=120"`
    Description string  `json:"description" validate:"notBlank,min=1,max=40"`
    CategoryId  string  `json:"categoryId" validate:"notBlank"`
    Price       float64 `json:"price" validate:"greaterThan=0"`
    CurrencyId  string  `json:"currencyId" validate:"notBlank"`
    Stock       int64   `json:"stock" validate:"greaterThan=-1"`
}

/* trimmed answers the body as the door stores it, so it is validated in that spelling */
func (instance updateRequest) trimmed() updateRequest {
    instance.Name = strings.TrimSpace(instance.Name)
    instance.Description = strings.TrimSpace(instance.Description)
    instance.CategoryId = strings.TrimSpace(instance.CategoryId)
    instance.CurrencyId = strings.TrimSpace(instance.CurrencyId)

    return instance
}
