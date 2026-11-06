package middleware

import (
    "errors"
    "io"
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/container"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

func newRateLimitListenerTestRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logging.NewNopLogger(), container.OverrideOptions{})

    return runtime.New(context.Background(), scope, serviceContainer)
}

func newRateLimitListenerTestRequestEvent(runtimeInstance runtimecontract.Runtime) *http.KernelRequestEvent {
    request := httptest.NewRequest(nethttp.MethodPost, "/api/item", nil)
    request.RemoteAddr = "203.0.113.7:4711"

    return http.NewKernelRequestEvent(runtimeInstance, testhelper.NewHttpTestRequestFromHttpRequest(request))
}

func TestRegisterRateLimitRequestListener_AnswersTooManyRequestsOnceTheBudgetIsGone(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterRateLimitRequestListener(
        dispatcher,
        NewFixedWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute}),
        RateLimitConfig{},
    )

    runtimeInstance := newRateLimitListenerTestRuntime()

    firstEvent := newRateLimitListenerTestRequestEvent(runtimeInstance)
    _, firstErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, firstEvent)
    if nil != firstErr {
        t.Fatalf("unexpected dispatch error: %v", firstErr)
    }
    if nil != firstEvent.Response() {
        t.Fatalf("expected the request inside the budget to pass untouched, got %#v", firstEvent.Response())
    }

    secondEvent := newRateLimitListenerTestRequestEvent(runtimeInstance)
    _, secondErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, secondEvent)
    if nil != secondErr {
        t.Fatalf("expected the refusal rather than a dispatch error, got %v", secondErr)
    }

    if httpException := exception.AsHttpException(secondEvent.Refusal()); nil == httpException || nethttp.StatusTooManyRequests != httpException.StatusCode() {
        t.Fatalf("expected the exhausted budget to refuse 429, got %v", secondEvent.Refusal())
    }
}

/* OnLimitExceeded is the application's, so a typed nil of a concrete response type is the shape a hand-written "no response" takes; through a bare nil check it reads as a live response, SetResponse normalizes it to nil, and the refused request is served unmetered — the guard must read it through IsNilInterface and serve the 429 fallback */
func TestRegisterRateLimitRequestListener_ATypedNilLimitResponseStillAnswers429(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterRateLimitRequestListener(
        dispatcher,
        NewFixedWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute}),
        RateLimitConfig{
            OnLimitExceeded: func(request httpcontract.Request) (httpcontract.Response, error) {
                return (*http.Response)(nil), nil
            },
        },
    )

    runtimeInstance := newRateLimitListenerTestRuntime()

    firstEvent := newRateLimitListenerTestRequestEvent(runtimeInstance)
    if _, firstErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, firstEvent); nil != firstErr {
        t.Fatalf("unexpected dispatch error: %v", firstErr)
    }

    secondEvent := newRateLimitListenerTestRequestEvent(runtimeInstance)
    if _, secondErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, secondEvent); nil != secondErr {
        t.Fatalf("expected the refusal to be a response rather than a dispatch error, got %v", secondErr)
    }

    response := secondEvent.Response()
    if nil == response || nethttp.StatusTooManyRequests != response.StatusCode() {
        t.Fatalf("expected the typed-nil limit response to fall through to the 429 fallback, got %#v", response)
    }
}

func TestRegisterRateLimitRequestListener_LeavesAnAnsweredRequestAloneAndUncharged(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    extractorCalls := 0
    limiter := NewFixedWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute})
    config := RateLimitConfig{
        KeyExtractor: func(request httpcontract.Request) string {
            extractorCalls++

            return "shared"
        },
    }

    RegisterRateLimitRequestListener(dispatcher, limiter, config)

    runtimeInstance := newRateLimitListenerTestRuntime()

    requestEvent := newRateLimitListenerTestRequestEvent(runtimeInstance)
    alreadySetResponse := http.JsonErrorResponse(nethttp.StatusServiceUnavailable, "maintenance")
    requestEvent.SetResponse(alreadySetResponse)

    _, err := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, requestEvent)
    if nil != err {
        t.Fatalf("unexpected dispatch error: %v", err)
    }

    if alreadySetResponse != requestEvent.Response() {
        t.Fatalf("expected the already-set response to stay, got %#v", requestEvent.Response())
    }

    if 0 != extractorCalls {
        t.Fatalf("expected an already-answered request to consume no budget, got %d extractor calls", extractorCalls)
    }
}

