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

func ApiCreateHandler() melodyhttpcontract.Handler {
    createProduct := melodyhttp.JsonHandler(
        func(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request, dto CreateRequest) (melodyhttpcontract.Response, error) {
            dto = dto.trimmed()
            if refusal := presenter.ApiRefusalOfInvalidBody(runtimeInstance, request, dto); nil != refusal {
                return refusal, nil
            }

            productService := service.MustGetProductService(runtimeInstance.Container())

            product, createErr := productService.Create(
                runtimeInstance,
                dto.Id,
                dto.Name,
                dto.Description,
                dto.CategoryId,
                dto.Price,
                dto.CurrencyId,
                dto.Stock,
            )
            if nil != createErr {
                status, message := createRefusalStatus(createErr)
                if nethttp.StatusInternalServerError != status {
                    return presenter.ApiError(runtimeInstance, request, status, message), nil
                }

                return presenter.ApiErrorWithErr(runtimeInstance, request, status, message, createErr), nil
            }

            return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusCreated, mapProduct(product)), nil
        },
        melodyhttp.WithJsonHandlerErrorResponder(apiJsonErrorResponder),
    )

    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        if false == melodysecurity.IsGranted(runtimeInstance, entity.RoleEditor) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "forbidden"), nil
        }

        return createProduct(runtimeInstance, writer, request)
    }
}

/* apiJsonErrorResponder answers through ApiRefusal, so a validation failure is rendered field by field and any other refusal keeps its generic message with the decoder's diagnosis in the debug-gated context. It returns the response: a responder that answers nothing leaves the framework's own refusal standing. */
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
    /* the id becomes a cache key component, whose grammar refuses spaces and newlines, so such a spelling is turned away here. The pattern reaches the published document as an OpenAPI 3.0 pattern facet (ECMA-262, no POSIX class), so \S is written. */
    Id          string  `json:"id" validate:"max=60,regex=^\\S+$"`
    Name        string  `json:"name" validate:"notBlank,min=2,max=120"`
    Description string  `json:"description" validate:"notBlank,min=1,max=40"`
    CategoryId  string  `json:"categoryId" validate:"notBlank"`
    Price       float64 `json:"price" validate:"greaterThan=0"`
    CurrencyId  string  `json:"currencyId" validate:"notBlank"`
    Stock       int64   `json:"stock" validate:"greaterThan=-1"`
}

/* createRefusalStatus answers the status and the public message of a refused write: a supplied identifier another product holds is a conflict, a category or a currency that names nothing is the caller's 400, any other failure is the catalogue's */
func createRefusalStatus(createErr error) (int, string) {
    if true == errors.Is(createErr, repository.ErrIdAlreadyExists) {
        return nethttp.StatusConflict, "id already exists"
    }

    if true == errors.Is(createErr, repository.ErrUnknownCategory) {
        return nethttp.StatusBadRequest, "categoryId: the category does not exist"
    }

    if true == errors.Is(createErr, repository.ErrUnknownCurrency) {
        return nethttp.StatusBadRequest, "currencyId: the currency does not exist"
    }

    return nethttp.StatusInternalServerError, "failed to create product"
}

/* trimmed answers the body as the door stores it, so it is validated in that spelling */
func (instance CreateRequest) trimmed() CreateRequest {
    instance.Id = strings.TrimSpace(instance.Id)
    instance.Name = strings.TrimSpace(instance.Name)
    instance.Description = strings.TrimSpace(instance.Description)
    instance.CategoryId = strings.TrimSpace(instance.CategoryId)
    instance.CurrencyId = strings.TrimSpace(instance.CurrencyId)

    return instance
}
