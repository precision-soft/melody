package http

import (
    "errors"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "reflect"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/eventseam"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

type afterFailureRecordingLogger struct {
    errorContextRecordingLogger

    warningMutex    sync.Mutex
    warningMessages []string
    warningContexts []loggingcontract.Context
    infoMessages    []string
}

func (instance *afterFailureRecordingLogger) Warning(message string, context loggingcontract.Context) {
    instance.warningMutex.Lock()
    defer instance.warningMutex.Unlock()

    instance.warningMessages = append(instance.warningMessages, message)
    instance.warningContexts = append(instance.warningContexts, context)
}

func (instance *afterFailureRecordingLogger) Info(message string, context loggingcontract.Context) {
    instance.warningMutex.Lock()
    defer instance.warningMutex.Unlock()

    instance.infoMessages = append(instance.infoMessages, message)
}

func (instance *afterFailureRecordingLogger) warnings() ([]string, []loggingcontract.Context) {
    instance.warningMutex.Lock()
    defer instance.warningMutex.Unlock()

    return append([]string(nil), instance.warningMessages...), append([]loggingcontract.Context(nil), instance.warningContexts...)
}

type closeRecordingBody struct {
    io.Reader
    closed int
}

func (instance *closeRecordingBody) Close() error {
    instance.closed = instance.closed + 1

    return nil
}

func responseWithClosingBody(status int, text string) (httpcontract.Response, *closeRecordingBody) {
    body := &closeRecordingBody{Reader: strings.NewReader(text)}
    response := TextResponse(status, text)
    response.SetBodyReader(body)

    return response, body
}

func TestKernelResponseEvent_AReplacementAfterAFailureIsRefusedWithoutTheDoor(t *testing.T) {
    original := TextResponse(nethttp.StatusOK, "original")
    other := TextResponse(nethttp.StatusTeapot, "other")
    payload := NewKernelResponseEvent(nil, original)

    leave := payload.EnterAfterFailureListener(eventseam.NewToken(), eventseam.AfterFailureListener{EventName: kernelcontract.EventKernelResponse, ListenerName: "late"})
    payload.SetResponse(other)
    leave()

    if original != payload.Response() {
        t.Fatalf("expected the standing response kept")
    }

    replacements := payload.takeAfterFailureWrites()
    if 1 != len(replacements) || true == replacements[0].admitted || other != replacements[0].written || original != replacements[0].standing || afterFailureWriteResponse != replacements[0].write {
        t.Fatalf("expected one refused replacement recorded, got %+v", replacements)
    }

    third := TextResponse(nethttp.StatusAccepted, "third")
    payload.SetResponse(third)
    if third != payload.Response() || 0 != len(payload.takeAfterFailureWrites()) {
        t.Fatalf("expected a replacement outside the pass taken without a record")
    }
}

func TestKernelResponseEvent_AReplacementAfterAFailureIsServedThroughTheDoor(t *testing.T) {
    original := TextResponse(nethttp.StatusOK, "original")
    other := TextResponse(nethttp.StatusTeapot, "other")
    payload := NewKernelResponseEvent(nil, original)

    leave := payload.EnterAfterFailureListener(eventseam.NewToken(), eventseam.AfterFailureListener{ListenerName: "late", ReplacesResponse: true})
    payload.SetResponse(other)
    leave()

    replacements := payload.takeAfterFailureWrites()
    if other != payload.Response() || 1 != len(replacements) || false == replacements[0].admitted || original != replacements[0].standing || other != replacements[0].written {
        t.Fatalf("expected the replacement served and recorded, got %+v", replacements)
    }
}

func TestKernelResponseEvent_SettingTheStandingResponseAfterAFailureIsNoReplacement(t *testing.T) {
    original := TextResponse(nethttp.StatusOK, "original")
    payload := NewKernelResponseEvent(nil, original)

    leave := payload.EnterAfterFailureListener(eventseam.NewToken(), eventseam.AfterFailureListener{ListenerName: "late"})
    payload.SetResponse(original)
    leave()

    if 0 != len(payload.takeAfterFailureWrites()) {
        t.Fatalf("expected no record for the standing response set again")
    }

    leaveOpen := payload.EnterAfterFailureListener(eventseam.NewToken(), eventseam.AfterFailureListener{ListenerName: "open", ReplacesResponse: true})
    leaveClosed := payload.EnterAfterFailureListener(eventseam.NewToken(), eventseam.AfterFailureListener{ListenerName: "closed"})
    leaveClosed()

    other := TextResponse(nethttp.StatusTeapot, "other")
    payload.SetResponse(other)
    leaveOpen()

    replacements := payload.takeAfterFailureWrites()
    if other != payload.Response() || 1 != len(replacements) || "open" != replacements[0].listener.ListenerName {
        t.Fatalf("expected the outer listener's door to judge after the inner one left, got %+v", replacements)
    }
}

func newReplacementKernelFixture(t *testing.T, door bool) (*httptest.ResponseRecorder, *afterFailureRecordingLogger, *closeRecordingBody, *closeRecordingBody) {
    t.Helper()

    handlerResponse, handlerBody := responseWithClosingBody(nethttp.StatusOK, "handler")
    replacement, replacementBody := responseWithClosingBody(nethttp.StatusTeapot, "replaced")

    router := NewRouter()
    router.Handle("/replaced", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return handlerResponse, nil
    }, RouteOptions{Methods: []string{nethttp.MethodGet}})

    recordingLogger := &afterFailureRecordingLogger{}
    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    dispatcher.DeclareEvent(kernelcontract.EventKernelResponse, eventcontract.EventConfig{TagsToRunAfterFailure: []string{"cleanup"}})
    dispatcher.AddListener(kernelcontract.EventKernelResponse, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        return errors.New("the response listener failed")
    }, eventcontract.ListenerConfig{Priority: 10})
    dispatcher.AddListener(kernelcontract.EventKernelResponse, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        eventValue.Payload().(*KernelResponseEvent).SetResponse(replacement)

        return nil
    }, eventcontract.ListenerConfig{Priority: 0, Name: "replacing", Tags: []string{"cleanup"}, Failure: eventcontract.ListenerFailureConfig{ReplacesResponseAfterFailure: door}})

    recorder := httptest.NewRecorder()
    NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/replaced", nil))

    return recorder, recordingLogger, handlerBody, replacementBody
}

