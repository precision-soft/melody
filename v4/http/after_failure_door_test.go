package http

import (
    "errors"
    "fmt"
    "strings"
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/eventseam"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
)

/* doorPayload is one of the four guarded payloads, with how a listener writes a response to it */
type doorPayload struct {
    name     string
    door     *afterFailureDoor
    guard    eventseam.AfterFailureGuard
    response func() httpcontract.Response
    write    func(response httpcontract.Response)
}

func doorPayloads() []doorPayload {
    request := testhelper.NewHttpTestRequestFromHttpRequest(httptest.NewRequest(nethttp.MethodGet, "/x", nil))

    requestEvent := NewKernelRequestEvent(newTestRuntime(), request)
    controllerEvent := NewKernelControllerEvent(newTestRuntime(), request)
    responseEvent := NewKernelResponseEvent(request, nil)
    exceptionEvent := NewKernelExceptionEvent(newTestRuntime(), request, errors.New("failed"), KernelExceptionEventConfig{})

    return []doorPayload{
        {"kernel.request", &requestEvent.afterFailureDoor, requestEvent, requestEvent.Response, requestEvent.SetResponse},
        {"kernel.controller", &controllerEvent.afterFailureDoor, controllerEvent, controllerEvent.Response, controllerEvent.SetResponse},
        {"kernel.response", &responseEvent.afterFailureDoor, responseEvent, responseEvent.Response, responseEvent.SetResponse},
        {"kernel.exception", &exceptionEvent.afterFailureDoor, exceptionEvent, exceptionEvent.Response, exceptionEvent.SetResponse},
    }
}

func TestAfterFailureDoor_EveryResponsePayloadGuardsAfterAFailure(t *testing.T) {
    for _, payload := range doorPayloads() {
        written := TextResponse(nethttp.StatusTeapot, "late")

        leave := payload.guard.EnterAfterFailureListener(eventseam.NewToken(), eventseam.AfterFailureListener{ListenerName: "closed"})
        payload.write(written)
        leave()

        writes := payload.door.takeAfterFailureWrites()
        if nil != payload.response() || 1 != len(writes) || true == writes[0].admitted || written != writes[0].written {
            t.Fatalf("%s: expected the closed door to ignore and record the write, got %v %+v", payload.name, payload.response(), writes)
        }

        leave = payload.guard.EnterAfterFailureListener(eventseam.NewToken(), eventseam.AfterFailureListener{ListenerName: "open", ReplacesResponse: true})
        payload.write(written)
        payload.write(written)
        leave()

        writes = payload.door.takeAfterFailureWrites()
        if written != payload.response() || 1 != len(writes) || false == writes[0].admitted {
            t.Fatalf("%s: expected the open door to store the write once and setting it again to record nothing, got %+v", payload.name, writes)
        }

        leave = payload.guard.EnterAfterFailureListener(eventseam.Token{}, eventseam.AfterFailureListener{ListenerName: "forged"})
        other := TextResponse(nethttp.StatusAccepted, "forged")
        payload.write(other)
        leave()

        if other != payload.response() || 0 != len(payload.door.takeAfterFailureWrites()) {
            t.Fatalf("%s: expected a zero token to enter nothing", payload.name)
        }
    }
}

func enterRequestPass(requestEvent *KernelRequestEvent, replaces bool) func() {
    return requestEvent.EnterAfterFailureListener(eventseam.NewToken(), eventseam.AfterFailureListener{EventName: kernelcontract.EventKernelRequest, ListenerName: "late", ReplacesResponse: replaces})
}

func TestKernelRequestEvent_ARefuseAfterAFailureIsIgnoredWithoutTheDoor(t *testing.T) {
    requestEvent := firstResponderRequest()
    standing := TextResponse(nethttp.StatusOK, "standing")
    requestEvent.SetResponse(standing)

    leave := enterRequestPass(requestEvent, false)
    requestEvent.Refuse(exception.Forbidden("late"))
    leave()

    writes := requestEvent.takeAfterFailureWrites()
    if nil != requestEvent.Refusal() || standing != requestEvent.Response() || true == requestEvent.decisionRefused || 1 != len(writes) || afterFailureWriteRefusal != writes[0].write || nil == writes[0].refusal {
        t.Fatalf("expected the refusal ignored and recorded, got %v %+v", requestEvent.Refusal(), writes)
    }
}

