package product

import (
    "math"
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/.example/domain/entity"
    "github.com/precision-soft/melody/.example/domain/service"
    "github.com/precision-soft/melody/.example/infra/http/handler/category"
    "github.com/precision-soft/melody/.example/infra/http/handler/currency"
    "github.com/precision-soft/melody/.example/infra/http/presenter"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

func ApiReadAllHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        productService := service.MustGetProductService(runtimeInstance.Container())

        products, listErr := productService.List()
        if nil != listErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to list products", listErr), nil
        }

        payload := make(readAllResponse, 0, len(products))

        for _, product := range products {
            if nil == product {
                continue
            }

            payload = append(payload, mapProduct(product))
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, payload), nil
    }
}

func ApiReadHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        id, exists := request.Param("id")
        if false == exists {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "id is required"), nil
        }

        if "" == strings.TrimSpace(id) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "id is required"), nil
        }

        productService := service.MustGetProductService(runtimeInstance.Container())

        product, found, findErr := productService.FindById(id)
        if nil != findErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to load product", findErr), nil
        }

        if false == found {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "not found"), nil
        }

        categoryService := service.MustGetCategoryService(runtimeInstance.Container())
        categories, categoriesErr := categoryService.List()
        if nil != categoriesErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to load categories", categoriesErr), nil
        }

        currencyService := service.MustGetCurrencyService(runtimeInstance.Container())
        currencies, currenciesErr := currencyService.List()
        if nil != currenciesErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to load currencies", currenciesErr), nil
        }

        response := readResponse{
            Product:    mapProduct(product),
            Categories: category.MapCategories(categories),
            Currencies: currency.MapCurrencies(currencies),
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, response), nil
    }
}

type productResponse struct {
    Id          string  `json:"id"`
    Name        string  `json:"name"`
    Description string  `json:"description"`
    CategoryId  string  `json:"categoryId"`
    Price       float64 `json:"price"`
    CurrencyId  string  `json:"currencyId"`
    Stock       int64   `json:"stock"`
    CreatedAt   string  `json:"createdAt"`
    UpdatedAt   string  `json:"updatedAt"`
}

type readAllResponse []productResponse

type readResponse struct {
    Product    productResponse             `json:"product"`
    Categories []category.CategoryResponse `json:"categories"`
    Currencies []currency.CurrencyResponse `json:"currencies"`
}

func mapProduct(product *entity.Product) productResponse {
    priceRounded := math.Round(product.Price*100.0) / 100.0

    return productResponse{
        Id:          product.Id,
        Name:        product.Name,
        Description: product.Description,
        CategoryId:  product.CategoryId,
        Price:       priceRounded,
        CurrencyId:  product.CurrencyId,
        Stock:       product.Stock,
        CreatedAt:   product.CreatedAt.Format(nethttp.TimeFormat),
        UpdatedAt:   product.UpdatedAt.Format(nethttp.TimeFormat),
    }
}
