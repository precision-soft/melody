package entity

import "time"

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

/* Currency carries the exchange rate beside the code because the catalogue quotes a product in one currency
   and a reader may ask for another. Rate is how many units of THIS currency one unit of the rate base
   costs, so a conversion between two currencies never needs to know what the base was:
   amount / rate(from) * rate(to) cancels it. RateAsOf is the instant the PROVIDER took the reading, not the
   moment this application stored it — a reader deciding whether a price is stale needs the age of the
   reading, and the two differ by however long the refresh was delayed. */
type Currency struct {
    Id       string
    Code     string
    Name     string
    Rate     float64
    RateAsOf time.Time
}
