package service

import (
    "fmt"
    "net/http"
    "sync/atomic"
    "testing"
    "time"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/httpclient"
)

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

func TestRateRefreshServiceRefresh_DoesNothingWithNoProviderConfigured(t *testing.T) {
    outcome, err := NewRateRefreshService(nil, "").Refresh(nil)
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

func TestRateRefreshService_RefusesForeignBaseWithoutChangingAnyQuote(t *testing.T) {
    for _, base := range []string{"USD", "", "EUR"} {
        t.Run(base, func(t *testing.T) {
            currencyService, dispatcher, runtimeInstance := currencyServiceUnderTest(t)
            currencyService.clock = &frozenClock{instant: currencyQuoteInstant.Add(time.Hour)}
            provider := newCountingRateProvider(t, func(writer http.ResponseWriter, request *http.Request) {
                writer.Header().Set("Content-Type", "application/json")
                _, _ = fmt.Fprintf(writer, "{\"base\":%q,\"asOf\":\"2026-09-07T09:30:00Z\",\"rates\":{\"EUR\":1,\"USD\":2,\"RON\":3}}", base)
            })
            client := provider.client(t)
            melodycontainer.MustRegister(runtimeInstance.Container(), ServiceRatesHttpClient,
                func(resolver melodycontainercontract.Resolver) (*httpclient.HttpClient, error) {
                    return client, nil
                },
            )
            type quote struct {
                rate float64
                asOf time.Time
            }
            before := map[string]quote{}
            currencies, err := currencyService.currencyRepository.All(runtimeInstance.Context())
            if nil != err {
                t.Fatal(err)
            }
            for _, currency := range currencies {
                before[currency.Id] = quote{currency.Rate, currency.RateAsOf}
            }

            outcome, refreshErr := NewRateRefreshService(currencyService, provider.server.URL).Refresh(runtimeInstance)
            if "EUR" == base {
                if nil != refreshErr || 0 == outcome.Updated {
                    t.Fatalf("valid base must update quotes: outcome=%+v error=%v", outcome, refreshErr)
                }
                return
            }
            if nil == refreshErr || 0 != outcome.Updated {
                t.Errorf("foreign or absent base must be refused before any update: outcome=%+v error=%v", outcome, refreshErr)
            }
            if events := dispatcher.names(); 0 != len(events) {
                t.Errorf("refused document emitted updates: %v", events)
            }
            stored, readErr := currencyService.currencyRepository.All(runtimeInstance.Context())
            if nil != readErr {
                t.Fatal(readErr)
            }
            for _, currency := range stored {
                if previous := before[currency.Id]; previous.rate != currency.Rate || false == previous.asOf.Equal(currency.RateAsOf) {
                    t.Errorf("refused document changed %s", currency.Id)
                }
            }
        })
    }
}

func TestRefreshNormalizesCodesAndContinuesAfterBadQuote(t *testing.T) {
    currencyService, dispatcher, runtimeInstance := currencyServiceUnderTest(t)
    provider := newCountingRateProvider(t, func(writer http.ResponseWriter, request *http.Request) {
        _, _ = fmt.Fprint(writer, `{"base":"EUR","asOf":"2026-09-07T09:00:00Z","rates":{"EUR":0," usd ":2,"ron":3}}`)
    })
    client := provider.client(t)
    melodycontainer.MustRegister(runtimeInstance.Container(), ServiceRatesHttpClient,
        func(resolver melodycontainercontract.Resolver) (*httpclient.HttpClient, error) { return client, nil })
    outcome, err := NewRateRefreshService(currencyService, provider.server.URL).Refresh(runtimeInstance)
    if nil == err || 2 != outcome.Updated || 2 != len(dispatcher.names()) { t.Fatalf("bad quote prevented later valid quotes: outcome=%+v events=%v err=%v", outcome, dispatcher.names(), err) }
    for id, wanted := range map[string]float64{"cur-usd":2, "cur-ron":3} {
        stored, _, _ := currencyService.currencyRepository.FindById(runtimeInstance.Context(), id)
        if wanted != stored.Rate { t.Fatalf("%s quote=%v; want %v", id, stored.Rate, wanted) }
    }
}
