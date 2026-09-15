package http

import (
    "context"
    "errors"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/event"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/session"
    sessioncontract "github.com/precision-soft/melody/v3/session/contract"
    "github.com/precision-soft/melody/v3/validation"
)

func TestKernel_ResponseListenerReplacesResponseOnSuccessPath(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/hello",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "original"), nil
        },
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if false == ok {
                return nil
            }

            responseEvent.SetResponse(TextResponse(nethttp.StatusAccepted, "replaced"))
            return nil
        },
        0,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/hello", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusAccepted != recorder.Code {
        t.Fatalf("expected listener-replaced status %d, got %d", nethttp.StatusAccepted, recorder.Code)
    }

    if "replaced" != recorder.Body.String() {
        t.Fatalf("expected listener-replaced body, got %q", recorder.Body.String())
    }
}

func TestKernel_ResponseListenerReplacesResponseOnPanicRecoveryPath(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/boom",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            panic("boom")
        },
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if false == ok {
                return nil
            }

            responseEvent.SetResponse(TextResponse(nethttp.StatusAccepted, "recovered-replaced"))
            return nil
        },
        0,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/boom", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusAccepted != recorder.Code {
        t.Fatalf("expected listener-replaced status %d on panic-recovery path, got %d", nethttp.StatusAccepted, recorder.Code)
    }

    if "recovered-replaced" != recorder.Body.String() {
        t.Fatalf("expected listener-replaced body on panic-recovery path, got %q", recorder.Body.String())
    }
}

func TestKernel_ClosesHandlerReturnedBodyWhenHandlerAlreadyStreamed(t *testing.T) {
    body := &closeRecordingReadCloser{}

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/stream-then-return",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            writer.WriteHeader(nethttp.StatusOK)

            return &Response{
                statusCode: nethttp.StatusOK,
                headers:    make(nethttp.Header),
                bodyReader: body,
            }, nil
        },
    )

    serviceContainer := newHttpTestContainer()
    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/stream-then-return", nil)

    handler.ServeHTTP(httptest.NewRecorder(), request)

    if 1 != body.closeCount {
        t.Fatalf("expected the discarded handler-returned response body to be closed exactly once, got %d", body.closeCount)
    }
}

func TestKernel_ClosesDiscardedResponseBodyWhenResponseListenerSwapsResponse(t *testing.T) {
    body := &closeRecordingReadCloser{}

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/file-then-swap",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return &Response{
                statusCode: nethttp.StatusOK,
                headers:    make(nethttp.Header),
                bodyReader: body,
            }, nil
        },
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if false == ok {
                return nil
            }

            responseEvent.SetResponse(TextResponse(nethttp.StatusAccepted, "replaced"))
            return nil
        },
        0,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/file-then-swap", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if 1 != body.closeCount {
        t.Fatalf("expected the discarded original response body to be closed exactly once after a response listener swapped the response, got %d", body.closeCount)
    }

    if nethttp.StatusAccepted != recorder.Code {
        t.Fatalf("expected the listener-replaced status %d, got %d", nethttp.StatusAccepted, recorder.Code)
    }
}

func TestKernel_DoesNotWriteDefaultResponseWhenHandlerAlreadyStreamed(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/stream",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            writer.WriteHeader(nethttp.StatusOK)

            _, writeErr := writer.Write([]byte("streamed"))
            if nil != writeErr {
                return nil, writeErr
            }

            return nil, nil
        },
    )

    serviceContainer := newHttpTestContainer()
    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/stream", nil)
    countingWriter := &writeHeaderCountingResponseWriter{
        ResponseWriter: httptest.NewRecorder(),
    }

    handler.ServeHTTP(countingWriter, request)

    if 1 != countingWriter.writeHeaderCount {
        t.Fatalf("expected exactly one WriteHeader call for a streamed response, got %d", countingWriter.writeHeaderCount)
    }
}

func TestKernel_PreservesHijackerForWrappedResponseWriter(t *testing.T) {
    sawHijacker := false
    var hijackErr error

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/ws",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            hijacker, isHijacker := writer.(nethttp.Hijacker)
            sawHijacker = isHijacker
            if true == isHijacker {
                connection, _, err := hijacker.Hijack()
                hijackErr = err
                if nil != connection {
                    _ = connection.Close()
                }
            }

            return nil, nil
        },
    )

    serviceContainer := newHttpTestContainer()
    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/ws", nil)
    underlying := &hijackableResponseWriter{
        ResponseWriter: httptest.NewRecorder(),
    }

    handler.ServeHTTP(underlying, request)

    if false == sawHijacker {
        t.Fatal("expected the kernel-wrapped response writer to preserve http.Hijacker")
    }

    if nil != hijackErr {
        t.Fatalf("expected Hijack to forward to the underlying writer, got %v", hijackErr)
    }

    if false == underlying.hijacked {
        t.Fatal("expected Hijack to reach the underlying response writer")
    }
}

func TestKernel_WritesDefaultResponseWhenHijackFails(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/ws",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            hijacker, isHijacker := writer.(nethttp.Hijacker)
            if true == isHijacker {
                _, _, _ = hijacker.Hijack()
            }

            return nil, nil
        },
    )

    serviceContainer := newHttpTestContainer()
    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/ws", nil)
    underlying := &failingHijackResponseWriter{
        ResponseWriter: httptest.NewRecorder(),
    }

    handler.ServeHTTP(underlying, request)

    if 0 == underlying.writeHeaderCount {
        t.Fatal("expected the kernel to write a default response after a failed hijack")
    }
}

func TestKernel_DoesNotRewriteResponseWhenHandlerStreamedThenPanicked(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/stream-panic",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            writer.WriteHeader(nethttp.StatusOK)

            _, writeErr := writer.Write([]byte("partial"))
            if nil != writeErr {
                return nil, writeErr
            }

            panic("boom after streaming")
        },
    )

    serviceContainer := newHttpTestContainer()
    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/stream-panic", nil)
    countingWriter := &writeHeaderCountingResponseWriter{
        ResponseWriter: httptest.NewRecorder(),
    }

    handler.ServeHTTP(countingWriter, request)

    if 1 != countingWriter.writeHeaderCount {
        t.Fatalf("expected exactly one WriteHeader call when a handler streamed then panicked, got %d", countingWriter.writeHeaderCount)
    }
}

func TestKernel_DoesNotDoublePersistSessionWhenWriteFailsAfterCommit(t *testing.T) {
    storage := &countingSessionStorage{
        Storage: session.NewInMemoryStorage(),
    }

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/save",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionValue, exists := request.Attributes().Get(RequestAttributeSession)
            if false == exists {
                t.Fatal("expected the request to carry a session")
            }

            sessionInstance, ok := sessionValue.(sessioncontract.Session)
            if false == ok {
                t.Fatal("expected the session attribute to be a session")
            }

            sessionInstance.Set("key", "value")

            return TextResponse(nethttp.StatusOK, "body"), nil
        },
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(storage)
    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/save", nil)

    handler.ServeHTTP(&writeFailingResponseWriter{}, request)

    if 1 != storage.saveCount {
        t.Fatalf("expected the session to be persisted exactly once across the first write and the panic-recovery write, got %d", storage.saveCount)
    }
}

func TestKernel_ServeHttpClosesScopeWhenRequestLoggerSetupFails(t *testing.T) {
    recorder := httptest.NewRecorder()

    codeAtScopeClose := 0

    recordingContainer := &scopeRecordingContainer{
        Container:    newHttpTestContainer(),
        failOverride: true,
        onClose: func() {
            codeAtScopeClose = recorder.Code
        },
    }

    handler := NewKernel(NewRouter()).ServeHttp(recordingContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/", nil)

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the wiring panic to be answered with a 500, got %d", recorder.Code)
    }

    if nethttp.StatusInternalServerError != codeAtScopeClose {
        t.Fatalf("expected the 500 to have been written before the scope closed, the response carried %d at that moment", codeAtScopeClose)
    }

    if nil == recordingContainer.scope {
        t.Fatalf("expected a request scope to have been created")
    }

    if false == recordingContainer.scope.closed {
        t.Fatalf("expected the request scope to be closed even when request logger setup fails")
    }
}

func TestKernel_FailsClosedWhenKernelRequestDispatchErrors(t *testing.T) {
    handlerRan := false

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/guarded",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            handlerRan = true

            return TextResponse(nethttp.StatusOK, "handled"), nil
        },
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)

    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return exception.NewError("resolution listener failed", nil, nil)
        },
        50,
    )

    accessControlRan := false
    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            accessControlRan = true

            return nil
        },
        20,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/guarded", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if true == accessControlRan {
        t.Fatalf("expected the lower-priority listener to be skipped by the dispatcher abort")
    }

    if true == handlerRan {
        t.Fatalf("expected the handler not to run when the kernel.request dispatch aborted with partially-run listeners")
    }

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected a fail-closed 500 when the kernel.request dispatch errored, got %d", recorder.Code)
    }
}