func TestKernel_AListenerReplacingTheResponseAfterAFailureIsRefusedAndWarned(t *testing.T) {
    recorder, recordingLogger, _, replacementBody := newReplacementKernelFixture(t, false)

    if nethttp.StatusOK != recorder.Code || "handler" != recorder.Body.String() {
        t.Fatalf("expected the handler's response served, got %d %q", recorder.Code, recorder.Body.String())
    }

    messages, contexts := recordingLogger.warnings()
    if 1 != len(messages) || "a listener's response replacement after a failure is refused" != messages[0] {
        t.Fatalf("expected one refusal warning, got %v", messages)
    }

    context := contexts[0]
    if kernelcontract.EventKernelResponse != context["eventName"] || "replacing" != context["listenerName"] || nethttp.StatusOK != context["servedResponseStatus"] || nethttp.StatusTeapot != context["refusedResponseStatus"] || "ListenerConfig.Failure.ReplacesResponseAfterFailure" != context["door"] || afterFailureRefusedByDoor != context["refusedBy"] || afterFailureWriteResponse != context["write"] {
        t.Fatalf("expected the warning to name the listener, the statuses and the door, got %v", context)
    }

    if 1 != replacementBody.closed {
        t.Fatalf("expected the refused response closed once, got %d", replacementBody.closed)
    }
}

func TestKernel_AListenerReplacingTheResponseAfterAFailureThroughTheDoorIsServedAndWarned(t *testing.T) {
    recorder, recordingLogger, handlerBody, _ := newReplacementKernelFixture(t, true)

    if nethttp.StatusTeapot != recorder.Code || "replaced" != recorder.Body.String() {
        t.Fatalf("expected the replacement served, got %d %q", recorder.Code, recorder.Body.String())
    }

    messages, contexts := recordingLogger.warnings()
    if 1 != len(messages) || "a listener replaced the response after a failure" != messages[0] || nethttp.StatusTeapot != contexts[0]["servedResponseStatus"] || nethttp.StatusOK != contexts[0]["replacedResponseStatus"] {
        t.Fatalf("expected one replacement warning with the statuses, got %v %v", messages, contexts)
    }

    if 1 != handlerBody.closed {
        t.Fatalf("expected the replaced response closed once, got %d", handlerBody.closed)
    }
}

