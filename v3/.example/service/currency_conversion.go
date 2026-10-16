package service

import (
    "math"
    "strings"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

/* FindCurrencyByCode answers the currency a caller named by its code. The comparison goes through foldCurrencyCode, the one spelling of what makes two codes the same name, which the rate refresh reads a provider's document through too. */
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

/* ConvertAmount restates an amount quoted in one currency in another. Both rates are quoted against one base, so dividing by the source rate and multiplying by the target's cancels it. The source rate is judged on the read path too, because a row this application did not write can hold a rate that divides by zero or flips a price's sign. */
func ConvertAmount(amount float64, from *entity.Currency, to *entity.Currency) (float64, error) {
    if nil == from || nil == to {
        return 0, exception.NewError("a conversion needs both currencies", nil, nil)
    }

    if 0 >= from.Rate || 0 >= to.Rate {
        return 0, exception.NewError(
            "a currency without a positive rate cannot take part in a conversion",
            exceptioncontract.Context{
                "fromCurrencyId": from.Id,
                "fromRate":       contextNumber(from.Rate),
                "toCurrencyId":   to.Id,
                "toRate":         contextNumber(to.Rate),
            },
            nil,
        )
    }

    converted := amount / from.Rate * to.Rate

    /* rounded to the cent at the door that produces the number, so a converted price and a quoted one are the same kind of value */
    rounded := math.Round(converted*100.0) / 100.0

    /* a row this application did not write can hold rates whose product is an infinity encoding/json refuses to render; the rounded value is judged because the rounding multiplies by a hundred first and can itself overflow */
    if true == math.IsInf(rounded, 0) || true == math.IsNaN(rounded) {
        return 0, exception.NewError(
            "the converted amount is not a finite number",
            exceptioncontract.Context{
                "amount":         contextNumber(amount),
                "fromCurrencyId": from.Id,
                "fromRate":       contextNumber(from.Rate),
                "toCurrencyId":   to.Id,
                "toRate":         contextNumber(to.Rate),
            },
            nil,
        )
    }

    return rounded, nil
}

/* foldCurrencyCode is the one spelling of what makes two codes the same name: trimmed, and upper-cased in ASCII alone. The Unicode case mapping would fold "ſ" (U+017F) onto S; anything outside ASCII is left as it came and so matches no code the catalogue holds. */
func foldCurrencyCode(code string) string {
    return strings.Map(func(character rune) rune {
        if 'a' <= character && 'z' >= character {
            return character - ('a' - 'A')
        }

        return character
    }, strings.TrimSpace(code))
}