func TestKernel_FailsClosedWhenKernelControllerDispatchErrors(t *testing.T) {
    handlerRan := false

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/guarded",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            handlerRan = true

            return TextResponse(nethttp.StatusOK, "handled"), nil
        },
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)

    dispatcher.AddListener(
        kernelcontract.EventKernelController,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return exception.NewError("controller guard listener failed", nil, nil)
        },
        50,
    )

    lowerPriorityRan := false
    dispatcher.AddListener(
        kernelcontract.EventKernelController,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            lowerPriorityRan = true

            return nil
        },
        20,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/guarded", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if true == lowerPriorityRan {
        t.Fatalf("expected the lower-priority kernel.controller listener to be skipped by the dispatcher abort")
    }

    if true == handlerRan {
        t.Fatalf("expected the handler not to run when the kernel.controller dispatch aborted with partially-run listeners")
    }

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected a fail-closed 500 when the kernel.controller dispatch errored, got %d", recorder.Code)
    }
}
func TestKernel_KernelRequestListenerResponseStillWinsOverDispatchError(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/denied",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "handled"), nil
        },
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)

    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*KernelRequestEvent)
            if false == ok {
                return nil
            }

            requestEvent.SetResponse(JsonErrorResponse(nethttp.StatusUnauthorized, "denied"))

            return exception.NewError("denied-event dispatch failed", nil, nil)
        },
        50,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/denied", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusUnauthorized != recorder.Code {
        t.Fatalf("expected the listener-set 401 to win over the synthesized 500, got %d", recorder.Code)
    }
}

func TestKernel_KernelRequestStoppingListenerThatAnswersStillFailsClosedBeforeARequiredListener(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/admin",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "handled"), nil
        },
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)

    requiredRegistration := dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        10,
    )

    registrar, isRegistrar := any(dispatcher).(eventcontract.RequiredListenerRegistrar)
    if false == isRegistrar {
        t.Fatalf("expected the dispatcher to mark required listeners")
    }

    registrar.MarkListenerRequired(requiredRegistration)

    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*KernelRequestEvent)
            if false == ok {
                return nil
            }

            requestEvent.SetResponse(TextResponse(nethttp.StatusOK, "from the cache"))
            eventValue.StopPropagation()

            return nil
        },
        50,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/admin", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the cached response to be dropped for the error page, got %d", recorder.Code)
    }

    if true == strings.Contains(recorder.Body.String(), "from the cache") {
        t.Fatalf("expected the cached body never to reach the writer, got %q", recorder.Body.String())
    }
}

func TestKernel_KernelRequestFailingMaySkipListenerThatAnswersStillFailsClosedBeforeARequiredListener(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/admin",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "handled"), nil
        },
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)

    requiredRegistration := dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        10,
    )

    registrar, isRegistrar := any(dispatcher).(eventcontract.RequiredListenerRegistrar)
    if false == isRegistrar {
        t.Fatalf("expected the dispatcher to mark required listeners")
    }

    registrar.MarkListenerRequired(requiredRegistration)

    cacheRegistration := dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*KernelRequestEvent)
            if false == ok {
                return nil
            }

            requestEvent.SetResponse(TextResponse(nethttp.StatusOK, "from the cache"))

            return errors.New("the cache listener's own failure")
        },
        50,
    )

    registrar.MarkListenerMaySkipRequiredListeners(cacheRegistration)

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/admin", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the failing listener's response to be dropped for the error page, got %d", recorder.Code)
    }

    if true == strings.Contains(recorder.Body.String(), "from the cache") {
        t.Fatalf("expected the cached body never to reach the writer, got %q", recorder.Body.String())
    }
}

func TestKernel_KernelControllerStoppingListenerThatAnswersStillFailsClosedBeforeARequiredListener(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/admin",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "handled"), nil
        },
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)

    requiredRegistration := dispatcher.AddListener(
        kernelcontract.EventKernelController,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        10,
    )

    registrar, isRegistrar := any(dispatcher).(eventcontract.RequiredListenerRegistrar)
    if false == isRegistrar {
        t.Fatalf("expected the dispatcher to mark required listeners")
    }

    registrar.MarkListenerRequired(requiredRegistration)

    dispatcher.AddListener(
        kernelcontract.EventKernelController,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            controllerEvent, ok := eventValue.Payload().(*KernelControllerEvent)
            if false == ok {
                return nil
            }

            controllerEvent.SetResponse(TextResponse(nethttp.StatusOK, "from the cache"))
            eventValue.StopPropagation()

            return nil
        },
        50,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/admin", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the cached response to be dropped for the error page, got %d", recorder.Code)
    }

    if true == strings.Contains(recorder.Body.String(), "from the cache") {
        t.Fatalf("expected the cached body never to reach the writer, got %q", recorder.Body.String())
    }
}

func TestKernel_ResponseListenerErrorDoesNotDropResponse(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/still-served",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "served"), nil
        },
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return exception.NewError("response listener failed", nil, nil)
        },
        0,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/still-served", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected the response to be written despite the kernel.response dispatch error, got %d", recorder.Code)
    }

    if "served" != recorder.Body.String() {
        t.Fatalf("expected the handler body to be written, got %q", recorder.Body.String())
    }
}

type failingSessionStorage struct{}

func (instance *failingSessionStorage) Load(sessionId string) (map[string]any, bool, error) {
    return nil, false, nil
}

func (instance *failingSessionStorage) Save(sessionId string, data map[string]any, ttl time.Duration) error {
    return exception.NewError("session backend down", nil, nil)
}

func (instance *failingSessionStorage) Delete(sessionId string) error {
    return exception.NewError("session backend down", nil, nil)
}

func (instance *failingSessionStorage) Close() error {
    return nil
}

func TestKernel_SessionSaveFailureAnswersFiveHundred(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/session-write",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionValue, exists := request.Attributes().Get(RequestAttributeSession)
            if false == exists {
                t.Fatalf("expected the session request attribute to be present")
            }

            sessionInstance := sessionValue.(sessioncontract.Session)
            sessionInstance.Set("key", "value")

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(&failingSessionStorage{})

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/session-write", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected a session-store outage to answer 500 rather than the handler's success, got %d", recorder.Code)
    }

    if "ok" == recorder.Body.String() {
        t.Fatalf("expected the handler's success body to be replaced when its session write was lost")
    }

    if "" != recorder.Header().Get("Set-Cookie") {
        t.Fatalf("expected no session cookie when the session could not be persisted, got %q", recorder.Header().Get("Set-Cookie"))
    }
}

func TestKernel_SessionSaveFailureOnPanicRecoveryPathStillDelivers500(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/session-write-boom",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionValue, _ := request.Attributes().Get(RequestAttributeSession)
            sessionInstance := sessionValue.(sessioncontract.Session)
            sessionInstance.Set("key", "value")

            panic("handler exploded after touching the session")
        },
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(&failingSessionStorage{})

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/session-write-boom", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the recovered 500 to be delivered despite the session-store outage, got %d", recorder.Code)
    }
}

type errorCountingLogger struct {
    errorCount int64
}

func (instance *errorCountingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *errorCountingLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *errorCountingLogger) Info(message string, context loggingcontract.Context) {}

func (instance *errorCountingLogger) Warning(message string, context loggingcontract.Context) {}

func (instance *errorCountingLogger) Error(message string, context loggingcontract.Context) {
    atomic.AddInt64(&instance.errorCount, 1)
}

func (instance *errorCountingLogger) Emergency(message string, context loggingcontract.Context) {}

func TestKernel_HandlerPathResponseDispatchErrorRespectsAlreadyLogged(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/logged-once",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    countingLogger := &errorCountingLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, countingLogger)

    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return exception.NewError("response listener failure", nil, nil)
        },
        0,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/logged-once", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected the response despite the dispatch error, got %d", recorder.Code)
    }

    if 1 != atomic.LoadInt64(&countingLogger.errorCount) {
        t.Fatalf("expected the dispatch error to be logged exactly once on the handler-response path, got %d error logs", atomic.LoadInt64(&countingLogger.errorCount))
    }
}

type closeTrackingReader struct {
    closed atomic.Bool
}

func (instance *closeTrackingReader) Read(buffer []byte) (int, error) {
    return 0, io.EOF
}

func (instance *closeTrackingReader) Close() error {
    instance.closed.Store(true)
    return nil
}

type panicOnceSessionStorage struct {
    panicked atomic.Bool
}

func (instance *panicOnceSessionStorage) Load(sessionId string) (map[string]any, bool, error) {
    return nil, false, nil
}

func (instance *panicOnceSessionStorage) Save(sessionId string, data map[string]any, ttl time.Duration) error {
    if true == instance.panicked.CompareAndSwap(false, true) {
        panic("session backend exploded")
    }

    return nil
}

func (instance *panicOnceSessionStorage) Delete(sessionId string) error {
    return nil
}

func (instance *panicOnceSessionStorage) Close() error {
    return nil
}

func TestKernel_PanicRecoveryClosesTheDiscardedFileBackedResponse(t *testing.T) {
    bodyReader := &closeTrackingReader{}
    storage := &panicOnceSessionStorage{}

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/file",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionValue, exists := request.Attributes().Get(RequestAttributeSession)
            if false == exists {
                t.Fatal("expected the request to carry a session")
            }

            sessionInstance, ok := sessionValue.(sessioncontract.Session)
            if false == ok {
                t.Fatal("expected the session attribute to be a session")
            }

            sessionInstance.Set("key", "value")

            response := &Response{}
            response.SetStatusCode(nethttp.StatusOK)
            response.SetHeaders(make(nethttp.Header))
            response.SetBodyReader(bodyReader)

            return response, nil
        },
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(storage)
    handler := NewKernel(router).ServeHttp(serviceContainer)

    handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/file", nil))

    if false == storage.panicked.Load() {
        t.Fatal("the session backend never panicked; the test does not exercise the recovery path")
    }

    if false == bodyReader.closed.Load() {
        t.Fatal("the discarded file-backed response body was never closed: one file descriptor leaks per request")
    }
}