func TestKernelRequestEvent_ARefuseAfterAFailureThroughTheDoorReplacesTheStandingDecision(t *testing.T) {
    requestEvent := firstResponderRequest()
    standing := TextResponse(nethttp.StatusOK, "standing")
    requestEvent.SetResponse(standing)

    leave := enterRequestPass(requestEvent, true)
    requestEvent.Refuse(exception.Forbidden("first"))
    second := exception.Forbidden("second")
    requestEvent.Refuse(second)
    leave()

    writes := requestEvent.takeAfterFailureWrites()
    if second != requestEvent.Refusal() || nil != requestEvent.Response() || 2 != len(writes) || standing != writes[0].standing {
        t.Fatalf("expected the door's refusals to replace the decision standing, got %v %v %+v", requestEvent.Refusal(), requestEvent.Response(), writes)
    }
}

func TestKernelRequestEvent_ASetResponseAfterAFailureThroughTheDoorClearsTheRefusal(t *testing.T) {
    requestEvent := firstResponderRequest()
    requestEvent.Refuse(exception.Forbidden("standing"))

    answer := TextResponse(nethttp.StatusOK, "late")
    leave := enterRequestPass(requestEvent, true)
    requestEvent.SetResponse(answer)
    leave()

    if nil != requestEvent.Refusal() || answer != requestEvent.Response() || true == requestEvent.decisionRefused {
        t.Fatalf("expected the answer to replace the refusal without the second-decision refusal, got %v %v", requestEvent.Refusal(), requestEvent.Response())
    }
}

func TestKernelRequestEvent_ANilRefuseAfterAFailureStillPanics(t *testing.T) {
    requestEvent := firstResponderRequest()

    leave := enterRequestPass(requestEvent, true)
    defer leave()

    testhelper.AssertPanicsWithError(t, func() {
        requestEvent.Refuse(nil)
    }, "a kernel.request listener refused a request with no error")

    if false == requestEvent.decisionRefused {
        t.Fatalf("expected a nil refusal to mark the decision refused")
    }
}

type doorKernelCase struct {
    eventName string
    tagged    func(eventValue eventcontract.Event)
    replaces  bool
    required  bool
    /* failing writes this response through the payload before it fails */
    standing httpcontract.Response
    /* failingWrite runs in the failing listener before it fails, beside standing */
    failingWrite func(eventValue eventcontract.Event)
    /* logger is the journal the kernel writes to, a fresh one when nil */
    logger *afterFailureRecordingLogger
}

/* runDoorKernel serves one request whose eventName dispatch fails, a tagged listener writing behind the failure; a required listener behind it when asked */
func runDoorKernel(t *testing.T, testCase doorKernelCase) (*httptest.ResponseRecorder, *afterFailureRecordingLogger, *bool) {
    t.Helper()

    handlerRan := false
    router := NewRouter()
    router.Handle("/door", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        handlerRan = true
        if kernelcontract.EventKernelException == testCase.eventName {
            return nil, exception.Forbidden("handler refused")
        }

        return TextResponse(nethttp.StatusOK, "handler"), nil
    }, RouteOptions{Methods: []string{nethttp.MethodGet}})

    recordingLogger := testCase.logger
    if nil == recordingLogger {
        recordingLogger = &afterFailureRecordingLogger{}
    }
    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    dispatcher.DeclareEvent(testCase.eventName, eventcontract.EventConfig{TagsToRunAfterFailure: []string{"cleanup"}})

    failed := false
    dispatcher.AddListener(testCase.eventName, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        if true == failed {
            return nil
        }
        failed = true

        if nil != testCase.standing {
            eventValue.Payload().(interface{ SetResponse(httpcontract.Response) }).SetResponse(testCase.standing)
        }

        if nil != testCase.failingWrite {
            testCase.failingWrite(eventValue)
        }

        return errors.New("the listener failed")
    }, eventcontract.ListenerConfig{Priority: 1000})

    wrote := false
    dispatcher.AddListener(testCase.eventName, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        if false == wrote {
            wrote = true
            testCase.tagged(eventValue)
        }

        return nil
    }, eventcontract.ListenerConfig{Priority: 900, Name: "late", Tags: []string{"cleanup"}, Failure: eventcontract.ListenerFailureConfig{ReplacesResponseAfterFailure: testCase.replaces}})

    if true == testCase.required {
        dispatcher.AddListener(testCase.eventName, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        }, eventcontract.ListenerConfig{Priority: 800, Failure: eventcontract.ListenerFailureConfig{Required: true}})
    }

    recorder := httptest.NewRecorder()
    NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/door", nil))

    return recorder, recordingLogger, &handlerRan
}

