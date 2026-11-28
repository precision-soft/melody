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

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/bound"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/httpclient"
)

const rateDocumentBody = `{"base":"EUR","asOf":"2026-09-07T09:00:00Z","rates":{"EUR":1,"USD":1.0842}}`

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

func (instance *countingRateProvider) client(t *testing.T) *httpclient.HttpClient {
    t.Helper()

    client, clientErr := httpclient.NewHttpClient(
        httpclient.HttpClientConfig{
            BaseUrl: instance.server.URL + "/v1/",
            Timeout: bound.Of(2 * time.Second),
        },
    )
    if nil != clientErr {
        t.Fatalf("unexpected error: %v", clientErr)
    }

    t.Cleanup(func() { _ = client.CloseWithContext(context.Background()) })

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

    reading, attempts, err := readRateDocument(httpclient.For(exampleTestRuntime(), provider.client(t)), melodyclock.NewSystemClock())
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

func TestReadRateDocument_SpendsEveryAttemptOnAProviderThatKeepsRefusing(t *testing.T) {
    provider := newCountingRateProvider(t, func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(http.StatusServiceUnavailable)
    })

    _, attempts, err := readRateDocument(httpclient.For(exampleTestRuntime(), provider.client(t)), melodyclock.NewSystemClock())
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

func TestReadRateDocument_AnswersACancellationDuringTheLastExchangeAsTheCause(t *testing.T) {
    lastExchangeStarted := make(chan struct{})
    var exchanges atomic.Int64
    provider := newCountingRateProvider(t, func(writer http.ResponseWriter, request *http.Request) {
        if 3 > exchanges.Add(1) {
            writer.WriteHeader(http.StatusServiceUnavailable)

            return
        }

        close(lastExchangeStarted)
        <-request.Context().Done()
    })

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    go func() {
        <-lastExchangeStarted
        cancel()
    }()

    _, attempts, err := readRateDocument(httpclient.For(exampleTestRuntimeUnder(ctx), provider.client(t)), melodyclock.NewSystemClock())
    if nil == err {
        t.Fatal("a reading whose run was cancelled mid-exchange succeeded")
    }

    var refusal *exception.Error
    if false == errors.As(err, &refusal) || "the rate reading was stopped by the cancellation of its run" != refusal.Message() {
        t.Fatalf("expected the cancellation answered by name, got %v", err)
    }

    if 3 != attempts {
        t.Errorf("expected the cancellation on the third attempt, got %d", attempts)
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

    reading, attempts, err := readRateDocument(httpclient.For(exampleTestRuntime(), provider.client(t)), melodyclock.NewSystemClock())
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

func TestReadRateDocument_DoesNotRepeatARequestTheProviderRefusedAsWrong(t *testing.T) {
    provider := newCountingRateProvider(t, func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(http.StatusNotFound)
    })

    _, attempts, err := readRateDocument(httpclient.For(exampleTestRuntime(), provider.client(t)), melodyclock.NewSystemClock())
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

func TestReadRateDocument_DoesNotRepeatAnAnswerThatIsNotARateDocument(t *testing.T) {
    provider := newCountingRateProvider(t, func(writer http.ResponseWriter, request *http.Request) {
        writer.Header().Set("Content-Type", "application/json")
        _, _ = writer.Write([]byte("not json at all"))
    })

    _, attempts, err := readRateDocument(httpclient.For(exampleTestRuntime(), provider.client(t)), melodyclock.NewSystemClock())
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

func rateRefreshUnderTest(t *testing.T, body string) (*RateRefreshService, *CurrencyService, melodycontainercontract.Runtime, *updateCountingCurrencyRepository) {
    t.Helper()

    return rateRefreshAnsweringInTurn(t, body)
}

func rateRefreshAnsweringInTurn(t *testing.T, bodyList ...string) (*RateRefreshService, *CurrencyService, melodycontainercontract.Runtime, *updateCountingCurrencyRepository) {
    t.Helper()

    answerList := make([]providerAnswer, 0, len(bodyList))
    for _, body := range bodyList {
        answerList = append(answerList, providerAnswer{body: body})
    }

    return rateRefreshAnswering(t, answerList...)
}

type providerAnswer struct {
    body       string
    clockAhead time.Duration
    age        string
    omitDate   bool
}

func rateRefreshAnswering(t *testing.T, answerList ...providerAnswer) (*RateRefreshService, *CurrencyService, melodycontainercontract.Runtime, *updateCountingCurrencyRepository) {
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
        runtimeInstance.serviceContainer,
        ServiceRatesHttpClient,
        func(resolver melodycontainercontract.Resolver) (*httpclient.HttpClient, error) {
            return client, nil
        },
        melodycontainer.RegisterOptions{},
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

    usd, _, _ := currencyService.FindById(exampleTestRuntime(), "cur-usd")
    if 1.0842 != usd.Rate {
        t.Errorf("cur-usd is quoted %v, wanted 1.0842", usd.Rate)
    }
}

func TestRateRefreshServiceRefresh_RefusesADocumentQuotedAgainstAnotherBase(t *testing.T) {
    refresh, currencyService, runtimeInstance, recordingRepository := rateRefreshUnderTest(t, `{"base":"USD","asOf":"2026-09-08T09:00:00Z","rates":{"USD":1,"RON":4.6}}`)

    before, _, _ := currencyService.FindById(exampleTestRuntime(), "cur-ron")

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil == err {
        t.Fatal("a document quoted against USD was accepted by a catalogue quoted against EUR")
    }

    if false == strings.Contains(err.Error(), "another base") {
        t.Errorf("the refusal reads %q, wanted it to name the base", err.Error())
    }

    logContext := exception.LogContext(err, exception.LogContextOptions{})
    if "USD" != logContext["documentBase"] || "EUR" != logContext["catalogueBase"] {
        t.Errorf("the refusal carries %v, wanted both bases named", logContext)
    }

    if 0 != outcome.Updated || 0 != recordingRepository.updates.Load() {
        t.Errorf("a refused document still wrote: %+v, %d updates", outcome, recordingRepository.updates.Load())
    }

    after, _, _ := currencyService.FindById(exampleTestRuntime(), "cur-ron")
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
    refresh, _, runtimeInstance, recordingRepository := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2026-09-08T09:01:00Z","rates":{"USD":1.2}}`)

    if _, err := refresh.Refresh(runtimeInstance); nil == err {
        t.Fatal("a document stamped a minute after the provider answered was accepted")
    }

    if 0 != recordingRepository.updates.Load() {
        t.Errorf("a document from the future wrote %d rows", recordingRepository.updates.Load())
    }
}

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

    usd, _, _ := currencyService.FindById(exampleTestRuntime(), "cur-usd")
    stamped := time.Date(2026, time.September, 8, 9, 4, 0, 0, time.UTC)
    if false == usd.ProviderRateAsOf.Equal(stamped) {
        t.Errorf("the provider's stamp was stored as %s, wanted %s as it came", usd.ProviderRateAsOf, stamped)
    }

    onThisClock := stamped.Add(-4 * time.Minute)
    if drift := usd.RateAsOf.Sub(onThisClock); drift < -time.Second || drift > time.Second {
        t.Errorf("the reading was stored at %s on this clock, wanted %s to within the second the date is read to", usd.RateAsOf, onThisClock)
    }
}

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

    usd, _, _ := currencyService.FindById(exampleTestRuntime(), "cur-usd")
    if 1 != outcome.Updated || 1.3 != usd.Rate {
        t.Errorf("the reading after the correction reported %+v and left cur-usd at %v, wanted it written at 1.3", outcome, usd.Rate)
    }
}

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

    usd, _, _ := currencyService.FindById(exampleTestRuntime(), "cur-usd")
    if 1 != outcome.Stale || 0 != outcome.Updated || 1.2 != usd.Rate {
        t.Errorf("the replay reported %+v and left cur-usd at %v, wanted it stale and 1.2 kept", outcome, usd.Rate)
    }
}

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

    usd, _, _ := currencyService.FindById(exampleTestRuntime(), "cur-usd")
    if 1 != outcome.Stale || 1.2 != usd.Rate || 0 != outcome.ProviderClockOffset {
        t.Errorf("the cached answer reported %+v and left cur-usd at %v, wanted it stale over a clock that agrees", outcome, usd.Rate)
    }
}

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