func TestKernel_AbortHandlerPanicClosesTheConnectionWithoutAResponse(t *testing.T) {
    router := NewRouter()

    router.Handle(
        nethttp.MethodGet,
        "/abort",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            panic(nethttp.ErrAbortHandler)
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    server := httptest.NewServer(handler)
    defer server.Close()

    response, requestErr := server.Client().Get(server.URL + "/abort")
    if nil == requestErr {
        defer response.Body.Close()

        t.Fatalf("expected the connection to be closed without a response, got status %d", response.StatusCode)
    }
}

func TestKernel_PublishesThePolicyResolvedSchemeOnTheRequest(t *testing.T) {
    router := NewRouter()

    observedScheme := ""

    router.Handle(
        nethttp.MethodGet,
        "/scheme",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            if schemeValue, exists := request.Attributes().Get(RequestAttributeScheme); true == exists {
                if schemeString, isString := schemeValue.(string); true == isString {
                    observedScheme = schemeString
                }
            }

            return TextResponse(200, "ok"), nil
        },
    )

    kernelInstance := NewKernel(router)
    kernelInstance.SetForwardedHeadersPolicy(httpcontract.ForwardedHeadersPolicy{
        TrustForwardedHeaders: true,
        TrustedProxyList:      []string{"10.0.0.0/8"},
    })

    handler := kernelInstance.ServeHttp(newHttpTestContainer())

    trustedRequest := httptest.NewRequest(nethttp.MethodGet, "/scheme", nil)
    trustedRequest.RemoteAddr = "10.0.0.7:44321"
    trustedRequest.Header.Set("X-Forwarded-Proto", "https")

    handler.ServeHTTP(httptest.NewRecorder(), trustedRequest)

    if "https" != observedScheme {
        t.Fatalf("expected the trusted proxy scheme to be published, got %q", observedScheme)
    }

    observedScheme = ""

    untrustedRequest := httptest.NewRequest(nethttp.MethodGet, "/scheme", nil)
    untrustedRequest.RemoteAddr = "203.0.113.9:44321"
    untrustedRequest.Header.Set("X-Forwarded-Proto", "https")

    handler.ServeHTTP(httptest.NewRecorder(), untrustedRequest)

    if "http" != observedScheme {
        t.Fatalf("expected an untrusted peer's forwarded scheme to be discarded, got %q", observedScheme)
    }
}

type loadFailingSessionStorage struct{}

func (instance *loadFailingSessionStorage) Load(sessionId string) (map[string]any, bool, error) {
    return nil, false, exception.NewError("session backend down", nil, nil)
}

func (instance *loadFailingSessionStorage) Save(sessionId string, data map[string]any, ttl time.Duration) error {
    return nil
}

func (instance *loadFailingSessionStorage) Delete(sessionId string) error {
    return nil
}

func (instance *loadFailingSessionStorage) Close() error {
    return nil
}

func TestKernel_SessionLoadFailureIsRecoveredIntoAnInternalServerError(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/session-load",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(&loadFailingSessionStorage{})

    terminateCount := 0

    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelTerminate,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            terminateCount = terminateCount + 1

            return nil
        },
        0,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/session-load", nil))

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected a session-store outage to produce a 500, got %d", recorder.Code)
    }

    if 1 != terminateCount {
        t.Fatalf("expected the kernel terminate event to fire once for a failed session load, got %d", terminateCount)
    }
}

func TestKernel_EmitsTheCookieOfTheSessionRepublishedOnTheRequest(t *testing.T) {
    serviceContainer := newHttpTestContainerWithSessionStorage(session.NewInMemoryStorage())
    sessionManager := session.SessionMustFromContainer(serviceContainer)

    republishedId := ""

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/republish",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            replacement := sessionManager.NewSession()
            replacement.Set("userId", "u-1")

            request.Attributes().Set(RequestAttributeSession, replacement)

            republishedId = replacement.Id()

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/republish", nil))

    cookies := recorder.Result().Cookies()
    if 1 != len(cookies) {
        t.Fatalf("expected one session cookie for the republished session, got %d", len(cookies))
    }

    if republishedId != cookies[0].Value {
        t.Fatalf("expected the republished session id %q in the cookie, got %q", republishedId, cookies[0].Value)
    }
}

func TestKernel_RotatedSessionEmitsTheNewIdAndDropsTheOldEntry(t *testing.T) {
    serviceContainer := newHttpTestContainerWithSessionStorage(session.NewInMemoryStorage())
    sessionManager := session.SessionMustFromContainer(serviceContainer)

    preRotationId := ""
    rotatedId := ""

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/login",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionValue, exists := request.Attributes().Get(RequestAttributeSession)
            if false == exists {
                t.Fatal("expected the request to carry a session")
            }

            sessionInstance, ok := sessionValue.(sessioncontract.Session)
            if false == ok {
                t.Fatal("expected the session attribute to be a session")
            }

            sessionInstance.Set("userId", "u-1")

            saveErr := sessionManager.SaveSession(sessionInstance)
            if nil != saveErr {
                return nil, saveErr
            }

            preRotationId = sessionInstance.Id()

            rotated, rotateErr := sessionManager.RegenerateSession(sessionInstance)
            if nil != rotateErr {
                return nil, rotateErr
            }

            request.Attributes().Set(RequestAttributeSession, rotated)

            rotatedId = rotated.Id()

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/login", nil))

    if preRotationId == rotatedId {
        t.Fatalf("expected the rotation to change the session id")
    }

    cookies := recorder.Result().Cookies()
    if 1 != len(cookies) {
        t.Fatalf("expected one session cookie, got %d", len(cookies))
    }

    if rotatedId != cookies[0].Value {
        t.Fatalf("expected the rotated session id %q in the cookie, got %q", rotatedId, cookies[0].Value)
    }

    if 0 != cookies[0].MaxAge {
        t.Fatalf("expected a live session cookie for the republished rotation, got MaxAge %d", cookies[0].MaxAge)
    }

    if nil != sessionManager.Session(preRotationId) {
        t.Fatalf("expected the pre-rotation session to be gone from storage")
    }

    reloaded := sessionManager.Session(rotatedId)
    if nil == reloaded {
        t.Fatalf("expected the rotated session to be stored")
    }

    if "u-1" != reloaded.String("userId") {
        t.Fatalf("expected the values to survive the rotation, got %q", reloaded.String("userId"))
    }
}

func TestKernel_RotationWithoutRepublishingClearsTheAbandonedSession(t *testing.T) {
    serviceContainer := newHttpTestContainerWithSessionStorage(session.NewInMemoryStorage())
    sessionManager := session.SessionMustFromContainer(serviceContainer)

    existingSession := sessionManager.NewSession()
    existingSession.Set("userId", "anonymous")

    saveErr := sessionManager.SaveSession(existingSession)
    if nil != saveErr {
        t.Fatalf("unexpected error: %v", saveErr)
    }

    existingId := existingSession.Id()

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/login-without-republish",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionValue, exists := request.Attributes().Get(RequestAttributeSession)
            if false == exists {
                t.Fatal("expected the request to carry a session")
            }

            sessionInstance, ok := sessionValue.(sessioncontract.Session)
            if false == ok {
                t.Fatal("expected the session attribute to be a session")
            }

            rotated, rotateErr := sessionManager.RegenerateSession(sessionInstance)
            if nil != rotateErr {
                return nil, rotateErr
            }

            rotated.Set("userId", "u-1")

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/login-without-republish", nil)
    request.AddCookie(
        &nethttp.Cookie{
            Name:  session.SessionCookieName,
            Value: existingId,
        },
    )

    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    cookies := recorder.Result().Cookies()
    if 1 != len(cookies) {
        t.Fatalf("expected the abandoned session to be cleared with one cookie, got %d", len(cookies))
    }

    if -1 != cookies[0].MaxAge {
        t.Fatalf("expected a clearing cookie so a forgotten republish logs the client out instead of stranding it on a deleted session, got MaxAge %d", cookies[0].MaxAge)
    }

    if nil != sessionManager.Session(existingId) {
        t.Fatalf("expected the pre-rotation session to be gone from storage")
    }
}

func TestKernel_SessionCookiePolicyWithoutSameSiteKeepsTheLaxDefault(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/session-write",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionValue, exists := request.Attributes().Get(RequestAttributeSession)
            if false == exists {
                t.Fatal("expected the request to carry a session")
            }

            sessionInstance, ok := sessionValue.(sessioncontract.Session)
            if false == ok {
                t.Fatal("expected the session attribute to be a session")
            }

            sessionInstance.Set("key", "value")

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    kernelInstance := NewKernel(router)
    kernelInstance.SetSessionCookiePolicy(
        httpcontract.SessionCookiePolicy{
            Domain: "example.com",
        },
    )

    handler := kernelInstance.ServeHttp(newHttpTestContainer())

    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/session-write", nil))

    cookies := recorder.Result().Cookies()
    if 1 != len(cookies) {
        t.Fatalf("expected one session cookie, got %d", len(cookies))
    }

    if nethttp.SameSiteLaxMode != cookies[0].SameSite {
        t.Fatalf("expected a policy that only names the domain to keep the SameSite=Lax default, got %v", cookies[0].SameSite)
    }

    if "/" != cookies[0].Path {
        t.Fatalf("expected the session cookie path fallback, got %q", cookies[0].Path)
    }
}

