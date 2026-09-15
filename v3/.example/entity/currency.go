package entity

import "time"

/* RateBaseCurrencyCode is the common base of every stored catalogue quote. */
const RateBaseCurrencyCode = "EUR"

func NewCurrency(
    id string,
    code string,
    name string,
    rate float64,
    rateAsOf time.Time,
) *Currency {
    return &Currency{
        Id:       id,
        Code:     code,
        Name:     name,
        Rate:     rate,
        RateAsOf: rateAsOf,
    }
}

/* Currency stores a rate against the shared EUR base: amount / source.Rate * target.Rate converts between currencies. RateAsOf is the provider’s observation time, not the local storage time. */
type Currency struct {
    Id       string
    Code     string
    Name     string
    Rate     float64
    RateAsOf time.Time
}
