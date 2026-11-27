package category

import (
    "errors"
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/presenter"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/.example/service"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
)

func ApiCreateHandler() melodyhttpcontract.Handler {
    createCategory := melodyhttp.JsonHandler(
        func(runtimeInstance melodycontainercontract.Runtime, request melodyhttpcontract.Request, dto CreateRequest) (melodyhttpcontract.Response, error) {
            dto = dto.trimmed()
            if refusal := presenter.ApiRefusalOfInvalidBody(runtimeInstance, request, dto); nil != refusal {
                return refusal, nil
            }

            categoryService := service.MustGetCategoryService(runtimeInstance.Context(), runtimeInstance.Scope())

            category, createErr := categoryService.Create(runtimeInstance, dto.Id, dto.Name)
            if nil != createErr {
                return createRefusal(runtimeInstance, request, createErr), nil
            }

            return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusCreated, MapCategories([]*entity.Category{category})[0]), nil
        },
        melodyhttp.JsonHandlerConfig{ErrorResponder: apiJsonErrorResponder},
    )

    return func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        if false == melodysecurity.IsGranted(runtimeInstance, entity.RoleEditor) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "forbidden"), nil
        }

        return createCategory(runtimeInstance, writer, request)
    }
}

/* a supplied identifier another category holds is a conflict, one whose number is at the ceiling the caller's 400; any other failure is the catalogue's */
func createRefusal(
    runtimeInstance melodycontainercontract.Runtime,
    request melodyhttpcontract.Request,
    createErr error,
) melodyhttpcontract.Response {
    if true == errors.Is(createErr, repository.ErrIdAlreadyExists) {
        return presenter.ApiError(runtimeInstance, request, nethttp.StatusConflict, "id already exists")
    }

    if true == errors.Is(createErr, repository.ErrIdentifierAtCeiling) {
        return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "id: "+repository.ErrIdentifierAtCeiling.Error())
    }

    return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to create category", createErr)
}

/* apiJsonErrorResponder answers through ApiRefusal, so a validation failure is rendered field by field and any other refusal keeps its generic message with the decoder's diagnosis in the debug-gated context. */
func apiJsonErrorResponder(
    runtimeInstance melodycontainercontract.Runtime,
    request melodyhttpcontract.Request,
    refusal melodyhttp.JsonHandlerRefusal,
) (melodyhttpcontract.Response, error) {
    return presenter.ApiRefusal(runtimeInstance, request, refusal.Status, refusal.Message, refusal.Cause), nil
}

/* bound by the openapi descriptor in config; keep it exported */
type CreateRequest struct {
    /* an empty id is minted by the repository; a supplied one becomes a cache key component, whose grammar refuses spaces and newlines */
    Id   string `json:"id" validate:"max=60,regex=^\\S+$"`
    Name string `json:"name" validate:"notBlank,min=2,max=120"`
}

/* trimmed answers the body as the door stores it, so it is validated in that spelling */
func (instance CreateRequest) trimmed() CreateRequest {
    instance.Id = strings.TrimSpace(instance.Id)
    instance.Name = strings.TrimSpace(instance.Name)

    return instance
}
