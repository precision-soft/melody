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

func ApiCreateHandler() melodyhttpcontract.Handler {
    createCurrency := melodyhttp.JsonHandler(
        func(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request, dto CreateRequest) (melodyhttpcontract.Response, error) {
            dto = dto.trimmed()
            if refusal := presenter.ApiRefusalOfInvalidBody(runtimeInstance, request, dto); nil != refusal {
                return refusal, nil
            }

            currencyService := service.MustGetCurrencyService(runtimeInstance.Container())

            currency, createErr := currencyService.Create(
                runtimeInstance,
                dto.Id,
                dto.Code,
                dto.Name,
                dto.Rate,
            )
            if nil != createErr {
                return createRefusal(runtimeInstance, request, createErr), nil
            }

            return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusCreated, MapCurrencies([]*entity.Currency{currency})[0]), nil
        },
        melodyhttp.WithJsonHandlerFailureResponder(apiJsonErrorResponder),
    )

    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        if false == melodysecurity.IsGranted(runtimeInstance, entity.RoleEditor) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "forbidden"), nil
        }

        return createCurrency(runtimeInstance, writer, request)
    }
}

/* a rate the body can carry and a conversion cannot use is the client's input, refused as the binding refuses a field; a supplied identifier another currency holds is a conflict, one whose number is at the ceiling the caller's 400; any other failure is the catalogue's */
func createRefusal(
    runtimeInstance melodyruntimecontract.Runtime,
    request melodyhttpcontract.Request,
    createErr error,
) melodyhttpcontract.Response {
    if true == errors.Is(createErr, service.ErrUnusableRate) {
        return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "rate: "+service.ErrUnusableRate.Error())
    }

    if true == errors.Is(createErr, repository.ErrIdAlreadyExists) {
        return presenter.ApiError(runtimeInstance, request, nethttp.StatusConflict, "id already exists")
    }

    if true == errors.Is(createErr, repository.ErrIdentifierAtCeiling) {
        return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "id: "+repository.ErrIdentifierAtCeiling.Error())
    }

    if true == errors.Is(createErr, repository.ErrCurrencyCodeAlreadyExists) {
        return presenter.ApiError(runtimeInstance, request, nethttp.StatusConflict, "code already exists")
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
    Code string  `json:"code" validate:"notBlank,currencyCode"`
    Name string  `json:"name" validate:"notBlank,min=2,max=120"`
    Rate float64 `json:"rate" validate:"greaterThan=0"`
}

/* trimmed answers the body as the door stores it, so it is validated in that spelling */
func (instance CreateRequest) trimmed() CreateRequest {
    instance.Id = strings.TrimSpace(instance.Id)
    instance.Code = strings.TrimSpace(instance.Code)
    instance.Name = strings.TrimSpace(instance.Name)

    return instance
}
