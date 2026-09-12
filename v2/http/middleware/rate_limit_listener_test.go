package middleware

import (
    "context"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/v2/clock"
    "github.com/precision-soft/melody/v2/container"
    "github.com/precision-soft/melody/v2/event"
    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/http"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    "github.com/precision-soft/melody/v2/logging"
    "github.com/precision-soft/melody/v2/runtime"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
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

/* OnLimitExceeded is the application's, so a typed nil of a concrete response type is the shape a hand-written "no response" takes; through a bare nil check it reads as a live response, SetResponse normalizes it to nil, and the refused request is served unmetered — the guard must read it through IsNilInterface and serve the 429 fallback */
func TestRegisterRateLimitRequestListener_ATypedNilLimitResponseStillAnswers429(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterRateLimitRequestListener(
        dispatcher,
        NewRateLimitConfig(
            NewFixedWindowLimiter(1, time.Minute),
            nil,
            func(request httpcontract.Request) (httpcontract.Response, error) {
                return (*http.Response)(nil), nil
            },
        ),
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

/* The request is an application-implementable contract, so a nil pointer of a request type reaches this
door as a non-nil interface and the read below dereferences it. The untyped literal a sibling probe passes
is the only shape a bare comparison already catches. */
func TestRegisterRateLimitRequestListener_ATypedNilRequestIsLeftAlone(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterRateLimitRequestListener(
        dispatcher,
        NewRateLimitConfig(NewFixedWindowLimiter(1, time.Minute), nil, nil),
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

/* the refusal is rendered through kernel.exception rather than returned to the caller, because returning it would abort the kernel.request dispatch onto the fail-closed 500 page and a deliberate 429 would come out a 500. Every other probe here reaches the hardcoded fallback below that dispatch, so the half the comment is written for — an application's exception listener answering the refusal — is the one nothing exercises: with the dispatch removed the fallback answers 429 all the same and every one of them stays green. */
func TestRegisterRateLimitRequestListener_TheRefusalIsRenderedThroughTheExceptionEvent(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    dispatcher.AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            exceptionEvent, ok := eventValue.Payload().(*http.KernelExceptionEvent)
            if false == ok {
                return nil
            }

            exceptionEvent.SetResponse(http.TextResponse(nethttp.StatusTooManyRequests, "rendered by the application"))

            return nil
        },
        0,
    )

    RegisterRateLimitRequestListener(
        dispatcher,
        NewRateLimitConfig(
            NewFixedWindowLimiter(1, time.Minute),
            nil,
            func(request httpcontract.Request) (httpcontract.Response, error) {
                return nil, exception.TooManyRequests("slow down")
            },
        ),
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
    if nil == response {
        t.Fatalf("expected the exhausted budget to be answered")
    }

    body, readErr := io.ReadAll(response.BodyReader())
    if nil != readErr {
        t.Fatalf("read body: %v", readErr)
    }

    /* the fallback below the dispatch answers 429 too, so the status alone cannot say which of the two produced this — the body names the listener */
    if "rendered by the application" != string(body) {
        t.Fatalf("expected the exception listener to render the refusal, got %q", string(body))
    }
}
