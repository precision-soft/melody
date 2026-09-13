package service

import (
    "math"
    "strings"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

/* FindCurrencyByCode answers the currency a caller named, matched on the code rather than on the identifier
   because a code is what a client of the catalogue knows — "USD", not "cur-usd". The comparison folds
   through foldCurrencyCode so a caller writing "usd" is answered, and that function is the ONE spelling of
   what makes two codes the same name: the rate refresh reads a provider's document through it too, so the
   two readings cannot drift from each other. */
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

/* ConvertAmount restates an amount quoted in one currency in another. Both rates are quoted against one
   base — one unit of that base costs Rate units of the currency — so dividing by the source rate and
   multiplying by the target's cancels the base, and neither this function nor its caller ever has to know
   what the base was.

   The source rate is judged even though the write doors already refuse a non-positive one, and the reason is
   the row rather than the code: a rate this application did not write — a volume carried over from before
   the column existed, a value edited by hand — divides an amount by zero and hands the reader an infinity
   the serializer cannot render, or flips the sign of every price it touches. That is true of the example
   whatever its database has been through, so the guard belongs on the read path as well as on the write
   one. */
func ConvertAmount(amount float64, from *entity.Currency, to *entity.Currency) (float64, error) {
    if nil == from || nil == to {
        return 0, exception.NewError("a conversion needs both currencies", nil, nil)
    }

    if 0 >= from.Rate || 0 >= to.Rate {
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

    /* the write doors bound every rate they admit, but this guard is on the read path for the same reason
       the one above is: a row this application did not write can hold a rate that is positive and still not
       a price, and the product of two such rates is an infinity that encoding/json refuses to render — a 500
       on the read door for a caller who sent a valid code */
    if true == math.IsInf(converted, 0) || true == math.IsNaN(converted) {
        return 0, exception.NewError(
            "the converted amount is not a finite number",
            exceptioncontract.Context{
                "amount":         amount,
                "fromCurrencyId": from.Id,
                "fromRate":       from.Rate,
                "toCurrencyId":   to.Id,
                "toRate":         to.Rate,
            },
            nil,
        )
    }

    /* rounded to the cent the way every price this application renders is, at the door that produces the
       number rather than at the one that prints it, so a converted price and a quoted one are the same kind
       of value wherever they are read */
    return math.Round(converted*100.0) / 100.0, nil
}

/* foldCurrencyCode is the one spelling of what makes two codes the same name. Trim then upper-case, through
   the standard library rather than a hand-rolled loop: an ISO 4217 code is three ASCII letters, so there is
   no case-folding subtlety to get right and nothing to reimplement. */
func foldCurrencyCode(code string) string {
    return strings.ToUpper(strings.TrimSpace(code))
}
