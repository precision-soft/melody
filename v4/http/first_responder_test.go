package http

import (
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/config"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

func firstResponderDispatch(t *testing.T, eventName string, payload any, decide func(payload any)) bool {
    t.Helper()

    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})
    dispatcher.AddListener(eventName, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        decide(eventValue.Payload())

        return nil
    }, eventcontract.ListenerConfig{Priority: 20})

    behindRan := false
    dispatcher.AddListener(eventName, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        behindRan = true

        return nil
    }, eventcontract.ListenerConfig{Priority: 10})

    if _, dispatchErr := dispatcher.DispatchName(newTestRuntime(), eventName, payload); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    return behindRan
}

func firstResponderRequest() *KernelRequestEvent {
    return NewKernelRequestEvent(newTestRuntime(), testhelper.NewHttpTestRequestFromHttpRequest(httptest.NewRequest(nethttp.MethodGet, "/x", nil)))
}

func TestKernelRequestEvent_AnAnswerStopsTheDispatch(t *testing.T) {
    if true == firstResponderDispatch(t, kernelcontract.EventKernelRequest, firstResponderRequest(), func(payload any) {
        payload.(*KernelRequestEvent).SetResponse(EmptyResponse(nethttp.StatusNoContent))
    }) {
        t.Fatalf("expected no kernel.request listener behind an answer to run")
    }
}

func TestKernelRequestEvent_ARefusalStopsTheDispatch(t *testing.T) {
    if true == firstResponderDispatch(t, kernelcontract.EventKernelRequest, firstResponderRequest(), func(payload any) {
        payload.(*KernelRequestEvent).Refuse(exception.Forbidden("no"))
    }) {
        t.Fatalf("expected no kernel.request listener behind a refusal to run")
    }
}

func TestKernelRequestEvent_AnUndecidedRequestRunsEveryListener(t *testing.T) {
    if false == firstResponderDispatch(t, kernelcontract.EventKernelRequest, firstResponderRequest(), func(payload any) {}) {
        t.Fatalf("expected the listener behind an undecided request to run")
    }
}

func TestKernelControllerEvent_AnAnswerStopsTheDispatch(t *testing.T) {
    payload := NewKernelControllerEvent(newTestRuntime(), testhelper.NewHttpTestRequestFromHttpRequest(httptest.NewRequest(nethttp.MethodGet, "/x", nil)))
    if true == firstResponderDispatch(t, kernelcontract.EventKernelController, payload, func(payload any) {
        payload.(*KernelControllerEvent).SetResponse(EmptyResponse(nethttp.StatusNoContent))
    }) {
        t.Fatalf("expected no kernel.controller listener behind an answer to run")
    }
}

func TestKernelResponseEvent_AReplacementDoesNotStopTheDispatch(t *testing.T) {
    request := testhelper.NewHttpTestRequestFromHttpRequest(httptest.NewRequest(nethttp.MethodGet, "/x", nil))
    payload := NewKernelResponseEvent(request, EmptyResponse(nethttp.StatusOK))

    if false == firstResponderDispatch(t, kernelcontract.EventKernelResponse, payload, func(payload any) {
        payload.(*KernelResponseEvent).SetResponse(EmptyResponse(nethttp.StatusAccepted))
    }) {
        t.Fatalf("expected the kernel.response chain to run on after a replacement")
    }

    if _, isStopper := any(payload).(eventcontract.PropagationStopper); true == isStopper {
        t.Fatalf("expected the kernel.response payload to be no propagation stopper")
    }
}