func doorWarning(t *testing.T, name string, recordingLogger *afterFailureRecordingLogger, expectedMessage string, expectedRefusedBy string) {
    t.Helper()

    messages, contexts := recordingLogger.warnings()
    for index, message := range messages {
        if expectedMessage == message && "late" == contexts[index]["listenerName"] {
            if "" != expectedRefusedBy && expectedRefusedBy != contexts[index]["refusedBy"] {
                t.Fatalf("%s: expected refusedBy %q, got %v", name, expectedRefusedBy, contexts[index])
            }

            return
        }
    }

    t.Fatalf("%s: expected the warning %q, got %v", name, expectedMessage, messages)
}

const (
    doorRefusedMessage  = "a listener's response replacement after a failure is refused"
    doorReplacedMessage = "a listener replaced the response after a failure"
)

func TestKernel_TheAfterFailureDoorOnKernelRequest(t *testing.T) {
    for _, testCase := range []struct {
        name           string
        replaces       bool
        required       bool
        refuse         bool
        nilResponse    bool
        expectedStatus int
        message        string
        refusedBy      string
    }{
        {"a closed door answers 500", false, false, false, false, nethttp.StatusInternalServerError, doorRefusedMessage, afterFailureRefusedByDoor},
        {"an open door serves the answer", true, false, false, false, nethttp.StatusTeapot, doorReplacedMessage, ""},
        {"an open door raises the refusal", true, false, true, false, nethttp.StatusForbidden, doorReplacedMessage, ""},
        {"a closed door ignores the refusal", false, false, true, false, nethttp.StatusInternalServerError, doorRefusedMessage, afterFailureRefusedByDoor},
        {"an admitted answer behind a skipped required listener answers 500", true, true, false, false, nethttp.StatusInternalServerError, doorRefusedMessage, afterFailureRefusedByRequiredListenerSkipped},
        {"an admitted nil answer runs no handler", true, false, false, true, nethttp.StatusInternalServerError, doorRefusedMessage, afterFailureRefusedByNoResponse},
    } {
        answer, answerBody := responseWithClosingBody(nethttp.StatusTeapot, "late")

        recorder, recordingLogger, handlerRan := runDoorKernel(t, doorKernelCase{
            eventName: kernelcontract.EventKernelRequest,
            replaces:  testCase.replaces,
            required:  testCase.required,
            tagged: func(eventValue eventcontract.Event) {
                requestEvent := eventValue.Payload().(*KernelRequestEvent)
                switch {
                case true == testCase.refuse:
                    requestEvent.Refuse(exception.Forbidden("late"))
                case true == testCase.nilResponse:
                    requestEvent.SetResponse(answer)
                    requestEvent.SetResponse(nil)
                default:
                    requestEvent.SetResponse(answer)
                }
            },
        })

        if testCase.expectedStatus != recorder.Code || true == *handlerRan {
            t.Fatalf("%s: expected %d without the handler, got %d (handler ran %v)", testCase.name, testCase.expectedStatus, recorder.Code, *handlerRan)
        }

        doorWarning(t, testCase.name, recordingLogger, testCase.message, testCase.refusedBy)

        if false == testCase.refuse && nethttp.StatusInternalServerError == testCase.expectedStatus && 1 != answerBody.closed {
            t.Fatalf("%s: expected the answer that lost closed once, closed %d", testCase.name, answerBody.closed)
        }
    }
}

