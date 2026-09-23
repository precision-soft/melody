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

/* rateDocumentClockSkew is how far ahead of this clock a provider's asOf may lie before the document is refused,
   for an answer that carried no readable date: five minutes is more than any pair of synchronised clocks drift
   and less than any interval the schedule runs at, so an instant beyond it is a provider whose clock is wrong or
   a document written by hand. An answer that carries its date is judged on the provider's own clock instead —
   see usableQuoteListOf — where no skew has to be guessed at. */
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
    /* ProviderClockMeasured and ProviderClockOffset say how the provider's stamps were moved onto this clock:
       an offset of zero on a measured clock is a provider whose clock agrees with this one to within what one
       answer can tell, while an unmeasured clock is an answer that carried no readable date — see
       providerClockReading. */
    ProviderClockMeasured bool
    ProviderClockOffset   time.Duration
}

/* rateDocument is the provider's answer. The rates are quoted against Base, one unit of Base costing that
   many units of the currency, and AsOf is when the provider took the reading, on the provider's clock — which
   is what this application stores, beside the same instant moved onto its own clock, so a reader can tell the
   age of a quote rather than the age of the last refresh run.
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

    /* the reading is written with its instant in both frames: the provider's stamp as it came, which names the
       reading, and the same instant moved onto this clock, on which its order against the stored reading is
       judged — so a provider whose clock was set back between two readings has the later one written, while a
       replay, whose stamp is old under a clock that answers now, is still older here */
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
            /* a refused QUOTE is counted and named, and the sweep goes on: the currencies after it are
               written on this run instead of waiting for a provider that may keep quoting that one badly.
               Any other error is the backend's — the repository, the cache drop of an unchanged quote, the
               dispatch after a written one — and is not the provider's fault: it stops the sweep and is
               handed back as itself, so a redis outage on a tick reads in the cron log as a redis outage
               and not as a quote the catalogue refused. The message names what the write door DID, not the
               class of the failure: a quote written before its dispatch failed is counted written, which it
               is, and the console said "could not be written" under a table counting it UPDATED; an
               unchanged quote whose cache drop failed was never written at all. */
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
func (instance *RateRefreshService) usableQuoteListOf(document rateDocument, providerClock providerClockReading) (map[string]float64, error) {
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

    /* a reading cannot have been taken after the provider answered with it, and both instants are on the
       provider's clock, so the judgement needs no guess at how far that clock is from this one; only an answer
       without a date is judged against this clock, under the skew */
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
    /* every spelling is gathered before any is judged, so the refusal of a code quoted under several spellings
       names ALL of them, sorted, and names the first such code in folded order when several are: judged while the
       document's map was walked, it named the first two spellings the walk happened to meet, and three spellings
       of one code read three different ways over as many runs. They travel in the message for the reason the base
       does above, and every colliding code travels in the context */
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

/* readRateDocument spends up to rateRefreshAttemptCount exchanges on one reading. What is retried is
   deliberately narrow: a call that never produced an answer, and an answer in the 5xx class, because those
   are the two a provider can recover from between one attempt and the next. A 4xx is NOT retried — the
   request is what is wrong, so repeating it repeats the mistake and spends the provider's budget doing it —
   and neither is a body that fails to decode, which a working provider does not send twice.

   The wait between two attempts is where the run's context is heard. A plain sleep there slept through a
   cancellation and sent the next attempt anyway, so a SIGTERM landing on a refused reading held the process
   for two more backoffs and two more exchanges; the reading now stops at the first wait that finds the run
   cancelled, and hands the cancellation back as its cause. One exchange in flight is still bounded by the
   client's own timeout, since the client's doors take no context. */
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

/* waitForRetry waits the backoff between two attempts, or answers the context's error the moment the run is
   cancelled; a timer rather than time.After, so a cancelled wait releases it at once. */
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