func TestKernel_ADecisionAheadOfARequiredRequestListener(t *testing.T) {
    for _, testCase := range []struct {
        name           string
        maySkip        bool
        answers        bool
        expectedStatus int
        devMessage     string
    }{
        {"an answer fails closed", false, true, nethttp.StatusInternalServerError, "event propagation stopped before a required listener ran (stopped by deciding)"},
        {"a refusal fails closed", false, false, nethttp.StatusInternalServerError, "refused: 403 not you; then: event propagation stopped before a required listener ran (stopped by deciding)"},
        {"an answer with the opt-out is served", true, true, nethttp.StatusTeapot, ""},
        {"a refusal with the opt-out is raised", true, false, nethttp.StatusForbidden, ""},
    } {
        for _, environment := range []string{config.EnvironmentDevelopment, config.EnvironmentProduction} {
            serviceContainer := newHttpTestContainerInEnvironment(environment)

            answer, answerBody := responseWithClosingBody(nethttp.StatusTeapot, "decided")

            eventDispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
            eventDispatcher.AddListener(kernelcontract.EventKernelRequest, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
                requestEvent := eventValue.Payload().(*KernelRequestEvent)
                if true == testCase.answers {
                    requestEvent.SetResponse(answer)
                } else {
                    requestEvent.Refuse(exception.Forbidden("not you"))
                }

                return nil
            }, eventcontract.ListenerConfig{Priority: 50, Name: "deciding", Failure: eventcontract.ListenerFailureConfig{MaySkipRequiredListeners: testCase.maySkip}})

            requiredRan := false
            eventDispatcher.AddListener(kernelcontract.EventKernelRequest, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
                requiredRan = true

                return nil
            }, eventcontract.ListenerConfig{Priority: 10, Failure: eventcontract.ListenerFailureConfig{Required: true}})

            handlerRan := false
            router := NewRouter()
            router.Handle("/guarded", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                handlerRan = true

                return EmptyResponse(nethttp.StatusOK), nil
            }, RouteOptions{Methods: []string{nethttp.MethodGet}})

            recorder := httptest.NewRecorder()
            NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/guarded", nil))

            if testCase.expectedStatus != recorder.Code || true == requiredRan || true == handlerRan {
                t.Fatalf("%s %s: expected %d without the required listener or the handler, got %d (required ran %v, handler ran %v)", testCase.name, environment, testCase.expectedStatus, recorder.Code, requiredRan, handlerRan)
            }

            if "" != testCase.devMessage && config.EnvironmentDevelopment == environment && false == strings.Contains(recorder.Body.String(), testCase.devMessage) {
                t.Fatalf("%s: expected the development page to name %q, got %s", testCase.name, testCase.devMessage, recorder.Body.String())
            }

            if true == testCase.answers && false == testCase.maySkip && 1 != answerBody.closed {
                t.Fatalf("%s %s: expected the half-made answer closed once, closed %d", testCase.name, environment, answerBody.closed)
            }
        }
    }
}

func TestEventHttpRequestProfile_IsTheProfilersEventName(t *testing.T) {
    if "http.request.profile" != httpcontract.EventHttpRequestProfile {
        t.Fatalf("expected the profiler's event name, got %q", httpcontract.EventHttpRequestProfile)
    }
}

func TestKernel_AnAnswerAheadOfARequiredControllerListenerFailsClosed(t *testing.T) {
    serviceContainer := newHttpTestContainer()

    eventDispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    eventDispatcher.AddListener(kernelcontract.EventKernelController, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        eventValue.Payload().(*KernelControllerEvent).SetResponse(EmptyResponse(nethttp.StatusTeapot))

        return nil
    }, eventcontract.ListenerConfig{Priority: 50})
    eventDispatcher.AddListener(kernelcontract.EventKernelController, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        return nil
    }, eventcontract.ListenerConfig{Priority: 10, Failure: eventcontract.ListenerFailureConfig{Required: true}})

    router := NewRouter()
    router.Handle("/guarded", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return EmptyResponse(nethttp.StatusOK), nil
    }, RouteOptions{Methods: []string{nethttp.MethodGet}})

    recorder := httptest.NewRecorder()
    NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/guarded", nil))

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the fail-closed 500, got %d", recorder.Code)
    }
}
