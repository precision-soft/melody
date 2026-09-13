package repository

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
)

/* the mysql dialect renders a time.Time in the value's own location and the driver reads the column back as
   UTC, so a provider's instant stamped with an offset went into the column as its wall clock and came back
   shifted by the offset: the row is built in UTC, the one place an entity becomes a row */
func TestNewCurrencyRow_CarriesTheInstantInUtc(t *testing.T) {
    quotedAt := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.FixedZone("EEST", 3*60*60))

    row := newCurrencyRow(entity.NewCurrency("cur-usd", "USD", "US Dollar", 1.0842, quotedAt))

    if time.UTC != row.RateAsOf.Location() {
        t.Errorf("the row carries the instant in %s, wanted UTC", row.RateAsOf.Location())
    }

    if false == row.RateAsOf.Equal(quotedAt) || 9 != row.RateAsOf.Hour() {
        t.Errorf("the row carries %s, wanted the same instant as 09:00:00Z", row.RateAsOf)
    }
}
