package service

import (
    "context"
    "errors"
    "sort"
    "strconv"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/httpclient"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* The two outbound clients this application holds, named here because their consumers live in this package and in reporting and the registration lives in config. */
const (
    ServiceRatesHttpClient        = "service.example.rates.http.client"
    ServiceReportExportHttpClient = "service.example.report.export.http.client"
)

const (
    ServiceRateRefreshService = "service-example-rate-refresh-service"
)

/* ratesLatestTarget is relative: the client resolves a target against its base url by RFC 3986, so "/latest" would replace the base path and ask for a resource above the one configured. */
const ratesLatestTarget = "latest"

/* a refresh runs on a schedule with nothing waiting on it, so three attempts survive a failure that lasts a moment and a fixed backoff keeps the whole refresh inside three budgets plus two waits */
const (
    rateRefreshAttemptCount = 3
    rateRefreshRetryBackoff = 200 * time.Millisecond
)

/* rateDocumentClockSkew is how far ahead of this clock a provider's asOf may lie, for an answer that carried no readable date, before the document is refused. An answer that carries its date is judged on the provider's own clock (see usableQuoteListOf), and the same bound limits how far that clock may be from this one. */
const rateDocumentClockSkew = 5 * time.Minute

//melody:service ServiceRateRefreshService
func NewRateRefreshService(
    currencyService *CurrencyService,
    clockInstance melodyclockcontract.Clock,
    ratesBaseUrl string,
    ratesBaseCurrency string,
) *RateRefreshService {
    return &RateRefreshService{
        currencyService:   currencyService,
        clock:             clockInstance,
        ratesBaseUrl:      ratesBaseUrl,
        ratesBaseCurrency: foldCurrencyCode(ratesBaseCurrency),
    }
}

/* RateRefreshService brings the catalogue's exchange rates up to the provider's. It writes through CurrencyService so the listeners that drop the cached currencies run; they run in the dispatching process, so on the in-process cache fallback the http server keeps what it cached until it restarts, and the refresh command says so. Every rate is quoted against the one base ratesBaseCurrency names, and a document quoted against another base is refused whole, before any write, naming both bases. The client is resolved when a refresh runs, so an http process that serves reads builds no transport. */
type RateRefreshService struct {
    currencyService   *CurrencyService
    clock             melodyclockcontract.Clock
    ratesBaseUrl      string
    ratesBaseCurrency string
}

/* RateRefreshOutcome is what a refresh did, every currency under exactly one heading. Skipped: the provider did not quote it, or it was deleted during the run. Unchanged: the quote the catalogue already held. Stale: a quote older than the one held. Refused: a quote the write door would not take; the sweep goes on past it, and the error names every refused currency. */
type RateRefreshOutcome struct {
    Configured bool
    Attempts   int
    Updated    int
    Unchanged  int
    Stale      int
    Skipped    int
    Refused    int
    AsOf       time.Time
    /* ProviderClockMeasured and ProviderClockOffset say how the provider's stamps were moved onto this clock; an unmeasured clock is an answer that carried no readable date (see providerClockReading). */
    ProviderClockMeasured bool
    ProviderClockOffset   time.Duration
}

/* rateDocument is the provider's answer: rates quoted against Base, one unit of Base costing that many units of the currency, and AsOf, when the provider took the reading, on its own clock. The base must be the catalogue's and the instant present and not in the future before any quote is written. */
type rateDocument struct {
    Base  string             `json:"base"`
    AsOf  time.Time          `json:"asOf"`
    Rates map[string]float64 `json:"rates"`
}

/* Refresh reads the provider once and writes every quote it recognises. With no provider configured it is a no-op that says so, so a schedule in an environment without one neither fails nor pretends to have worked. */
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

    reading, attempts, readErr := readRateDocument(runtimeInstance.Context(), client, instance.clock)
    if nil != readErr {
        return RateRefreshOutcome{Configured: true, Attempts: attempts}, readErr
    }

    document := reading.document
    outcome := RateRefreshOutcome{
        Configured:            true,
        Attempts:              attempts,
        AsOf:                  document.AsOf,
        ProviderClockMeasured: reading.providerClock.Measured,
        ProviderClockOffset:   reading.providerClock.Offset,
    }

    quoteList, documentErr := instance.usableQuoteListOf(document, reading.providerClock)
    if nil != documentErr {
        return outcome, documentErr
    }

    /* the reading carries its instant in both frames: the provider's stamp, which names it, and the instant on this clock, on which its order against the stored reading is judged */
    quoteAsOf := reading.providerClock.onThisClock(document.AsOf)

    currencies, listErr := instance.currencyService.List()
    if nil != listErr {
        return outcome, listErr
    }

    var refusedCurrencyList []string
    var firstRefusal error

    for _, currency := range currencies {
        if nil == currency {
            continue
        }

        rate, quoted := quoteList[foldCurrencyCode(currency.Code)]
        if false == quoted {
            outcome.Skipped++

            continue
        }

        _, updateOutcome, updateErr := instance.currencyService.UpdateRate(runtimeInstance, currency.Id, entity.NewRateQuote(rate, quoteAsOf, document.AsOf))
        if nil != updateErr {
            /* a refused quote is counted and named and the sweep goes on; any other error is the backend's, stops the sweep and is answered as itself. The message names what the write door did: a quote written before its dispatch failed counts as written */
            if false == errors.Is(updateErr, ErrUnusableRate) {
                message := "the rate refresh stopped at " + currency.Code + ": the quote could not be written"
                switch updateOutcome {
                case RateUpdateWritten:
                    outcome.Updated++
                    message = "the rate refresh stopped after " + currency.Code + ": the quote was written, but the listeners that drop its cache entries were not told; the entries stand until the next tick"
                case RateUpdateUnchanged:
                    outcome.Unchanged++
                    message = "the rate refresh stopped at " + currency.Code + ": the cache entries of an unchanged quote could not be dropped"
                }

                return outcome, exception.NewError(
                    message,
                    exceptioncontract.Context{"currencyCode": currency.Code, "outcome": rateUpdateOutcomeName(updateOutcome)},
                    updateErr,
                )
            }

            outcome.Refused++
            refusedCurrencyList = append(refusedCurrencyList, currency.Code)
            if nil == firstRefusal {
                firstRefusal = updateErr
            }

            continue
        }

        switch updateOutcome {
        case RateUpdateWritten:
            outcome.Updated++
        case RateUpdateUnchanged:
            outcome.Unchanged++
        case RateUpdateStale:
            outcome.Stale++
        default:
            outcome.Skipped++
        }
    }

    if 0 < outcome.Refused {
        /* the console prints the message alone, so it names the refused currencies and the reason of the first */
        return outcome, exception.NewError(
            "the rate provider quoted currencies the catalogue refused ("+strings.Join(refusedCurrencyList, ", ")+": "+firstRefusal.Error()+"); every other quote was written",
            exceptioncontract.Context{
                "refusedCurrencyList": refusedCurrencyList,
                "updated":             outcome.Updated,
            },
            firstRefusal,
        )
    }

    return outcome, nil
}

