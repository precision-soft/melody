package service

import (
    "time"
    "errors"
    "fmt"

    "github.com/precision-soft/melody/v3/.example/entity"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/httpclient"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* The two outbound clients this application holds, named here rather than where they are registered because
   the consumers live in this package and in reporting, while the registration lives in config — the same
   arrangement the server-sent-event hub already uses. */
const (
    ServiceRatesHttpClient        = "service.example.rates.http.client"
    ServiceReportExportHttpClient = "service.example.report.export.http.client"
)

const (
    ServiceRateRefreshService = "service-example-rate-refresh-service"
)

const ratesLatestTarget = "latest"

const (
    rateRefreshAttemptCount = 3
    rateRefreshRetryBackoff = 200 * time.Millisecond
)

//melody:service ServiceRateRefreshService
func NewRateRefreshService(
    currencyService *CurrencyService,
    ratesBaseUrl string,
) *RateRefreshService {
    return &RateRefreshService{
        currencyService: currencyService,
        ratesBaseUrl:    ratesBaseUrl,
    }
}

/* RateRefreshService brings the exchange rates the catalogue quotes with up to the provider's. It writes
   through CurrencyService and not through the repository because the currency list and every currency by id
   are cached, and the listeners that drop those entries are subscribed to the event the service dispatches:
   a rate written behind the cache is a rate no reader ever sees. The guarantee holds on the SHARED cache:
   the listeners run in the process that dispatched, which is the scheduler or a console command and never
   the http server, so on the in-process fallback the server keeps the currencies it cached until it
   restarts — the refresh command says so on its output when that is the wiring it ran under.

   The client is resolved when a refresh runs rather than taken in the constructor, and that has an
   observable consequence rather than being a preference: nothing else in this application calls the
   provider, so an http process that serves reads all day resolves no client, builds no transport and opens
   no connection pool. */
type RateRefreshService struct {
    currencyService *CurrencyService
    ratesBaseUrl    string
}

/* RateRefreshOutcome counts updated, failed and skipped quotes. Skipped includes both currencies absent from the provider document and currencies deleted before their update. */
type RateRefreshOutcome struct {
    Configured bool
    Attempts   int
    Updated    int
    Skipped    int
    Failed     int
    AsOf       time.Time
}

type rateDocument struct {
    Base  string             `json:"base"`
    AsOf  time.Time          `json:"asOf"`
    Rates map[string]float64 `json:"rates"`
}

/* Refresh reads the provider once and writes every quote it recognises. With no provider configured it is a
   no-op that says so, the shape every optional door in this application takes, so a schedule that runs it
   in an environment without one neither fails nor pretends to have worked. */
func (instance *RateRefreshService) Refresh(runtimeInstance melodyruntimecontract.Runtime) (RateRefreshOutcome, error) {
    if "" == instance.ratesBaseUrl {
        return RateRefreshOutcome{Configured: false}, nil
    }

    client, resolveErr := melodycontainer.FromResolver[*httpclient.HttpClient](
        runtimeInstance.Container(),
        ServiceRatesHttpClient,
    )
    if nil != resolveErr {
        return RateRefreshOutcome{Configured: true}, resolveErr
    }

    document, attempts, readErr := readRateDocument(client)
    if nil != readErr {
        return RateRefreshOutcome{Configured: true, Attempts: attempts}, readErr
    }

    outcome := RateRefreshOutcome{Configured: true, Attempts: attempts, AsOf: document.AsOf}

    if entity.RateBaseCurrencyCode != foldCurrencyCode(document.Base) {
        return outcome, exception.NewError(
            "the rate provider uses a different base from the catalogue",
            exceptioncontract.Context{
                "expectedBase": entity.RateBaseCurrencyCode,
                "receivedBase": document.Base,
            },
            nil,
        )
    }

    if document.AsOf.IsZero() || document.AsOf.After(instance.currencyService.clock.Now()) {
        return outcome, fmt.Errorf("the provider quote instant must be present and not in the future")
    }
    document.AsOf = document.AsOf.UTC()
    outcome.AsOf = document.AsOf
    normalizedRates := make(map[string]float64, len(document.Rates))
    for code, rate := range document.Rates {
        normalized := foldCurrencyCode(code)
        if _, exists := normalizedRates[normalized]; exists {
            return outcome, fmt.Errorf("the rate document contains duplicate currency code %q", normalized)
        }
        normalizedRates[normalized] = rate
    }

    currencies, listErr := instance.currencyService.List()
    if nil != listErr {
        return outcome, listErr
    }

    var failures []error
    for _, currency := range currencies {
        if nil == currency {
            continue
        }

        rate, quoted := normalizedRates[foldCurrencyCode(currency.Code)]
        if false == quoted {
            outcome.Skipped++

            continue
        }

        _, updated, updateErr := instance.currencyService.UpdateRate(runtimeInstance, currency.Id, rate, document.AsOf)
        if true == updated { outcome.Updated++ }
        if nil != updateErr {
            outcome.Failed++
            failures = append(failures, fmt.Errorf("refresh %s: %w", currency.Id, updateErr))
            continue
        }
        if false == updated {
            outcome.Skipped++

            continue
        }

    }

    return outcome, errors.Join(failures...)
}

func readRateDocument(client *httpclient.HttpClient) (rateDocument, int, error) {
    var lastErr error

    for attempt := 1; attempt <= rateRefreshAttemptCount; attempt++ {
        if 1 < attempt {
            time.Sleep(rateRefreshRetryBackoff)
        }

        response, requestErr := client.Get(ratesLatestTarget)
        if nil != requestErr {
            lastErr = requestErr

            continue
        }

        if true == response.IsServerError() {
            lastErr = exception.NewError(
                "the rate provider refused the reading",
                exceptioncontract.Context{
                    "status":  response.StatusCode(),
                    "attempt": attempt,
                },
                nil,
            )

            continue
        }

        if false == response.IsSuccess() {
            return rateDocument{}, attempt, exception.NewError(
                "the rate provider answered a status the reading cannot be taken from",
                exceptioncontract.Context{
                    "status":  response.StatusCode(),
                    "attempt": attempt,
                },
                nil,
            )
        }

        var document rateDocument
        if decodeErr := response.Json(&document); nil != decodeErr {
            return rateDocument{}, attempt, exception.NewError(
                "the rate provider answered a body that is not a rate document",
                exceptioncontract.Context{
                    "status": response.StatusCode(),
                },
                decodeErr,
            )
        }

        return document, attempt, nil
    }

    return rateDocument{}, rateRefreshAttemptCount, exception.NewError(
        "the rate provider could not be read",
        exceptioncontract.Context{
            "attempts": rateRefreshAttemptCount,
        },
        lastErr,
    )
}

func MustGetRateRefreshService(resolver melodycontainercontract.Resolver) *RateRefreshService {
    return melodycontainer.MustFromResolver[*RateRefreshService](
        resolver,
        ServiceRateRefreshService,
    )
}
