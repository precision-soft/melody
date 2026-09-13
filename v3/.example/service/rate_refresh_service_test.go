package service

import (
    "net/http"
    "net/http/httptest"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/httpclient"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const rateDocumentBody = `{"base":"EUR","asOf":"2026-09-07T09:00:00Z","rates":{"EUR":1,"USD":1.0842}}`

/* countingRateProvider answers what the caller asked it to and counts how many exchanges it was given. The
   count is the whole property the retry has: an attempt that never reached the provider is indistinguishable
   from one that did, from anywhere else. */
type countingRateProvider struct {
    server   *httptest.Server
    requests atomic.Int64
}

func newCountingRateProvider(t *testing.T, handler func(writer http.ResponseWriter, request *http.Request)) *countingRateProvider {
    t.Helper()

    provider := &countingRateProvider{}
    provider.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        provider.requests.Add(1)
        handler(writer, request)
    }))

    t.Cleanup(provider.server.Close)

    return provider
}

/* client builds the client the way the composition root does — a base url carrying its trailing slash, so
   the relative target the service names resolves under it rather than replacing it. */
func (instance *countingRateProvider) client(t *testing.T) *httpclient.HttpClient {
    t.Helper()

    client := httpclient.NewHttpClient(
        httpclient.NewHttpClientConfig(instance.server.URL+"/v1/", 2*time.Second, nil),
    )

    t.Cleanup(func() { _ = client.Close() })

    return client
}

func TestReadRateDocument_ReadsTheDocumentInOneExchangeWhenTheProviderAnswers(t *testing.T) {
    provider := newCountingRateProvider(t, func(writer http.ResponseWriter, request *http.Request) {
        if "/v1/latest" != request.URL.Path {
            t.Errorf("the service asked for %q, wanted the target resolved under the base", request.URL.Path)
        }

        writer.Header().Set("Content-Type", "application/json")
        _, _ = writer.Write([]byte(rateDocumentBody))
    })

    document, attempts, err := readRateDocument(provider.client(t))
    if nil != err {
        t.Fatalf("reading the document failed: %v", err)
    }

    if 1 != attempts {
        t.Errorf("the reading took %d attempts, wanted one", attempts)
    }

    if int64(1) != provider.requests.Load() {
        t.Errorf("the provider was given %d exchanges, wanted one", provider.requests.Load())
    }

    if "EUR" != document.Base {
        t.Errorf("the document names base %q, wanted EUR", document.Base)
    }

    if 1.0842 != document.Rates["USD"] {
        t.Errorf("the document quotes USD at %v, wanted 1.0842", document.Rates["USD"])
    }

    if time.Date(2026, time.September, 7, 9, 0, 0, 0, time.UTC) != document.AsOf.UTC() {
        t.Errorf("the document is stamped %s, wanted the provider's instant", document.AsOf.UTC())
    }
}

/* the number of exchanges is asserted as a VALUE rather than against the constant the code reads: derived
   from it, both sides would move together and a retry shortened to a single attempt would still pass. */
func TestReadRateDocument_SpendsEveryAttemptOnAProviderThatKeepsRefusing(t *testing.T) {
    provider := newCountingRateProvider(t, func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(http.StatusServiceUnavailable)
    })

    _, attempts, err := readRateDocument(provider.client(t))
    if nil == err {
        t.Fatal("a provider that answered 503 to every attempt was read successfully")
    }

    if 3 != attempts {
        t.Errorf("the reading took %d attempts, wanted three", attempts)
    }

    if int64(3) != provider.requests.Load() {
        t.Errorf("the provider was given %d exchanges, wanted three", provider.requests.Load())
    }
}

func TestReadRateDocument_TakesTheReadingFromAProviderThatRecoversOnTheSecondAttempt(t *testing.T) {
    provider := newCountingRateProvider(t, func(writer http.ResponseWriter, request *http.Request) {
        writer.Header().Set("Content-Type", "application/json")
        _, _ = writer.Write([]byte(rateDocumentBody))
    })

    var seen atomic.Int64
    provider.server.Config.Handler = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        provider.requests.Add(1)
        if 1 == seen.Add(1) {
            writer.WriteHeader(http.StatusBadGateway)

            return
        }

        writer.Header().Set("Content-Type", "application/json")
        _, _ = writer.Write([]byte(rateDocumentBody))
    })

    document, attempts, err := readRateDocument(provider.client(t))
    if nil != err {
        t.Fatalf("a provider that recovered was not read: %v", err)
    }

    if 2 != attempts {
        t.Errorf("the reading took %d attempts, wanted two", attempts)
    }

    if 1.0842 != document.Rates["USD"] {
        t.Errorf("the document quotes USD at %v, wanted 1.0842", document.Rates["USD"])
    }
}

