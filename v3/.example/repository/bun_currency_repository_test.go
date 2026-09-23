package repository

import (
    "strings"
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

/* the write judges the row as the repository contract says, in the one statement: not over a newer reading on
   this clock unless it names the same reading, and never over the reading it already holds */
func TestUpdateQuoteQuery_WritesOverTheRowOnlyUnderTheContract(t *testing.T) {
    repositoryInstance := &bunCurrencyRepository{database: newRenderingDatabase()}

    stamped := time.Date(2026, time.September, 8, 9, 4, 0, 0, time.UTC)
    rendered := repositoryInstance.updateQuoteQuery("cur-usd", entity.NewRateQuote(1.2, stamped.Add(-4*time.Minute), stamped)).String()

    for _, wanted := range []string{
        "SET rate = 1.2, rate_as_of = '2026-09-08 09:00:00+00:00', provider_rate_as_of = '2026-09-08 09:04:00+00:00' ",
        "WHERE (id = 'cur-usd') AND ((rate_as_of <= '2026-09-08 09:00:00+00:00' OR provider_rate_as_of = '2026-09-08 09:04:00+00:00')) AND (NOT (rate = 1.2 AND provider_rate_as_of = '2026-09-08 09:04:00+00:00'))",
    } {
        if false == strings.Contains(rendered, wanted) {
            t.Errorf("the statement lacks %q:\n%s", wanted, rendered)
        }
    }
}