func TestKernel_RouteAttributesCannotReplaceTheKernelOwnedAttributes(t *testing.T) {
    router := NewRouter()

    observedScheme := ""
    observedSessionIsSession := false

    router.HandleWithOptions(
        "/owned",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            if schemeValue, exists := request.Attributes().Get(RequestAttributeScheme); true == exists {
                if schemeString, isString := schemeValue.(string); true == isString {
                    observedScheme = schemeString
                }
            }

            sessionValue, exists := request.Attributes().Get(RequestAttributeSession)
            if true == exists {
                _, observedSessionIsSession = sessionValue.(sessioncontract.Session)
            }

            return TextResponse(200, "ok"), nil
        },
        NewRouteOptions(
            "owned",
            []string{nethttp.MethodGet},
            "",
            nil,
            nil,
            nil,
            nil,
            0,
            map[string]any{
                "scheme":                "spoofed",
                "session":               "spoofed",
                RequestAttributeScheme:  "spoofed",
                RequestAttributeSession: "spoofed",
            },
        ),
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/owned", nil))

    if "http" != observedScheme {
        t.Fatalf("expected the kernel-resolved scheme to survive a route attribute, got %q", observedScheme)
    }

    if false == observedSessionIsSession {
        t.Fatalf("expected the kernel-published session to survive a route attribute")
    }
}

func TestKernel_DispatchesResponseEventForHandlerReturningNoResponse(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodDelete,
        "/nothing",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, nil
        },
    )

    serviceContainer := newHttpTestContainer()

    dispatchCount := 0
    observedStatusCode := -1

    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if false == ok {
                return nil
            }

            dispatchCount = dispatchCount + 1

            if nil == responseEvent.Response() {
                return nil
            }

            observedStatusCode = responseEvent.Response().StatusCode()
            responseEvent.Response().Headers().Set("Access-Control-Allow-Origin", "https://example.test")

            return nil
        },
        0,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodDelete, "/nothing", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if 1 != dispatchCount {
        t.Fatalf("expected kernel.response to be dispatched once for the handler that returned no response, got %d dispatches", dispatchCount)
    }

    if nethttp.StatusNoContent != observedStatusCode {
        t.Fatalf("expected the listener to be handed the synthesized status %d, got %d", nethttp.StatusNoContent, observedStatusCode)
    }

    if nethttp.StatusNoContent != recorder.Code {
        t.Fatalf("expected status %d, got %d", nethttp.StatusNoContent, recorder.Code)
    }

    if "https://example.test" != recorder.Header().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected the listener's header to reach the client, got %q", recorder.Header().Get("Access-Control-Allow-Origin"))
    }
}

func TestKernel_ResponseListenerMayReplaceTheSynthesizedEmptyResponse(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/nothing",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, nil
        },
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if false == ok {
                return nil
            }

            responseEvent.SetResponse(TextResponse(nethttp.StatusTeapot, "replaced"))

            return nil
        },
        0,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/nothing", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusTeapot != recorder.Code {
        t.Fatalf("expected the listener-replaced status %d, got %d", nethttp.StatusTeapot, recorder.Code)
    }

    if "replaced" != recorder.Body.String() {
        t.Fatalf("expected the listener-replaced body, got %q", recorder.Body.String())
    }
}


func TestKernel_PanicRecoveryLogsPanicSiteStack(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/boom",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            panic("handler exploded")
        },
    )

    recordingLogger := &errorContextRecordingLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger)

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/boom", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the recovered 500, got %d", recorder.Code)
    }

    context, logged := recordingLogger.errorContextFor("unhandled http error")
    if false == logged {
        t.Fatalf("expected the recovered panic to be logged as an unhandled http error")
    }

    panicStack, hasStack := context["panicStack"].(string)
    if false == hasStack || "" == panicStack {
        t.Fatalf("expected the log record to carry the panic-site stack, got context %v", context)
    }

    if false == strings.Contains(panicStack, "goroutine") {
        t.Fatalf("expected a goroutine stack in the panicStack field, got %q", panicStack)
    }
}


func TestKernel_ErrorHandlerPanicOnHandlerErrorPathClosesBodyAndDelivers500(t *testing.T) {
    bodyReader := &closeTrackingReader{}

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/fail-with-open-body",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            response := &Response{
                statusCode: nethttp.StatusOK,
                headers:    make(nethttp.Header),
                bodyReader: bodyReader,
            }

            return response, exception.NewError("handler failed after opening the body", nil, nil)
        },
    )

    kernel := NewKernel(router)
    kernel.SetErrorHandler(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request, err error) httpcontract.Response {
            panic("error handler exploded")
        },
    )

    handler := kernel.ServeHttp(newHttpTestContainer())

    request := httptest.NewRequest(nethttp.MethodGet, "/fail-with-open-body", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the default 500 despite the error-handler panic, got %d", recorder.Code)
    }

    if false == bodyReader.closed.Load() {
        t.Fatalf("expected the failed handler's response body to be closed despite the error-handler panic")
    }
}

func TestKernel_ErrorHandlerPanicOnNotFoundPathClosesBodyAndDelivers500(t *testing.T) {
    bodyReader := &closeTrackingReader{}

    kernel := NewKernel(NewRouter())
    kernel.SetNotFoundHandler(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            response := &Response{
                statusCode: nethttp.StatusNotFound,
                headers:    make(nethttp.Header),
                bodyReader: bodyReader,
            }

            return response, exception.NewError("not found handler failed after opening the body", nil, nil)
        },
    )
    kernel.SetErrorHandler(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request, err error) httpcontract.Response {
            panic("error handler exploded")
        },
    )

    handler := kernel.ServeHttp(newHttpTestContainer())

    request := httptest.NewRequest(nethttp.MethodGet, "/no-such-route", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the default 500 despite the error-handler panic, got %d", recorder.Code)
    }

    if false == bodyReader.closed.Load() {
        t.Fatalf("expected the failed not-found response body to be closed despite the error-handler panic")
    }
}

func TestKernel_ErrorHandlerPanicOnRecoveryPathClosesBodyAndDelivers500(t *testing.T) {
    bodyReader := &closeTrackingReader{}

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/session-write-then-file",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionValue, _ := request.Attributes().Get(RequestAttributeSession)
            sessionInstance := sessionValue.(sessioncontract.Session)
            sessionInstance.Set("key", "value")

            response := &Response{
                statusCode: nethttp.StatusOK,
                headers:    make(nethttp.Header),
                bodyReader: bodyReader,
            }

            return response, nil
        },
    )

    kernel := NewKernel(router)
    kernel.SetErrorHandler(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request, err error) httpcontract.Response {
            panic("error handler exploded")
        },
    )

    sessionStorage := &panicOnceSessionStorage{}
    handler := kernel.ServeHttp(newHttpTestContainerWithSessionStorage(sessionStorage))

    request := httptest.NewRequest(nethttp.MethodGet, "/session-write-then-file", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the recovered 500 despite the error-handler panic on the recovery path, got %d", recorder.Code)
    }

    if false == bodyReader.closed.Load() {
        t.Fatalf("expected the in-flight response body to be closed despite the error-handler panic on the recovery path")
    }
}


func TestKernel_OversizedUrlencodedFormIsRefusedWith413(t *testing.T) {
    handlerInvoked := false

    router := NewRouter()
    router.Handle(
        nethttp.MethodPost,
        "/form",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            handlerInvoked = true

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    oversizedForm := "value=" + strings.Repeat("a", 2*1024*1024)
    request := httptest.NewRequest(nethttp.MethodPost, "/form", strings.NewReader(oversizedForm))
    request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusRequestEntityTooLarge != recorder.Code {
        t.Fatalf("expected an oversized urlencoded form to be refused with 413, got %d", recorder.Code)
    }

    if true == handlerInvoked {
        t.Fatalf("expected the handler not to run for an oversized urlencoded form")
    }
}

type brokenBodyReader struct {
    served bool
}

func (instance *brokenBodyReader) Read(buffer []byte) (int, error) {
    if false == instance.served && 0 < len(buffer) {
        instance.served = true
        buffer[0] = 'a'

        return 1, nil
    }

    return 0, exception.NewError("client aborted the upload", nil, nil)
}

func TestKernel_BrokenUrlencodedBodyIsRefusedWith400(t *testing.T) {
    handlerInvoked := false

    router := NewRouter()
    router.Handle(
        nethttp.MethodPost,
        "/form",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            handlerInvoked = true

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    request := httptest.NewRequest(nethttp.MethodPost, "/form", &brokenBodyReader{})
    request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusBadRequest != recorder.Code {
        t.Fatalf("expected a broken urlencoded body to be refused with 400, got %d", recorder.Code)
    }

    if true == handlerInvoked {
        t.Fatalf("expected the handler not to run for a broken urlencoded body")
    }
}

type typedNilReturningSessionManager struct {
    delegate sessioncontract.Manager
}

func (instance *typedNilReturningSessionManager) Session(sessionId string) sessioncontract.Session {
    var typedNil *session.Session

    return typedNil
}

func (instance *typedNilReturningSessionManager) NewSession() sessioncontract.Session {
    return instance.delegate.NewSession()
}

func (instance *typedNilReturningSessionManager) RegenerateSession(sessionInstance sessioncontract.Session) (sessioncontract.Session, error) {
    return instance.delegate.RegenerateSession(sessionInstance)
}

func (instance *typedNilReturningSessionManager) SaveSession(sessionInstance sessioncontract.Session) error {
    return instance.delegate.SaveSession(sessionInstance)
}

func (instance *typedNilReturningSessionManager) DeleteSession(sessionId string) error {
    return instance.delegate.DeleteSession(sessionId)
}

func (instance *typedNilReturningSessionManager) Close() error {
    return instance.delegate.Close()
}

func TestKernel_MintsASessionWhenTheManagerAnswersWithATypedNil(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/session",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            attributeValue, exists := request.Attributes().Get(RequestAttributeSession)
            if false == exists {
                return TextResponse(nethttp.StatusInternalServerError, "no session attribute"), nil
            }

            publishedSession, isSession := attributeValue.(sessioncontract.Session)
            if false == isSession {
                return TextResponse(nethttp.StatusInternalServerError, "not a session"), nil
            }

            publishedSession.Set("touched", "yes")

            return TextResponse(nethttp.StatusOK, publishedSession.Id()), nil
        },
    )

    storage := session.NewInMemoryStorage()

    serviceContainer := newHttpTestContainerWithSessionManager(
        &typedNilReturningSessionManager{
            delegate: session.NewManager(storage, 30*time.Minute),
        },
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/session", nil)
    request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})

    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected the request to be served a usable session, got status %d body %q", recorder.Code, recorder.Body.String())
    }

    if "" == strings.TrimSpace(recorder.Body.String()) {
        t.Fatalf("expected the minted session to carry an id")
    }
}

