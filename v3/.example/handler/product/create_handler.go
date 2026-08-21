package product

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

func ApiCreateHandler() melodyhttpcontract.Handler {
    createProduct := melodyhttp.JsonHandler(
        func(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request, dto CreateRequest) (melodyhttpcontract.Response, error) {
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

/* @info the cause travels with the refusal, so the responder answers through ApiErrorWithErr rather
   than ApiError: the decoder's own diagnosis and the per-field validation collection reach the error
   context and the debug trace instead of dying at this boundary. Returning the response rather than
   nothing is what keeps the refusal a refusal — a responder that answers nothing leaves the framework's
   own refusal standing, and returning a nil pair used to be read as a handler that answered nothing at
   all and served an empty 204 for a rejected write. */
func apiJsonErrorResponder(
    runtimeInstance melodyruntimecontract.Runtime,
    request melodyhttpcontract.Request,
    status int,
    message string,
    cause error,
) (melodyhttpcontract.Response, error) {
    return presenter.ApiErrorWithErr(runtimeInstance, request, status, message, cause), nil
}

/* @important bound by the openapi descriptor in config; keep it exported */
type CreateRequest struct {
    Id          string  `json:"id" validate:"max=60"`
    Name        string  `json:"name" validate:"notBlank,min=2,max=120"`
    Description string  `json:"description" validate:"notBlank,min=1,max=40"`
    CategoryId  string  `json:"categoryId" validate:"notBlank"`
    Price       float64 `json:"price" validate:"greaterThan=0"`
    CurrencyId  string  `json:"currencyId" validate:"notBlank"`
    Stock       int64   `json:"stock" validate:"greaterThan=-1"`
}
