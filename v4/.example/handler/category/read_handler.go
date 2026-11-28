package category

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/presenter"
    "github.com/precision-soft/melody/v4/.example/service"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
)

type CategoryResponse struct {
    Id   string `json:"id"`
    Name string `json:"name"`
}

func ApiReadAllHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        categoryService := service.MustGetCategoryService(runtimeInstance.Context(), runtimeInstance.Scope())

        /* the listing is the cached one the writes drop through the category listeners */
        categories, listErr := categoryService.List(runtimeInstance)
        if nil != listErr {
            return nil, listErr
        }

        payload := MapCategories(categories)

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, payload), nil
    }
}

func MapCategories(categories []*entity.Category) []CategoryResponse {
    payload := make([]CategoryResponse, 0, len(categories))

    for _, category := range categories {
        if nil == category {
            continue
        }

        payload = append(payload, CategoryResponse{
            Id:   category.Id,
            Name: category.Name,
        })
    }

    return payload
}