type warningRecordingLogger struct {
    mutex           sync.Mutex
    warningMessages []string
    warningContexts []loggingcontract.Context
}

func (instance *warningRecordingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *warningRecordingLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *warningRecordingLogger) Info(message string, context loggingcontract.Context) {}

func (instance *warningRecordingLogger) Warning(message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.warningMessages = append(instance.warningMessages, message)
    instance.warningContexts = append(instance.warningContexts, context)
}

func (instance *warningRecordingLogger) Error(message string, context loggingcontract.Context) {}

func (instance *warningRecordingLogger) Emergency(message string, context loggingcontract.Context) {
}

func (instance *warningRecordingLogger) hasWarning(message string) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for _, loggedMessage := range instance.warningMessages {
        if message == loggedMessage {
            return true
        }
    }

    return false
}

func (instance *warningRecordingLogger) warningContextFor(message string) (loggingcontract.Context, bool) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for index, loggedMessage := range instance.warningMessages {
        if message == loggedMessage {
            return instance.warningContexts[index], true
        }
    }

    return nil, false
}

func TestKernel_TheNoRouteRecordRedactsQueryValues(t *testing.T) {
    router := NewRouter()

    recordingLogger := &warningRecordingLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger)

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/missing?token=secret123&user=alice", nil)
    handler.ServeHTTP(httptest.NewRecorder(), request)

    warningContext, logged := recordingLogger.warningContextFor("no route matched")
    if false == logged {
        t.Fatalf("expected the unrouted request to leave its warning, got %v", recordingLogger.warningMessages)
    }

    query, _ := warningContext["query"].(string)
    if true == strings.Contains(query, "secret123") || true == strings.Contains(query, "alice") {
        t.Fatalf("expected every query value redacted in the no-route record, got %q", query)
    }
    if false == strings.Contains(query, "token") || false == strings.Contains(query, "user") {
        t.Fatalf("expected the query parameter names kept in the no-route record, got %q", query)
    }
}

func TestKernel_TheMethodNotAllowedRecordRedactsQueryValues(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodPost,
        "/only-post",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "posted"), nil
        },
    )

    recordingLogger := &warningRecordingLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger)

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/only-post?token=secret123", nil)
    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, request)

    if nethttp.StatusMethodNotAllowed != recorder.Code {
        t.Fatalf("expected the wrong method answered 405, got %d", recorder.Code)
    }

    warningContext, logged := recordingLogger.warningContextFor("method not allowed")
    if false == logged {
        t.Fatalf("expected the refused method to leave its warning, got %v", recordingLogger.warningMessages)
    }

    query, _ := warningContext["query"].(string)
    if true == strings.Contains(query, "secret123") {
        t.Fatalf("expected the query value redacted in the 405 record, got %q", query)
    }
    if false == strings.Contains(query, "token") {
        t.Fatalf("expected the query parameter name kept in the 405 record, got %q", query)
    }
}

func TestKernel_LogsWhenACommittedResponseDropsARotatedSession(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/stream",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            writer.WriteHeader(nethttp.StatusOK)
            _, _ = writer.Write([]byte("streamed"))

            if _, err := RegenerateRequestSession(request); nil != err {
                return nil, err
            }

            return nil, nil
        },
    )

    recordingLogger := &warningRecordingLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger)

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/stream", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if false == recordingLogger.hasWarning("session not persisted: the response was already committed and the request does not name this session") {
        t.Fatalf("expected the dropped session write to be logged, got warnings %v", recordingLogger.warningMessages)
    }
}

func TestKernel_ServesTheResponseOfAStoppingListenerWhenNothingRequiredWasSkipped(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/hello",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "handler"), nil
        },
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*KernelRequestEvent)
            if false == ok {
                return nil
            }

            requestEvent.SetResponse(TextResponse(nethttp.StatusOK, "from the cache"))
            eventValue.StopPropagation()

            return nil
        },
        100,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/hello", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected %d, got %d", nethttp.StatusOK, recorder.Code)
    }

    if "from the cache" != recorder.Body.String() {
        t.Fatalf("expected the stopping listener's response, got %q", recorder.Body.String())
    }
}

type noMatchRouter struct {
    *Router
}

func (instance *noMatchRouter) Match(method string, path string, host string, scheme string) (*httpcontract.MatchResult, bool) {
    return nil, false
}

func TestKernel_ServeHttpAnswersARouterThatReportsNoMatchWithANilResult(t *testing.T) {
    router := &noMatchRouter{Router: NewRouter()}

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    request := httptest.NewRequest(nethttp.MethodGet, "/anything", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusNotFound != recorder.Code {
        t.Fatalf("expected %d, got %d", nethttp.StatusNotFound, recorder.Code)
    }
}

func TestKernel_ServeHttpAnswersARequestWhoseTerminateListenerPanics(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/hello",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "handler"), nil
        },
    )

    serviceContainer := newHttpTestContainer()

    event.EventDispatcherMustFromContainer(serviceContainer).AddListener(
        kernelcontract.EventKernelTerminate,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            panic("terminate listener")
        },
        0,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/hello", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected %d, got %d", nethttp.StatusOK, recorder.Code)
    }
}

func TestKernel_ServeHttpAnswersATypedNilResponseFromAHandlerWithTheEmptyDefault(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/hello",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            var unassignedResponse *Response

            return unassignedResponse, nil
        },
    )

    serviceContainer := newHttpTestContainer()

    responseSeenByListener := false

    event.EventDispatcherMustFromContainer(serviceContainer).AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if false == ok {
                return nil
            }

            responseSeenByListener = nil != responseEvent.Response()

            return nil
        },
        0,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/hello", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusNoContent != recorder.Code {
        t.Fatalf("expected %d, got %d", nethttp.StatusNoContent, recorder.Code)
    }

    if false == responseSeenByListener {
        t.Fatalf("expected the empty default to reach the response listener, it saw no response at all")
    }
}

type explodingError struct{}

func (instance *explodingError) Error() string {
    panic("the error cannot render itself")
}

func TestKernel_ServeHttpClosesTheChainResponseWhenTheErrorLogPanics(t *testing.T) {
    bodyReader := &closeRecordingReadCloser{}

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/hello",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            response := EmptyResponse(nethttp.StatusOK)
            response.SetBodyReader(bodyReader)

            return response, &explodingError{}
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    request := httptest.NewRequest(nethttp.MethodGet, "/hello", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if 0 == bodyReader.closeCount {
        t.Fatalf("expected the response the chain produced to be closed by the recovery, it was never seen")
    }
}

type failingSaveSessionManager struct {
    inner sessioncontract.Manager
}

func (instance *failingSaveSessionManager) Session(sessionId string) sessioncontract.Session {
    return instance.inner.Session(sessionId)
}

func (instance *failingSaveSessionManager) NewSession() sessioncontract.Session {
    return instance.inner.NewSession()
}

func (instance *failingSaveSessionManager) RegenerateSession(sessionInstance sessioncontract.Session) (sessioncontract.Session, error) {
    return instance.inner.RegenerateSession(sessionInstance)
}

func (instance *failingSaveSessionManager) SaveSession(sessionInstance sessioncontract.Session) error {
    return exception.NewError("session backend is down", nil, nil)
}

func (instance *failingSaveSessionManager) DeleteSession(sessionId string) error {
    return instance.inner.DeleteSession(sessionId)
}

func (instance *failingSaveSessionManager) Close() error {
    return instance.inner.Close()
}

func TestKernel_ASessionSaveOutageReachesTerminateAsTheFiveHundredTheClientReceived(t *testing.T) {
    manager := &failingSaveSessionManager{
        inner: session.NewManager(session.NewInMemoryStorage(), 30*time.Minute),
    }

    serviceContainer := newHttpTestContainerWithSessionManager(manager)

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/login",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionValue, exists := request.Attributes().Get(RequestAttributeSession)
            if false == exists {
                t.Fatalf("expected the kernel to publish a session on the request")
            }

            sessionInstance := sessionValue.(sessioncontract.Session)
            sessionInstance.Set("identity", "someone")

            return TextResponse(nethttp.StatusOK, "welcome"), nil
        },
    )

    terminateStatus := -1
    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelTerminate,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            terminateEvent, ok := eventValue.Payload().(*KernelTerminateEvent)
            if false == ok || nil == terminateEvent.Response() {
                return nil
            }

            terminateStatus = terminateEvent.Response().StatusCode()

            return nil
        },
        0,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/login", nil))

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the save outage to answer the client 500, got %d", recorder.Code)
    }

    if nethttp.StatusInternalServerError != terminateStatus {
        t.Fatalf("expected kernel.terminate to see the 500 the client received, got %d", terminateStatus)
    }
}

