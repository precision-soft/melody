package currency

import (
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/presenter"
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
                currencyService := service.MustGetCurrencyService(runtimeInstance.Container())

                currency, found, updateErr := currencyService.Update(
                    runtimeInstance,
                    id,
                    strings.TrimSpace(dto.Code),
                    strings.TrimSpace(dto.Name),
                )
                if nil != updateErr {
                    return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to update currency", updateErr), nil
                }

                if false == found {
                    return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "not found"), nil
                }

                return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, MapCurrencies([]*entity.Currency{currency})[0]), nil
            },
            melodyhttp.WithJsonHandlerErrorResponder(apiJsonErrorResponder),
        )

        return updateCurrency(runtimeInstance, writer, request)
    }
}

type updateRequest struct {
    Code string `json:"code" validate:"notBlank,currencyCode"`
    Name string `json:"name" validate:"notBlank,min=2,max=120"`
}