func TestRegisterRateLimitRequestListener_ChargesTheBudgetBeforeTheSecurityChainRuns(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterRateLimitRequestListener(
        dispatcher,
        NewFixedWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute}),
        RateLimitConfig{},
    )

    authenticatorPaidRounds := 0
    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*http.KernelRequestEvent)
            if false == ok || nil == requestEvent {
                return nil
            }

            if nil != requestEvent.Response() || nil != requestEvent.Refusal() {
                return nil
            }

            authenticatorPaidRounds++

            return nil
        },
        50,
    )

    runtimeInstance := newRateLimitListenerTestRuntime()

    firstEvent := newRateLimitListenerTestRequestEvent(runtimeInstance)
    _, firstErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, firstEvent)
    if nil != firstErr {
        t.Fatalf("unexpected dispatch error: %v", firstErr)
    }

    secondEvent := newRateLimitListenerTestRequestEvent(runtimeInstance)
    _, secondErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, secondEvent)
    if nil != secondErr {
        t.Fatalf("unexpected dispatch error: %v", secondErr)
    }

    if httpException := exception.AsHttpException(secondEvent.Refusal()); nil == httpException || nethttp.StatusTooManyRequests != httpException.StatusCode() {
        t.Fatalf("expected the second request refused 429, got %v", secondEvent.Refusal())
    }

    if 1 != authenticatorPaidRounds {
        t.Fatalf("expected the refused request to never pay an authenticator round, got %d", authenticatorPaidRounds)
    }
}

func TestRegisterRateLimitRequestListener_RefusesAMissingLimiter(t *testing.T) {
    for _, limiter := range []httpcontract.RateLimiter{nil, (*FixedWindowLimiter)(nil)} {
        assertPanicsNamingArgument(
            t,
            func() { RegisterRateLimitRequestListener(event.NewEventDispatcher(clock.NewSystemClock()), limiter, RateLimitConfig{}) },
            "limiter is required for rate limit request listener",
            "limiter",
            "RegisterRateLimitRequestListener",
        )
    }
}

/* the listener door classifies the caller's cancellation apart from a store failure, the way its middleware twin does: at error every client that hung up mid-round-trip paged the operator for a healthy store — and this door meters every request, ahead of authentication, so it sees more of those than the middleware ever does. */
func TestRegisterRateLimitRequestListener_ACancelledLimiterCallIsRecordedAtWarning(t *testing.T) {
    capture := &rateLimitCaptureLogger{Logger: logging.NewNopLogger()}

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})
    runtimeInstance := runtime.New(context.Background(), scope, serviceContainer)

    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterRateLimitRequestListener(
        dispatcher,
        &cancellingRuntimeLimiter{},
        RateLimitConfig{},
    )

    requestEvent := newRateLimitListenerTestRequestEvent(runtimeInstance)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, requestEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if 1 != capture.warningCalls || 0 != capture.errorCalls {
        t.Fatalf("expected one warning and no error for the cancelled call, got %d warnings %d errors", capture.warningCalls, capture.errorCalls)
    }
}

/* The request is an application-implementable contract, so a nil pointer of a request type reaches this
door as a non-nil interface and the read below dereferences it. The untyped literal a sibling probe passes
is the only shape a bare comparison already catches. */
func TestRegisterRateLimitRequestListener_ATypedNilRequestIsLeftAlone(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterRateLimitRequestListener(
        dispatcher,
        NewFixedWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute}),
        RateLimitConfig{},
    )

    runtimeInstance := newRateLimitListenerTestRuntime()

    var unassignedRequest *testhelper.HttpTestRequest

    requestEvent := http.NewKernelRequestEvent(runtimeInstance, unassignedRequest)

    _, err := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, requestEvent)
    if nil != err {
        t.Fatalf("unexpected dispatch error: %v", err)
    }

    if nil != requestEvent.Response() {
        t.Fatalf("expected no response for a request the listener cannot read")
    }
}

func TestRegisterRateLimitRequestListener_TheRefusalCarriesTheLimitHandlersError(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    limitErr := exception.TooManyRequests("slow down")

    RegisterRateLimitRequestListener(
        dispatcher,
        NewFixedWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute}),
        RateLimitConfig{
            OnLimitExceeded: func(request httpcontract.Request) (httpcontract.Response, error) {
                return nil, limitErr
            },
        },
    )

    runtimeInstance := newRateLimitListenerTestRuntime()

    firstEvent := newRateLimitListenerTestRequestEvent(runtimeInstance)
    if _, firstErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, firstEvent); nil != firstErr {
        t.Fatalf("unexpected dispatch error: %v", firstErr)
    }

    secondEvent := newRateLimitListenerTestRequestEvent(runtimeInstance)
    if _, secondErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, secondEvent); nil != secondErr {
        t.Fatalf("expected the refusal rather than a dispatch error, got %v", secondErr)
    }

    if false == errors.Is(secondEvent.Refusal(), limitErr) || nil != secondEvent.Response() {
        t.Fatalf("expected the limit handler's error as the refusal, got %v, %#v", secondEvent.Refusal(), secondEvent.Response())
    }
}

