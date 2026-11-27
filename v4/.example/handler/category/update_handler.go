package category

import (
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/presenter"
    "github.com/precision-soft/melody/v4/.example/service"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
)

/* ApiUpdateHandler renames a category */
func ApiUpdateHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        if false == melodysecurity.IsGranted(runtimeInstance, entity.RoleEditor) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "forbidden"), nil
        }

        id, exists := request.Parameter("id")
        if false == exists {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "id is required"), nil
        }

        id = strings.TrimSpace(id)
        if "" == id {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "id is required"), nil
        }

        updateCategory := melodyhttp.JsonHandler(
            func(runtimeInstance melodycontainercontract.Runtime, request melodyhttpcontract.Request, dto UpdateRequest) (melodyhttpcontract.Response, error) {
                dto = dto.trimmed()
                if refusal := presenter.ApiRefusalOfInvalidBody(runtimeInstance, request, dto); nil != refusal {
                    return refusal, nil
                }

                categoryService := service.MustGetCategoryService(runtimeInstance.Context(), runtimeInstance.Scope())

                category, found, updateErr := categoryService.Update(runtimeInstance, id, dto.Name)
                if nil != updateErr {
                    return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to update category", updateErr), nil
                }

                if false == found {
                    return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "not found"), nil
                }

                return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, MapCategories([]*entity.Category{category})[0]), nil
            },
            melodyhttp.JsonHandlerConfig{ErrorResponder: apiJsonErrorResponder},
        )

        return updateCategory(runtimeInstance, writer, request)
    }
}

type UpdateRequest struct {
    Name string `json:"name" validate:"notBlank,min=2,max=120"`
}

/* trimmed answers the body as the door stores it, so it is validated in that spelling */
func (instance UpdateRequest) trimmed() UpdateRequest {
    instance.Name = strings.TrimSpace(instance.Name)

    return instance
}
