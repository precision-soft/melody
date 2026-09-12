package service

import (
    "time"

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

/* ratesLatestTarget is RELATIVE, and that is load-bearing rather than stylistic: the client resolves a
   target against its base url by RFC 3986, so an absolute-path spelling ("/latest") would replace the base
   path entirely and ask the provider for a resource one segment above the one configured. */
const ratesLatestTarget = "latest"

/* rateRefreshAttemptCount is three because a refresh runs on a schedule with nothing waiting on it: the
   failure worth surviving is the one that lasts less than a moment — a provider restarting behind a load
   balancer, a connection reset — and a fourth attempt buys nothing a run five minutes later does not.

   rateRefreshRetryBackoff is short for the same reason and is a FIXED wait rather than a growing one: three
   attempts do not span enough time for a growing wait to mean anything, and a fixed one keeps the whole
   refresh inside a bound a reader can compute — three budgets plus two waits. */
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
   a rate written behind the cache is a rate no reader ever sees.

   The client is resolved when a refresh runs rather than taken in the constructor, and that has an
   observable consequence rather than being a preference: nothing else in this application calls the
   provider, so an http process that serves reads all day resolves no client, builds no transport and opens
   no connection pool. */
type RateRefreshService struct {
    currencyService *CurrencyService
    ratesBaseUrl    string
}

/* RateRefreshOutcome is what a refresh did, in numbers a caller can print and a test can assert. Skipped
   counts two things that are both "no quote was written and nothing went wrong": a currency the provider did
   not quote, which is the ordinary case because a provider is entitled to quote fewer currencies than a
   catalogue carries, and one that stopped existing between the listing and its own update, which is a
   delete landing inside the run. Neither is a failure and neither is worth a second counter — what a
   caller does about them is the same. */
type RateRefreshOutcome struct {
    Configured bool
    Attempts   int
    Updated    int
    Skipped    int
    AsOf       time.Time
}

/* rateDocument is the provider's answer. The rates are quoted against Base, one unit of Base costing that
   many units of the currency, and AsOf is when the provider took the reading — which is what this
   application stores, so a reader can tell the age of a quote rather than the age of the last refresh run. */
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

    currencies, listErr := instance.currencyService.List()
    if nil != listErr {
        return outcome, listErr
    }

    for _, currency := range currencies {
        if nil == currency {
            continue
        }

        rate, quoted := document.Rates[currency.Code]
        if false == quoted {
            outcome.Skipped++

            continue
        }

        _, updated, updateErr := instance.currencyService.UpdateRate(runtimeInstance, currency.Id, rate, document.AsOf)
        if nil != updateErr {
            return outcome, updateErr
        }
        if false == updated {
            outcome.Skipped++

            continue
        }

        outcome.Updated++
    }

    return outcome, nil
}

/* readRateDocument spends up to rateRefreshAttemptCount exchanges on one reading. What is retried is
   deliberately narrow: a call that never produced an answer, and an answer in the 5xx class, because those
   are the two a provider can recover from between one attempt and the next. A 4xx is NOT retried — the
   request is what is wrong, so repeating it repeats the mistake and spends the provider's budget doing it —
   and neither is a body that fails to decode, which a working provider does not send twice. */
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
