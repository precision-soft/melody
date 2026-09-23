package service

import (
    "context"
    "errors"
    "fmt"
    "net/http"
    "net/http/httptest"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/httpclient"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
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

    reading, attempts, err := readRateDocument(context.Background(), provider.client(t), melodyclock.NewSystemClock())
    if nil != err {
        t.Fatalf("reading the document failed: %v", err)
    }

    if 1 != attempts {
        t.Errorf("the reading took %d attempts, wanted one", attempts)
    }

    if int64(1) != provider.requests.Load() {
        t.Errorf("the provider was given %d exchanges, wanted one", provider.requests.Load())
    }

    if "EUR" != reading.document.Base {
        t.Errorf("the document names base %q, wanted EUR", reading.document.Base)
    }

    if 1.0842 != reading.document.Rates["USD"] {
        t.Errorf("the document quotes USD at %v, wanted 1.0842", reading.document.Rates["USD"])
    }

    if time.Date(2026, time.September, 7, 9, 0, 0, 0, time.UTC) != reading.document.AsOf.UTC() {
        t.Errorf("the document is stamped %s, wanted the provider's instant", reading.document.AsOf.UTC())
    }
}

/* the number of exchanges is asserted as a VALUE rather than against the constant the code reads: derived
   from it, both sides would move together and a retry shortened to a single attempt would still pass. */
func TestReadRateDocument_SpendsEveryAttemptOnAProviderThatKeepsRefusing(t *testing.T) {
    provider := newCountingRateProvider(t, func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(http.StatusServiceUnavailable)
    })

    _, attempts, err := readRateDocument(context.Background(), provider.client(t), melodyclock.NewSystemClock())
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

    reading, attempts, err := readRateDocument(context.Background(), provider.client(t), melodyclock.NewSystemClock())
    if nil != err {
        t.Fatalf("a provider that recovered was not read: %v", err)
    }

    if 2 != attempts {
        t.Errorf("the reading took %d attempts, wanted two", attempts)
    }

    if 1.0842 != reading.document.Rates["USD"] {
        t.Errorf("the document quotes USD at %v, wanted 1.0842", reading.document.Rates["USD"])
    }
}

/* a 4xx is the request being wrong, so repeating it repeats the mistake. The assertion that carries the
   property is the EXCHANGE COUNT, not the error: a retried 404 fails in the end too, and only the count
   tells the two apart. */
func TestReadRateDocument_DoesNotRepeatARequestTheProviderRefusedAsWrong(t *testing.T) {
    provider := newCountingRateProvider(t, func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(http.StatusNotFound)
    })

    _, attempts, err := readRateDocument(context.Background(), provider.client(t), melodyclock.NewSystemClock())
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

    _, attempts, err := readRateDocument(context.Background(), provider.client(t), melodyclock.NewSystemClock())
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

    return rateRefreshAnsweringInTurn(t, body)
}

/* rateRefreshAnsweringInTurn is rateRefreshUnderTest over a provider that answers the bodies given one per
   request, in order, and the last one from then on: what a refresh does is sometimes decided by the reading
   a PREVIOUS run stored. The provider's clock agrees with the refresh's on every answer. */
func rateRefreshAnsweringInTurn(t *testing.T, bodyList ...string) (*RateRefreshService, *CurrencyService, melodyruntimecontract.Runtime, *updateCountingCurrencyRepository) {
    t.Helper()

    answerList := make([]providerAnswer, 0, len(bodyList))
    for _, body := range bodyList {
        answerList = append(answerList, providerAnswer{body: body})
    }

    return rateRefreshAnswering(t, answerList...)
}

/* providerAnswer is one answer of the stub provider: its body, and the provider's clock when it answered —
   clockAhead of the refresh's clock, written into the Date header the way a server writes its own, with the
   Age a cache in front of it adds when age is set, and no Date at all when omitDate is set. */
type providerAnswer struct {
    body       string
    clockAhead time.Duration
    age        string
    omitDate   bool
}

