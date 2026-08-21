package middleware

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/event"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func newRateLimitListenerTestRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logging.NewNopLogger())

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
        NewRateLimitConfig(NewFixedWindowLimiter(1, time.Minute), nil, nil),
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
        t.Fatalf("expected the refusal to be a response rather than a dispatch error, got %v", secondErr)
    }

    response := secondEvent.Response()
    if nil == response || nethttp.StatusTooManyRequests != response.StatusCode() {
        t.Fatalf("expected the exhausted budget to answer 429, got %#v", response)
    }
}

func TestRegisterRateLimitRequestListener_LeavesAnAnsweredRequestAloneAndUncharged(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    extractorCalls := 0
    config := NewRateLimitConfig(
        NewFixedWindowLimiter(1, time.Minute),
        func(request httpcontract.Request) string {
            extractorCalls++

            return "shared"
        },
        nil,
    )

    RegisterRateLimitRequestListener(dispatcher, config)

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
        NewRateLimitConfig(NewFixedWindowLimiter(1, time.Minute), nil, nil),
    )

    authenticatorPaidRounds := 0
    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*http.KernelRequestEvent)
            if false == ok || nil == requestEvent {
                return nil
            }

            /* the real token-resolution listener declines an answered request the same way */
            if nil != requestEvent.Response() {
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

    if nil == secondEvent.Response() || nethttp.StatusTooManyRequests != secondEvent.Response().StatusCode() {
        t.Fatalf("expected the second request refused 429, got %#v", secondEvent.Response())
    }

    if 1 != authenticatorPaidRounds {
        t.Fatalf("expected the refused request to never pay an authenticator round, got %d", authenticatorPaidRounds)
    }
}

func TestRegisterRateLimitRequestListener_RefusesAMissingLimiter(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            RegisterRateLimitRequestListener(
                event.NewEventDispatcher(clock.NewSystemClock()),
                nil,
            )
        },
        "limiter is required for rate limit request listener",
    )
}

/* the listener door classifies the caller's cancellation apart from a store failure, the way its middleware twin does: at error every client that hung up mid-round-trip paged the operator for a healthy store — and this door meters every request, ahead of authentication, so it sees more of those than the middleware ever does. */
func TestRegisterRateLimitRequestListener_ACancelledLimiterCallIsRecordedAtWarning(t *testing.T) {
    capture := &rateLimitCaptureLogger{Logger: logging.NewNopLogger()}

    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, capture)
    runtimeInstance := runtime.New(context.Background(), scope, serviceContainer)

    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterRateLimitRequestListener(
        dispatcher,
        NewRateLimitConfig(&cancellingRuntimeLimiter{}, nil, nil),
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