func TestKernel_AnOuterMiddlewarePanicAfterNextClosesTheChainResponse(t *testing.T) {
    bodyReader := &closeRecordingReadCloser{}

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/stream",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            response := EmptyResponse(nethttp.StatusOK)
            response.SetBodyReader(bodyReader)

            return response, nil
        },
    )

    panickingOuterMiddleware := func(next httpcontract.Handler) httpcontract.Handler {
        return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            response, nextErr := next(runtimeInstance, writer, request)
            _ = response
            _ = nextErr

            panic(exception.NewError("outer middleware failed after the handler answered", nil, nil))
        }
    }

    kernelInstance := NewKernel(router)
    kernelInstance.Use(panickingOuterMiddleware)
    handler := kernelInstance.ServeHttp(newHttpTestContainer())

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/stream", nil))

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the panic to answer 500, got %d", recorder.Code)
    }

    if 1 != bodyReader.closeCount {
        t.Fatalf("expected the recovery defer to close the chain response exactly once, got %d", bodyReader.closeCount)
    }
}

func TestKernel_HasErrorHandlerReportsInstallation(t *testing.T) {
    kernel := NewKernel(NewRouter())

    if true == kernel.HasErrorHandler() {
        t.Fatalf("expected no error handler on a fresh kernel")
    }

    kernel.SetErrorHandler(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request, err error) httpcontract.Response {
            return nil
        },
    )

    if false == kernel.HasErrorHandler() {
        t.Fatalf("expected the installed error handler to be reported")
    }
}

func TestKernel_ErrorResponseCarriesTheRequestIdOnce(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/fail",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, exception.NewError("kernel test failure", nil, nil)
        },
    )
    router.Handle(
        nethttp.MethodGet,
        "/ok",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    serviceContainer := newHttpTestContainer()
    RegisterKernelExceptionListener(event.EventDispatcherMustFromContainer(serviceContainer), false)

    handler := NewKernel(router).ServeHttp(serviceContainer)

    errorRecorder := httptest.NewRecorder()
    handler.ServeHTTP(errorRecorder, httptest.NewRequest(nethttp.MethodGet, "/fail", nil))

    errorValues := errorRecorder.Result().Header.Values(HeaderRequestId)
    if 1 != len(errorValues) || "" == errorValues[0] {
        t.Fatalf("expected exactly one request id on the error response, got %v", errorValues)
    }

    successRecorder := httptest.NewRecorder()
    handler.ServeHTTP(successRecorder, httptest.NewRequest(nethttp.MethodGet, "/ok", nil))

    successValues := successRecorder.Result().Header.Values(HeaderRequestId)
    if 1 != len(successValues) || "" == successValues[0] {
        t.Fatalf("expected exactly one request id on the success response, got %v", successValues)
    }
}

func TestLogHandlerError_ADeliberate4xxFilesOneWarningAndMarksTheError(t *testing.T) {
    capture := &exceptionListenerCaptureLogger{}
    handlerErr := exception.TooManyRequests("rate limit exceeded")

    logHandlerError(capture, "controller handler error", handlerErr, httptest.NewRequest(nethttp.MethodGet, "/limited", nil))

    if 1 != capture.warningCalls || 0 != capture.errorCalls {
        t.Fatalf("expected one warning and no error, got %d warnings %d errors", capture.warningCalls, capture.errorCalls)
    }

    if false == exception.IsAlreadyLogged(handlerErr) {
        t.Fatal("expected the record to mark the error, so the exception listener does not file it again")
    }
}

func TestLogHandlerError_AServerFaultKeepsTheErrorLevel(t *testing.T) {
    capture := &exceptionListenerCaptureLogger{}

    logHandlerError(capture, "controller handler error", exception.NewError("boom", nil, nil), httptest.NewRequest(nethttp.MethodGet, "/broken", nil))

    if 1 != capture.errorCalls || 0 != capture.warningCalls {
        t.Fatalf("expected one error and no warning, got %d errors %d warnings", capture.errorCalls, capture.warningCalls)
    }
}

func TestLogHandlerError_AnAlreadyLoggedErrorFilesNothing(t *testing.T) {
    capture := &exceptionListenerCaptureLogger{}
    handlerErr := exception.NewError("boom", nil, nil)
    _ = exception.MarkLogged(handlerErr)

    logHandlerError(capture, "controller handler error", handlerErr, httptest.NewRequest(nethttp.MethodGet, "/broken", nil))

    if 0 != capture.errorCalls || 0 != capture.warningCalls {
        t.Fatalf("expected no record for an already-logged failure, got %d errors %d warnings", capture.errorCalls, capture.warningCalls)
    }
}

func TestLogHandlerError_TheRequestContextsOwnCancellationIsAWarningNamingTheClient(t *testing.T) {
    capture := &exceptionListenerCaptureLogger{}

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()
    request := httptest.NewRequest(nethttp.MethodGet, "/slow", nil).WithContext(cancelledContext)

    logHandlerError(capture, "controller handler error", context.Canceled, request)

    if 1 != capture.warningCalls || 0 != capture.errorCalls {
        t.Fatalf("expected one warning and no error, got %d warnings %d errors", capture.warningCalls, capture.errorCalls)
    }

    if "request cancelled by client" != capture.lastMessage {
        t.Fatalf("expected the record to name the client's disconnect, got %q", capture.lastMessage)
    }
}

func TestLogHandlerError_ACancellationWithALiveRequestContextStaysAnError(t *testing.T) {
    capture := &exceptionListenerCaptureLogger{}

    logHandlerError(capture, "controller handler error", context.Canceled, httptest.NewRequest(nethttp.MethodGet, "/slow", nil))

    if 1 != capture.errorCalls || 0 != capture.warningCalls {
        t.Fatalf("expected one error and no warning, got %d errors %d warnings", capture.errorCalls, capture.warningCalls)
    }
}

func TestSetForwardedHeadersPolicy_CopiesTheTrustedProxyList(t *testing.T) {
    kernel := NewKernel(NewRouter())

    trustedProxyList := []string{"10.0.0.0/8"}
    kernel.SetForwardedHeadersPolicy(httpcontract.ForwardedHeadersPolicy{
        TrustForwardedHeaders: true,
        TrustedProxyList:      trustedProxyList,
    })

    trustedProxyList[0] = "0.0.0.0/0"

    if "10.0.0.0/8" != kernel.options.ForwardedHeadersPolicy.TrustedProxyList[0] {
        t.Fatalf("expected the kernel to keep its own copy of the trusted list, got %q", kernel.options.ForwardedHeadersPolicy.TrustedProxyList[0])
    }
}

type recordCountingLogger struct {
    loggingcontract.Logger

    mutex         sync.Mutex
    errorMessages []string
    lastContext   loggingcontract.Context
}

func (instance *recordCountingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *recordCountingLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *recordCountingLogger) Info(message string, context loggingcontract.Context) {}

func (instance *recordCountingLogger) Warning(message string, context loggingcontract.Context) {}

func (instance *recordCountingLogger) Error(message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.errorMessages = append(instance.errorMessages, message)
    instance.lastContext = context
}

func (instance *recordCountingLogger) Emergency(message string, context loggingcontract.Context) {}

func serveAndCountErrorRecords(t *testing.T, handler httpcontract.Handler) *recordCountingLogger {
    t.Helper()

    router := NewRouter()
    router.Handle(nethttp.MethodGet, "/subject", handler)

    countingLogger := &recordCountingLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, countingLogger)
    RegisterKernelExceptionListener(event.EventDispatcherMustFromContainer(serviceContainer), false)

    NewKernel(router).
        ServeHttp(serviceContainer).
        ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/subject", nil))

    return countingLogger
}

func TestKernel_AForeignHandlerErrorFilesOneRecordNotTwo(t *testing.T) {
    countingLogger := serveAndCountErrorRecords(
        t,
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, errors.New("plain handler failure")
        },
    )

    if 1 != len(countingLogger.errorMessages) {
        t.Fatalf("expected one error record for one handler failure, got %v", countingLogger.errorMessages)
    }
}

func TestKernel_ARuntimePanicFilesOneRecordNotTwo(t *testing.T) {
    countingLogger := serveAndCountErrorRecords(
        t,
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            var nilMap map[string]string
            nilMap["boom"] = "boom"

            return nil, nil
        },
    )

    if 1 != len(countingLogger.errorMessages) {
        t.Fatalf("expected one error record for one runtime panic, got %v", countingLogger.errorMessages)
    }
}