func TestKernel_TheAfterFailureDoorOnKernelController(t *testing.T) {
    for _, testCase := range []struct {
        name           string
        replaces       bool
        nilResponse    bool
        expectedStatus int
        message        string
        refusedBy      string
    }{
        {"a closed door answers 500", false, false, nethttp.StatusInternalServerError, doorRefusedMessage, afterFailureRefusedByDoor},
        {"an open door serves the answer", true, false, nethttp.StatusTeapot, doorReplacedMessage, ""},
        {"an admitted nil answer answers 500", true, true, nethttp.StatusInternalServerError, doorRefusedMessage, afterFailureRefusedByNoResponse},
        {"an admitted answer behind a skipped required listener answers 500", true, false, nethttp.StatusInternalServerError, doorRefusedMessage, afterFailureRefusedByRequiredListenerSkipped},
    } {
        answer, answerBody := responseWithClosingBody(nethttp.StatusTeapot, "late")

        recorder, recordingLogger, handlerRan := runDoorKernel(t, doorKernelCase{
            eventName: kernelcontract.EventKernelController,
            replaces:  testCase.replaces,
            required:  afterFailureRefusedByRequiredListenerSkipped == testCase.refusedBy,
            tagged: func(eventValue eventcontract.Event) {
                eventValue.Payload().(*KernelControllerEvent).SetResponse(answer)
                if true == testCase.nilResponse {
                    eventValue.Payload().(*KernelControllerEvent).SetResponse(nil)
                }
            },
        })

        if testCase.expectedStatus != recorder.Code || true == *handlerRan {
            t.Fatalf("%s: expected %d without the handler, got %d (handler ran %v)", testCase.name, testCase.expectedStatus, recorder.Code, *handlerRan)
        }

        doorWarning(t, testCase.name, recordingLogger, testCase.message, testCase.refusedBy)

        if (false == testCase.replaces || true == testCase.nilResponse) && 1 != answerBody.closed {
            t.Fatalf("%s: expected the refused answer closed once, closed %d", testCase.name, answerBody.closed)
        }
    }
}

func TestKernel_TheAfterFailureDoorOnKernelException(t *testing.T) {
    for _, testCase := range []struct {
        name           string
        replaces       bool
        expectedStatus int
        message        string
    }{
        {"a closed door renders the error", false, nethttp.StatusForbidden, doorRefusedMessage},
        {"an open door serves the answer", true, nethttp.StatusTeapot, doorReplacedMessage},
    } {
        answer, _ := responseWithClosingBody(nethttp.StatusTeapot, "late")

        recorder, recordingLogger, _ := runDoorKernel(t, doorKernelCase{
            eventName: kernelcontract.EventKernelException,
            replaces:  testCase.replaces,
            tagged: func(eventValue eventcontract.Event) {
                eventValue.Payload().(*KernelExceptionEvent).SetResponse(answer)
            },
        })

        if testCase.expectedStatus != recorder.Code {
            t.Fatalf("%s: expected %d, got %d", testCase.name, testCase.expectedStatus, recorder.Code)
        }

        doorWarning(t, testCase.name, recordingLogger, testCase.message, "")

        _, dropped := recordingLogger.errorContextFor("the kernel.exception dispatch failed with a response set, which is dropped")
        if true == testCase.replaces && true == dropped {
            t.Fatalf("%s: expected the admitted answer not recorded as dropped", testCase.name)
        }
    }
}

func TestKernel_AnExitAfterAWriteStillWarnsAndClosesIt(t *testing.T) {
    for _, testCase := range []struct {
        eventName string
        replaces  bool
    }{
        {kernelcontract.EventKernelRequest, false},
        {kernelcontract.EventKernelRequest, true},
        {kernelcontract.EventKernelController, false},
        {kernelcontract.EventKernelController, true},
        {kernelcontract.EventKernelException, false},
        {kernelcontract.EventKernelException, true},
    } {
        eventName := testCase.eventName
        refused, refusedBody := responseWithClosingBody(nethttp.StatusTeapot, "late")
        recordingLogger := &afterFailureRecordingLogger{}

        func() {
            defer func() {
                _ = recover()
            }()

            runDoorKernel(t, doorKernelCase{
                eventName: eventName,
                replaces:  testCase.replaces,
                logger:    recordingLogger,
                tagged: func(eventValue eventcontract.Event) {
                    eventValue.Payload().(interface{ SetResponse(httpcontract.Response) }).SetResponse(refused)
                    exception.Exit(exception.NewExitError(3, exception.NewError("the process is done", nil, nil)))
                },
            })
        }()

        if 1 != refusedBody.closed {
            t.Fatalf("%s door %v: expected the write closed once on the exit, closed %d", eventName, testCase.replaces, refusedBody.closed)
        }

        expectedRefusedBy := afterFailureRefusedByExit
        if false == testCase.replaces {
            expectedRefusedBy = afterFailureRefusedByDoor
        }
        doorWarning(t, eventName, recordingLogger, doorRefusedMessage, expectedRefusedBy)
    }
}