/* a 4xx is the request being wrong, so repeating it repeats the mistake. The assertion that carries the
   property is the EXCHANGE COUNT, not the error: a retried 404 fails in the end too, and only the count
   tells the two apart. */
func TestReadRateDocument_DoesNotRepeatARequestTheProviderRefusedAsWrong(t *testing.T) {
    provider := newCountingRateProvider(t, func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(http.StatusNotFound)
    })

    _, attempts, err := readRateDocument(provider.client(t))
    if nil == err {
        t.Fatal("a 404 was read as a rate document")
    }

    if 1 != attempts {
        t.Errorf("the reading took %d attempts, wanted one", attempts)
    }

    if int64(1) != provider.requests.Load() {
        t.Errorf("the provider was given %d exchanges, wanted one", provider.requests.Load())
    }
}

/* a body that does not decode is not something a working provider sends twice, so it is not retried either */
func TestReadRateDocument_DoesNotRepeatAnAnswerThatIsNotARateDocument(t *testing.T) {
    provider := newCountingRateProvider(t, func(writer http.ResponseWriter, request *http.Request) {
        writer.Header().Set("Content-Type", "application/json")
        _, _ = writer.Write([]byte("not json at all"))
    })

    _, attempts, err := readRateDocument(provider.client(t))
    if nil == err {
        t.Fatal("a body that is not json was read as a rate document")
    }

    if 1 != attempts {
        t.Errorf("the reading took %d attempts, wanted one", attempts)
    }

    if int64(1) != provider.requests.Load() {
        t.Errorf("the provider was given %d exchanges, wanted one", provider.requests.Load())
    }
}

/* the unconfigured arm answers before it touches the runtime, which is what lets this probe hand it none:
   a service that reached for the container would panic here instead of answering, so the nil is the
   assertion as much as the outcome is. */
func TestRateRefreshServiceRefresh_DoesNothingWithNoProviderConfigured(t *testing.T) {
    outcome, err := NewRateRefreshService(nil, nil, "", "EUR").Refresh(nil)
    if nil != err {
        t.Fatalf("an unconfigured refresh failed: %v", err)
    }

    if true == outcome.Configured {
        t.Error("an unconfigured refresh reported a configured provider")
    }

    if 0 != outcome.Attempts || 0 != outcome.Updated || 0 != outcome.Skipped {
        t.Errorf("an unconfigured refresh reported %+v, wanted every count at zero", outcome)
    }
}

/* rateRefreshUnderTest is the refresh over the real currency service, the in-memory catalogue and the real
   http client, against a provider answering the body given; the rates client is registered in the runtime's
   container under the name the composition root uses, so the refresh resolves it the way production does. The
   clock is frozen at the seed's instant plus a day, so "the future" and "older than stored" are both
   nameable. */
func rateRefreshUnderTest(t *testing.T, body string) (*RateRefreshService, *CurrencyService, melodyruntimecontract.Runtime, *updateCountingCurrencyRepository) {
    t.Helper()

    provider := newCountingRateProvider(t, func(writer http.ResponseWriter, request *http.Request) {
        writer.Header().Set("Content-Type", "application/json")
        _, _ = writer.Write([]byte(body))
    })

    currencyService, _, runtimeInstance := currencyServiceUnderTest(t)
    recordingRepository := &updateCountingCurrencyRepository{CurrencyRepository: currencyService.currencyRepository}
    currencyService.currencyRepository = recordingRepository

    client := provider.client(t)
    melodycontainer.MustRegister(
        runtimeInstance.Container().(melodycontainercontract.Registrar),
        ServiceRatesHttpClient,
        func(resolver melodycontainercontract.Resolver) (*httpclient.HttpClient, error) {
            return client, nil
        },
    )

    clockInstance := &frozenClock{instant: currencyQuoteInstant.Add(24 * time.Hour)}

    return NewRateRefreshService(currencyService, clockInstance, provider.server.URL+"/v1/", "eur"), currencyService, runtimeInstance, recordingRepository
}