func TestKernel_APanicWithAnAlreadyLoggedErrorFilesNoSecondRecord(t *testing.T) {
    handlerRan := false

    countingLogger := serveAndCountErrorRecords(
        t,
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            handlerRan = true

            markedErr := exception.NewError("already recorded by the handler", nil, nil)
            _ = exception.MarkLogged(markedErr)

            panic(markedErr)
        },
    )

    if false == handlerRan {
        t.Fatalf("expected the request to reach the handler that panics")
    }

    if 0 != len(countingLogger.errorMessages) {
        t.Fatalf("expected no record for a panic carrying an already-logged error, got %v", countingLogger.errorMessages)
    }
}

func TestKernel_AMelodyHandlerErrorStillFilesOneRecord(t *testing.T) {
    countingLogger := serveAndCountErrorRecords(
        t,
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, exception.NewError("melody handler failure", nil, nil)
        },
    )

    if 1 != len(countingLogger.errorMessages) {
        t.Fatalf("expected one error record, got %v", countingLogger.errorMessages)
    }
}

func TestLogHandlerError_TheRecordNamesTheMethod(t *testing.T) {
    countingLogger := &recordCountingLogger{}

    logHandlerError(
        countingLogger,
        "controller handler error",
        errors.New("failure"),
        httptest.NewRequest(nethttp.MethodPatch, "/articles/7", nil),
    )

    if nethttp.MethodPatch != countingLogger.lastContext["method"] {
        t.Fatalf("expected the record to name the method, got %v", countingLogger.lastContext["method"])
    }

    if "/articles/7" != countingLogger.lastContext["path"] {
        t.Fatalf("expected the record to name the path, got %v", countingLogger.lastContext["path"])
    }
}

func TestKernel_SetMethodPolicyReachesTheAutomaticOptionsAnswer(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/articles",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "articles"), nil
        },
    )

    serviceContainer := newHttpTestContainer()

    kernelInstance := NewKernel(router)

    policy := DefaultKernelOptions().MethodPolicy
    policy.AutomaticOptions = false
    kernelInstance.SetMethodPolicy(policy)

    handler := kernelInstance.ServeHttp(serviceContainer)

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodOptions, "/articles", nil))

    if nethttp.StatusMethodNotAllowed != recorder.Code {
        t.Fatalf("expected the policy to refuse the unrouted OPTIONS with %d, got %d", nethttp.StatusMethodNotAllowed, recorder.Code)
    }

    if true == strings.Contains(recorder.Header().Get("Allow"), nethttp.MethodOptions) {
        t.Fatalf("expected Allow to stop advertising the method the policy no longer answers, got %q", recorder.Header().Get("Allow"))
    }
}

func TestKernel_MethodPolicyKeepsTheAutomaticOptionsAnswerByDefault(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/articles",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "articles"), nil
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodOptions, "/articles", nil))

    if nethttp.StatusNoContent != recorder.Code {
        t.Fatalf("expected the shipped policy to answer OPTIONS with %d, got %d", nethttp.StatusNoContent, recorder.Code)
    }
}

type kernelHandlerErrorCaptureLogger struct {
    loggingcontract.Logger
    warningMessages []string
    errorMessages   []string
}

func (instance *kernelHandlerErrorCaptureLogger) Warning(message string, context loggingcontract.Context) {
    instance.warningMessages = append(instance.warningMessages, message)
}

func (instance *kernelHandlerErrorCaptureLogger) Error(message string, context loggingcontract.Context) {
    instance.errorMessages = append(instance.errorMessages, message)
}

func TestLogHandlerError_AGenuineFieldRefusalStaysAtWarning(t *testing.T) {
    capture := &kernelHandlerErrorCaptureLogger{Logger: logging.NewNopLogger()}

    httpException := exception.BadRequest("validation failed")
    httpException.SetContext(map[string]any{
        "errors": validation.ValidationErrors{
            validation.NewValidationError("email", "this field is required", validation.ConstraintNotBlankErrorIsBlank, nil),
        },
    })

    logHandlerError(capture, "controller handler error", httpException, httptest.NewRequest("POST", "/api/subscribe", nil))

    if 1 != len(capture.warningMessages) || 0 != len(capture.errorMessages) {
        t.Fatalf("expected the field refusal at warning, got warnings %v and errors %v", capture.warningMessages, capture.errorMessages)
    }
}

func TestKernel_OpenRequestScopesRisesWhileARequestIsServedAndFallsWhenItsScopeCloses(t *testing.T) {
    handlerEntered := make(chan struct{})
    releaseHandler := make(chan struct{})

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/slow",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            close(handlerEntered)
            <-releaseHandler

            return TextResponse(nethttp.StatusOK, "served"), nil
        },
    )

    serviceContainer := newHttpTestContainer()

    kernelInstance := NewKernel(router)

    if 0 != kernelInstance.OpenRequestScopes() {
        t.Fatalf("expected no open scope before anything is served, got %d", kernelInstance.OpenRequestScopes())
    }

    handler := kernelInstance.ServeHttp(serviceContainer)

    served := make(chan struct{})
    go func() {
        defer close(served)

        handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/slow", nil))
    }()

    <-handlerEntered

    if 1 != kernelInstance.OpenRequestScopes() {
        t.Fatalf("expected one open scope while the handler runs, got %d", kernelInstance.OpenRequestScopes())
    }

    close(releaseHandler)
    <-served

    if 0 != kernelInstance.OpenRequestScopes() {
        t.Fatalf("expected the scope to be released when the request returned, got %d", kernelInstance.OpenRequestScopes())
    }
}

func TestKernel_OpenRequestScopesIsReleasedWhenTheHandlerPanics(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/boom",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            panic("boom")
        },
    )

    kernelInstance := NewKernel(router)

    handler := kernelInstance.ServeHttp(newHttpTestContainer())

    handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/boom", nil))

    if 0 != kernelInstance.OpenRequestScopes() {
        t.Fatalf("expected the panicking request to release its scope, got %d", kernelInstance.OpenRequestScopes())
    }
}

