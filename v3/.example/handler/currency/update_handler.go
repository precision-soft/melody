package currency

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

/* ApiUpdateHandler renames a currency; the rate is not part of the body, since only the refresh writes a quote */
func ApiUpdateHandler() melodyhttpcontract.Handler {
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

        updateCurrency := melodyhttp.JsonHandler(
            func(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request, dto updateRequest) (melodyhttpcontract.Response, error) {
                dto = dto.trimmed()
                if refusal := presenter.ApiRefusalOfInvalidBody(runtimeInstance, request, dto); nil != refusal {
                    return refusal, nil
                }

                currencyService := service.MustGetCurrencyService(runtimeInstance.Container())

                currency, found, updateErr := currencyService.Update(
                    runtimeInstance,
                    id,
                    dto.Code,
                    dto.Name,
                )
                if true == errors.Is(updateErr, repository.ErrCurrencyCodeAlreadyExists) {
                    return presenter.ApiError(runtimeInstance, request, nethttp.StatusConflict, "code already exists"), nil
                }

                if nil != updateErr {
                    return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to update currency", updateErr), nil
                }

                if false == found {
                    return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "not found"), nil
                }

                return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, MapCurrencies([]*entity.Currency{currency})[0]), nil
            },
            melodyhttp.WithJsonHandlerFailureResponder(apiJsonErrorResponder),
        )

        return updateCurrency(runtimeInstance, writer, request)
    }
}

type updateRequest struct {
    Code string `json:"code" validate:"notBlank,currencyCode"`
    Name string `json:"name" validate:"notBlank,min=2,max=120"`
}

/* trimmed answers the body as the door stores it, so it is validated in that spelling */
func (instance updateRequest) trimmed() updateRequest {
    instance.Code = strings.TrimSpace(instance.Code)
    instance.Name = strings.TrimSpace(instance.Name)

    return instance
}
