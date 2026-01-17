package product

import (
	"encoding/json"
	nethttp "net/http"
	"strings"

	"github.com/precision-soft/melody/.example/domain/entity"
	"github.com/precision-soft/melody/.example/domain/service"
	"github.com/precision-soft/melody/.example/infra/http/presenter"
	melodyhttpcontract "github.com/precision-soft/melody/http/contract"
	melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
	melodysecurity "github.com/precision-soft/melody/security"
	melodyvalidation "github.com/precision-soft/melody/validation"
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
	Id          string  `json:"id" validation:"omitempty,min=1,max=60"`
	Name        string  `json:"name" validation:"required,min=2,max=120"`
	Description string  `json:"description" validation:"required,min=1,max=40"`
	CategoryId  string  `json:"categoryId" validation:"required"`
	Price       float64 `json:"price" validation:"required,min=0"`
	CurrencyId  string  `json:"currencyId" validation:"required"`
	Stock       int64   `json:"stock" validation:"required,min=0"`
}