/* usableQuoteListOf judges the document as a whole before any quote is written and answers its rates keyed on the folded code. It refuses the whole document for a base that is not the catalogue's, an instant absent or beyond the clock skew, or two codes that fold onto one name. */
func (instance *RateRefreshService) usableQuoteListOf(document rateDocument, providerClock providerClockReading) (map[string]float64, error) {
    /* an empty catalogue base is refused first: the parameter falls onto its default only when the key is absent, so an empty RATES_BASE_CURRENCY arrives here as "" */
    if "" == instance.ratesBaseCurrency {
        return nil, exception.NewError(
            "the catalogue's base currency is not configured (RATES_BASE_CURRENCY is empty), so no rate document can be judged against it",
            nil,
            nil,
        )
    }

    documentBase := foldCurrencyCode(document.Base)
    if "" == documentBase {
        return nil, exception.NewError(
            "the rate document names no base it is quoted against (the catalogue's is "+instance.ratesBaseCurrency+")",
            exceptioncontract.Context{"catalogueBase": instance.ratesBaseCurrency},
            nil,
        )
    }

    if documentBase != instance.ratesBaseCurrency {
        /* both bases travel in the message because the cli engine echoes a failure's message alone; the provider's spelling is bounded and its control characters escaped, since it is the provider's text */
        return nil, exception.NewError(
            "the rate document is quoted against another base than the catalogue's (document "+consoleSpellingOf(document.Base)+", catalogue "+instance.ratesBaseCurrency+")",
            exceptioncontract.Context{
                "documentBase":  document.Base,
                "catalogueBase": instance.ratesBaseCurrency,
            },
            nil,
        )
    }

    if true == document.AsOf.IsZero() {
        return nil, exception.NewError("the rate document carries no instant the reading was taken at", nil, nil)
    }

    /* the provider's clock is trusted within the skew in either direction: beyond it a late clock cannot be told from a replay that kept its Date, so it is refused by name */
    if true == providerClock.exceeds(rateDocumentClockSkew) {
        return nil, exception.NewError(
            "the rate provider's clock is "+providerClock.Offset.String()+" off this one, beyond the "+rateDocumentClockSkew.String()+" skew a reading is admitted under; the answer may be a replay of an old one",
            exceptioncontract.Context{
                "offset":      providerClock.Offset.String(),
                "uncertainty": providerClock.Uncertainty.String(),
                "skew":        rateDocumentClockSkew.String(),
                "date":        providerClock.Date,
                "age":         providerClock.Age,
            },
            nil,
        )
    }

    /* a reading cannot be taken after the provider answered with it, and both instants are on the provider's clock; only an answer without a date is judged against this clock, under the skew */
    latestAcceptable := instance.clock.Now().Add(rateDocumentClockSkew)
    if true == providerClock.Measured {
        latestAcceptable = providerClock.AnsweredAt
    }

    if true == document.AsOf.After(latestAcceptable) {
        return nil, exception.NewError(
            "the rate document is stamped in the future",
            exceptioncontract.Context{
                "asOf":             document.AsOf,
                "latestAcceptable": latestAcceptable,
            },
            nil,
        )
    }

    quoteList := make(map[string]float64, len(document.Rates))
    /* every spelling is gathered before any is judged, so the refusal names all spellings of a colliding code, sorted, and the first such code in folded order, the same way on every run */
    spellingListByFolded := make(map[string][]string, len(document.Rates))
    for code, rate := range document.Rates {
        folded := foldCurrencyCode(code)
        spellingListByFolded[folded] = append(spellingListByFolded[folded], code)
        quoteList[folded] = rate
    }

    var collidingCodeList []string
    for folded, spellingList := range spellingListByFolded {
        if 1 < len(spellingList) {
            sort.Strings(spellingList)
            collidingCodeList = append(collidingCodeList, folded)
        }
    }

    if 0 < len(collidingCodeList) {
        sort.Strings(collidingCodeList)

        firstCode := collidingCodeList[0]
        spellingList := spellingListByFolded[firstCode]

        renderedSpellingList := make([]string, 0, len(spellingList))
        for _, spelling := range spellingList {
            renderedSpellingList = append(renderedSpellingList, consoleSpellingOf(spelling))
        }

        return nil, exception.NewError(
            "the rate document quotes one currency under "+strconv.Itoa(len(spellingList))+" spellings ("+strings.Join(renderedSpellingList, ", ")+")",
            exceptioncontract.Context{
                "code":              firstCode,
                "spellingList":      spellingList,
                "collidingCodeList": collidingCodeList,
            },
            nil,
        )
    }

    return quoteList, nil
}