func TestRateRefreshServiceRefresh_WritesEveryQuoteOfADocumentOnTheCataloguesBase(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2026-09-08T09:00:00Z","rates":{"EUR":1,"USD":1.0842,"RON":4.9761}}`)

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil != err {
        t.Fatalf("the refresh failed: %v", err)
    }

    if 3 != outcome.Updated || 0 != outcome.Skipped || 0 != outcome.Unchanged || 0 != outcome.Stale || 0 != outcome.Refused {
        t.Errorf("the refresh reported %+v, wanted three updated and nothing else", outcome)
    }

    usd, _, _ := currencyService.FindById("cur-usd")
    if 1.0842 != usd.Rate {
        t.Errorf("cur-usd is quoted %v, wanted 1.0842", usd.Rate)
    }
}

/* a document quoted against another base is refused whole, before a single write: the rates in it are not
   rates this table can hold, and writing the ones it quotes would leave the catalogue with two bases and
   every conversion between the two groups false */
func TestRateRefreshServiceRefresh_RefusesADocumentQuotedAgainstAnotherBase(t *testing.T) {
    refresh, currencyService, runtimeInstance, recordingRepository := rateRefreshUnderTest(t, `{"base":"USD","asOf":"2026-09-08T09:00:00Z","rates":{"USD":1,"RON":4.6}}`)

    before, _, _ := currencyService.FindById("cur-ron")

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil == err {
        t.Fatal("a document quoted against USD was accepted by a catalogue quoted against EUR")
    }

    if false == strings.Contains(err.Error(), "another base") {
        t.Errorf("the refusal reads %q, wanted it to name the base", err.Error())
    }

    logContext := exception.LogContext(err)
    if "USD" != logContext["documentBase"] || "EUR" != logContext["catalogueBase"] {
        t.Errorf("the refusal carries %v, wanted both bases named", logContext)
    }

    if 0 != outcome.Updated || 0 != recordingRepository.updates.Load() {
        t.Errorf("a refused document still wrote: %+v, %d updates", outcome, recordingRepository.updates.Load())
    }

    after, _, _ := currencyService.FindById("cur-ron")
    if before.Rate != after.Rate {
        t.Errorf("a refused document moved cur-ron from %v to %v", before.Rate, after.Rate)
    }
}

func TestRateRefreshServiceRefresh_RefusesADocumentWithoutAnInstant(t *testing.T) {
    refresh, _, runtimeInstance, recordingRepository := rateRefreshUnderTest(t, `{"base":"EUR","rates":{"USD":1.2}}`)

    if _, err := refresh.Refresh(runtimeInstance); nil == err {
        t.Fatal("a document without asOf was accepted")
    }

    if 0 != recordingRepository.updates.Load() {
        t.Errorf("a document without an instant wrote %d rows", recordingRepository.updates.Load())
    }
}

func TestRateRefreshServiceRefresh_RefusesADocumentStampedInTheFuture(t *testing.T) {
    refresh, _, runtimeInstance, recordingRepository := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2026-09-08T10:06:00Z","rates":{"USD":1.2}}`)

    if _, err := refresh.Refresh(runtimeInstance); nil == err {
        t.Fatal("a document stamped six minutes past the clock was accepted")
    }

    if 0 != recordingRepository.updates.Load() {
        t.Errorf("a document from the future wrote %d rows", recordingRepository.updates.Load())
    }
}

/* five minutes of clock skew are admitted: a provider a few seconds ahead of this clock is an ordinary
   provider */
func TestRateRefreshServiceRefresh_AdmitsADocumentInsideTheClockSkew(t *testing.T) {
    refresh, _, runtimeInstance, _ := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2026-09-08T09:04:00Z","rates":{"USD":1.2}}`)

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil != err {
        t.Fatalf("a document four minutes ahead of the clock was refused: %v", err)
    }

    if 1 != outcome.Updated {
        t.Errorf("the refresh reported %+v, wanted one update", outcome)
    }
}

/* a document older than the reading the catalogue holds is a replay; the newer reading is kept and the
   run says so under its own heading, not under "updated" or "skipped" */
func TestRateRefreshServiceRefresh_CountsAReplayedDocumentAsStale(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2025-12-01T09:00:00Z","rates":{"EUR":1,"USD":9.9,"RON":9.9}}`)

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil != err {
        t.Fatalf("a replayed document failed instead of being kept out: %v", err)
    }

    if 3 != outcome.Stale || 0 != outcome.Updated {
        t.Errorf("a replayed document reported %+v, wanted three stale", outcome)
    }

    usd, _, _ := currencyService.FindById("cur-usd")
    if 1.1 != usd.Rate {
        t.Errorf("a replayed document moved cur-usd to %v", usd.Rate)
    }
}