func TestRateRefreshServiceRefresh_RefusesAReplayThatKeptItsDateAndDroppedItsAge(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshAnswering(
        t,
        providerAnswer{body: `{"base":"EUR","asOf":"2026-09-08T08:10:00Z","rates":{"USD":1.2}}`},
        providerAnswer{body: `{"base":"EUR","asOf":"2026-09-08T07:59:00Z","rates":{"USD":1.3}}`, clockAhead: -time.Hour},
    )

    if _, err := refresh.Refresh(runtimeInstance); nil != err {
        t.Fatalf("the first reading failed: %v", err)
    }

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil == err || false == strings.Contains(err.Error(), "the rate provider's clock is ") || false == strings.Contains(err.Error(), "beyond the 5m0s skew") {
        t.Fatalf("the replay reported %+v, %v; wanted it refused naming the clock", outcome, err)
    }

    refusalContext := exception.LogContext(err, exception.LogContextOptions{})
    if "" == refusalContext["date"] || nil == refusalContext["offset"] {
        t.Errorf("the refusal did not name the Date and the offset it read: %v", refusalContext)
    }

    usd, _, _ := currencyService.FindById(exampleTestRuntime(), "cur-usd")
    if 1.2 != usd.Rate {
        t.Errorf("the replay moved cur-usd to %v, wanted 1.2 kept", usd.Rate)
    }
}

