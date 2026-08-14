package http

import (
    nethttp "net/http"
    "testing"
    "time"

    bagcontract "github.com/precision-soft/melody/bag/contract"
    "github.com/precision-soft/melody/clock"
    "github.com/precision-soft/melody/event"
    eventcontract "github.com/precision-soft/melody/event/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func TestKernelHttpProfilerListener_EmitsProfileWhenAttributesAreNil(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    RegisterKernelHttpProfilerListener(dispatcher)

    var capturedProfile *HttpRequestProfile
    dispatcher.AddListener(
        EventHttpRequestProfile,
        func(_ runtimecontract.Runtime, eventValue eventcontract.Event) error {
            capturedProfile, _ = eventValue.Payload().(*HttpRequestProfile)

            return nil
        },
        0,
    )

    request := &nilAttributesRequest{
        requestContext: &staticRequestContext{
            requestId: "request-123",
            startedAt: time.Now(),
        },
    }

    responseEvent := NewKernelResponseEvent(request, nil)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelResponse, responseEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if nil == capturedProfile {
        t.Fatalf("expected a profile to be dispatched when Attributes() is nil but RequestContext() is set")
    }

    if "request-123" != capturedProfile.RequestId() {
        t.Fatalf("expected request id request-123, got %q", capturedProfile.RequestId())
    }
}

type nilAttributesRequest struct {
    requestContext httpcontract.RequestContext
}

func (instance *nilAttributesRequest) HttpRequest() *nethttp.Request { return nil }

func (instance *nilAttributesRequest) Param(name string) (string, bool) { return "", false }

func (instance *nilAttributesRequest) Params() map[string]string { return nil }

func (instance *nilAttributesRequest) Query() bagcontract.ParameterBag { return nil }

func (instance *nilAttributesRequest) Post() bagcontract.ParameterBag { return nil }

func (instance *nilAttributesRequest) Attributes() bagcontract.ParameterBag { return nil }

func (instance *nilAttributesRequest) Header(name string) string { return "" }

func (instance *nilAttributesRequest) RouteName() string { return "" }

func (instance *nilAttributesRequest) RoutePattern() string { return "" }

func (instance *nilAttributesRequest) RuntimeInstance() runtimecontract.Runtime { return nil }

func (instance *nilAttributesRequest) RequestContext() httpcontract.RequestContext {
    return instance.requestContext
}

type staticRequestContext struct {
    requestId string
    startedAt time.Time
}

func (instance *staticRequestContext) RequestId() string { return instance.requestId }

func (instance *staticRequestContext) StartedAt() time.Time { return instance.startedAt }