/* an answer a door admitted after a failure replaces the half-made response the failed listener left, which the kernel closes once */
func TestKernel_AnAdmittedAnswerClosesTheResponseItReplaced(t *testing.T) {
    for _, eventName := range []string{kernelcontract.EventKernelRequest, kernelcontract.EventKernelController} {
        standing, standingBody := responseWithClosingBody(nethttp.StatusAccepted, "half made")
        answer, answerBody := responseWithClosingBody(nethttp.StatusTeapot, "late")

        recorder, _, _ := runDoorKernel(t, doorKernelCase{
            eventName: eventName,
            replaces:  true,
            standing:  standing,
            tagged: func(eventValue eventcontract.Event) {
                eventValue.Payload().(interface{ SetResponse(httpcontract.Response) }).SetResponse(answer)
            },
        })

        if nethttp.StatusTeapot != recorder.Code || 1 != standingBody.closed || 1 != answerBody.closed {
            t.Fatalf("%s: expected the answer served and the half-made response closed once, got %d, %d and %d closes", eventName, recorder.Code, standingBody.closed, answerBody.closed)
        }
    }
}

/* a door-closed write that would erase the refusal standing at the failure is refused like any other: the refusal is raised */
func TestKernel_ADoorClosedNilAnswerDoesNotEraseAStandingRefusal(t *testing.T) {
    recorder, recordingLogger, _ := runDoorKernel(t, doorKernelCase{
        eventName: kernelcontract.EventKernelRequest,
        failingWrite: func(eventValue eventcontract.Event) {
            eventValue.Payload().(*KernelRequestEvent).Refuse(exception.Forbidden("denied"))
        },
        tagged: func(eventValue eventcontract.Event) {
            eventValue.Payload().(*KernelRequestEvent).SetResponse(nil)
        },
    })

    if nethttp.StatusForbidden != recorder.Code {
        t.Fatalf("expected the standing refusal raised, got %d", recorder.Code)
    }

    doorWarning(t, "erase", recordingLogger, doorRefusedMessage, afterFailureRefusedByDoor)
}

func TestKernel_AnAdmittedResponseReplacementBehindASkippedRequiredListenerIsNotServed(t *testing.T) {
    answer, answerBody := responseWithClosingBody(nethttp.StatusTeapot, "late")

    recorder, recordingLogger, _ := runDoorKernel(t, doorKernelCase{
        eventName: kernelcontract.EventKernelResponse,
        replaces:  true,
        required:  true,
        tagged: func(eventValue eventcontract.Event) {
            eventValue.Payload().(*KernelResponseEvent).SetResponse(answer)
        },
    })

    if nethttp.StatusOK != recorder.Code || "handler" != recorder.Body.String() || 1 != answerBody.closed {
        t.Fatalf("expected the response the failure left served and the replacement closed once, got %d %q and %d closes", recorder.Code, recorder.Body.String(), answerBody.closed)
    }

    doorWarning(t, "response", recordingLogger, doorRefusedMessage, afterFailureRefusedByRequiredListenerSkipped)
}

func TestKernel_AnAdmittedExceptionAnswerBehindASkippedRequiredListenerIsNotServed(t *testing.T) {
    answer, answerBody := responseWithClosingBody(nethttp.StatusTeapot, "late")

    recorder, recordingLogger, _ := runDoorKernel(t, doorKernelCase{
        eventName: kernelcontract.EventKernelException,
        replaces:  true,
        required:  true,
        tagged: func(eventValue eventcontract.Event) {
            eventValue.Payload().(*KernelExceptionEvent).SetResponse(answer)
        },
    })

    if nethttp.StatusForbidden != recorder.Code || 1 != answerBody.closed {
        t.Fatalf("expected the framework's rendering and the answer closed once, got %d and %d closes", recorder.Code, answerBody.closed)
    }

    doorWarning(t, "exception", recordingLogger, doorRefusedMessage, afterFailureRefusedByRequiredListenerSkipped)
}