func TestRateRefreshServiceRefresh_BoundsTheProviderClockInBothDirections(t *testing.T) {
    ahead, _, aheadRuntime, aheadRepository := rateRefreshAnswering(t, providerAnswer{
        body:       `{"base":"EUR","asOf":"2026-09-08T09:05:00Z","rates":{"USD":1.2}}`,
        clockAhead: 6 * time.Minute,
    })

    if _, err := ahead.Refresh(aheadRuntime); nil == err || 0 != aheadRepository.updates.Load() {
        t.Fatalf("a provider six minutes ahead was admitted: %v, %d writes", err, aheadRepository.updates.Load())
    }

    behind, currencyService, behindRuntime, _ := rateRefreshAnswering(t, providerAnswer{
        body:       `{"base":"EUR","asOf":"2026-09-08T08:55:00Z","rates":{"USD":1.2}}`,
        clockAhead: -4 * time.Minute,
    })

    outcome, err := behind.Refresh(behindRuntime)
    if nil != err || 1 != outcome.Updated {
        t.Fatalf("a provider four minutes behind reported %+v, %v; wanted it written", outcome, err)
    }

    usd, _, _ := currencyService.FindById(exampleTestRuntime(), "cur-usd")
    if drift := usd.RateAsOf.Sub(time.Date(2026, time.September, 8, 8, 59, 0, 0, time.UTC)); drift < -time.Second || drift > time.Second {
        t.Errorf("the reading was stored at %s on this clock, wanted four minutes after its stamp", usd.RateAsOf)
    }
}

func TestRateRefreshServiceRefresh_ADatelessReadingAheadDoesNotPinTheCatalogue(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshAnswering(
        t,
        providerAnswer{body: `{"base":"EUR","asOf":"2026-09-08T09:04:00Z","rates":{"USD":1.2}}`, omitDate: true},
        providerAnswer{body: `{"base":"EUR","asOf":"2026-09-08T09:00:00Z","rates":{"USD":1.3}}`, omitDate: true},
    )

    if _, err := refresh.Refresh(runtimeInstance); nil != err {
        t.Fatalf("the first reading failed: %v", err)
    }

    held, _, _ := currencyService.currencyRepository.FindById(context.Background(), "cur-usd")
    if false == held.ProviderRateAsOf.Equal(time.Date(2026, time.September, 8, 9, 4, 0, 0, time.UTC)) || false == held.RateAsOf.Equal(time.Date(2026, time.September, 8, 9, 0, 0, 0, time.UTC)) {
        t.Errorf("the reading was stored at %s on this clock and %s as stamped, wanted the arrival and the stamp as it came", held.RateAsOf, held.ProviderRateAsOf)
    }

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil != err {
        t.Fatalf("the honest reading failed: %v", err)
    }

    usd, _, _ := currencyService.FindById(exampleTestRuntime(), "cur-usd")
    if 1 != outcome.Updated || 1.3 != usd.Rate {
        t.Errorf("the honest reading reported %+v and left cur-usd at %v, wanted it written at 1.3", outcome, usd.Rate)
    }
}

