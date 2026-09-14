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
    if nil != refreshErr && 0 == outcome.Updated && 0 == outcome.Failed {
        return refreshErr
    }

    writer := commandContext.Writer()

    if false == outcome.Configured {
        _, _ = fmt.Fprintln(writer, "no rate provider is configured, so no rate was read")

        return nil
    }

    headers := []string{
        "AS_OF",
        "ATTEMPTS",
        "UPDATED",
        "SKIPPED",
        "FAILED",
    }

    rows := [][]string{
        {
            outcome.AsOf.UTC().Format(time.RFC3339),
            fmt.Sprintf("%d", outcome.Attempts),
            fmt.Sprintf("%d", outcome.Updated),
            fmt.Sprintf("%d", outcome.Skipped),
            fmt.Sprintf("%d", outcome.Failed),
        },
    }

    /* the same table helper product:list prints through, rendered into the command's own writer so the
       section that drives this command can read what it printed without capturing a process stream */
    fprintTable(writer, headers, rows)

    /* the refresh writes through the service so the listeners drop the cached currencies — in THIS process. On the shared cache the server rereads the new rate; on the in-process fallback the server keeps the rate it cached, which is the one failure the two GoDocs of the write path say the design prevents, and it prevents it only with redis. */
    if true == cacheIsProcessLocal(runtimeInstance) {
        _, _ = fmt.Fprintln(writer, processLocalCacheNotice)
    }

    return refreshErr
}

var _ melodyclicontract.Command = (*CurrencyRefreshRatesCommand)(nil)