/* rateRefreshAnswering is rateRefreshAnsweringInTurn with the provider's clock stated per answer. */
func rateRefreshAnswering(t *testing.T, answerList ...providerAnswer) (*RateRefreshService, *CurrencyService, melodyruntimecontract.Runtime, *updateCountingCurrencyRepository) {
    t.Helper()

    clockInstance := &frozenClock{instant: currencyQuoteInstant.Add(24 * time.Hour)}

    var served atomic.Int64
    provider := newCountingRateProvider(t, func(writer http.ResponseWriter, request *http.Request) {
        answer := answerList[min(int(served.Add(1))-1, len(answerList)-1)]

        if true == answer.omitDate {
            writer.Header()["Date"] = nil
        } else {
            writer.Header().Set("Date", clockInstance.Now().Add(answer.clockAhead).UTC().Format(http.TimeFormat))
        }

        if "" != answer.age {
            writer.Header().Set("Age", answer.age)
        }

        writer.Header().Set("Content-Type", "application/json")
        _, _ = writer.Write([]byte(answer.body))
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

/* a reading cannot have been taken after the provider answered with it: both instants are on the provider's
   clock, so the refusal needs no guess at how far that clock is from this one, and a stamp one minute past the
   answer is refused where the skew used to admit five */
func TestRateRefreshServiceRefresh_RefusesADocumentStampedInTheFuture(t *testing.T) {
    refresh, _, runtimeInstance, recordingRepository := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2026-09-08T09:01:00Z","rates":{"USD":1.2}}`)

    if _, err := refresh.Refresh(runtimeInstance); nil == err {
        t.Fatal("a document stamped a minute after the provider answered was accepted")
    }

    if 0 != recordingRepository.updates.Load() {
        t.Errorf("a document from the future wrote %d rows", recordingRepository.updates.Load())
    }
}

/* a provider whose clock runs ahead of this one is an ordinary provider: its answer says so, the Date it writes
   being ahead by as much as its stamps are, and the reading is stored with its stamp as it came and its instant
   moved onto this clock */
func TestRateRefreshServiceRefresh_AdmitsAProviderWhoseClockRunsAhead(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshAnswering(t, providerAnswer{
        body:       `{"base":"EUR","asOf":"2026-09-08T09:04:00Z","rates":{"USD":1.2}}`,
        clockAhead: 4 * time.Minute,
    })

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil != err {
        t.Fatalf("a provider four minutes ahead of this clock was refused: %v", err)
    }

    if 1 != outcome.Updated || false == outcome.ProviderClockMeasured || outcome.ProviderClockOffset < 4*time.Minute || outcome.ProviderClockOffset > 4*time.Minute+time.Second {
        t.Fatalf("the refresh reported %+v, wanted one update over a clock measured four minutes ahead", outcome)
    }

    usd, _, _ := currencyService.FindById("cur-usd")
    stamped := time.Date(2026, time.September, 8, 9, 4, 0, 0, time.UTC)
    if false == usd.ProviderRateAsOf.Equal(stamped) {
        t.Errorf("the provider's stamp was stored as %s, wanted %s as it came", usd.ProviderRateAsOf, stamped)
    }

    onThisClock := stamped.Add(-4 * time.Minute)
    if drift := usd.RateAsOf.Sub(onThisClock); drift < -time.Second || drift > time.Second {
        t.Errorf("the reading was stored at %s on this clock, wanted %s to within the second the date is read to", usd.RateAsOf, onThisClock)
    }
}

/* the provider's clock ran five minutes ahead and was then set back: judged stamp against stamp, the reading
   stamped before the correction was newer than every honest reading of the five minutes after it, and each was
   kept out as stale. On this clock the two land in the order they were taken */
func TestRateRefreshServiceRefresh_AProviderClockSetBackDoesNotPinTheCatalogue(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshAnswering(
        t,
        providerAnswer{body: `{"base":"EUR","asOf":"2026-09-08T09:04:59Z","rates":{"USD":1.2}}`, clockAhead: 5 * time.Minute},
        providerAnswer{body: `{"base":"EUR","asOf":"2026-09-08T09:00:00Z","rates":{"USD":1.3}}`},
    )

    if _, err := refresh.Refresh(runtimeInstance); nil != err {
        t.Fatalf("the reading of the clock running ahead was refused: %v", err)
    }

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil != err {
        t.Fatalf("the reading after the correction failed: %v", err)
    }

    usd, _, _ := currencyService.FindById("cur-usd")
    if 1 != outcome.Updated || 1.3 != usd.Rate {
        t.Errorf("the reading after the correction reported %+v and left cur-usd at %v, wanted it written at 1.3", outcome, usd.Rate)
    }
}

/* a replay keeps its old stamp under a clock that answers now, so on this clock it is older than the reading it
   replays, whatever the provider's clock is off by: stored at the clock's own instant, as the clamp used to store a
   reading ahead of it, a replay of the minutes before was written over the newer reading */
func TestRateRefreshServiceRefresh_KeepsOutAReplayFromAProviderWhoseClockRunsAhead(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshAnswering(
        t,
        providerAnswer{body: `{"base":"EUR","asOf":"2026-09-08T09:04:00Z","rates":{"USD":1.2}}`, clockAhead: 4 * time.Minute},
        providerAnswer{body: `{"base":"EUR","asOf":"2026-09-08T09:02:00Z","rates":{"USD":1.3}}`, clockAhead: 4*time.Minute + 30*time.Second},
    )

    if _, err := refresh.Refresh(runtimeInstance); nil != err {
        t.Fatalf("the first reading failed: %v", err)
    }

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil != err {
        t.Fatalf("the replay failed instead of being kept out: %v", err)
    }

    usd, _, _ := currencyService.FindById("cur-usd")
    if 1 != outcome.Stale || 0 != outcome.Updated || 1.2 != usd.Rate {
        t.Errorf("the replay reported %+v and left cur-usd at %v, wanted it stale and 1.2 kept", outcome, usd.Rate)
    }
}

/* the same document read on two runs is the same reading, named by the provider's stamp: the instant this
   clock gives it is measured again on each arrival — here the second answer's date falls one second later, the
   resolution the date is read to — and stored at the clock's instant, as the clamp used to store it, it was never
   the reading already held: every run wrote it again, dispatched, and dropped the cache */
func TestRateRefreshServiceRefresh_TheSameDocumentFromAClockAheadIsUnchangedOnTheNextRun(t *testing.T) {
    refresh, _, runtimeInstance, recordingRepository := rateRefreshAnswering(
        t,
        providerAnswer{body: `{"base":"EUR","asOf":"2026-09-08T09:00:30Z","rates":{"USD":1.2}}`, clockAhead: 30 * time.Second},
        providerAnswer{body: `{"base":"EUR","asOf":"2026-09-08T09:00:30Z","rates":{"USD":1.2}}`, clockAhead: 31 * time.Second},
    )

    if _, err := refresh.Refresh(runtimeInstance); nil != err {
        t.Fatalf("the first run failed: %v", err)
    }

    updatesBefore := recordingRepository.updates.Load()

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil != err {
        t.Fatalf("the second run failed: %v", err)
    }

    if 1 != outcome.Unchanged || 0 != outcome.Updated || updatesBefore != recordingRepository.updates.Load() {
        t.Errorf("the second run reported %+v with %d writes, wanted the reading unchanged and nothing written", outcome, recordingRepository.updates.Load()-updatesBefore)
    }
}

/* a cache in front of the provider keeps the origin's Date on an answer it hands out again and says how long
   it kept it in Age: read without the Age, an hour-old document from a cache was a provider whose clock runs an
   hour late, and its old stamp, moved forward by that hour, was written over the newer reading */
func TestRateRefreshServiceRefresh_ReadsTheAgeOfACachedAnswer(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshAnswering(
        t,
        providerAnswer{body: `{"base":"EUR","asOf":"2026-09-08T08:59:00Z","rates":{"USD":1.2}}`},
        providerAnswer{body: `{"base":"EUR","asOf":"2026-09-08T08:00:00Z","rates":{"USD":1.3}}`, clockAhead: -time.Hour, age: "3600"},
    )

    if _, err := refresh.Refresh(runtimeInstance); nil != err {
        t.Fatalf("the first reading failed: %v", err)
    }

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil != err {
        t.Fatalf("the cached answer failed: %v", err)
    }

    usd, _, _ := currencyService.FindById("cur-usd")
    if 1 != outcome.Stale || 1.2 != usd.Rate || 0 != outcome.ProviderClockOffset {
        t.Errorf("the cached answer reported %+v and left cur-usd at %v, wanted it stale over a clock that agrees", outcome, usd.Rate)
    }
}

/* an answer without a date says nothing of the provider's clock: its stamps are taken as they came, the run
   says the clock went unmeasured, and a stamp ahead of this clock is judged under the skew */
func TestRateRefreshServiceRefresh_AnAnswerWithoutADateIsJudgedUnderTheSkew(t *testing.T) {
    admitted, _, admittedRuntime, _ := rateRefreshAnswering(t, providerAnswer{
        body:     `{"base":"EUR","asOf":"2026-09-08T09:04:00Z","rates":{"USD":1.2}}`,
        omitDate: true,
    })

    outcome, err := admitted.Refresh(admittedRuntime)
    if nil != err || 1 != outcome.Updated || true == outcome.ProviderClockMeasured || 0 != outcome.ProviderClockOffset {
        t.Fatalf("a dateless answer four minutes ahead reported %+v, %v; wanted it written over an unmeasured clock", outcome, err)
    }

    refused, _, refusedRuntime, recordingRepository := rateRefreshAnswering(t, providerAnswer{
        body:     `{"base":"EUR","asOf":"2026-09-08T09:06:00Z","rates":{"USD":1.2}}`,
        omitDate: true,
    })

    if _, err := refused.Refresh(refusedRuntime); nil == err || 0 != recordingRepository.updates.Load() {
        t.Fatalf("a dateless answer six minutes ahead was not refused: %v, %d writes", err, recordingRepository.updates.Load())
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

/* the refusal names both spellings the provider used, in the message the cli engine echoes: the folded
   name alone says which currency is ambiguous but not what the document wrote */
func TestRateRefreshServiceRefresh_NamesBothSpellingsOfACurrencyQuotedTwice(t *testing.T) {
    refresh, _, runtimeInstance, _ := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2026-09-08T09:00:00Z","rates":{"usd":1.2,"USD":1.3}}`)

    _, err := refresh.Refresh(runtimeInstance)
    if nil == err {
        t.Fatal("a document quoting USD under two spellings was accepted")
    }

    if false == strings.Contains(err.Error(), `"USD"`) || false == strings.Contains(err.Error(), `"usd"`) {
        t.Errorf("the refusal does not name both spellings: %v", err)
    }
}

/* every spelling of a currency quoted more than twice is named, and the refusal reads the same on every run:
   judged while the document's map was walked, three spellings read three different ways over as many runs */
func TestRateRefreshServiceRefresh_NamesEverySpellingOfACurrencyTheSameOnEveryRun(t *testing.T) {
    messageSet := map[string]bool{}

    for run := 0; run < 50; run++ {
        refresh, _, runtimeInstance, _ := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2026-09-08T09:00:00Z","rates":{"usd":1.2,"USD":1.3,"Usd":1.4}}`)

        _, err := refresh.Refresh(runtimeInstance)
        if nil == err {
            t.Fatal("a document quoting USD under three spellings was accepted")
        }

        messageSet[err.Error()] = true
    }

    if 1 != len(messageSet) {
        t.Fatalf("the refusal read %d different ways over fifty runs: %v", len(messageSet), messageSet)
    }

    for message := range messageSet {
        for _, spelling := range []string{`"USD"`, `"Usd"`, `"usd"`} {
            if false == strings.Contains(message, spelling) {
                t.Errorf("the refusal does not name %s: %s", spelling, message)
            }
        }
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

    /* the console line — the cli engine prints the message alone — names the currency AND the reason, and the
       first refusal stays the cause so errors.Is reaches the rate sentinel */
    if false == strings.Contains(err.Error(), "(RON: ") || false == strings.Contains(err.Error(), "positive, finite number") {
        t.Errorf("the message reads %q, wanted the refused currency with its reason", err.Error())
    }

    if false == errors.Is(err, ErrUnusableRate) {
        t.Errorf("the refusal does not carry the rate sentinel as its cause: %v", err)
    }

    if false == outcome.AsOf.Equal(time.Date(2026, time.September, 8, 9, 0, 0, 0, time.UTC)) {
        t.Errorf("the outcome carries %v as the document's instant, wanted the document's", outcome.AsOf)
    }

    usd, _, _ := currencyService.FindById("cur-usd")
    if 1.2 != usd.Rate {
        t.Errorf("the quote after the refused one was not written: cur-usd %v", usd.Rate)
    }
}

/* vanishingCurrencyRepository lists a currency and then answers absent when it is looked up — a currency
   deleted between the sweep's listing and its write */
type vanishingCurrencyRepository struct {
    repository.CurrencyRepository
    vanishedId string
}

func (instance *vanishingCurrencyRepository) FindById(ctx context.Context, id string) (*entity.Currency, bool, error) {
    if id == instance.vanishedId {
        return nil, false, nil
    }

    return instance.CurrencyRepository.FindById(ctx, id)
}

/* a currency deleted inside the run is counted skipped — not unchanged, which would say the quote was held */
func TestRateRefreshServiceRefresh_CountsACurrencyDeletedInsideTheRunAsSkipped(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2026-09-08T09:00:00Z","rates":{"EUR":1,"USD":1.0842,"RON":4.9761}}`)
    currencyService.currencyRepository = &vanishingCurrencyRepository{CurrencyRepository: currencyService.currencyRepository, vanishedId: "cur-ron"}

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil != err {
        t.Fatalf("a vanished currency failed the run: %v", err)
    }

    if 2 != outcome.Updated || 1 != outcome.Skipped || 0 != outcome.Unchanged {
        t.Fatalf("the refresh reported %+v, wanted two updated and the vanished one skipped", outcome)
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

/* microsecondCurrencyRepository stores a quote's instant the way the DATETIME(6) column does — truncated to
   the microsecond — so a test can see what the database sees: an instant compared at the nanosecond against
   the row it wrote was never equal to it. */
type microsecondCurrencyRepository struct {
    repository.CurrencyRepository
}

func (instance *microsecondCurrencyRepository) Update(ctx context.Context, currency *entity.Currency) (bool, error) {
    stored := *currency
    stored.RateAsOf = stored.RateAsOf.Truncate(time.Microsecond)

    return instance.CurrencyRepository.Update(ctx, &stored)
}

func (instance *microsecondCurrencyRepository) UpdateQuote(ctx context.Context, id string, quote entity.RateQuote) (bool, error) {
    return instance.CurrencyRepository.UpdateQuote(ctx, id, entity.NewRateQuote(quote.Rate, quote.AsOf.Truncate(time.Microsecond), quote.ProviderAsOf.Truncate(time.Microsecond)))
}

/* a provider stamping time.Now() serialises nine decimals, and the column holds six: the second run of the
   same document has to read as unchanged against the row the first run wrote, not as a full-row update the
   driver reports as no row — which the sweep counted as the currency having vanished, and which skipped the
   cache drop the unchanged branch exists for. The document is stamped a second BEFORE the frozen clock: one
   ahead of it is stored at the clock's instant, which has no sub-microsecond digits to truncate. */
func TestRateRefreshServiceRefresh_JudgesTheInstantAtTheResolutionTheColumnHolds(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2026-09-08T08:59:59.123456789Z","rates":{"EUR":1,"USD":1.0842,"RON":4.9761}}`)
    currencyService.currencyRepository = &microsecondCurrencyRepository{CurrencyRepository: currencyService.currencyRepository}

    if outcome, err := refresh.Refresh(runtimeInstance); nil != err || 3 != outcome.Updated {
        t.Fatalf("the first refresh reported %+v, %v; wanted three updated", outcome, err)
    }

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil != err {
        t.Fatalf("the second refresh failed: %v", err)
    }

    if 3 != outcome.Unchanged || 0 != outcome.Skipped || 0 != outcome.Updated {
        t.Fatalf("the second refresh reported %+v, wanted three unchanged over a column that holds microseconds", outcome)
    }

    usd, _, _ := currencyService.FindById("cur-usd")
    if false == usd.RateAsOf.Equal(time.Date(2026, time.September, 8, 8, 59, 59, 123456000, time.UTC)) {
        t.Fatalf("the stored instant is %v, wanted the document's truncated to the microsecond", usd.RateAsOf)
    }
}

/* a catalogue base that is empty — RATES_BASE_CURRENCY present and blank, which the default does not cover —
   refuses every document rather than folding onto the one that names no base */
func TestRateRefreshServiceRefresh_RefusesEveryDocumentUnderAnEmptyCatalogueBase(t *testing.T) {
    refresh, _, runtimeInstance, recordingRepository := rateRefreshUnderTest(t, `{"asOf":"2026-09-08T09:00:00Z","rates":{"EUR":1,"USD":1.0842,"RON":4.9761}}`)
    refresh.ratesBaseCurrency = ""

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil == err || false == strings.Contains(err.Error(), "RATES_BASE_CURRENCY is empty") {
        t.Fatalf("a document without a base was judged against an empty catalogue base: %+v, %v", outcome, err)
    }

    if 0 != recordingRepository.updates.Load() {
        t.Fatalf("the refusal came after %d writes, wanted none", recordingRepository.updates.Load())
    }
}

/* refusingDispatcher refuses every dispatch, the way a listener whose backend is gone would. */
type refusingDispatcher struct {
    melodyeventcontract.EventDispatcher
}

func (instance *refusingDispatcher) DispatchName(runtimeInstance melodyruntimecontract.Runtime, eventName string, payload any) (melodyeventcontract.Event, error) {
    return nil, errors.New("redis: connection refused")
}

/* a backend that fails is not a quote the catalogue refused: the sweep stops and hands the failure back as
   itself, with the quote written before its dispatch failed counted as written — where the previous form
   counted every currency refused and told the cron log that every other quote was written. The message says
   what the door did: the quote WAS written and its listeners were not told, where the line under a table
   counting it UPDATED used to read "could not be written" */
func TestRateRefreshServiceRefresh_StopsOnABackendFailureInsteadOfBlamingTheProvider(t *testing.T) {
    refresh, currencyService, runtimeInstance, recordingRepository := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2026-09-08T09:00:00Z","rates":{"EUR":1,"USD":1.0842,"RON":4.9761}}`)
    currencyService.eventDispatcher = &refusingDispatcher{EventDispatcher: currencyService.eventDispatcher}

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil == err {
        t.Fatal("a refused dispatch was swallowed")
    }

    if true == strings.Contains(err.Error(), "the catalogue refused") || true == strings.Contains(err.Error(), "could not be written") || false == strings.Contains(err.Error(), "stopped after EUR: the quote was written, but the listeners that drop its cache entries were not told") {
        t.Fatalf("the failure reads %q, wanted the written quote named as written and the backend named rather than the provider", err.Error())
    }

    if "written" != exception.LogContext(err)["outcome"] {
        t.Fatalf("the failure does not carry the outcome the message names: %v", exception.LogContext(err))
    }

    if false == strings.Contains(err.Error(), "connection refused") && false == strings.Contains(fmt.Sprint(exception.LogContext(err)), "connection refused") {
        t.Fatalf("the failure does not carry its cause: %v", err)
    }

    if 0 != outcome.Refused || 1 != outcome.Updated || 1 != recordingRepository.updates.Load() {
        t.Fatalf("the sweep reported %+v after %d writes, wanted one written, none refused, and the sweep stopped", outcome, recordingRepository.updates.Load())
    }
}

/* a document that names no base is refused as such — the previous line read "(document , catalogue EUR)", a
   hole where the provider's spelling should be */
func TestRateRefreshServiceRefresh_RefusesADocumentThatNamesNoBaseAsSuch(t *testing.T) {
    refresh, _, runtimeInstance, recordingRepository := rateRefreshUnderTest(t, `{"asOf":"2026-09-08T09:00:00Z","rates":{"EUR":1,"USD":1.0842}}`)

    _, err := refresh.Refresh(runtimeInstance)
    if nil == err || false == strings.Contains(err.Error(), "names no base it is quoted against (the catalogue's is EUR)") {
        t.Fatalf("a document without a base was refused as %v, wanted the refusal to say it names none", err)
    }

    if 0 != recordingRepository.updates.Load() {
        t.Fatalf("the refusal came after %d writes, wanted none", recordingRepository.updates.Load())
    }
}

/* refusingCache refuses every delete, the way the cache drop of an unchanged quote meets its backend under a
   redis outage; every other door is the recording cache's. */
type refusingCache struct {
    *ttlRecordingCache
}

func (instance *refusingCache) Delete(key string) error {
    return errors.New("redis: connection refused")
}

/* an unchanged quote is never written: the sweep stops on the cache drop the unchanged branch performs, and the
   message names the drop — the line used to say the quote could not be written, over a quote the door had
   compared and left as it was — and the quote is counted unchanged, which it is */
func TestRateRefreshServiceRefresh_NamesTheCacheDropThatFailedOverAnUnchangedQuote(t *testing.T) {
    refresh, currencyService, runtimeInstance, recordingRepository := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2026-09-08T09:00:00Z","rates":{"EUR":1,"USD":1.0842,"RON":4.9761}}`)

    if _, err := refresh.Refresh(runtimeInstance); nil != err {
        t.Fatalf("the first refresh failed: %v", err)
    }
    updatesAfterFirst := recordingRepository.updates.Load()

    currencyService.cache = &refusingCache{ttlRecordingCache: currencyService.cache.(*ttlRecordingCache)}

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil == err {
        t.Fatal("a refused cache drop was swallowed")
    }

    if true == strings.Contains(err.Error(), "could not be written") || false == strings.Contains(err.Error(), "stopped at EUR: the cache entries of an unchanged quote could not be dropped") {
        t.Fatalf("the failure reads %q, wanted the cache drop of an unchanged quote named", err.Error())
    }

    if "unchanged" != exception.LogContext(err)["outcome"] {
        t.Fatalf("the failure does not carry the outcome the message names: %v", exception.LogContext(err))
    }

    if 1 != outcome.Unchanged || 0 != outcome.Updated || updatesAfterFirst != recordingRepository.updates.Load() {
        t.Fatalf("the sweep reported %+v after %d writes, wanted one unchanged, none written, and the sweep stopped", outcome, recordingRepository.updates.Load()-updatesAfterFirst)
    }
}

/* a refresh whose run is cancelled between two attempts stops there: the wait between attempts slept through
   the cancellation and the next attempt went out anyway, so a SIGTERM landing on a refused reading waited up
   to two backoffs and two more exchanges before the process could leave */
func TestRateRefreshServiceRefresh_StopsRetryingOnceTheRunIsCancelled(t *testing.T) {
    runContext, cancelRun := context.WithCancel(context.Background())
    defer cancelRun()

    provider := newCountingRateProvider(t, func(writer http.ResponseWriter, request *http.Request) {
        cancelRun()
        writer.WriteHeader(http.StatusServiceUnavailable)
    })

    currencyService, _, baseRuntime := currencyServiceUnderTest(t)
    client := provider.client(t)
    melodycontainer.MustRegister(
        baseRuntime.Container().(melodycontainercontract.Registrar),
        ServiceRatesHttpClient,
        func(resolver melodycontainercontract.Resolver) (*httpclient.HttpClient, error) {
            return client, nil
        },
    )

    refresh := NewRateRefreshService(currencyService, &frozenClock{instant: currencyQuoteInstant.Add(24 * time.Hour)}, provider.server.URL+"/v1/", "eur")

    _, err := refresh.Refresh(melodyruntime.New(runContext, baseRuntime.Scope(), baseRuntime.Container()))
    if false == errors.Is(err, context.Canceled) {
        t.Errorf("a cancelled refresh answered %v, wanted the cancellation", err)
    }

    if int64(1) != provider.requests.Load() {
        t.Errorf("a refresh cancelled after its first attempt gave the provider %d exchanges, wanted one", provider.requests.Load())
    }
}
