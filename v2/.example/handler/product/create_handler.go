package product

import (
    "encoding/json"
    "errors"
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/v2/.example/entity"
    "github.com/precision-soft/melody/v2/.example/presenter"
    "github.com/precision-soft/melody/v2/.example/repository"
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

        /* the door stores the body trimmed, so it validates that spelling: a name of one rune padded to two is refused, not stored */
        dto = dto.trimmed()

        validatorInstance := melodyvalidation.ValidatorMustFromContainer(runtimeInstance.Container())

        validationErrors := validatorInstance.Validate(dto)
        if nil != validationErrors {
            return presenter.ApiValidationError(runtimeInstance, request, validationErrors), nil
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

            return presenter.ApiErrorWithErr(runtimeInstance, request, status, message, createErr), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusCreated, mapProduct(product)), nil
    }
}

type createRequest struct {
    /* the id becomes a cache key component and the backend grammar refuses spaces and newlines inside a key, so a spelling the grammar refuses is turned away here instead of landing in the database and failing every later cache write */
    Id          string  `json:"id" validate:"max=60,regex=^[^[:space:]]+$"`
    Name        string  `json:"name" validate:"notBlank,min=2,max=120"`
    Description string  `json:"description" validate:"notBlank,min=1,max=40"`
    CategoryId  string  `json:"categoryId" validate:"notBlank"`
    Price       float64 `json:"price" validate:"greaterThan=0"`
    CurrencyId  string  `json:"currencyId" validate:"notBlank"`
    Stock       int64   `json:"stock" validate:"greaterThan=-1"`
}

/* createRefusalStatus answers the status and the public message of a refused create: a supplied identifier another product holds is a conflict, any other failure is the catalogue's */
func createRefusalStatus(createErr error) (int, string) {
    if true == errors.Is(createErr, repository.ErrIdAlreadyExists) {
        return nethttp.StatusConflict, "id already exists"
    }

    return nethttp.StatusInternalServerError, "failed to create product"
}

/* trimmed answers the body as the door stores it */
func (instance createRequest) trimmed() createRequest {
    instance.Id = strings.TrimSpace(instance.Id)
    instance.Name = strings.TrimSpace(instance.Name)
    instance.Description = strings.TrimSpace(instance.Description)
    instance.CategoryId = strings.TrimSpace(instance.CategoryId)
    instance.CurrencyId = strings.TrimSpace(instance.CurrencyId)

    return instance
}