func TestKernel_AReplacementInACleanDispatchWritesNoWarning(t *testing.T) {
    for _, door := range []bool{true, false} {
        router := NewRouter()
        router.Handle("/clean", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "handler"), nil
        }, RouteOptions{Methods: []string{nethttp.MethodGet}})

        recordingLogger := &afterFailureRecordingLogger{}
        serviceContainer := newHttpTestContainer()
        serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

        dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
        dispatcher.DeclareEvent(kernelcontract.EventKernelResponse, eventcontract.EventConfig{TagsToRunAfterFailure: []string{"cleanup"}})
        dispatcher.AddListener(kernelcontract.EventKernelResponse, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.Payload().(*KernelResponseEvent).SetResponse(TextResponse(nethttp.StatusTeapot, "replaced"))

            return nil
        }, eventcontract.ListenerConfig{Tags: []string{"cleanup"}, Failure: eventcontract.ListenerFailureConfig{ReplacesResponseAfterFailure: door}})

        recorder := httptest.NewRecorder()
        NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/clean", nil))

        messages, _ := recordingLogger.warnings()
        if nethttp.StatusTeapot != recorder.Code || 0 != len(messages) {
            t.Fatalf("door %v: expected the replacement served without a warning, got %d %v", door, recorder.Code, messages)
        }
    }
}

func TestKernel_TheAccessLogStillRunsAfterAFailedTerminateListener(t *testing.T) {
    for _, declared := range []bool{true, false} {
        router := NewRouter()
        router.Handle("/logged", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "handler"), nil
        }, RouteOptions{Methods: []string{nethttp.MethodGet}})

        recordingLogger := &afterFailureRecordingLogger{}
        serviceContainer := newHttpTestContainer()
        serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

        dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
        if true == declared {
            dispatcher.DeclareEvent(kernelcontract.EventKernelTerminate, eventcontract.EventConfig{TagsToRunAfterFailure: []string{kernelcontract.ListenerTagKernelAccessLog}})
        }
        dispatcher.AddListener(kernelcontract.EventKernelTerminate, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return errors.New("the terminate listener failed")
        }, eventcontract.ListenerConfig{Priority: 0})
        RegisterKernelTerminateAccessLogListener(dispatcher, AccessLogConfig{})

        recorder := httptest.NewRecorder()
        NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/logged", nil))

        recordingLogger.warningMutex.Lock()
        accessLogged := 0
        for _, message := range recordingLogger.infoMessages {
            if "request completed" == message {
                accessLogged = accessLogged + 1
            }
        }
        recordingLogger.warningMutex.Unlock()

        expected := 0
        if true == declared {
            expected = 1
        }

        if expected != accessLogged {
            t.Fatalf("declared %v: expected %d access log records, got %d (%v)", declared, expected, accessLogged, recordingLogger.infoMessages)
        }

        if _, journaled := recordingLogger.errorContextFor("kernel terminate error"); false == journaled {
            t.Fatalf("declared %v: expected the terminate failure journaled, got %v", declared, recordingLogger.errorMessages)
        }
    }
}

func TestKernel_ARefusingListenerThatFailsAheadOfARequiredListenerNamesBothCausesInDevelopment(t *testing.T) {
    for _, environment := range []string{config.EnvironmentDevelopment, config.EnvironmentProduction} {
        serviceContainer := newHttpTestContainerInEnvironment(environment)

        eventDispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
        eventDispatcher.AddListener(kernelcontract.EventKernelRequest, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.Payload().(*KernelRequestEvent).Refuse(exception.Forbidden("not you"))

            return errors.New("the refusing listener failed")
        }, eventcontract.ListenerConfig{Priority: 50, Name: "failing", Failure: eventcontract.ListenerFailureConfig{MaySkipRequiredListeners: true}})
        eventDispatcher.AddListener(kernelcontract.EventKernelRequest, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        }, eventcontract.ListenerConfig{Priority: 10, Failure: eventcontract.ListenerFailureConfig{Required: true}})

        recorder := httptest.NewRecorder()
        NewKernel(NewRouter(), KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/guarded", nil))

        if nethttp.StatusInternalServerError != recorder.Code {
            t.Fatalf("%s: expected the fail-closed 500, got %d", environment, recorder.Code)
        }

        body := recorder.Body.String()
        if config.EnvironmentDevelopment == environment {
            if false == strings.Contains(body, "refused: 403 not you; then: event propagation stopped before a required listener ran (stopped by failing): event listener returned error: the refusing listener failed") {
                t.Fatalf("expected both causes named in development, got %s", body)
            }

            continue
        }

        if false == strings.Contains(body, "internal server error") || true == strings.Contains(body, "not you") || true == strings.Contains(body, "failing") {
            t.Fatalf("expected the production message alone, got %s", body)
        }
    }
}

