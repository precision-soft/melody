package entity

import "time"

/* NewCurrency builds a currency whose reading was taken in this application's own reference frame — the seed and
   the operator's create — where the provider's stamp and the instant on this clock are one and the same. */
func NewCurrency(
    id string,
    code string,
    name string,
    rate float64,
    rateAsOf time.Time,
) *Currency {
    return NewQuotedCurrency(id, code, name, NewRateQuote(rate, rateAsOf, rateAsOf))
}

/* NewQuotedCurrency builds a currency holding the reading a quote carries, both of its instants included. */
func NewQuotedCurrency(
    id string,
    code string,
    name string,
    quote RateQuote,
) *Currency {
    return &Currency{
        Id:               id,
        Code:             code,
        Name:             name,
        Rate:             quote.Rate,
        RateAsOf:         quote.AsOf,
        ProviderRateAsOf: quote.ProviderAsOf,
    }
}

/* Currency carries the exchange rate beside the code because the catalogue quotes a product in one currency
   and a reader may ask for another. Rate is how many units of THIS currency one unit of the rate base
   costs, so a conversion between two currencies never needs to know what the base was:
   amount / rate(from) * rate(to) cancels it — which is arithmetic only while every rate in the catalogue
   shares that one base, the reason the refresh names it and refuses a document quoted against another.

   The reading carries two instants, one per reference frame — see RateQuote. RateAsOf is when the reading was
   taken, on THIS application's clock: a reader deciding whether a price is stale needs the age of the reading,
   not the moment it happened to be stored, and every judgement of order is made on it. ProviderRateAsOf is the
   provider's own stamp, as it came, which is what names the reading. */
type Currency struct {
    Id               string
    Code             string
    Name             string
    Rate             float64
    RateAsOf         time.Time
    ProviderRateAsOf time.Time
}

/* Quote answers the reading the currency holds. */
func (instance *Currency) Quote() RateQuote {
    return NewRateQuote(instance.Rate, instance.RateAsOf, instance.ProviderRateAsOf)
}