/* rateReading is one answer of the provider: its document, and its clock read against this one. */
type rateReading struct {
    document      rateDocument
    providerClock providerClockReading
}

/* readRateDocument spends up to rateRefreshAttemptCount exchanges on one reading. Only a call that produced no answer and a 5xx answer are retried: a 4xx repeats the caller's mistake and an undecodable body is not sent twice by a working provider. The wait between attempts stops at the first cancellation of the run and answers it as the cause; an exchange in flight is bounded by the client's own timeout. */
func readRateDocument(ctx context.Context, client *httpclient.HttpClient, clockInstance melodyclockcontract.Clock) (rateReading, int, error) {
    var lastErr error

    for attempt := 1; attempt <= rateRefreshAttemptCount; attempt++ {
        if 1 < attempt {
            if waitErr := waitForRetry(ctx); nil != waitErr {
                return rateReading{}, attempt - 1, exception.NewError(
                    "the rate reading was stopped by the cancellation of its run",
                    exceptioncontract.Context{
                        "attempts": attempt - 1,
                    },
                    waitErr,
                )
            }
        }

        sentAt := clockInstance.Now()
        response, requestErr := client.Get(ratesLatestTarget)
        receivedAt := clockInstance.Now()
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
            return rateReading{}, attempt, exception.NewError(
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
            return rateReading{}, attempt, exception.NewError(
                "the rate provider answered a body that is not a rate document",
                exceptioncontract.Context{
                    "status": response.StatusCode(),
                },
                decodeErr,
            )
        }

        return rateReading{
            document:      document,
            providerClock: readProviderClock(response.Headers(), sentAt, receivedAt),
        }, attempt, nil
    }

    return rateReading{}, rateRefreshAttemptCount, exception.NewError(
        "the rate provider could not be read",
        exceptioncontract.Context{
            "attempts": rateRefreshAttemptCount,
        },
        lastErr,
    )
}

/* waitForRetry waits the backoff between two attempts, or answers the context's error once the run is cancelled; a timer rather than time.After, so a cancelled wait releases it at once. */
func waitForRetry(ctx context.Context) error {
    timer := time.NewTimer(rateRefreshRetryBackoff)
    defer timer.Stop()

    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-timer.C:
        return nil
    }
}

func MustGetRateRefreshService(resolver melodycontainercontract.Resolver) *RateRefreshService {
    return melodycontainer.MustFromResolver[*RateRefreshService](
        resolver,
        ServiceRateRefreshService,
    )
}

/* consoleSpellingOf bounds a provider-supplied text for a console line to a short prefix with its control characters spelled out, so it neither floods nor repaints the terminal. */
func consoleSpellingOf(text string) string {
    const consoleSpellingLimit = 16

    spelling := text
    if consoleSpellingLimit < len(spelling) {
        spelling = spelling[:consoleSpellingLimit] + "…"
    }

    return strconv.Quote(spelling)
}