func TestRateRefreshServiceRefresh_AdmitsAReplayThatKeptItsDateInsideTheSkew(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshAnswering(
        t,
        providerAnswer{body: `{"base":"EUR","asOf":"2026-09-08T08:58:00Z","rates":{"USD":1.2}}`},
        providerAnswer{body: `{"base":"EUR","asOf":"2026-09-08T08:56:00Z","rates":{"USD":1.3}}`, clockAhead: -4 * time.Minute},
    )

    if _, err := refresh.Refresh(runtimeInstance); nil != err {
        t.Fatalf("the first reading failed: %v", err)
    }

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil != err || 1 != outcome.Updated {
        t.Fatalf("the replay inside the skew reported %+v, %v; the bound admits it", outcome, err)
    }

    held, _, _ := currencyService.currencyRepository.FindById(context.Background(), "cur-usd")
    if 1.3 != held.Rate || false == held.ProviderRateAsOf.Equal(time.Date(2026, time.September, 8, 8, 56, 0, 0, time.UTC)) {
        t.Errorf("the replay left cur-usd at %v stamped %s, wanted 1.3 under its own stamp", held.Rate, held.ProviderRateAsOf)
    }
}

func TestRateRefreshServiceRefresh_AdmitsADatelessReplayFromTheLeadWindow(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshAnswering(
        t,
        providerAnswer{body: `{"base":"EUR","asOf":"2026-09-08T09:04:00Z","rates":{"USD":1.2}}`, omitDate: true},
        providerAnswer{body: `{"base":"EUR","asOf":"2026-09-08T09:03:00Z","rates":{"USD":1.3}}`, omitDate: true},
    )

    if _, err := refresh.Refresh(runtimeInstance); nil != err {
        t.Fatalf("the first reading failed: %v", err)
    }

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil != err || 1 != outcome.Updated {
        t.Fatalf("the dateless replay from the lead window reported %+v, %v; the arrival order admits it", outcome, err)
    }

    held, _, _ := currencyService.currencyRepository.FindById(context.Background(), "cur-usd")
    if 1.3 != held.Rate || false == held.ProviderRateAsOf.Equal(time.Date(2026, time.September, 8, 9, 3, 0, 0, time.UTC)) {
        t.Errorf("the replay left cur-usd at %v stamped %s, wanted 1.3 under its own stamp", held.Rate, held.ProviderRateAsOf)
    }
}

func TestRateRefreshServiceRefresh_CountsAReplayedDocumentAsStale(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2025-12-01T09:00:00Z","rates":{"EUR":1,"USD":9.9,"RON":9.9}}`)

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil != err {
        t.Fatalf("a replayed document failed instead of being kept out: %v", err)
    }

    if 3 != outcome.Stale || 0 != outcome.Updated {
        t.Errorf("a replayed document reported %+v, wanted three stale", outcome)
    }

    usd, _, _ := currencyService.FindById(exampleTestRuntime(), "cur-usd")
    if 1.1 != usd.Rate {
        t.Errorf("a replayed document moved cur-usd to %v", usd.Rate)
    }
}

func TestRateRefreshServiceRefresh_MatchesTheDocumentsCodesFolded(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshUnderTest(t, `{"base":"eur","asOf":"2026-09-08T09:00:00Z","rates":{"usd":1.2," ron ":4.7}}`)

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil != err {
        t.Fatalf("the refresh failed: %v", err)
    }

    if 2 != outcome.Updated || 1 != outcome.Skipped {
        t.Errorf("the refresh reported %+v, wanted two updated (usd, ron) and one skipped (eur)", outcome)
    }

    usd, _, _ := currencyService.FindById(exampleTestRuntime(), "cur-usd")
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

func TestRateRefreshServiceRefresh_WritesTheGoodQuotesPastARefusedOne(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2026-09-08T09:00:00Z","rates":{"EUR":1,"RON":0,"USD":1.2}}`)

    outcome, err := refresh.Refresh(runtimeInstance)
    if nil == err {
        t.Fatal("a document with a zero quote reported no failure")
    }

    if 2 != outcome.Updated || 1 != outcome.Refused {
        t.Errorf("the refresh reported %+v, wanted two updated and one refused", outcome)
    }

    refusedList, _ := exception.LogContext(err, exception.LogContextOptions{})["refusedCurrencyList"].([]string)
    if 1 != len(refusedList) || "RON" != refusedList[0] {
        t.Errorf("the refusal names %v, wanted RON", refusedList)
    }

    if false == strings.Contains(err.Error(), "(RON: ") || false == strings.Contains(err.Error(), "positive, finite number") {
        t.Errorf("the message reads %q, wanted the refused currency with its reason", err.Error())
    }

    if false == errors.Is(err, ErrUnusableRate) {
        t.Errorf("the refusal does not carry the rate sentinel as its cause: %v", err)
    }

    if false == outcome.AsOf.Equal(time.Date(2026, time.September, 8, 9, 0, 0, 0, time.UTC)) {
        t.Errorf("the outcome carries %v as the document's instant, wanted the document's", outcome.AsOf)
    }

    usd, _, _ := currencyService.FindById(exampleTestRuntime(), "cur-usd")
    if 1.2 != usd.Rate {
        t.Errorf("the quote after the refused one was not written: cur-usd %v", usd.Rate)
    }
}

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

    usd, _, _ := currencyService.FindById(exampleTestRuntime(), "cur-usd")
    if 1.1 != usd.Rate {
        t.Errorf("cur-usd moved to %v on a quote that is not a price", usd.Rate)
    }
}

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