func TestKernel_AnAdmittedAnswerBehindASecondDecisionAnswers500(t *testing.T) {
    answer, answerBody := responseWithClosingBody(nethttp.StatusTeapot, "late")

    recorder, recordingLogger, _ := runDoorKernel(t, doorKernelCase{
        eventName: kernelcontract.EventKernelRequest,
        replaces:  true,
        failingWrite: func(eventValue eventcontract.Event) {
            requestEvent := eventValue.Payload().(*KernelRequestEvent)
            requestEvent.SetResponse(TextResponse(nethttp.StatusOK, "first"))
            requestEvent.Refuse(exception.Forbidden("second"))
        },
        tagged: func(eventValue eventcontract.Event) {
            eventValue.Payload().(*KernelRequestEvent).SetResponse(answer)
        },
    })

    if nethttp.StatusInternalServerError != recorder.Code || 1 != answerBody.closed {
        t.Fatalf("expected the fail-closed 500 and the answer closed once, got %d and %d closes", recorder.Code, answerBody.closed)
    }

    doorWarning(t, "second decision", recordingLogger, doorRefusedMessage, afterFailureRefusedBySecondDecision)
}

func TestKernel_TheAfterFailureWarningNamesTheWrite(t *testing.T) {
    _, recordingLogger, _ := runDoorKernel(t, doorKernelCase{
        eventName: kernelcontract.EventKernelRequest,
        replaces:  true,
        tagged: func(eventValue eventcontract.Event) {
            eventValue.Payload().(*KernelRequestEvent).Refuse(exception.Forbidden("late refusal"))
        },
    })

    messages, contexts := recordingLogger.warnings()
    if 0 == len(messages) || doorReplacedMessage != messages[0] || afterFailureWriteRefusal != contexts[0]["write"] || false == strings.Contains(fmt.Sprint(contexts[0]["refusal"]), "late refusal") {
        t.Fatalf("expected the refusal write named with its error, got %v %v", messages, contexts)
    }

    if _, hasServed := contexts[0]["servedResponseStatus"]; true == hasServed {
        t.Fatalf("expected no served status for a refusal, got %v", contexts[0])
    }
}

func TestKernel_ADoorClosedWriteOnKernelRequestNamesTheStandingResponseUnserved(t *testing.T) {
    standing, _ := responseWithClosingBody(nethttp.StatusAccepted, "half made")

    _, recordingLogger, _ := runDoorKernel(t, doorKernelCase{
        eventName: kernelcontract.EventKernelRequest,
        standing:  standing,
        tagged: func(eventValue eventcontract.Event) {
            eventValue.Payload().(*KernelRequestEvent).SetResponse(TextResponse(nethttp.StatusTeapot, "late"))
        },
    })

    _, contexts := recordingLogger.warnings()
    if 1 != len(contexts) || nethttp.StatusAccepted != contexts[0]["standingResponseStatus"] {
        t.Fatalf("expected the standing status under its own key, got %v", contexts)
    }

    if _, hasServed := contexts[0]["servedResponseStatus"]; true == hasServed {
        t.Fatalf("expected no served status where the kernel answers for itself, got %v", contexts[0])
    }
}

/* the response the failed listener left is the payload's own when a later listener's exit unwinds the dispatch, though no write touched it: the exit closes it once */
func TestKernel_AnExitClosesTheResponseTheFailureLeft(t *testing.T) {
    for _, eventName := range []string{kernelcontract.EventKernelRequest, kernelcontract.EventKernelController, kernelcontract.EventKernelException} {
        standing, standingBody := responseWithClosingBody(nethttp.StatusAccepted, "half made")

        func() {
            defer func() {
                _ = recover()
            }()

            runDoorKernel(t, doorKernelCase{
                eventName: eventName,
                standing:  standing,
                tagged: func(eventValue eventcontract.Event) {
                    exception.Exit(exception.NewExitError(3, exception.NewError("the process is done", nil, nil)))
                },
            })
        }()

        if 1 != standingBody.closed {
            t.Fatalf("%s: expected the abandoned response closed once on the exit, closed %d", eventName, standingBody.closed)
        }
    }
}
