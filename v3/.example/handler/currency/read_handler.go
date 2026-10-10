package currency

import (
    nethttp "net/http"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/presenter"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* CurrencyResponse carries the rate with the code, so a client can tell which currencies the catalogue converts into and how old the quote is. RateAsOf is rendered in RFC 3339 and is the instant the provider took the reading. */
type CurrencyResponse struct {
    Id       string  `json:"id"`
    Code     string  `json:"code"`
    Name     string  `json:"name"`
    Rate     float64 `json:"rate"`
    RateAsOf string  `json:"rateAsOf"`
}

func ApiReadAllHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        currencyRepository := repository.MustGetCurrencyRepository(runtimeInstance.Container())

        currencies, allErr := currencyRepository.All(runtimeInstance.Context())
        if nil != allErr {
            return nil, allErr
        }

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
            Id:       currency.Id,
            Code:     currency.Code,
            Name:     currency.Name,
            Rate:     currency.Rate,
            RateAsOf: currency.RateAsOf.UTC().Format(time.RFC3339),
        })
    }

    return payload
}