type unavailableRefusal struct{}

func (instance unavailableRefusal) Error() string {
    return "the store is down"
}

func dispatchSkippingARequiredListener(t *testing.T, listenerName string, listenerErr error) error {
    t.Helper()

    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})

    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            if nil == listenerErr {
                eventValue.StopPropagation()
            }

            return listenerErr
        },
        eventcontract.ListenerConfig{Priority: 100, Name: listenerName},
    )
    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        eventcontract.ListenerConfig{Failure: eventcontract.ListenerFailureConfig{Required: true}},
    )

    _, dispatchErr := dispatcher.Dispatch(newTestRuntime(), event.NewEvent(kernelcontract.EventKernelRequest, nil, time.Now()))

    var skippedErr *event.RequiredListenerSkippedError
    if false == errors.As(dispatchErr, &skippedErr) {
        t.Fatalf("expected the dispatch to refuse with a skipped required listener, got %T (%v)", dispatchErr, dispatchErr)
    }

    return skippedErr
}

func TestDebugKernelRequestFailureMessage(t *testing.T) {
    plainErr := errors.New("plain failure")
    skippedWithCause := dispatchSkippingARequiredListener(t, "failing", errors.New("the cause"))
    skippedByStop := dispatchSkippingARequiredListener(t, "stopping", nil)

    for _, testCase := range []struct {
        name     string
        refusal  error
        err      error
        expected string
    }{
        {"no refusal, a plain failure", nil, plainErr, "plain failure"},
        {"no refusal, a skipped listener with a cause", nil, skippedWithCause, "event propagation stopped before a required listener ran (stopped by failing): event listener returned error: the cause"},
        {"no refusal, a skipped listener by a stop", nil, skippedByStop, "event propagation stopped before a required listener ran (stopped by stopping)"},
        {"a plain refusal", errors.New("plain refusal"), plainErr, "refused: plain refusal; then: plain failure"},
        {"an unavailable refusal", exception.MarkInfrastructureFailure(unavailableRefusal{}), plainErr, "refused: 503 service unavailable; then: plain failure"},
        {"a 429 refusal", exception.NewHttpError(nethttp.StatusTooManyRequests, exception.HttpErrorConfig{Message: "slow down"}), plainErr, "refused: 429 slow down; then: plain failure"},
    } {
        if message := debugKernelRequestFailureMessage(testCase.refusal, testCase.err); testCase.expected != message {
            t.Fatalf("%s: expected %q, got %q", testCase.name, testCase.expected, message)
        }
    }
}

func TestKernelResponseEvent_AZeroTokenEntersNothing(t *testing.T) {
    original := TextResponse(nethttp.StatusOK, "original")
    payload := NewKernelResponseEvent(nil, original)

    leave := payload.EnterAfterFailureListener(eventseam.Token{}, eventseam.AfterFailureListener{ListenerName: "forged", ReplacesResponse: true})
    defer leave()

    payload.SetResponse(TextResponse(nethttp.StatusTeapot, "forged"))
    if nethttp.StatusTeapot != payload.Response().StatusCode() || 0 != len(payload.takeAfterFailureWrites()) {
        t.Fatalf("expected a zero token to change nothing")
    }

    tokenType := reflect.TypeOf(eventseam.Token{})
    for index := 0; index < tokenType.NumField(); index++ {
        if true == tokenType.Field(index).IsExported() {
            t.Fatalf("expected the token to carry unexported fields alone, so no type outside v4 is assignable to it")
        }
    }

    if 0 == tokenType.NumField() {
        t.Fatalf("expected the token to carry an unexported field")
    }
}