/* the codes of the document are folded the way the catalogue's are matched, so a provider writing "usd"
   quotes the same currency the seed calls USD */
func TestRateRefreshServiceRefresh_MatchesTheDocumentsCodesFolded(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshUnderTest(t, `{"base":"eur","asOf":"2026-09-08T09:00:00Z","rates":{"usd":1.2," ron ":4.7}}`)

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil != err {
        t.Fatalf("the refresh failed: %v", err)
    }

    if 2 != outcome.Updated || 1 != outcome.Skipped {
        t.Errorf("the refresh reported %+v, wanted two updated (usd, ron) and one skipped (eur)", outcome)
    }

    usd, _, _ := currencyService.FindById("cur-usd")
    if 1.2 != usd.Rate {
        t.Errorf("cur-usd is quoted %v, wanted the lower-case quote 1.2", usd.Rate)
    }
}

func TestRateRefreshServiceRefresh_RefusesADocumentThatQuotesOneCurrencyTwice(t *testing.T) {
    refresh, _, runtimeInstance, recordingRepository := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2026-09-08T09:00:00Z","rates":{"usd":1.2,"USD":1.3}}`)

    if _, err := refresh.Refresh(runtimeInstance); nil == err {
        t.Fatal("a document quoting USD under two spellings was accepted")
    }

    if 0 != recordingRepository.updates.Load() {
        t.Errorf("an ambiguous document wrote %d rows", recordingRepository.updates.Load())
    }
}

/* one bad quote does not stop the sweep: the currencies after it are written on the same run, the refusal is
   counted and named, and the run still fails so the schedule hears about the quote */
func TestRateRefreshServiceRefresh_WritesTheGoodQuotesPastARefusedOne(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2026-09-08T09:00:00Z","rates":{"EUR":1,"RON":0,"USD":1.2}}`)

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil == err {
        t.Fatal("a document with a zero quote reported no failure")
    }

    if 2 != outcome.Updated || 1 != outcome.Refused {
        t.Errorf("the refresh reported %+v, wanted two updated and one refused", outcome)
    }

    refusedList, _ := exception.LogContext(err)["refusedCurrencyList"].([]string)
    if 1 != len(refusedList) || "RON" != refusedList[0] {
        t.Errorf("the refusal names %v, wanted RON", refusedList)
    }

    usd, _, _ := currencyService.FindById("cur-usd")
    if 1.2 != usd.Rate {
        t.Errorf("the quote after the refused one was not written: cur-usd %v", usd.Rate)
    }
}

func TestRateRefreshServiceRefresh_RefusesAQuoteThatIsNotAUsablePrice(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2026-09-08T09:00:00Z","rates":{"USD":1e308}}`)

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil == err || 1 != outcome.Refused {
        t.Fatalf("a quote of 1e308 was taken: %+v, %v", outcome, err)
    }

    usd, _, _ := currencyService.FindById("cur-usd")
    if 1.1 != usd.Rate {
        t.Errorf("cur-usd moved to %v on a quote that is not a price", usd.Rate)
    }
}

/* the same document twice: the second run writes nothing and says so, where the previous form issued a
   full-row UPDATE per currency and, on mysql, read its zero affected rows as three vanished currencies */
func TestRateRefreshServiceRefresh_ReportsAnUnmovedDocumentAsUnchangedWithoutWriting(t *testing.T) {
    refresh, _, runtimeInstance, recordingRepository := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2026-09-08T09:00:00Z","rates":{"EUR":1,"USD":1.0842,"RON":4.9761}}`)

    if _, err := refresh.Refresh(runtimeInstance); nil != err {
        t.Fatalf("the first refresh failed: %v", err)
    }

    updatesAfterFirst := recordingRepository.updates.Load()

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil != err {
        t.Fatalf("the second refresh failed: %v", err)
    }

    if 3 != outcome.Unchanged || 0 != outcome.Updated || 0 != outcome.Skipped {
        t.Errorf("the second refresh reported %+v, wanted three unchanged", outcome)
    }

    if updatesAfterFirst != recordingRepository.updates.Load() {
        t.Errorf("the second refresh issued %d UPDATE statements, wanted none", recordingRepository.updates.Load()-updatesAfterFirst)
    }
}
