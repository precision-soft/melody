package http

import (
    "net/http"
    "testing"
    "time"

    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

/* capturingEventDispatcher records what the registrar hands it instead of dispatching. The listener under test reads nothing off the runtime and nothing off the dispatcher, so driving it directly is both sufficient and the only way to reach it without booting a kernel — and a kernel would supply the very 204 whose absence this pins. */
type capturingEventDispatcher struct {
    eventName string
    listener  eventcontract.EventListener
    priority  int
}

func (instance *capturingEventDispatcher) AddListener(
    eventName string,
    listener eventcontract.EventListener,
    priority int,
) eventcontract.ListenerRegistration {
    instance.eventName = eventName
    instance.listener = listener
    instance.priority = priority

    return eventcontract.ListenerRegistration{EventName: eventName, ListenerId: 1}
}

func (instance *capturingEventDispatcher) RemoveListener(registration eventcontract.ListenerRegistration) bool {
    return false
}

func (instance *capturingEventDispatcher) AddSubscriber(subscriber eventcontract.EventSubscriber) {
}

func (instance *capturingEventDispatcher) RemoveSubscriber(subscriber eventcontract.EventSubscriber) int {
    return 0
}

func (instance *capturingEventDispatcher) Dispatch(
    runtimeInstance runtimecontract.Runtime,
    event eventcontract.Event,
) (eventcontract.Event, error) {
    return event, nil
}

func (instance *capturingEventDispatcher) DispatchName(
    runtimeInstance runtimecontract.Runtime,
    eventName string,
    payload any,
) (eventcontract.Event, error) {
    return nil, nil
}

/* payloadEvent carries a payload and nothing else. The listener reads only Payload(), so building the real event type here would drag the event package into the http tests for no assertion. */
type payloadEvent struct {
    payload any
}

func (instance *payloadEvent) Name() string {
    return kernelcontract.EventKernelResponse
}

func (instance *payloadEvent) Payload() any {
    return instance.payload
}

func (instance *payloadEvent) Timestamp() time.Time {
    return time.Time{}
}

func (instance *payloadEvent) StopPropagation() {
}

func (instance *payloadEvent) IsPropagationStopped() bool {
    return false
}

func captureResponseNormalizerListener(t *testing.T) eventcontract.EventListener {
    t.Helper()

    dispatcher := &capturingEventDispatcher{}
    RegisterKernelResponseNormalizerListener(dispatcher)

    if kernelcontract.EventKernelResponse != dispatcher.eventName {
        t.Fatalf("expected the normalizer to listen on %q, got %q", kernelcontract.EventKernelResponse, dispatcher.eventName)
    }

    if nil == dispatcher.listener {
        t.Fatal("the normalizer registered no listener")
    }

    return dispatcher.listener
}

/* the kernel replaces a handler's nil with an empty 204 before it dispatches this event, and writeResponse answers a nil the same way, so a synthesis here would be a second place deciding what "no response" means. */
func TestKernelResponseNormalizerListener_LeavesANilResponseAlone(t *testing.T) {
    listener := captureResponseNormalizerListener(t)

    responseEvent := NewKernelResponseEvent(nil, nil)

    listenerErr := listener(nil, &payloadEvent{payload: responseEvent})
    if nil != listenerErr {
        t.Fatalf("unexpected listener error: %v", listenerErr)
    }

    if nil != responseEvent.Response() {
        t.Fatalf("expected the normalizer to leave a nil response alone, it synthesized %#v", responseEvent.Response())
    }
}

func TestKernelResponseNormalizerListener_FillsTheStatusCodeAndHeaders(t *testing.T) {
    listener := captureResponseNormalizerListener(t)

    response := NewResponse(0, nil)
    responseEvent := NewKernelResponseEvent(nil, response)

    listenerErr := listener(nil, &payloadEvent{payload: responseEvent})
    if nil != listenerErr {
        t.Fatalf("unexpected listener error: %v", listenerErr)
    }

    if http.StatusOK != responseEvent.Response().StatusCode() {
        t.Fatalf("expected an unset status code to become 200, got %d", responseEvent.Response().StatusCode())
    }

    if nil == responseEvent.Response().Headers() {
        t.Fatal("expected the normalizer to install an empty header map")
    }
}
