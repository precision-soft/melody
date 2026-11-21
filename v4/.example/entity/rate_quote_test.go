package entity

import (
    "testing"
    "time"
)

/* the reading is named by the provider's stamp and the rate; the instant on this clock is taken again on every arrival and is not part of what the reading is */
func TestRateQuote_NamesTheSameReadingByTheProvidersStampAndTheRate(t *testing.T) {
    stamped := time.Date(2026, time.September, 8, 9, 0, 0, 0, time.UTC)
    held := NewRateQuote(1.2, stamped.Add(-4*time.Minute), stamped)

    if false == NewRateQuote(1.2, stamped.Add(-4*time.Minute+700*time.Millisecond), stamped).NamesTheSameReadingAs(held) {
        t.Errorf("a reading measured again onto this clock was not the same reading")
    }

    if true == NewRateQuote(1.3, held.AsOf, stamped).NamesTheSameReadingAs(held) {
        t.Errorf("a re-quote at another rate was the same reading")
    }

    if true == NewRateQuote(1.2, held.AsOf, stamped.Add(time.Microsecond)).NamesTheSameReadingAs(held) {
        t.Errorf("another stamp was the same reading")
    }
}

/* a currency built on this clock holds one instant in both frames */
func TestNewCurrency_HoldsTheReadingInBothFramesAtOneInstant(t *testing.T) {
    instant := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
    currency := NewCurrency("cur-usd", "USD", "US Dollar", 1.1, instant)

    if false == currency.RateAsOf.Equal(instant) || false == currency.ProviderRateAsOf.Equal(instant) {
        t.Fatalf("expected both instants at %s, got %s and %s", instant, currency.RateAsOf, currency.ProviderRateAsOf)
    }

    if quote := currency.Quote(); 1.1 != quote.Rate || false == quote.AsOf.Equal(instant) || false == quote.ProviderAsOf.Equal(instant) {
        t.Errorf("expected the quote to carry the currency's reading, got %+v", quote)
    }
}
