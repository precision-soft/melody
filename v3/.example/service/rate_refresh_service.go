package service

import (
    "errors"
    "strconv"
    "strings"
    "time"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
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

/* rateDocumentClockSkew is how far into the future a provider's asOf may lie before the document is refused:
   five minutes is more than any pair of synchronised clocks drift and less than any interval the schedule
   runs at, so an instant beyond it is a provider whose clock is wrong or a document written by hand, and a
   reading stamped in the future would make every later, correct reading look stale beside it. */
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

/* RateRefreshService brings the exchange rates the catalogue quotes with up to the provider's. It writes
   through CurrencyService and not through the repository because the currency list and every currency by id
   are cached, and the listeners that drop those entries are subscribed to the event the service dispatches:
   a rate written behind the cache is a rate no reader ever sees. The guarantee holds on the SHARED cache:
   the listeners run in the process that dispatched, which is the scheduler or a console command and never
   the http server, so on the in-process fallback the server keeps the currencies it cached until it
   restarts — the refresh command says so on its output when that is the wiring it ran under.

   Every rate in the catalogue is quoted against ONE base, named by ratesBaseCurrency — the parameter the
   configuration declares, with the seed's base as its default — and a document is written only when it is
   quoted against that same base: a conversion cancels the base by dividing one rate by the other, which is
   arithmetic only while both rates share it, so a provider quoting against another base, or a document that
   quotes a subset of the catalogue, would leave the table with two bases and every conversion between the
   two groups false. The document is refused whole, before any write, and the refusal names both bases.

   The client is resolved when a refresh runs rather than taken in the constructor, and that has an
   observable consequence rather than being a preference: nothing else in this application calls the
   provider, so an http process that serves reads all day resolves no client, builds no transport and opens
   no connection pool. */
type RateRefreshService struct {
    currencyService   *CurrencyService
    clock             melodyclockcontract.Clock
    ratesBaseUrl      string
    ratesBaseCurrency string
}

/* RateRefreshOutcome is what a refresh did, in numbers a caller can print and a test can assert. Every
   currency of the catalogue lands under exactly one heading. Skipped is "no quote was written and nothing
   went wrong": a currency the provider did not quote, which is the ordinary case because a provider is
   entitled to quote fewer currencies than a catalogue carries, or one that stopped existing between the
   listing and its own update, which is a delete landing inside the run. Unchanged is a quote the catalogue
   already held, at the instant it already held it — the ordinary answer of a provider between two moves.
   Stale is a quote older than the one stored, a replay the catalogue keeps its newer reading over. Refused
   is a quote the write door would not take, and the sweep goes on past it: the currencies after a bad quote
   are written on the same run, and the error the refresh hands back names every currency it refused. */
type RateRefreshOutcome struct {
    Configured bool
    Attempts   int
    Updated    int
    Unchanged  int
    Stale      int
    Skipped    int
    Refused    int
    AsOf       time.Time
}

/* rateDocument is the provider's answer. The rates are quoted against Base, one unit of Base costing that
   many units of the currency, and AsOf is when the provider took the reading — which is what this
   application stores, so a reader can tell the age of a quote rather than the age of the last refresh run.
   Both halves are load-bearing, and both are judged before a quote is written: the base must be the
   catalogue's, and the instant must be present and not in the future. */
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

    quoteList, documentErr := instance.usableQuoteListOf(document)
    if nil != documentErr {
        return outcome, documentErr
    }

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

        _, updateOutcome, updateErr := instance.currencyService.UpdateRate(runtimeInstance, currency.Id, rate, document.AsOf)
        if nil != updateErr {
            /* a refused QUOTE is counted and named, and the sweep goes on: the currencies after it are
               written on this run instead of waiting for a provider that may keep quoting that one badly.
               Any other error is the backend's — the repository, the cache drop of an unchanged quote, the
               dispatch after a written one — and is not the provider's fault: it stops the sweep and is
               handed back as itself, so a redis outage on a tick reads in the cron log as a redis outage
               and not as a quote the catalogue refused. A quote already written before its dispatch failed
               is counted written, which it is. */
            if false == errors.Is(updateErr, ErrUnusableRate) {
                if RateUpdateWritten == updateOutcome {
                    outcome.Updated++
                }

                return outcome, exception.NewError(
                    "the rate refresh stopped at "+currency.Code+": the quote could not be written",
                    exceptioncontract.Context{"currencyCode": currency.Code},
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
        /* the console line names the currencies AND why the first was refused: the reason lived in the
           context alone, which the cli engine does not print, so an operator read "(USD)" and had to open
           the journal for the rate that was not a price */
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

/* usableQuoteListOf judges the document as a whole before a single quote is written, and hands back its
   rates keyed on the folded code, the spelling the catalogue is matched on. Three refusals, each of the
   whole document: a base that is not the catalogue's, since a rate against another base is not a rate this
   table can hold; an instant absent or beyond the clock skew, since the instant is what every reader judges
   the age of a quote by; and two codes that fold onto one name, since the document then says two things
   about one currency and nothing chooses between them. */
func (instance *RateRefreshService) usableQuoteListOf(document rateDocument) (map[string]float64, error) {
    /* a catalogue base that is empty is refused before the document is compared against it: the parameter
       falls onto its default only when the key is absent, so RATES_BASE_CURRENCY= in a deployment template
       reached here as "", and a document naming no base folded onto it and was written whole */
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
        /* both bases travel in the message as well as in the context: the cli engine echoes a failure's
           message alone, and an operator reading a cron log needs to see WHICH base the provider quoted.
           The provider's spelling is bounded and its control characters escaped before it reaches a
           console line: it is the provider's text, not this application's */
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

    latestAcceptable := instance.clock.Now().Add(rateDocumentClockSkew)
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
    for code, rate := range document.Rates {
        folded := foldCurrencyCode(code)
        if _, seen := quoteList[folded]; true == seen {
            return nil, exception.NewError(
                "the rate document quotes one currency under two spellings",
                exceptioncontract.Context{"code": folded},
                nil,
            )
        }

        quoteList[folded] = rate
    }

    return quoteList, nil
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

/* consoleSpellingOf bounds a provider-supplied text for a console line — the message of a refusal is printed
   as it is — to a short prefix with its control characters spelled out, so a base of a thousand bytes or one
   carrying an escape sequence neither floods nor repaints the terminal. */
func consoleSpellingOf(text string) string {
    const consoleSpellingLimit = 16

    spelling := text
    if consoleSpellingLimit < len(spelling) {
        spelling = spelling[:consoleSpellingLimit] + "…"
    }

    return strconv.Quote(spelling)
}