func TestRegisterRateLimitRequestListener_ATypedNilLimitErrorFallsBackTo429(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterRateLimitRequestListener(
        dispatcher,
        NewFixedWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute}),
        RateLimitConfig{
            OnLimitExceeded: func(request httpcontract.Request) (httpcontract.Response, error) {
                return nil, (*exception.HttpException)(nil)
            },
        },
    )

    runtimeInstance := newRateLimitListenerTestRuntime()

    firstEvent := newRateLimitListenerTestRequestEvent(runtimeInstance)
    if _, firstErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, firstEvent); nil != firstErr {
        t.Fatalf("unexpected dispatch error: %v", firstErr)
    }

    secondEvent := newRateLimitListenerTestRequestEvent(runtimeInstance)
    if _, secondErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, secondEvent); nil != secondErr {
        t.Fatalf("unexpected dispatch error: %v", secondErr)
    }

    if nil == secondEvent.Response() || nethttp.StatusTooManyRequests != secondEvent.Response().StatusCode() {
        t.Fatalf("expected the 429 fallback, got %#v", secondEvent.Response())
    }
}

func TestRegisterRateLimitRequestListener_StandsAsideOnARefusal(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    limiter := NewFixedWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute})
    RegisterRateLimitRequestListener(dispatcher, limiter, RateLimitConfig{})

    runtimeInstance := newRateLimitListenerTestRuntime()

    for range 2 {
        refusedEvent := newRateLimitListenerTestRequestEvent(runtimeInstance)
        refusedEvent.Refuse(exception.Forbidden("refused earlier"))

        if _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, refusedEvent); nil != dispatchErr {
            t.Fatalf("expected the refused request left alone, got %v", dispatchErr)
        }
    }

    freshEvent := newRateLimitListenerTestRequestEvent(runtimeInstance)
    if _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, freshEvent); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if nil != freshEvent.Refusal() || nil != freshEvent.Response() {
        t.Fatalf("expected the budget untouched by refused requests, got %v, %#v", freshEvent.Refusal(), freshEvent.Response())
    }
}

func TestRegisterRateLimitRequestListener_AnIpv6ClientSharesOneBudgetAcrossItsSlash64(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterRateLimitRequestListener(
        dispatcher,
        NewFixedWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute}),
        RateLimitConfig{},
    )

    runtimeInstance := newRateLimitListenerTestRuntime()

    for _, testCase := range []struct {
        remoteAddress string
        refused       bool
    }{
        {"[2001:db8:1:2::1]:4711", false},
        {"[2001:db8:1:2::ffff]:4711", true},
        {"[2001:db8:1:3::1]:4711", false},
        {"203.0.113.7:4711", false},
        {"[::ffff:203.0.113.7]:4711", true},
        {"203.0.113.8:4711", false},
    } {
        request := httptest.NewRequest(nethttp.MethodPost, "/api/item", nil)
        request.RemoteAddr = testCase.remoteAddress

        requestEvent := http.NewKernelRequestEvent(runtimeInstance, testhelper.NewHttpTestRequestFromHttpRequest(request))
        _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, requestEvent)
        if nil != dispatchErr {
            t.Fatalf("unexpected dispatch error: %v", dispatchErr)
        }

        if testCase.refused != (nil != requestEvent.Refusal()) {
            t.Fatalf("expected %q refused=%v, got %v", testCase.remoteAddress, testCase.refused, requestEvent.Refusal())
        }
    }
}

type closeCountingBody struct {
    closed int
}

func (instance *closeCountingBody) Read(buffer []byte) (int, error) { return 0, io.EOF }

func (instance *closeCountingBody) Close() error {
    instance.closed++

    return nil
}

func TestRegisterRateLimitRequestListener_AResponseBesideALimitErrorIsClosed(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    body := &closeCountingBody{}

    RegisterRateLimitRequestListener(
        dispatcher,
        NewFixedWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute}),
        RateLimitConfig{
            OnLimitExceeded: func(request httpcontract.Request) (httpcontract.Response, error) {
                response := http.EmptyResponse(nethttp.StatusTeapot)
                response.SetBodyReader(body)

                return response, exception.TooManyRequests("slow down")
            },
        },
    )

    runtimeInstance := newRateLimitListenerTestRuntime()

    for range 2 {
        requestEvent := newRateLimitListenerTestRequestEvent(runtimeInstance)
        if _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, requestEvent); nil != dispatchErr {
            t.Fatalf("unexpected dispatch error: %v", dispatchErr)
        }
    }

    if 1 != body.closed {
        t.Fatalf("expected the discarded body closed once, got %d", body.closed)
    }
}