/* time.Now() serialises nine decimals and the column holds six */
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

    usd, _, _ := currencyService.FindById(exampleTestRuntime(), "cur-usd")
    if false == usd.RateAsOf.Equal(time.Date(2026, time.September, 8, 8, 59, 59, 123456000, time.UTC)) {
        t.Fatalf("the stored instant is %v, wanted the document's truncated to the microsecond", usd.RateAsOf)
    }
}

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

    if "written" != exception.LogContext(err, exception.LogContextOptions{})["outcome"] {
        t.Fatalf("the failure does not carry the outcome the message names: %v", exception.LogContext(err, exception.LogContextOptions{}))
    }

    if false == strings.Contains(err.Error(), "connection refused") && false == strings.Contains(fmt.Sprint(exception.LogContext(err, exception.LogContextOptions{})), "connection refused") {
        t.Fatalf("the failure does not carry its cause: %v", err)
    }

    if 0 != outcome.Refused || 1 != outcome.Updated || 1 != recordingRepository.updates.Load() {
        t.Fatalf("the sweep reported %+v after %d writes, wanted one written, none refused, and the sweep stopped", outcome, recordingRepository.updates.Load())
    }
}

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

type refusingCache struct {
    *ttlRecordingCache
}

func (instance *refusingCache) Delete(runtimeInstance melodycontainercontract.Runtime, key string) error {
    return errors.New("redis: connection refused")
}

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

    if "unchanged" != exception.LogContext(err, exception.LogContextOptions{})["outcome"] {
        t.Fatalf("the failure does not carry the outcome the message names: %v", exception.LogContext(err, exception.LogContextOptions{}))
    }

    if 1 != outcome.Unchanged || 0 != outcome.Updated || updatesAfterFirst != recordingRepository.updates.Load() {
        t.Fatalf("the sweep reported %+v after %d writes, wanted one unchanged, none written, and the sweep stopped", outcome, recordingRepository.updates.Load()-updatesAfterFirst)
    }
}

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
        baseRuntime.serviceContainer,
        ServiceRatesHttpClient,
        func(resolver melodycontainercontract.Resolver) (*httpclient.HttpClient, error) {
            return client, nil
        },
        melodycontainer.RegisterOptions{},
    )

    refresh := NewRateRefreshService(currencyService, &frozenClock{instant: currencyQuoteInstant.Add(24 * time.Hour)}, provider.server.URL+"/v1/", "eur")

    _, err := refresh.Refresh(melodyruntime.WithContext(baseRuntime, runContext))
    if false == errors.Is(err, context.Canceled) {
        t.Errorf("a cancelled refresh answered %v, wanted the cancellation", err)
    }

    if int64(1) != provider.requests.Load() {
        t.Errorf("a refresh cancelled after its first attempt gave the provider %d exchanges, wanted one", provider.requests.Load())
    }
}

type cancelingCurrencyRepository struct {
    repository.CurrencyRepository
    cancel context.CancelFunc
}

func (instance *cancelingCurrencyRepository) All(ctx context.Context) ([]*entity.Currency, error) {
    instance.cancel()

    return nil, waitForTheCallerContext(ctx)
}

func TestRateRefreshServiceRefresh_ListsTheCatalogueThroughTheRunsContext(t *testing.T) {
    refresh, currencyService, runtimeInstance, _ := rateRefreshUnderTest(t, `{"base":"EUR","asOf":"2026-09-08T09:00:00Z","rates":{"EUR":1,"USD":1.0842,"RON":4.9761}}`)

    ctx, cancel := context.WithCancel(context.Background())
    t.Cleanup(cancel)
    currencyService.currencyRepository = &cancelingCurrencyRepository{CurrencyRepository: currencyService.currencyRepository, cancel: cancel}

    _, err := refresh.Refresh(melodyruntime.WithContext(runtimeInstance, ctx))
    if false == errors.Is(err, context.Canceled) {
        t.Fatalf("expected the interrupted run to end on its context, got %v", err)
    }
}
