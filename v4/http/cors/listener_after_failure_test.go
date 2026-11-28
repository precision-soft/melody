package cors

import (
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/clock"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/http"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

func failingListener(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
    return errors.New("a listener above failed")
}

func TestResponseListener_DecoratesAfterAFailedListenerAndReplacesNothing(t *testing.T) {
    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})
    dispatcher.DeclareEvent(kernelcontract.EventKernelResponse, eventcontract.EventConfig{TagsToRunAfterFailure: []string{kernelcontract.ListenerTagKernelCors}})
    RegisterListeners(dispatcher, NewService(Config{AllowOrigins: []string{AllowOriginAll}}))
    dispatcher.AddListener(kernelcontract.EventKernelResponse, failingListener, eventcontract.ListenerConfig{Priority: 0})

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    request.Header.Set("Origin", "https://example.com")

    response := http.TextResponse(nethttp.StatusServiceUnavailable, "down")
    bodyReader := response.BodyReader()
    payload := http.NewKernelResponseEvent(testhelper.NewHttpTestRequestFromHttpRequest(request), response)

    if _, err := dispatcher.DispatchName(newTestRuntime(), kernelcontract.EventKernelResponse, payload); nil == err {
        t.Fatalf("expected the failure answered")
    }

    if response != payload.Response() || nethttp.StatusServiceUnavailable != response.StatusCode() || bodyReader != response.BodyReader() {
        t.Fatalf("expected the same response, status and body")
    }

    if "" == response.Headers().Get("Access-Control-Allow-Origin") || false == strings.Contains(response.Headers().Get("Vary"), "Origin") {
        t.Fatalf("expected the cross-origin headers added after the failure, got %v", response.Headers())
    }
}

func TestRegisterListeners_TagsTheResponseListenerOnly(t *testing.T) {
    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})
    RegisterListeners(dispatcher, NewService(Config{AllowOrigins: []string{AllowOriginAll}}))

    for _, registeredEvent := range dispatcher.RegisteredEvents() {
        for _, listener := range registeredEvent.Listeners {
            expected := ""
            if kernelcontract.EventKernelResponse == registeredEvent.EventName {
                expected = kernelcontract.ListenerTagKernelCors
            }

            if expected != strings.Join(listener.Config.Tags, ",") || true == listener.Config.Failure.ReplacesResponseAfterFailure {
                t.Fatalf("%s: expected tags %q and no replace door, got %v %v", registeredEvent.EventName, expected, listener.Config.Tags, listener.Config.Failure.ReplacesResponseAfterFailure)
            }
        }
    }
}

func TestRequestListener_DoesNotAnswerAPreflightAfterAFailure(t *testing.T) {
    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})
    dispatcher.DeclareEvent(kernelcontract.EventKernelRequest, eventcontract.EventConfig{TagsToRunAfterFailure: []string{kernelcontract.ListenerTagKernelCors}})
    RegisterRequestListener(dispatcher, NewService(Config{AllowOrigins: []string{AllowOriginAll}}))
    dispatcher.AddListener(kernelcontract.EventKernelRequest, failingListener, eventcontract.ListenerConfig{Priority: RequestListenerPriority + 10})

    requestEvent := http.NewKernelRequestEvent(newTestRuntime(), testhelper.NewHttpTestRequestFromHttpRequest(newPreflightTestRequest("/api/item")))

    _, _ = dispatcher.DispatchName(newTestRuntime(), kernelcontract.EventKernelRequest, requestEvent)

    if nil != requestEvent.Response() {
        t.Fatalf("expected no preflight answer after a failure")
    }
}
