package category

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v2/.example/domain/entity"
    "github.com/precision-soft/melody/v2/.example/domain/repository"
    "github.com/precision-soft/melody/v2/.example/infra/http/presenter"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type CategoryResponse struct {
    Id   string `json:"id"`
    Name string `json:"name"`
}

func ApiReadAllHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        categoryRepository := repository.MustGetCategoryRepository(runtimeInstance.Container())

        categories := categoryRepository.All()

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
