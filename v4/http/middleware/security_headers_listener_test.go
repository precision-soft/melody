package middleware

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v4/clock"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/http"
    "github.com/precision-soft/melody/v4/http/cors"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

func securityHeadersDispatcher(t *testing.T) eventcontract.EventDispatcher {
    t.Helper()

    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})
    RegisterSecurityHeadersResponseListener(dispatcher, NewSecurityHeaders(SecurityHeadersConfig{}))

    return dispatcher
}

func TestSecurityHeadersResponseListener_DecoratesAListenerProducedResponse(t *testing.T) {
    dispatcher := securityHeadersDispatcher(t)

    refusal := http.EmptyResponse(nethttp.StatusForbidden)
    refusal.SetHeaders(nil)

    if _, dispatchErr := dispatcher.DispatchName(newRateLimitListenerTestRuntime(), kernelcontract.EventKernelResponse, http.NewKernelResponseEvent(securityHeadersRequest("https", nil), refusal)); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if "DENY" != refusal.Headers().Get(http.HeaderFrameOptions) || "max-age=31536000" != refusal.Headers().Get(http.HeaderStrictTransportSecurity) || nethttp.StatusForbidden != refusal.StatusCode() {
        t.Fatalf("expected the refusal decorated and its status kept, got %d %v", refusal.StatusCode(), refusal.Headers())
    }
}

func TestSecurityHeadersResponseListener_RunsAfterCors(t *testing.T) {
    dispatcher := securityHeadersDispatcher(t)

    seenAtCors := ""
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent := eventValue.Payload().(*http.KernelResponseEvent)
            seenAtCors = responseEvent.Response().Headers().Get(http.HeaderFrameOptions)
            responseEvent.Response().Headers().Set(http.HeaderFrameOptions, "SAMEORIGIN")

            return nil
        },
        eventcontract.ListenerConfig{Priority: cors.ResponseListenerPriority},
    )

    response := http.EmptyResponse(nethttp.StatusOK)
    if _, dispatchErr := dispatcher.DispatchName(newRateLimitListenerTestRuntime(), kernelcontract.EventKernelResponse, http.NewKernelResponseEvent(securityHeadersRequest("http", nil), response)); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if "" != seenAtCors || "SAMEORIGIN" != response.Headers().Get(http.HeaderFrameOptions) {
        t.Fatalf("expected the listener to run after the cors priority and keep what ran before it, saw %q, got %v", seenAtCors, response.Headers())
    }
}

func TestSecurityHeadersResponseListener_NilResponse_Ignored(t *testing.T) {
    dispatcher := securityHeadersDispatcher(t)

    var typedNil *http.Response
    if _, dispatchErr := dispatcher.DispatchName(newRateLimitListenerTestRuntime(), kernelcontract.EventKernelResponse, http.NewKernelResponseEvent(securityHeadersRequest("http", nil), typedNil)); nil != dispatchErr {
        t.Fatalf("expected a nil response ignored, got %v", dispatchErr)
    }
}

func TestSecurityHeadersResponseListener_CarriesTheFrameworkTag(t *testing.T) {
    dispatcher := securityHeadersDispatcher(t)

    for _, registeredEvent := range dispatcher.RegisteredEvents() {
        if kernelcontract.EventKernelResponse != registeredEvent.EventName {
            continue
        }

        for _, listener := range registeredEvent.Listeners {
            if SecurityHeadersResponseListenerPriority == listener.Config.Priority && 1 == len(listener.Config.Tags) && kernelcontract.ListenerTagKernelSecurityHeaders == listener.Config.Tags[0] {
                return
            }
        }
    }

    t.Fatal("expected the listener registered on kernel.response at its priority carrying melody.security_headers")
}

func TestRegisterSecurityHeadersResponseListener_NilSecurityHeaders_Refused(t *testing.T) {
    assertPanicsNamingArgument(t, func() {
        RegisterSecurityHeadersResponseListener(event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()}), nil)
    }, "the security headers are nil", "securityHeaders", "RegisterSecurityHeadersResponseListener")
}
