package entity

import "time"

func NewRateQuote(rate float64, asOf time.Time, providerAsOf time.Time) RateQuote {
    return RateQuote{Rate: rate, AsOf: asOf, ProviderAsOf: providerAsOf}
}

/* RateQuote is one reading of a rate, with its instant in the two reference frames it is read in.

   ProviderAsOf is the instant the provider stamped, on the PROVIDER's clock, kept as it came. It names the
   reading: two documents carrying the same stamp are the same reading, whatever this clock read when each
   arrived, so it is what "the quote the catalogue already holds" is judged on.

   AsOf is the same instant moved onto THIS application's clock, by the offset measured between the two clocks
   when the reading arrived. Every judgement of ORDER is made on it: the provider's clock may run ahead of this
   one, or be set back between two readings, and a stamp compared with a stamp taken before that correction
   read an honest reading as older than one stamped by the clock that was wrong. A replay is still older on
   this clock — its stamp is old and the clock that answers it is current — while a clock set back moves the
   stamp and the answer together.

   The two instants are one and the same for a reading taken on this clock, which is what NewCurrency builds. */
type RateQuote struct {
    Rate         float64
    AsOf         time.Time
    ProviderAsOf time.Time
}

/* NamesTheSameReadingAs answers whether two quotes are the one reading of the provider: the same stamp, on the
   clock that stamped both, and the same rate. The instant on this clock is left out on purpose — it is measured
   again on every arrival, to the resolution the provider's clock is readable at, and a reading read twice
   would otherwise never be the same reading twice. */
func (instance RateQuote) NamesTheSameReadingAs(other RateQuote) bool {
    return instance.Rate == other.Rate && true == instance.ProviderAsOf.Equal(other.ProviderAsOf)
}
