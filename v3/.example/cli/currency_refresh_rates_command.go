package cli

import (
    "fmt"
    "time"

    "github.com/precision-soft/melody/v3/.example/service"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type CurrencyRefreshRatesCommand struct{}

func NewCurrencyRefreshRatesCommand() *CurrencyRefreshRatesCommand {
    return &CurrencyRefreshRatesCommand{}
}

func (instance *CurrencyRefreshRatesCommand) Name() string {
    return "example:currency:refresh-rates"
}

func (instance *CurrencyRefreshRatesCommand) Description() string {
    return "reads the exchange rates from the configured provider and writes the ones it quotes"
}

func (instance *CurrencyRefreshRatesCommand) Flags() []melodyclicontract.Flag {
    return nil
}

/* Run is what the schedule calls. It asks for no --force the way the reset does: a refresh overwrites a
   number that is meant to be overwritten, and the state it replaces is reproduced by running it again.

   What it does insist on is a non-zero exit when the provider could not be read. A schedule that treats a
   silent zero as success would report a working refresh over an application whose rates have not moved for
   days, which is the one failure of this command that nobody would otherwise see. */
func (instance *CurrencyRefreshRatesCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext melodyclicontract.Context) error {
    refreshService := service.MustGetRateRefreshService(runtimeInstance.Container())

    outcome, refreshErr := refreshService.Refresh(runtimeInstance)

    /* a run that refused some quotes still wrote the others, and one a backend stopped part way wrote the
       ones before it: the table below is the only place the operator reads how many, so it is printed
       whenever the sweep touched a currency, and the failure takes the exit code after it. A failure that
       touched none — the provider unread, the document refused whole — has no table to show. */
    if nil != refreshErr && 0 == outcome.Updated+outcome.Unchanged+outcome.Stale+outcome.Refused {
        return refreshErr
    }

    writer := commandContext.Writer()

    if false == outcome.Configured {
        _, _ = fmt.Fprintln(writer, "no rate provider is configured, so no rate was read")

        return nil
    }

    /* the columns keep the order the live band reads them in (instant, attempts, updated, skipped), then UNCHANGED for a provider between two moves, STALE for a replayed document and REFUSED for a quote the catalogue would not take, with the run's exit code naming the refused currencies; the provider's clock closes the row, the offset its stamps are moved by onto this clock or "unmeasured" for an answer that carried no date */
    headers := []string{
        "AS_OF",
        "ATTEMPTS",
        "UPDATED",
        "SKIPPED",
        "UNCHANGED",
        "STALE",
        "REFUSED",
        "PROVIDER_CLOCK",
    }

    rows := [][]string{
        {
            outcome.AsOf.UTC().Format(time.RFC3339),
            fmt.Sprintf("%d", outcome.Attempts),
            fmt.Sprintf("%d", outcome.Updated),
            fmt.Sprintf("%d", outcome.Skipped),
            fmt.Sprintf("%d", outcome.Unchanged),
            fmt.Sprintf("%d", outcome.Stale),
            fmt.Sprintf("%d", outcome.Refused),
            providerClockColumn(outcome),
        },
    }

    /* the same table helper product:list prints through, rendered into the command's own writer so the
       section that drives this command can read what it printed without capturing a process stream */
    fprintTable(writer, headers, rows)

    /* the refresh writes through the service so the listeners drop the cached currencies in this process: on the shared cache the server rereads the new rate, and on the in-process fallback it keeps the rate it cached until it restarts */
    if true == cacheIsProcessLocal(runtimeInstance) {
        _, _ = fmt.Fprintln(writer, processLocalCacheNotice)
    }

    return refreshErr
}

/* providerClockColumn renders how the provider's stamps were moved onto this clock: the offset, signed, with
   "+0s" for a clock that agrees with this one, and "unmeasured" when the answer carried no date. */
func providerClockColumn(outcome service.RateRefreshOutcome) string {
    if false == outcome.ProviderClockMeasured {
        return "unmeasured"
    }

    if 0 <= outcome.ProviderClockOffset {
        return "+" + outcome.ProviderClockOffset.String()
    }

    return outcome.ProviderClockOffset.String()
}

var _ melodyclicontract.Command = (*CurrencyRefreshRatesCommand)(nil)
