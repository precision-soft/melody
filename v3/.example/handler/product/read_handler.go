package product

import (
    "fmt"
    "errors"
    "math"
    nethttp "net/http"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/handler/category"
    "github.com/precision-soft/melody/v3/.example/handler/currency"
    "github.com/precision-soft/melody/v3/.example/presenter"
    "github.com/precision-soft/melody/v3/.example/service"
    melodybag "github.com/precision-soft/melody/v3/bag"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const convertedCurrencyQueryParameter = "currency"

var errConversionRequest = errors.New("invalid conversion request")

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

        converted, convertErr := convertedPriceFor(request, product, currencies)
        if nil != convertErr {
            return conversionErrorResponse(runtimeInstance, request, convertErr), nil
        }

        response.Converted = converted

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, response), nil
    }
}


type ProductResponse struct {
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

type readAllResponse []ProductResponse

type readResponse struct {
    Product    ProductResponse             `json:"product"`
    Categories []category.CategoryResponse `json:"categories"`
    Currencies []currency.CurrencyResponse `json:"currencies"`

    /* Converted is omitted when the request does not ask for currency conversion. */
    Converted *ConvertedPriceResponse `json:"converted,omitempty"`
}

/* ConvertedPriceResponse is the product's price restated in the currency the caller named. It carries the
   older timestamp of the two input quotes, so a conversion cannot report fresher data than either rate it uses. */
type ConvertedPriceResponse struct {
    CurrencyId string  `json:"currencyId"`
    Code       string  `json:"code"`
    Price      float64 `json:"price"`
    RateAsOf   string  `json:"rateAsOf"`
}

func convertedPriceFor(
    request melodyhttpcontract.Request,
    product *entity.Product,
    currencies []*entity.Currency,
) (*ConvertedPriceResponse, error) {
    requestedCode, present, indexErr := melodybag.StringAt(request.Query(), convertedCurrencyQueryParameter, 0)
    if nil != indexErr {
        return nil, fmt.Errorf("%w: %v", errConversionRequest, indexErr)
    }

    if false == present || "" == strings.TrimSpace(requestedCode) {
        return nil, nil
    }

    target, found := service.FindCurrencyByCode(currencies, requestedCode)
    if false == found {
        return nil, fmt.Errorf("%w: unknown currency code %q", errConversionRequest, requestedCode)
    }

    source, sourceFound := currencyById(currencies, product.CurrencyId)
    if false == sourceFound {
        return nil, fmt.Errorf("the product is quoted in a currency the catalogue does not carry")
    }

    price, convertErr := service.ConvertAmount(product.Price, source, target)
    if nil != convertErr {
        return nil, convertErr
    }

    rateAsOf := target.RateAsOf
    if source.RateAsOf.Before(rateAsOf) {
        rateAsOf = source.RateAsOf
    }

    return &ConvertedPriceResponse{
        CurrencyId: target.Id,
        Code:       target.Code,
        Price:      price,
        RateAsOf:   rateAsOf.UTC().Format(time.RFC3339),
    }, nil
}

func currencyById(currencies []*entity.Currency, currencyId string) (*entity.Currency, bool) {
    for _, currency := range currencies {
        if nil == currency {
            continue
        }

        if currencyId == currency.Id {
            return currency, true
        }
    }

    return nil, false
}

func mapProduct(product *entity.Product) ProductResponse {
    priceRounded := math.Round(product.Price*100.0) / 100.0

    return ProductResponse{
        Id:          product.Id,
        Name:        product.Name,
        Description: product.Description,
        CategoryId:  product.CategoryId,
        Price:       priceRounded,
        CurrencyId:  product.CurrencyId,
        Stock:       product.Stock,

        CreatedAt:   product.CreatedAt.UTC().Format(nethttp.TimeFormat),
        UpdatedAt:   product.UpdatedAt.UTC().Format(nethttp.TimeFormat),
    }
}

func conversionErrorResponse(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request, err error) melodyhttpcontract.Response {
    if errors.Is(err, errConversionRequest) {
        return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, err.Error())
    }
    return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "currency conversion is unavailable", err)
}