func TestKernel_AResponseRefusedTwiceIsClosedOnceAndWarnedTwice(t *testing.T) {
    replacement, replacementBody := responseWithClosingBody(nethttp.StatusTeapot, "replaced")

    router := NewRouter()
    router.Handle("/twice", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return TextResponse(nethttp.StatusOK, "handler"), nil
    }, RouteOptions{Methods: []string{nethttp.MethodGet}})

    recordingLogger := &afterFailureRecordingLogger{}
    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    dispatcher.DeclareEvent(kernelcontract.EventKernelResponse, eventcontract.EventConfig{TagsToRunAfterFailure: []string{"cleanup"}})
    dispatcher.AddListener(kernelcontract.EventKernelResponse, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        return errors.New("the response listener failed")
    }, eventcontract.ListenerConfig{Priority: 10})
    dispatcher.AddListener(kernelcontract.EventKernelResponse, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        eventValue.Payload().(*KernelResponseEvent).SetResponse(replacement)
        eventValue.Payload().(*KernelResponseEvent).SetResponse(replacement)

        return nil
    }, eventcontract.ListenerConfig{Tags: []string{"cleanup"}})

    recorder := httptest.NewRecorder()
    NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/twice", nil))

    messages, _ := recordingLogger.warnings()
    if nethttp.StatusOK != recorder.Code || 2 != len(messages) || 1 != replacementBody.closed {
        t.Fatalf("expected 200, two warnings and one close, got %d %v %d", recorder.Code, messages, replacementBody.closed)
    }
}

func TestKernel_AnExitAfterARefusedReplacementStillWarnsAndClosesIt(t *testing.T) {
    replacement, replacementBody := responseWithClosingBody(nethttp.StatusTeapot, "replaced")

    router := NewRouter()
    router.Handle("/exiting", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return TextResponse(nethttp.StatusOK, "handler"), nil
    }, RouteOptions{Methods: []string{nethttp.MethodGet}})

    recordingLogger := &afterFailureRecordingLogger{}
    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    dispatcher.DeclareEvent(kernelcontract.EventKernelResponse, eventcontract.EventConfig{TagsToRunAfterFailure: []string{"cleanup"}})
    dispatcher.AddListener(kernelcontract.EventKernelResponse, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        return errors.New("the response listener failed")
    }, eventcontract.ListenerConfig{Priority: 10})
    replaced := false
    dispatcher.AddListener(kernelcontract.EventKernelResponse, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        if false == replaced {
            replaced = true
            eventValue.Payload().(*KernelResponseEvent).SetResponse(replacement)
        }

        return nil
    }, eventcontract.ListenerConfig{Priority: 5, Tags: []string{"cleanup"}})
    exited := false
    dispatcher.AddListener(kernelcontract.EventKernelResponse, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        if false == exited {
            exited = true
            exception.Exit(exception.NewExitError(3, exception.NewError("the process is done", nil, nil)))
        }

        return nil
    }, eventcontract.ListenerConfig{Priority: 0, Tags: []string{"cleanup"}})

    recorder := httptest.NewRecorder()
    func() {
        defer func() {
            _ = recover()
        }()

        NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/exiting", nil))
    }()

    messages, _ := recordingLogger.warnings()
    if 1 != len(messages) || 1 != replacementBody.closed {
        t.Fatalf("expected the refused replacement warned and closed once on the exit, got %v and %d closes", messages, replacementBody.closed)
    }
}

func TestKernelResponseEvent_AnAdmittedNilResponseIsWarnedAs204(t *testing.T) {
    payload := NewKernelResponseEvent(nil, TextResponse(nethttp.StatusOK, "original"))

    leave := payload.EnterAfterFailureListener(eventseam.NewToken(), eventseam.AfterFailureListener{ListenerName: "emptying", ReplacesResponse: true})
    payload.SetResponse(nil)
    leave()

    recordingLogger := &afterFailureRecordingLogger{}
    warnAfterFailureWrites(recordingLogger, payload.takeAfterFailureWrites(), afterFailureVerdict{nilResponseStatus: nethttp.StatusNoContent})

    messages, contexts := recordingLogger.warnings()
    if 1 != len(messages) || nethttp.StatusNoContent != contexts[0]["servedResponseStatus"] || nethttp.StatusOK != contexts[0]["replacedResponseStatus"] {
        t.Fatalf("expected the nil response read as the 204 the kernel writes, got %v %v", messages, contexts)
    }
}
