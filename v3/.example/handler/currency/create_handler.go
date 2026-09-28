package currency

import (
    "errors"
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

func ApiCreateHandler() melodyhttpcontract.Handler {
    createCurrency := melodyhttp.JsonHandler(
        func(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request, dto CreateRequest) (melodyhttpcontract.Response, error) {
            currencyService := service.MustGetCurrencyService(runtimeInstance.Container())

            currency, createErr := currencyService.Create(
                runtimeInstance,
                strings.TrimSpace(dto.Id),
                strings.TrimSpace(dto.Code),
                strings.TrimSpace(dto.Name),
                dto.Rate,
            )
            if nil != createErr {
                return createRefusal(runtimeInstance, request, createErr), nil
            }

            return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusCreated, MapCurrencies([]*entity.Currency{currency})[0]), nil
        },
        melodyhttp.WithJsonHandlerErrorResponder(apiJsonErrorResponder),
    )

    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        if false == melodysecurity.IsGranted(runtimeInstance, entity.RoleEditor) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "forbidden"), nil
        }

        return createCurrency(runtimeInstance, writer, request)
    }
}

/* a rate the body can carry and a conversion cannot use is the client's input, refused as the binding refuses a field; any other failure is the catalogue's */
func createRefusal(
    runtimeInstance melodyruntimecontract.Runtime,
    request melodyhttpcontract.Request,
    createErr error,
) melodyhttpcontract.Response {
    if true == errors.Is(createErr, service.ErrUnusableRate) {
        return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "rate: "+service.ErrUnusableRate.Error())
    }

    return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to create currency", createErr)
}

/* apiJsonErrorResponder answers through ApiRefusal, so a validation failure is rendered field by field and any other refusal keeps its generic message with the decoder's diagnosis in the debug-gated context. */
func apiJsonErrorResponder(
    runtimeInstance melodyruntimecontract.Runtime,
    request melodyhttpcontract.Request,
    status int,
    message string,
    cause error,
) (melodyhttpcontract.Response, error) {
    return presenter.ApiRefusal(runtimeInstance, request, status, message, cause), nil
}

/* bound by the openapi descriptor in config; keep it exported */
type CreateRequest struct {
    /* an empty id is minted by the repository; a supplied one becomes a cache key component, whose grammar refuses spaces and newlines */
    Id   string  `json:"id" validate:"max=60,regex=^\\S+$"`
    Code string  `json:"code" validate:"notBlank,regex=^[A-Z]{3}$"`
    Name string  `json:"name" validate:"notBlank,min=2,max=120"`
    Rate float64 `json:"rate" validate:"greaterThan=0"`
}
