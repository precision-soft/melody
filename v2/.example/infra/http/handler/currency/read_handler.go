package currency

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v2/.example/domain/entity"
    "github.com/precision-soft/melody/v2/.example/domain/repository"
    "github.com/precision-soft/melody/v2/.example/infra/http/presenter"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type CurrencyResponse struct {
    Id   string `json:"id"`
    Code string `json:"code"`
    Name string `json:"name"`
}

func ApiReadAllHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        currencyRepository := repository.MustGetCurrencyRepository(runtimeInstance.Container())

        currencies := currencyRepository.All()

        payload := MapCurrencies(currencies)

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, payload), nil
    }
}

func MapCurrencies(currencies []*entity.Currency) []CurrencyResponse {
    payload := make([]CurrencyResponse, 0, len(currencies))

    for _, currency := range currencies {
        if nil == currency {
            continue
        }

        payload = append(payload, CurrencyResponse{
            Id:   currency.Id,
            Code: currency.Code,
            Name: currency.Name,
        })
    }

    return payload
}