func TestKernel_RefusesNonCanonicalRequestPathBeforeTheHandler(t *testing.T) {
    handlerRan := false

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/admin/*path...",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            handlerRan = true
            return TextResponse(nethttp.StatusOK, "admin"), nil
        },
    )

    serviceContainer := newHttpTestContainer()
    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/admin/x/../../login", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusBadRequest != recorder.Code {
        t.Fatalf("expected a non-canonical path to be refused with %d, got %d", nethttp.StatusBadRequest, recorder.Code)
    }

    if true == handlerRan {
        t.Fatalf("the handler ran for a non-canonical path that should have been refused before routing to it")
    }
}

func TestKernel_RefusesAWhitespacePaddedRequestPathBeforeTheHandler(t *testing.T) {
    for _, rawPath := range []string{"/admin%20", "/admin%09", "/admin%C2%A0"} {
        handlerRan := false

        router := NewRouter()
        router.Handle(
            nethttp.MethodGet,
            "/*path...",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                handlerRan = true
                return TextResponse(nethttp.StatusOK, "admin"), nil
            },
        )

        serviceContainer := newHttpTestContainer()
        handler := NewKernel(router).ServeHttp(serviceContainer)

        request := httptest.NewRequest(nethttp.MethodGet, rawPath, nil)
        recorder := httptest.NewRecorder()

        handler.ServeHTTP(recorder, request)

        if nethttp.StatusBadRequest != recorder.Code {
            t.Fatalf("expected %q to be refused with %d, got %d", rawPath, nethttp.StatusBadRequest, recorder.Code)
        }

        if true == handlerRan {
            t.Fatalf("the handler ran for %q, which should have been refused before routing to it", rawPath)
        }
    }
}

func TestKernel_RefusesALeadingWhitespacePathAHandlerInFrontHandedIt(t *testing.T) {
    for _, rawPath := range []string{"/api%20/public", "/api%09/public", "/api%C2%A0/public"} {
        handlerRan := false

        router := NewRouter()
        router.Handle(
            nethttp.MethodGet,
            "/*path...",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                handlerRan = true
                return TextResponse(nethttp.StatusOK, "public"), nil
            },
        )

        serviceContainer := newHttpTestContainer()
        handler := nethttp.StripPrefix("/api", NewKernel(router).ServeHttp(serviceContainer))

        request := httptest.NewRequest(nethttp.MethodGet, rawPath, nil)
        recorder := httptest.NewRecorder()

        handler.ServeHTTP(recorder, request)

        if nethttp.StatusBadRequest != recorder.Code {
            t.Fatalf("expected %q to be refused with %d once the prefix was stripped, got %d", rawPath, nethttp.StatusBadRequest, recorder.Code)
        }

        if true == handlerRan {
            t.Fatalf("the handler ran for %q, which should have been refused before routing to it", rawPath)
        }
    }
}

func TestKernel_AsksTheCanonicalQuestionOfThePathAsSpelled(t *testing.T) {
    for rawPath, expected := range map[string]struct {
        code    int
        catchAll bool
    }{
        "/a%2F..%2Fb": {code: nethttp.StatusOK, catchAll: true},
        "/public%2F":  {code: nethttp.StatusOK, catchAll: true},
        "/public":     {code: nethttp.StatusOK, catchAll: false},
        "/a/../b":     {code: nethttp.StatusBadRequest, catchAll: false},
    } {
        reached := ""

        router := NewRouter()
        router.Handle(
            nethttp.MethodGet,
            "/public",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                reached = "public"
                return TextResponse(nethttp.StatusOK, "public"), nil
            },
        )
        router.Handle(
            nethttp.MethodGet,
            "/*path...",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                reached = "catch-all"
                return TextResponse(nethttp.StatusOK, "catch-all"), nil
            },
        )

        handler := NewKernel(router).ServeHttp(newHttpTestContainer())

        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, rawPath, nil))

        if expected.code != recorder.Code {
            t.Fatalf("expected %q to answer %d, got %d", rawPath, expected.code, recorder.Code)
        }

        if expected.catchAll != ("catch-all" == reached) {
            t.Fatalf("expected %q to reach the catch-all: %v, reached %q", rawPath, expected.catchAll, reached)
        }
    }
}

func TestKernel_ServesCanonicalRequestPathThroughToTheHandler(t *testing.T) {
    handlerRan := false

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/admin/*path...",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            handlerRan = true
            return TextResponse(nethttp.StatusOK, "admin"), nil
        },
    )

    serviceContainer := newHttpTestContainer()
    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/admin/secret", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected a canonical path to reach the handler with %d, got %d", nethttp.StatusOK, recorder.Code)
    }

    if false == handlerRan {
        t.Fatalf("the handler did not run for a canonical path")
    }
}

func TestNormalizeBodyLimitError_MapsMaxBytesErrorTo413(t *testing.T) {
    maxBytesError := &nethttp.MaxBytesError{Limit: 1048576}

    normalized := normalizeBodyLimitError(maxBytesError)

    httpException := exception.AsHttpException(normalized)
    if nil == httpException {
        t.Fatalf("expected the max-bytes error to become an HttpException, got %T", normalized)
    }
    if nethttp.StatusRequestEntityTooLarge != httpException.StatusCode() {
        t.Fatalf("expected status %d, got %d", nethttp.StatusRequestEntityTooLarge, httpException.StatusCode())
    }
    if false == errors.Is(normalized, maxBytesError) {
        t.Fatalf("expected the original max-bytes error to stay in the cause chain")
    }
}

func TestNormalizeBodyLimitError_LeavesOtherErrorsUntouched(t *testing.T) {
    plainErr := errors.New("some handler failure")

    if plainErr != normalizeBodyLimitError(plainErr) {
        t.Fatalf("expected a non-body-limit error to be returned unchanged")
    }

    if nil != normalizeBodyLimitError(nil) {
        t.Fatalf("expected nil to stay nil")
    }
}

func TestKernel_RefusesAMalformedTrustedProxyEntry(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewKernel(NewRouter()).SetForwardedHeadersPolicy(httpcontract.ForwardedHeadersPolicy{
                TrustForwardedHeaders: true,
                TrustedProxyList:      []string{"10.0.0.0/8", "10.0.0"},
            })
        },
        "trusted proxy entry is neither a CIDR prefix nor an address",
    )
}

func TestKernel_RefusesEveryConfigurationDoorOnceItStartedServing(t *testing.T) {
    for _, testCase := range []struct {
        door   string
        mutate func(kernel *Kernel)
    }{
        {"Use", func(kernel *Kernel) {
            kernel.Use(func(next httpcontract.Handler) httpcontract.Handler { return next })
        }},
        {"SetNotFoundHandler", func(kernel *Kernel) { kernel.SetNotFoundHandler(nil) }},
        {"SetErrorHandler", func(kernel *Kernel) { kernel.SetErrorHandler(nil) }},
        {"SetForwardedHeadersPolicy", func(kernel *Kernel) {
            kernel.SetForwardedHeadersPolicy(httpcontract.ForwardedHeadersPolicy{})
        }},
        {"SetSessionCookiePolicy", func(kernel *Kernel) {
            kernel.SetSessionCookiePolicy(httpcontract.SessionCookiePolicy{})
        }},
        {"SetMethodPolicy", func(kernel *Kernel) { kernel.SetMethodPolicy(httpcontract.MethodPolicy{}) }},
    } {
        kernel := NewKernel(NewRouter())

        testCase.mutate(kernel)

        _ = kernel.ServeHttp(newHttpTestContainer())

        testhelper.AssertPanicsWithError(t, func() {
            testCase.mutate(kernel)
        }, "may not configure the http kernel after it started serving")
    }
}

func TestKernel_RefusesARouteRegisteredAfterItStartedServing(t *testing.T) {
    router := NewRouter()
    kernel := NewKernel(router)

    router.Handle(nethttp.MethodGet, "/before", routeRegistryTestHandler())

    _ = kernel.ServeHttp(newHttpTestContainer())

    testhelper.AssertPanicsWithError(t, func() {
        router.Handle(nethttp.MethodGet, "/after", routeRegistryTestHandler())
    }, "may not register a route after the http kernel started serving")
}

func TestKernel_LeavesTheRouteTableReadableAfterItStartedServing(t *testing.T) {
    router := NewRouter()
    kernel := NewKernel(router)

    router.HandleNamed("article.show", nethttp.MethodGet, "/articles/:id", routeRegistryTestHandler())

    _ = kernel.ServeHttp(newHttpTestContainer())

    if 1 != len(router.RouteDefinitions()) {
        t.Fatalf("expected the route table to stay readable while serving")
    }

    if _, found := router.RouteDefinition("article.show"); false == found {
        t.Fatalf("expected a named route to stay resolvable while serving")
    }
}

func TestKernel_ServesWhileAConfigurationDoorIsRefused(t *testing.T) {
    router := NewRouter()
    router.Handle(nethttp.MethodGet, "/slow", func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        return NewResponse(nethttp.StatusOK, []byte("ok")), nil
    })

    kernel := NewKernel(router)
    handler := kernel.ServeHttp(newHttpTestContainer())

    started := make(chan struct{})
    finished := make(chan struct{})

    go func() {
        defer close(finished)
        close(started)

        for index := 0; index < 50; index++ {
            recorder := httptest.NewRecorder()
            handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/slow", nil))
        }
    }()

    <-started

    for index := 0; index < 50; index++ {
        func() {
            defer func() {
                if nil == recover() {
                    t.Errorf("expected the configuration door to refuse while requests are in flight")
                }
            }()

            kernel.Use(func(next httpcontract.Handler) httpcontract.Handler { return next })
        }()
    }

    <-finished
}

func TestKernel_AbortHandlerPanicStillClosesTheResponseInFlight(t *testing.T) {
    bodyReader := &closeTrackingReader{}

    router := NewRouter()

    router.Handle(
        nethttp.MethodGet,
        "/file",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            response := &Response{}
            response.SetStatusCode(nethttp.StatusOK)
            response.SetHeaders(make(nethttp.Header))
            response.SetBodyReader(bodyReader)

            return response, nil
        },
    )

    kernel := NewKernel(router)

    kernel.Use(func(next httpcontract.Handler) httpcontract.Handler {
        return func(
            runtimeInstance runtimecontract.Runtime,
            writer nethttp.ResponseWriter,
            request httpcontract.Request,
        ) (httpcontract.Response, error) {
            _, _ = next(runtimeInstance, writer, request)

            panic(nethttp.ErrAbortHandler)
        }
    })

    handler := kernel.ServeHttp(newHttpTestContainer())

    func() {
        defer func() {
            recovered := recover()
            if nethttp.ErrAbortHandler != recovered {
                t.Fatalf("expected the abort sentinel to keep travelling, got %v", recovered)
            }
        }()

        handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/file", nil))
    }()

    if false == bodyReader.closed.Load() {
        t.Fatal("the file-backed response in flight was never closed on the abort path: one descriptor leaks per aborted request")
    }
}

func TestKernel_MatchesTheRouteOnThePathAsSpelled(t *testing.T) {
    reached := ""

    router := NewRouter()
    router.Handle(nethttp.MethodGet, "/admin/users", func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        reached = "two-segment"

        return NewResponse(nethttp.StatusOK, []byte("ok")), nil
    })
    router.Handle(nethttp.MethodGet, "/:name", func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        reached = "one-segment:" + request.Params()["name"]

        return NewResponse(nethttp.StatusOK, []byte("ok")), nil
    })

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/admin%2Fusers", nil)
    handler.ServeHTTP(httptest.NewRecorder(), httpRequest)

    if "one-segment:admin/users" != reached {
        t.Fatalf("the encoded separator was read as a path separator: reached %q", reached)
    }
}

func TestRouter_EqualPriorityIsWonByTheFirstRegistration(t *testing.T) {
    router := NewRouter()

    router.HandleNamed("first", nethttp.MethodGet, "/users/:id", routeRegistryTestHandler())
    router.HandleNamed("second", nethttp.MethodGet, "/users/new", routeRegistryTestHandler())

    _, params, attributes := router.match(nethttp.MethodGet, "/users/new", "", "https")

    if "new" != params["id"] {
        t.Fatalf("expected the first registration to win the tie, got params %v", params)
    }

    if "first" != attributes[RouteAttributeName] {
        t.Fatalf("expected the first registration to win the tie, got route %v", attributes[RouteAttributeName])
    }
}

func TestKernel_RefusesWhitespacePathBeforeTheHandler(t *testing.T) {
    handlerRan := false

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/admin/*path...",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            handlerRan = true
            return TextResponse(nethttp.StatusOK, "admin"), nil
        },
    )

    serviceContainer := newHttpTestContainer()
    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/admin/secret%20", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusBadRequest != recorder.Code {
        t.Fatalf("expected a non-canonical path to be refused with %d, got %d", nethttp.StatusBadRequest, recorder.Code)
    }

    if true == handlerRan {
        t.Fatalf("the handler ran for a non-canonical path that should have been refused before routing to it")
    }
}
