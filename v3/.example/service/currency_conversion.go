package service

import (
    "math"
    "strings"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

/* FindCurrencyByCode answers the currency a caller named, matched on the code rather than on the identifier
   because a code is what a client of the catalogue knows — "USD", not "cur-usd". The comparison folds case
   so a caller writing "usd" is answered, and it is the ONLY spelling this application gives a code: the
   nomenclature is seeded in upper case and nothing else compares codes, so there is no second reading for
   this one to drift from. */
func FindCurrencyByCode(currencies []*entity.Currency, code string) (*entity.Currency, bool) {
    folded := foldCurrencyCode(code)
    if "" == folded {
        return nil, false
    }

    for _, currency := range currencies {
        if nil == currency {
            continue
        }

        if folded == foldCurrencyCode(currency.Code) {
            return currency, true
        }
    }

    return nil, false
}

/* ConvertAmount calculates amount / sourceRate * targetRate for rates against a common base. It rejects invalid rates on the read path, including values inserted outside the service. */
func ConvertAmount(amount float64, from *entity.Currency, to *entity.Currency) (float64, error) {
    if nil == from || nil == to {
        return 0, exception.NewError("a conversion needs both currencies", nil, nil)
    }

    if 0 >= from.Rate || 0 >= to.Rate || math.IsInf(from.Rate, 0) || math.IsInf(to.Rate, 0) || math.IsNaN(from.Rate) || math.IsNaN(to.Rate) {
        return 0, exception.NewError(
            "a currency without a positive rate cannot take part in a conversion",
            exceptioncontract.Context{
                "fromCurrencyId": from.Id,
                "fromRate":       from.Rate,
                "toCurrencyId":   to.Id,
                "toRate":         to.Rate,
            },
            nil,
        )
    }

    converted := amount / from.Rate * to.Rate

    rounded := math.Round(converted*100.0) / 100.0
    if math.IsInf(rounded, 0) || math.IsNaN(rounded) {
        return 0, exception.NewError("the converted amount is outside the supported numeric range", nil, nil)
    }
    return rounded, nil
}

func foldCurrencyCode(code string) string {
    return strings.ToUpper(strings.TrimSpace(code))
}
