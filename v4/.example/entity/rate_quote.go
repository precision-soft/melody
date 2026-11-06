package entity

import "time"

func NewRateQuote(rate float64, asOf time.Time, providerAsOf time.Time) RateQuote {
    return RateQuote{Rate: rate, AsOf: asOf, ProviderAsOf: providerAsOf}
}

/* RateQuote is one reading of a rate, its instant in two reference frames: ProviderAsOf is the provider's own stamp, kept as it came, which names the reading, so two documents with the same stamp are the same reading. AsOf is that instant moved onto this application's clock by the offset between the two clocks when the reading arrived, and every judgement of order is made on it, since the provider's clock may run ahead or be set back between readings. A replay that kept its old Date, or carries none, is admitted within the five-minute bound the refresh trusts an offset to. */
type RateQuote struct {
    Rate         float64
    AsOf         time.Time
    ProviderAsOf time.Time
}

/* NamesTheSameReadingAs answers whether two quotes are the one reading of the provider: the same stamp, on the clock that stamped both, and the same rate. The instant on this clock is left out on purpose: it is taken again on every arrival, so a reading read twice would otherwise never be the same reading. */
func (instance RateQuote) NamesTheSameReadingAs(other RateQuote) bool {
    return instance.Rate == other.Rate && true == instance.ProviderAsOf.Equal(other.ProviderAsOf)
}
