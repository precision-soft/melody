package http

import (
    "context"
    "errors"
    "fmt"
    "io"
    "math"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/cache"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/precision-soft/melody/v4/serializer"
    serializercontract "github.com/precision-soft/melody/v4/serializer/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
    "github.com/precision-soft/melody/v4/validation"
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

    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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
    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/file-then-swap", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    /* an EventKernelResponse listener swapped the response, so the original file-backed body must be closed rather than leaked */
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
    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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
    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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
    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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
    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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
        Storage: session.NewInMemoryStorage(session.InMemoryStorageConfig{}),
    }

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/save",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatal("expected the request to carry a session")
            }

            sessionInstance.Set("key", "value")

            return TextResponse(nethttp.StatusOK, "body"), nil
        },
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(storage)
    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/save", nil)

    /* the write fails after the headers were committed, so the first writeResponse persists the session and then panics, and the panic-recovery path re-enters writeResponse. */
    handler.ServeHTTP(&writeFailingResponseWriter{}, request)

    if 1 != storage.saveCount {
        t.Fatalf("expected the session to be persisted exactly once across the first write and the panic-recovery write, got %d", storage.saveCount)
    }
}

/* the early guard has to sit between the terminate guard and the scope close, which the status read at close time proves */
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

    handler := NewKernel(NewRouter(), KernelConfig{}).ServeHttp(recordingContainer)

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

    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)

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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)

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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)

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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/denied", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusUnauthorized != recorder.Code {
        t.Fatalf("expected the listener-set 401 to win over the synthesized 500, got %d", recorder.Code)
    }
}

/* a required listener sits behind the one that stops and answers, so the kernel must drop that response for the error page; it tells this case from the neighbouring one by the type of the dispatch error, not by whether a response exists */
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

    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)

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

    /* the cache-shaped listener: it answers the request from its own store and stops, at a priority ahead of the required one */
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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

/* the may-skip twin of the test above: the listener fails after producing a response, with a required listener behind it */
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

    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)

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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

/* the kernel.controller twin of the test above, for the same reason and with the same measurement behind it */
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

    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)

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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return exception.NewError("response listener failed", nil, nil)
        },
        0,
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

func (instance *failingSessionStorage) CloseWithContext(closeContext context.Context) error {
    return nil
}

func TestKernel_SessionSaveFailureAnswersFiveHundred(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/session-write",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatalf("expected the session request attribute to be present")
            }
            sessionInstance.Set("key", "value")

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(&failingSessionStorage{})

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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
            sessionInstance, _ := RequestSession(request)
            sessionInstance.Set("key", "value")

            panic("handler exploded after touching the session")
        },
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(&failingSessionStorage{})

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/session-write-boom", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the recovered 500 to be delivered despite the session-store outage, got %d", recorder.Code)
    }
}

type errorCountingLogger struct {
    errorCount   int64
    warningCount int64
}

func (instance *errorCountingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *errorCountingLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *errorCountingLogger) Info(message string, context loggingcontract.Context) {}

func (instance *errorCountingLogger) Warning(message string, context loggingcontract.Context) {
    atomic.AddInt64(&instance.warningCount, 1)
}

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
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, countingLogger, container.OverrideOptions{})

    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return exception.NewError("response listener failure", nil, nil)
        },
        0,
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/logged-once", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected the response despite the dispatch error, got %d", recorder.Code)
    }

    /* the dispatcher already logs "event listener error" once and marks the returned wrapper as logged; the handler-response finalization block must respect that mark (logEventDispatchError) instead of re-logging it — inline logging here would produce two error lines for one failure */
    if 1 != atomic.LoadInt64(&countingLogger.errorCount) {
        t.Fatalf("expected the dispatch error to be logged exactly once on the handler-response path, got %d error logs", atomic.LoadInt64(&countingLogger.errorCount))
    }
}

/* closeTrackingReader stands in for a file-backed response body (FileResponse / static ServeReader): the only thing that matters here is whether the kernel closed the descriptor. */
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

/* panicOnceSessionStorage blows up on its first Save, the way a database driver does on a lost connection. writeResponse persists the session before WriteToHttpResponseWriter registers the body's deferred Close, so that panic unwinds with the response's descriptor still open. It succeeds afterwards so the kernel's recovery path can finish and write the error response. */
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

func (instance *panicOnceSessionStorage) CloseWithContext(closeContext context.Context) error {
    return nil
}

/* writeResponse persists the session BEFORE WriteToHttpResponseWriter registers the body's deferred Close, so a panic in the session backend unwinds with the file-backed response assigned to finalResponse and its descriptor still open. The recover handler replaces finalResponse with an error response; unless it closes the discarded one, every such request leaks a file descriptor. */
func TestKernel_PanicRecoveryClosesTheDiscardedFileBackedResponse(t *testing.T) {
    bodyReader := &closeTrackingReader{}
    storage := &panicOnceSessionStorage{}

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/file",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatal("expected the request to carry a session")
            }

            /* dirty the session so writeResponse persists it — and panics doing so */
            sessionInstance.Set("key", "value")

            response := &Response{}
            response.SetStatusCode(nethttp.StatusOK)
            response.SetHeaders(make(nethttp.Header))
            response.SetBodyReader(bodyReader)

            return response, nil
        },
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(storage)
    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/file", nil))

    if false == storage.panicked.Load() {
        t.Fatal("the session backend never panicked; the test does not exercise the recovery path")
    }

    if false == bodyReader.closed.Load() {
        t.Fatal("the discarded file-backed response body was never closed: one file descriptor leaks per request")
    }
}

/* the dispatcher contains a listener's panic per listener and answers it as an error, with ONE exception:
   an exit error, which it re-raises by contract so it reaches the edge of the process. On the recovery
   path that publish is where the response built BEFORE the panic is decided, so a listener leaving by
   that one route took the decision with it — nothing was written, nothing below it ran, and the
   file-backed response lost its only reference with the descriptor still open. One leak per request that
   reaches it. The close is the frame's now, not the step's, so the route the listener leaves by cannot
   change it. */
func TestKernel_PanicRecoveryClosesTheDiscardedResponseWhenAListenerLeavesByExit(t *testing.T) {
    original := &closeRecordingReadCloser{}
    storage := &panicOnceSessionStorage{}

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/file-then-exit",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatal("expected the request to carry a session")
            }

            /* dirty the session so writeResponse persists it — and panics doing so, ahead of the body write */
            sessionInstance.Set("key", "value")

            return &Response{
                statusCode: nethttp.StatusOK,
                headers:    make(nethttp.Header),
                bodyReader: original,
            }, nil
        },
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(storage)

    /* the first publish is the ordinary one, before the write; the exit has to fall on the SECOND, which
       is the recovery's re-publish — the one that decides what happens to the response the panic left behind */
    publishes := 0
    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            publishes++
            if 2 > publishes {
                return nil
            }

            exception.Exit(exception.NewExitError(7, exception.NewError("the listener is leaving", nil, nil)))

            return nil
        },
        0,
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/file-then-exit", nil))

    if false == storage.panicked.Load() {
        t.Fatal("the session backend never panicked; the test does not exercise the recovery path")
    }

    if 2 != publishes {
        t.Fatalf("expected the recovery to re-publish, so the exit falls on that publish, got %d", publishes)
    }

    /* the exit does not reach the edge: the kernel's LAST guard — registered under the scope close, for a
       panic raised inside the main guard after it has already recovered once — takes it and files its own
       degraded record, which is why the recorder still carries a body. What the exit does take with it is
       everything after the publish inside the write step, which is where the discarded response was
       decided; the count below is the whole of what this pin is about. */
    if 0 == recorder.Body.Len() {
        t.Fatal("expected the kernel's last guard to have answered; the request produced no response at all")
    }

    if 1 != original.closeCount {
        t.Fatalf("expected the discarded file-backed body to be closed exactly once, got %d: one descriptor leaks per request", original.closeCount)
    }
}

/* the response a kernel.response listener swaps in is the one ServeHttp names while the write runs: a session backend that panics inside the write — ahead of the body write's own deferred Close — unwinds into the recovery with the swapped-in file-backed response holding its descriptor, and the recovery closes what ServeHttp names. Named by value, returned only when the write returned, the recovery closed the discarded original a second time and the swapped-in one never. */
func TestKernel_PanicRecoveryClosesTheResponseAListenerSwappedInNotTheDiscardedOneTwice(t *testing.T) {
    original := &closeRecordingReadCloser{}
    swapped := &closeRecordingReadCloser{}
    storage := &panicOnceSessionStorage{}

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/file-then-swap",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatal("expected the request to carry a session")
            }

            /* dirty the session so writeResponse persists it — and panics doing so, ahead of the body write */
            sessionInstance.Set("key", "value")

            return &Response{
                statusCode: nethttp.StatusOK,
                headers:    make(nethttp.Header),
                bodyReader: original,
            }, nil
        },
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(storage)

    /* the listener swaps once: the recovery publishes its error response on the same event, and a second swap would hand the closed body back to the write */
    swappedOnce := false
    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if false == ok || true == swappedOnce {
                return nil
            }
            swappedOnce = true

            responseEvent.SetResponse(&Response{
                statusCode: nethttp.StatusAccepted,
                headers:    make(nethttp.Header),
                bodyReader: swapped,
            })

            return nil
        },
        0,
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/file-then-swap", nil))

    if false == storage.panicked.Load() {
        t.Fatal("the session backend never panicked; the test does not exercise the recovery path")
    }

    if 1 != original.closeCount {
        t.Fatalf("expected the discarded original body to be closed exactly once, by the swap, got %d", original.closeCount)
    }

    if 1 != swapped.closeCount {
        t.Fatalf("expected the swapped-in body to be closed exactly once, by the recovery, got %d", swapped.closeCount)
    }
}

/* the sister of the pin above, on the listener the pin above steps round: one that answers with the response it already swapped in — a listener holding it in a per-request field, one memoising its own decoration — is handed that response back on the recovery's re-publish of the same event. Closing the panicked response BEFORE that publish closed the very body the write then read, on the response actually served; the close waits until the publish has said what is being written. */
func TestKernel_PanicRecoveryDoesNotCloseAResponseTheRepublishHandsBack(t *testing.T) {
    original := &closeRecordingReadCloser{}
    swapped := &closeRecordingReadCloser{}
    storage := &panicOnceSessionStorage{}

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/file-then-swap-always",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatal("expected the request to carry a session")
            }

            sessionInstance.Set("key", "value")

            return &Response{
                statusCode: nethttp.StatusOK,
                headers:    make(nethttp.Header),
                bodyReader: original,
            }, nil
        },
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(storage)

    swappedResponse := &Response{
        statusCode: nethttp.StatusAccepted,
        headers:    make(nethttp.Header),
        bodyReader: swapped,
    }

    publishes := 0
    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if false == ok {
                return nil
            }

            publishes++
            responseEvent.SetResponse(swappedResponse)

            return nil
        },
        0,
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/file-then-swap-always", nil))

    if false == storage.panicked.Load() {
        t.Fatal("the session backend never panicked; the test does not exercise the recovery path")
    }

    if 2 != publishes {
        t.Fatalf("the probe no longer exercises the re-publish: %d publishes", publishes)
    }

    if 1 != original.closeCount {
        t.Fatalf("expected the discarded original body to be closed exactly once, got %d", original.closeCount)
    }

    /* once, by the write that served it. Closed ahead of the re-publish it was closed twice — and the first of those closes came BEFORE the write read it. */
    if 1 != swapped.closeCount {
        t.Fatalf("expected the body the re-publish handed back to be closed once, by the write that served it, got %d", swapped.closeCount)
    }
}

/* net/http documents this sentinel as "abort the connection and suppress the log", and a reverse proxy panics with it on every client disconnect mid-stream; converted into an error, an aborted upload would be answered with a 500 and an error line */
func TestKernel_AbortHandlerPanicClosesTheConnectionWithoutAResponse(t *testing.T) {
    router := NewRouter()

    router.Handle(
        nethttp.MethodGet,
        "/abort",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            panic(nethttp.ErrAbortHandler)
        },
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    server := httptest.NewServer(handler)
    defer server.Close()

    response, requestErr := server.Client().Get(server.URL + "/abort")
    if nil == requestErr {
        defer response.Body.Close()

        t.Fatalf("expected the connection to be closed without a response, got status %d", response.StatusCode)
    }
}

/* the kernel resolves the scheme through the configured forwarded-headers policy and publishes it on the request; a listener has no access to that policy, so without the attribute the access log reported http for every request a trusted proxy terminated as https */
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

    kernelInstance := NewKernel(router, KernelConfig{})
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

func (instance *loadFailingSessionStorage) CloseWithContext(closeContext context.Context) error {
    return nil
}

/* erroringSessionManager answers the errors it is given from its two load doors and hands every other call to the manager it wraps */
type erroringSessionManager struct {
    inner              sessioncontract.Manager
    sessionErr         error
    sessionBesideError sessioncontract.Session
    newSessionErr      error
    newSessionCalls    int
}

func (instance *erroringSessionManager) Session(sessionId string) (sessioncontract.Session, error) {
    if nil != instance.sessionErr {
        return instance.sessionBesideError, instance.sessionErr
    }

    return instance.inner.Session(sessionId)
}

func (instance *erroringSessionManager) NewSession() (sessioncontract.Session, error) {
    instance.newSessionCalls = instance.newSessionCalls + 1

    if nil != instance.newSessionErr {
        return nil, instance.newSessionErr
    }

    return instance.inner.NewSession()
}

func (instance *erroringSessionManager) RegenerateSession(sessionInstance sessioncontract.Session) (sessioncontract.Session, error) {
    return instance.inner.RegenerateSession(sessionInstance)
}

func (instance *erroringSessionManager) SaveSession(sessionInstance sessioncontract.Session) error {
    return instance.inner.SaveSession(sessionInstance)
}

func (instance *erroringSessionManager) DeleteSession(sessionId string) error {
    return instance.inner.DeleteSession(sessionId)
}

func (instance *erroringSessionManager) CloseWithContext(closeContext context.Context) error {
    return instance.inner.CloseWithContext(closeContext)
}

const sessionOutageTestCookieValue = "0123456789abcdef0123456789abcdef"

type sessionOutageServed struct {
    recorder          *httptest.ResponseRecorder
    handlerCalls      int
    handlerSawSession bool
    terminateCalls    int
    errorRecords      int64
}

func serveOverSessionManager(t *testing.T, sessionManager sessioncontract.Manager, path string, withCookie bool) sessionOutageServed {
    t.Helper()

    served := sessionOutageServed{}

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/page",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            served.handlerCalls = served.handlerCalls + 1

            publishedSession, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                return nil, sessionReadErr
            }

            served.handlerSawSession = false == internal.IsNilInterface(publishedSession)

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )
    router.Handle(
        nethttp.MethodGet,
        "/quiet",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            served.handlerCalls = served.handlerCalls + 1

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    countingLogger := &errorCountingLogger{}

    serviceContainer := newHttpTestContainerWithSessionManager(sessionManager)
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, countingLogger, container.OverrideOptions{})

    event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelTerminate,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            served.terminateCalls = served.terminateCalls + 1

            return nil
        },
        0,
    )

    handler := NewKernel(router, KernelConfig{UnavailableRetryAfter: bound.Of(7 * time.Second)}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, path, nil)
    if true == withCookie {
        request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: sessionOutageTestCookieValue})
    }

    served.recorder = httptest.NewRecorder()

    handler.ServeHTTP(served.recorder, request)

    served.errorRecords = atomic.LoadInt64(&countingLogger.errorCount)

    return served
}

func TestKernel_ASessionStorageOutageIsMetOnlyByTheRouteThatReadsIt(t *testing.T) {
    cases := map[string]struct {
        path       string
        withCookie bool
        status     int
        retryAfter string
    }{
        "a route reading the session with a cookie":    {path: "/page", withCookie: true, status: nethttp.StatusServiceUnavailable, retryAfter: "7"},
        "a route reading the session without a cookie": {path: "/page", withCookie: false, status: nethttp.StatusServiceUnavailable, retryAfter: "7"},
        "a route not reading it with a cookie":         {path: "/quiet", withCookie: true, status: nethttp.StatusOK},
        "a route not reading it without a cookie":      {path: "/quiet", withCookie: false, status: nethttp.StatusOK},
        "an unrouted request with a cookie":            {path: "/missing", withCookie: true, status: nethttp.StatusNotFound},
    }

    for name, testCase := range cases {
        sessionManager := session.NewManager(&loadFailingSessionStorage{}, session.ManagerConfig{Ttl: bound.Of(30 * time.Minute)})

        served := serveOverSessionManager(t, sessionManager, testCase.path, testCase.withCookie)

        if testCase.status != served.recorder.Code || testCase.retryAfter != served.recorder.Header().Get("Retry-After") {
            t.Fatalf("%s: expected %d with Retry-After %q, got %d %q", name, testCase.status, testCase.retryAfter, served.recorder.Code, served.recorder.Header().Get("Retry-After"))
        }

        if "" != served.recorder.Header().Get("Set-Cookie") {
            t.Fatalf("%s: expected no cookie written during a session storage outage, got %q", name, served.recorder.Header().Get("Set-Cookie"))
        }

        if 1 != served.terminateCalls {
            t.Fatalf("%s: expected the kernel terminate event once, got %d", name, served.terminateCalls)
        }
    }
}

func TestKernel_ASessionStorageOutageFilesOneErrorRecord(t *testing.T) {
    for _, withCookie := range []bool{true, false} {
        sessionManager := session.NewManager(&loadFailingSessionStorage{}, session.ManagerConfig{Ttl: bound.Of(30 * time.Minute)})

        served := serveOverSessionManager(t, sessionManager, "/page", withCookie)

        if 1 != served.errorRecords {
            t.Fatalf("expected one error record for the outage (cookie %v), got %d", withCookie, served.errorRecords)
        }
    }
}

func TestKernel_ASessionLoadErrorDoesNotFallThroughToANewSession(t *testing.T) {
    sessionManager := &erroringSessionManager{
        inner:      session.NewManager(session.NewInMemoryStorage(session.InMemoryStorageConfig{}), session.ManagerConfig{Ttl: bound.Of(30 * time.Minute)}),
        sessionErr: exception.MarkInfrastructureFailure(errors.New("session store is down")),
    }

    served := serveOverSessionManager(t, sessionManager, "/page", true)

    if nethttp.StatusServiceUnavailable != served.recorder.Code {
        t.Fatalf("expected the load error answered 503, got %d", served.recorder.Code)
    }

    if 0 != sessionManager.newSessionCalls || true == served.handlerSawSession || "" != served.recorder.Header().Get("Set-Cookie") {
        t.Fatalf("expected no fresh session, none served and no cookie over a failed load, got %d mints, served %v, cookie %q", sessionManager.newSessionCalls, served.handlerSawSession, served.recorder.Header().Get("Set-Cookie"))
    }
}

func TestKernel_AnUnmarkedManagerErrorAnswers500(t *testing.T) {
    sessionManager := &erroringSessionManager{
        inner:         session.NewManager(session.NewInMemoryStorage(session.InMemoryStorageConfig{}), session.ManagerConfig{Ttl: bound.Of(30 * time.Minute)}),
        newSessionErr: errors.New("could not generate unique session id"),
    }

    served := serveOverSessionManager(t, sessionManager, "/page", false)

    if nethttp.StatusInternalServerError != served.recorder.Code || "" != served.recorder.Header().Get("Retry-After") {
        t.Fatalf("expected an unmarked manager error answered 500 with no Retry-After, got %d %q", served.recorder.Code, served.recorder.Header().Get("Retry-After"))
    }

    if true == served.handlerSawSession || "" != served.recorder.Header().Get("Set-Cookie") {
        t.Fatalf("expected neither a session nor a cookie over a failed mint")
    }
}

func TestKernel_ASessionStorageOutageReachesTheExceptionListenersRecordedOnce(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/page",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            _, sessionReadErr := RequestSession(request)

            return nil, sessionReadErr
        },
    )

    countingLogger := &errorCountingLogger{}

    sessionManager := session.NewManager(&loadFailingSessionStorage{}, session.ManagerConfig{Ttl: bound.Of(30 * time.Minute)})
    serviceContainer := newHttpTestContainerWithSessionManager(sessionManager)
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, countingLogger, container.OverrideOptions{})

    listenerCalls := 0
    listenerSawUnavailable := false
    event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            exceptionEvent := eventValue.Payload().(*KernelExceptionEvent)

            listenerCalls = listenerCalls + 1
            listenerSawUnavailable = exception.AnswersUnavailable(exceptionEvent.Err())

            exceptionEvent.SetResponse(TextResponse(nethttp.StatusTeapot, "answered by the application"))

            return nil
        },
        0,
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/page", nil))

    if 1 != listenerCalls || false == listenerSawUnavailable {
        t.Fatalf("expected the application's listener to meet the marked outage once, got %d calls, unavailable %v", listenerCalls, listenerSawUnavailable)
    }

    if nethttp.StatusTeapot != recorder.Code {
        t.Fatalf("expected the listener's response served, got %d", recorder.Code)
    }

    if 1 != atomic.LoadInt64(&countingLogger.errorCount) {
        t.Fatalf("expected the kernel to file the outage once before the listeners, got %d records", atomic.LoadInt64(&countingLogger.errorCount))
    }
}

/* typedNilErrorSessionManager answers its manager's sessions beside a typed-nil error, which a bare comparison takes for an error */
type typedNilErrorSessionManager struct {
    sessioncontract.Manager
}

func (instance *typedNilErrorSessionManager) Session(sessionId string) (sessioncontract.Session, error) {
    var typedNil *exception.Error

    sessionInstance, _ := instance.Manager.Session(sessionId)

    return sessionInstance, typedNil
}

func (instance *typedNilErrorSessionManager) NewSession() (sessioncontract.Session, error) {
    var typedNil *exception.Error

    sessionInstance, _ := instance.Manager.NewSession()

    return sessionInstance, typedNil
}

func TestKernel_ATypedNilErrorBesideASessionIsNoError(t *testing.T) {
    sessionManager := &typedNilErrorSessionManager{
        Manager: session.NewManager(session.NewInMemoryStorage(session.InMemoryStorageConfig{}), session.ManagerConfig{Ttl: bound.Of(30 * time.Minute)}),
    }

    for _, withCookie := range []bool{true, false} {
        served := serveOverSessionManager(t, sessionManager, "/page", withCookie)

        if nethttp.StatusOK != served.recorder.Code || 1 != served.handlerCalls || false == served.handlerSawSession || 0 != served.errorRecords {
            t.Fatalf("expected a typed-nil error read as none and a session published (cookie %v), got %d, %d handler calls, session %v, %d records", withCookie, served.recorder.Code, served.handlerCalls, served.handlerSawSession, served.errorRecords)
        }
    }
}

func TestKernel_ASessionAnsweredBesideALoadErrorIsNotSavedWhenTheResponsePathPanics(t *testing.T) {
    innerManager := session.NewManager(session.NewInMemoryStorage(session.InMemoryStorageConfig{}), session.ManagerConfig{Ttl: bound.Of(30 * time.Minute)})

    halfMade := mustSession(innerManager.NewSession())
    halfMade.Set("user", "alice")

    sessionManager := &erroringSessionManager{
        inner:              innerManager,
        sessionErr:         exception.MarkInfrastructureFailure(errors.New("session store is down")),
        sessionBesideError: halfMade,
    }

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/page",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            _, sessionReadErr := RequestSession(request)

            return nil, sessionReadErr
        },
    )
    serviceContainer := newHttpTestContainerWithSessionManager(sessionManager)

    responsePanics := true
    event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            if true == responsePanics {
                responsePanics = false

                /* an exit error is the panic the dispatcher re-raises, so it reaches the kernel's recovery with the session it holds */
                panic(exception.NewExitError(1, exception.NewError("response listener exited", nil, nil)))
            }

            return nil
        },
        0,
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/page", nil)
    request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: sessionOutageTestCookieValue})

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, request)

    if "" != recorder.Header().Get("Set-Cookie") {
        t.Fatalf("expected the session answered beside the error never saved nor written, got %q", recorder.Header().Get("Set-Cookie"))
    }
}

func TestKernel_EmitsTheCookieOfTheSessionRepublishedOnTheRequest(t *testing.T) {
    serviceContainer := newHttpTestContainerWithSessionStorage(session.NewInMemoryStorage(session.InMemoryStorageConfig{}))
    sessionManager := session.SessionMustFromContainer(t.Context(), serviceContainer)

    republishedId := ""

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/republish",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            replacement := mustSession(sessionManager.NewSession())
            replacement.Set("userId", "u-1")

            _ = SetRequestSession(request, replacement)

            republishedId = replacement.Id()

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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
    serviceContainer := newHttpTestContainerWithSessionStorage(session.NewInMemoryStorage(session.InMemoryStorageConfig{}))
    sessionManager := session.SessionMustFromContainer(t.Context(), serviceContainer)

    preRotationId := ""
    rotatedId := ""

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/login",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatal("expected the request to carry a session")
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

            _ = SetRequestSession(request, rotated)

            rotatedId = rotated.Id()

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

    if nil != mustSession(sessionManager.Session(preRotationId)) {
        t.Fatalf("expected the pre-rotation session to be gone from storage")
    }

    reloaded := mustSession(sessionManager.Session(rotatedId))
    if nil == reloaded {
        t.Fatalf("expected the rotated session to be stored")
    }

    if "u-1" != reloaded.String("userId") {
        t.Fatalf("expected the values to survive the rotation, got %q", reloaded.String("userId"))
    }
}

func TestKernel_RotationWithoutRepublishingClearsTheAbandonedSession(t *testing.T) {
    serviceContainer := newHttpTestContainerWithSessionStorage(session.NewInMemoryStorage(session.InMemoryStorageConfig{}))
    sessionManager := session.SessionMustFromContainer(t.Context(), serviceContainer)

    existingSession := mustSession(sessionManager.NewSession())
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
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatal("expected the request to carry a session")
            }

            rotated, rotateErr := sessionManager.RegenerateSession(sessionInstance)
            if nil != rotateErr {
                return nil, rotateErr
            }

            rotated.Set("userId", "u-1")

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

    if nil != mustSession(sessionManager.Session(existingId)) {
        t.Fatalf("expected the pre-rotation session to be gone from storage")
    }
}

func TestKernel_SessionCookiePolicyWithoutSameSiteKeepsTheLaxDefault(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/session-write",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatal("expected the request to carry a session")
            }

            sessionInstance.Set("key", "value")

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    kernelInstance := NewKernel(router, KernelConfig{})
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

            if _, sessionReadErr := RequestSession(request); nil == sessionReadErr {
                observedSessionIsSession = true
            }

            return TextResponse(200, "ok"), nil
        },
        RouteOptions{
            Name:    "owned",
            Methods: []string{nethttp.MethodGet},
            Attributes: map[string]any{
                "scheme":  "spoofed",
                "session": "spoofed",
            },
        },
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

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

    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

/* A listener may still replace the synthesized empty response, the same way it may replace any other. */
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

    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

/* the recovery boundary must record the stack of the panic site: the error value alone names the symptom but not the line that raised it, and net/http's own stack print never fires for a panic this recovery absorbs — without the frames a production nil-pointer is unlocatable from the logs */

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
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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


func newContainerWithPanickingExceptionListener(t *testing.T, serviceContainer containercontract.Container) containercontract.Container {
    t.Helper()

    event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            panic("listener exploded")
        },
        0,
    )

    return serviceContainer
}

func TestKernel_AListenerPanicOnHandlerErrorPathClosesBodyAndDelivers500(t *testing.T) {
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

    kernel := NewKernel(router, KernelConfig{})
    handler := kernel.ServeHttp(newContainerWithPanickingExceptionListener(t, newHttpTestContainer()))

    request := httptest.NewRequest(nethttp.MethodGet, "/fail-with-open-body", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the default 500 despite the listener panic, got %d", recorder.Code)
    }

    if false == bodyReader.closed.Load() {
        t.Fatalf("expected the failed handler's response body to be closed despite the listener panic")
    }
}

func TestKernel_AListenerPanicOnNotFoundPathClosesBodyAndDelivers500(t *testing.T) {
    bodyReader := &closeTrackingReader{}

    kernel := NewKernel(NewRouter(), KernelConfig{})
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
    handler := kernel.ServeHttp(newContainerWithPanickingExceptionListener(t, newHttpTestContainer()))

    request := httptest.NewRequest(nethttp.MethodGet, "/no-such-route", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the default 500 despite the listener panic, got %d", recorder.Code)
    }

    if false == bodyReader.closed.Load() {
        t.Fatalf("expected the failed not-found response body to be closed despite the listener panic")
    }
}

func TestKernel_AListenerPanicOnRecoveryPathClosesBodyAndDelivers500(t *testing.T) {
    bodyReader := &closeTrackingReader{}

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/session-write-then-file",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, _ := RequestSession(request)
            sessionInstance.Set("key", "value")

            response := &Response{
                statusCode: nethttp.StatusOK,
                headers:    make(nethttp.Header),
                bodyReader: bodyReader,
            }

            return response, nil
        },
    )

    kernel := NewKernel(router, KernelConfig{})
    sessionStorage := &panicOnceSessionStorage{}
    handler := kernel.ServeHttp(newContainerWithPanickingExceptionListener(t, newHttpTestContainerWithSessionStorage(sessionStorage)))

    request := httptest.NewRequest(nethttp.MethodGet, "/session-write-then-file", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the recovered 500 despite the listener panic on the recovery path, got %d", recorder.Code)
    }

    if false == bodyReader.closed.Load() {
        t.Fatalf("expected the in-flight response body to be closed despite the listener panic on the recovery path")
    }
}

/* a urlencoded body whose read failed must refuse the request the way the json path refuses the identical condition: dispatching it would hand the handler a syntactically valid request whose form is simply empty, and an oversized submission would be processed as an empty one */

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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

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

/* brokenBodyReader fails mid-read the way a client aborting an upload does. */
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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

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

/* Session reports "no such session" the way a careless implementation does: by returning a nil pointer of its own session type, which is not equal to nil once it is carried in the interface. */
func (instance *typedNilReturningSessionManager) Session(sessionId string) (sessioncontract.Session, error) {
    var typedNil *session.Session

    return typedNil, nil
}

func (instance *typedNilReturningSessionManager) NewSession() (sessioncontract.Session, error) {
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

func (instance *typedNilReturningSessionManager) CloseWithContext(closeContext context.Context) error {
    return instance.delegate.CloseWithContext(context.Background())
}

/* The session manager is a replaceable service, so the kernel must test what it hands back with IsNilInterface rather than against nil: a typed nil passes a bare comparison, the kernel skips NewSession and publishes it, and the first handler that touches the session dereferences nil. The request must be served a working session instead. */
func TestKernel_MintsASessionWhenTheManagerAnswersWithATypedNil(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/session",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            publishedSession, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                return TextResponse(nethttp.StatusInternalServerError, "no session"), nil
            }

            /* a typed nil reaches here as a non-nil interface and panics on the first call */
            publishedSession.Set("touched", "yes")

            return TextResponse(nethttp.StatusOK, publishedSession.Id()), nil
        },
    )

    storage := session.NewInMemoryStorage(session.InMemoryStorageConfig{})

    serviceContainer := newHttpTestContainerWithSessionManager(
        &typedNilReturningSessionManager{
            delegate: session.NewManager(storage, session.ManagerConfig{Ttl: bound.Of(30*time.Minute)}),
        },
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

/* the no-route record is written for exactly the requests nobody routed, and a query string routinely carries a credential — the record keeps the parameter names for diagnosis and must not carry the values into the journal */
func TestKernel_TheNoRouteRecordRedactsQueryValues(t *testing.T) {
    router := NewRouter()

    recordingLogger := &warningRecordingLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

/* the method-not-allowed record redacts the same way: the 405 is answered before any handler runs, so this record is the only trace of the request — names kept, values withheld */
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
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

/* A handler that commits its own response and then rotates the session loses everything the session held: the rotation deletes the previous entry, and the response path refuses to store the replacement because no Set-Cookie can reach the client on a committed response. Refusing the write is right, but it must not be silent — without a line in the log this is indistinguishable from the ordinary case the branch exists for, a first-time visitor on a stream with nothing worth storing. */
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
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/stream", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if false == recordingLogger.hasWarning("session not persisted: the response was already committed and the request does not name this session") {
        t.Fatalf("expected the dropped session write to be logged, got warnings %v", recordingLogger.warningMessages)
    }
}

/* a stopping listener that skipped nothing required keeps answering the request: the refusal must not cost every short-circuiting listener its response */
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

    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

/* noMatchRouter reports "no match" the way the contract permits and the framework's own router does not: a nil result beside the false flag. */
type noMatchRouter struct {
    *Router
}

func (instance *noMatchRouter) Match(method string, path string, host string, scheme string) (*httpcontract.MatchResult, bool) {
    return nil, false
}

func TestKernel_ServeHttpAnswersARouterThatReportsNoMatchWithANilResult(t *testing.T) {
    router := &noMatchRouter{Router: NewRouter()}

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    request := httptest.NewRequest(nethttp.MethodGet, "/anything", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusNotFound != recorder.Code {
        t.Fatalf("expected %d, got %d", nethttp.StatusNotFound, recorder.Code)
    }
}

/* The terminate dispatch is the one in ServeHttp with no recovery above it — its defer is registered before the recovery defer, so it runs after that one has already fired. It needs none: the dispatcher recovers a listener panic per listener and hands it back as an error. This pins that division, because a recovery added here would swallow the one panic the dispatcher re-raises on purpose, a deliberate exit. */
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

    event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelTerminate,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            panic("terminate listener")
        },
        0,
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/hello", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected %d, got %d", nethttp.StatusOK, recorder.Code)
    }
}

/* The listener is what the assertion turns on, not the status: writeResponse answers 204 for an absent response whichever way it became absent, so a status alone cannot tell the kernel's door from the writer's fallback. A listener is the only thing that decorates a response, and it must see the empty 204 the door built rather than the nothing a typed nil would have carried this far. */
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

    event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

/* explodingError is what an application error looks like when its own rendering is broken. It is the input that reaches the window: a listener cannot panic out of a dispatch — the dispatcher recovers it per listener — so the only thing left between the chain returning and the response being published is the log context the kernel builds from the error, which reads it by calling Error(). */
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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

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

func (instance *failingSaveSessionManager) Session(sessionId string) (sessioncontract.Session, error) {
    return instance.inner.Session(sessionId)
}

func (instance *failingSaveSessionManager) NewSession() (sessioncontract.Session, error) {
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

func (instance *failingSaveSessionManager) CloseWithContext(closeContext context.Context) error {
    return instance.inner.CloseWithContext(context.Background())
}

func TestKernel_ASessionSaveOutageReachesTerminateAsTheFiveHundredTheClientReceived(t *testing.T) {
    manager := &failingSaveSessionManager{
        inner: session.NewManager(session.NewInMemoryStorage(session.InMemoryStorageConfig{}), session.ManagerConfig{Ttl: bound.Of(30*time.Minute)}),
    }

    serviceContainer := newHttpTestContainerWithSessionManager(manager)

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/login",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatalf("expected the kernel to publish a session on the request")
            }
            sessionInstance.Set("identity", "someone")

            return TextResponse(nethttp.StatusOK, "welcome"), nil
        },
    )

    terminateStatus := -1
    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

    kernelInstance := NewKernel(router, KernelConfig{})
    kernelInstance.Use([]httpcontract.Middleware{panickingOuterMiddleware})
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
    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

/* the discipline of the one record a handler failure files: an error something upstream already logged files nothing, a deliberate 4xx is a refusal at warning, a 5xx keeps error, the client's own cancellation is named for what it is — and every branch hands back a marked occurrence so the exception listener attaches coordinates instead of filing the failure a second time, the error value itself left unmarked. */
func TestLogHandlerError_ADeliberate4xxFilesOneWarningAndMarksTheError(t *testing.T) {
    capture := &exceptionListenerCaptureLogger{}
    handlerErr := exception.TooManyRequests("rate limit exceeded")

    reportedErr := logHandlerError(capture, "controller handler error", handlerErr, httptest.NewRequest(nethttp.MethodGet, "/limited", nil))

    if 1 != capture.warningCalls || 0 != capture.errorCalls {
        t.Fatalf("expected one warning and no error, got %d warnings %d errors", capture.warningCalls, capture.errorCalls)
    }

    if false == exception.IsAlreadyLogged(reportedErr) {
        t.Fatal("expected the record to mark the reported occurrence, so the exception listener does not file it again")
    }

    if true == exception.IsAlreadyLogged(handlerErr) {
        t.Fatal("expected the handler's error value to stay unmarked, so the next request that returns it files its own record")
    }

    if nethttp.StatusTooManyRequests != exception.AsHttpException(reportedErr).StatusCode() {
        t.Fatal("expected the occurrence to keep the status of the error it carries")
    }
}

func TestLogHandlerError_AServerFaultKeepsTheErrorLevel(t *testing.T) {
    capture := &exceptionListenerCaptureLogger{}

    logHandlerError(capture, "controller handler error", exception.NewError("boom", nil, nil), httptest.NewRequest(nethttp.MethodGet, "/broken", nil))

    if 1 != capture.errorCalls || 0 != capture.warningCalls {
        t.Fatalf("expected one error and no warning, got %d errors %d warnings", capture.errorCalls, capture.warningCalls)
    }
}

/* the classification is pinned on the writer that runs FIRST: a validation exception a handler returns is filed and MARKED here, so the exception listener, which carries the same rule, only attaches coordinates to it and never reaches its error branch; a struct tag naming a rule that does not exist refuses every request the route will ever serve, so it is an error, not a warning among the users who mistyped their address */
func TestLogHandlerError_ARuleWiringFaultIsFiledAtError(t *testing.T) {
    capture := &exceptionListenerCaptureLogger{}

    httpException := exception.BadRequest("validation failed")
    httpException.SetContext(map[string]any{
        "validationErrors": validation.ValidationErrors{
            validation.NewValidationError("email", "unknown validation rule", validation.ErrorUnknownRule, map[string]any{"rule": "reqiured"}),
        },
    })

    logHandlerError(capture, "controller handler error", httpException, httptest.NewRequest(nethttp.MethodPost, "/api/subscribe", nil))

    if 1 != capture.errorCalls || 0 != capture.warningCalls {
        t.Fatalf("expected the wiring fault at error, got %d errors %d warnings", capture.errorCalls, capture.warningCalls)
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

func TestLogHandlerError_ACancellationCarryingTheApplicationsCauseStaysAnError(t *testing.T) {
    capture := &exceptionListenerCaptureLogger{}

    causedContext, cancel := context.WithCancelCause(context.Background())
    cancel(errors.New("application shutdown"))
    request := httptest.NewRequest(nethttp.MethodGet, "/slow", nil).WithContext(causedContext)

    logHandlerError(capture, "controller handler error", context.Canceled, request)

    if 1 != capture.errorCalls || 0 != capture.warningCalls {
        t.Fatalf("expected one error and no warning, got %d errors %d warnings", capture.errorCalls, capture.warningCalls)
    }
}

/* a cancellation returned while the request context is alive is NOT the client's disconnect — a handler bug cancelled something of its own — and stays at the error level a genuine fault carries. */
func TestLogHandlerError_ACancellationWithALiveRequestContextStaysAnError(t *testing.T) {
    capture := &exceptionListenerCaptureLogger{}

    logHandlerError(capture, "controller handler error", context.Canceled, httptest.NewRequest(nethttp.MethodGet, "/slow", nil))

    if 1 != capture.errorCalls || 0 != capture.warningCalls {
        t.Fatalf("expected one error and no warning, got %d errors %d warnings", capture.errorCalls, capture.warningCalls)
    }
}

/* the trusted proxy list decides every request's proxy trust; retained live, a caller reusing its slice rewrote the decision mid-serving as a data race. The setter copies. */
func TestSetForwardedHeadersPolicy_CopiesTheTrustedProxyList(t *testing.T) {
    kernel := NewKernel(NewRouter(), KernelConfig{})

    trustedProxyList := []string{"10.0.0.0/8"}
    kernel.SetForwardedHeadersPolicy(httpcontract.ForwardedHeadersPolicy{
        TrustForwardedHeaders: true,
        TrustedProxyList:      trustedProxyList,
    })

    trustedProxyList[0] = "0.0.0.0/0"

    if "10.0.0.0/8" != kernel.policies.forwardedHeadersPolicy.TrustedProxyList[0] {
        t.Fatalf("expected the kernel to keep its own copy of the trusted list, got %q", kernel.policies.forwardedHeadersPolicy.TrustedProxyList[0])
    }
}

/* recordCountingLogger counts what each level received, so a test can assert that one failure left one record. */
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
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, countingLogger, container.OverrideOptions{})
    NewKernel(router, KernelConfig{}).
        ServeHttp(serviceContainer).
        ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/subject", nil))

    return countingLogger
}

/* a handler's plain errors.New carries no AlreadyLogged implementer, so the mark the kernel writes had nowhere to land and the exception listener filed the same failure a second time */
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

/* the value a runtime panic recovers to is a runtime.Error, the case the recovery exists for, and it carries the mark no better than a plain error does */
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

/* the recovery reads the mark through exception.IsAlreadyLogged: a handler that panics with an error it already recorded — through exception.Logged, or wrapped around a marked carrier — is not filed a second time under "unhandled http error". */
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

    /* an empty journal is also what a request that never reached the handler leaves behind — a route that stopped matching, a container that refused to build — and the helper drops the recorder, so nothing else here can tell the two apart. The three siblings assert a record was written and are non-vacuous for that reason; this one asserts an absence and needs its own witness. */
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

    kernelInstance := NewKernel(router, KernelConfig{})

    kernelInstance.SetMethodPolicy(MethodPolicy{AutomaticOptionsDisabled: true})

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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

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

/* a deliberate 4xx blaming the submitted VALUE is a refusal and keeps the warning it always had: the classification must separate the declaration from the value, not raise every 4xx */
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

/* the counter is what a shutdown reads to tell a drained server from one that still has work inside it, so it must rise for the whole time a request is being served and fall exactly when the scope closes. The proof holds the handler open and reads the count from another goroutine: reading it after the request returned would pass just as well against a counter that was never incremented at all. */
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

    kernelInstance := NewKernel(router, KernelConfig{})

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

/* the release rides the same defer as the close rather than the handler's return, so a request whose handler panics — the path with its own recovery defers above this one — still gives its scope back. A leaked count would make every later shutdown wait out its whole budget and then report a drain failure for a server with nothing in it. */
func TestKernel_OpenRequestScopesIsReleasedWhenTheHandlerPanics(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/boom",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            panic("boom")
        },
    )

    kernelInstance := NewKernel(router, KernelConfig{})

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
    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    /* the path folds to "/login", which the access-control matcher would authorize as a different rule than the admin handler the router reaches; the kernel must refuse it before either runs */
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

/* the whitespace spelling routes to the catch-all under "/admin" while the access-control matcher trims it and answers with the rule of "/admin"; refused at the kernel, neither is asked */
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
        handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

/* the LEADING twin of the padded path on this major: Go's own server refuses a request line that does not begin with "/", but a handler mounted in front of the kernel that rewrites the path — the standard library's StripPrefix — hands the kernel " /public" for "/api%20/public". As routed that is "%20/public", a target that does not begin with "/", which the canonical guard leaves to the router; the guard asks the leading form of the decoded path as well, so this major answers 400 as the two frozen majors do and the upgrade notes promise */
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
        handler := nethttp.StripPrefix("/api", NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer))

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

/* the canonical question is asked of the spelling the router reads: "/a%2F..%2Fb" is ONE segment naming "a/../b" for the router, and the guard reads it the same way instead of refusing the decoded "/a/../b" — while the real fold "/a/../b" is still refused, and "/public%2F" reaches the catch-all as the resource "public/" the router serves it as */
func TestKernel_AsksTheCanonicalQuestionOfThePathAsSpelled(t *testing.T) {
    for rawPath, expected := range map[string]struct {
        code     int
        catchAll bool
    }{
        "/a%2F..%2Fb":  {code: nethttp.StatusOK, catchAll: true},
        "/a%2F..%2Fb{": {code: nethttp.StatusOK, catchAll: true},
        "/public%2F":   {code: nethttp.StatusOK, catchAll: true},
        "/public":      {code: nethttp.StatusOK, catchAll: false},
        "/a/../b":      {code: nethttp.StatusBadRequest, catchAll: false},
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

        handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

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
    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

/* a multipart upload past the body limit surfaces as a handler's *MaxBytesError, which is not an HttpException and so was rendered 500 at error level while the urlencoded and json paths answered 413; the normalizer maps it onto a 413 HttpException so the three body paths agree, and leaves any other error untouched. */
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
            NewKernel(NewRouter(), KernelConfig{}).SetForwardedHeadersPolicy(httpcontract.ForwardedHeadersPolicy{
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
            kernel.Use([]httpcontract.Middleware{func(next httpcontract.Handler) httpcontract.Handler { return next }})
        }},
        {"SetNotFoundHandler", func(kernel *Kernel) { kernel.SetNotFoundHandler(nil) }},
        {"SetForwardedHeadersPolicy", func(kernel *Kernel) {
            kernel.SetForwardedHeadersPolicy(httpcontract.ForwardedHeadersPolicy{})
        }},
        {"SetSessionCookiePolicy", func(kernel *Kernel) {
            kernel.SetSessionCookiePolicy(httpcontract.SessionCookiePolicy{})
        }},
        {"SetMethodPolicy", func(kernel *Kernel) { kernel.SetMethodPolicy(httpcontract.MethodPolicy{}) }},
        {"SetSessionPolicy", func(kernel *Kernel) { kernel.SetSessionPolicy(httpcontract.SessionPolicy{EmptyOnOutage: true}) }},
    } {
        kernel := NewKernel(NewRouter(), KernelConfig{})

        /* configuring BEFORE the handler is built stays legal, which is the half that proves the guard discriminates rather than refusing everything */
        testCase.mutate(kernel)

        _ = kernel.ServeHttp(newHttpTestContainer())

        testhelper.AssertPanicsWithError(t, func() {
            testCase.mutate(kernel)
        }, "may not configure the http kernel after it started serving")
    }
}

func TestKernel_RefusesARouteRegisteredAfterItStartedServing(t *testing.T) {
    router := NewRouter()
    kernel := NewKernel(router, KernelConfig{})

    router.Handle(nethttp.MethodGet, "/before", routeRegistryTestHandler())

    _ = kernel.ServeHttp(newHttpTestContainer())

    testhelper.AssertPanicsWithError(t, func() {
        router.Handle(nethttp.MethodGet, "/after", routeRegistryTestHandler())
    }, "may not register a route after the http kernel started serving")
}

func TestKernel_LeavesTheRouteTableReadableAfterItStartedServing(t *testing.T) {
    router := NewRouter()
    kernel := NewKernel(router, KernelConfig{})

    router.HandleNamed("article.show", nethttp.MethodGet, "/articles/:id", routeRegistryTestHandler())

    _ = kernel.ServeHttp(newHttpTestContainer())

    /* the openapi document and the route manifest are served FROM a handler, so freezing the reading doors alongside the writing ones would break the very routes that publish the table */
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

    kernel := NewKernel(router, KernelConfig{})
    handler := kernel.ServeHttp(newHttpTestContainer())

    /* a request is held open across the refusal, so the guard is exercised against a kernel that really is serving rather than one that merely built a handler; under -race this is the shape that would report the write the refusal prevents */
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

            kernel.Use([]httpcontract.Middleware{func(next httpcontract.Handler) httpcontract.Handler { return next }})
        }()
    }

    <-finished
}

/* the abort sentinel suppresses the response, not the ownership of what it holds, so a deliberate abort over a file-backed response still closes the response in flight; invokeErrorHandlerSafely refuses to honour the sentinel for the same reason. */
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

    kernel := NewKernel(router, KernelConfig{})

    /* the panic is raised by an OUTER middleware AFTER next() returned, which is the window in which the response the chain produced is held only by the recording shim */
    kernel.Use(
        []httpcontract.Middleware{
            func(next httpcontract.Handler) httpcontract.Handler {
                return func(
                    runtimeInstance runtimecontract.Runtime,
                    writer nethttp.ResponseWriter,
                    request httpcontract.Request,
                ) (httpcontract.Response, error) {
                    _, _ = next(runtimeInstance, writer, request)

                    panic(nethttp.ErrAbortHandler)
                }
            },
        },
    )

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

/* the route is matched on the path as the client SPELLED it: net/http decodes "%2F" into a separator, so "/admin%2Fusers" — one segment naming a resource called "admin/users" — became two segments and reached the "/admin/users" handler, while a proxy rule written against the raw request line matched neither spelling. */
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

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    for rawPath, expected := range map[string]string{
        "/admin%2Fusers":         "one-segment:admin/users",
        "/admin%2Fusers{":        "one-segment:admin/users{",
        "/admin%2Fusers|x":       "one-segment:admin/users|x",
        "/admin%2Fusers\xc3\xa9": "one-segment:admin/users\xc3\xa9",
    } {
        reached = ""

        httpRequest := httptest.NewRequest(nethttp.MethodGet, rawPath, nil)
        handler.ServeHTTP(httptest.NewRecorder(), httpRequest)

        if expected != reached {
            t.Fatalf("the encoded separator of %q was read as a path separator: reached %q", rawPath, reached)
        }
    }
}

func TestKernel_RefusesARawPathAFrontHandlerLeftStale(t *testing.T) {
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

    kernelHandler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    pathOnlyRewrite := nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        request.URL.Path = strings.TrimPrefix(request.URL.Path, "/api")
        kernelHandler.ServeHTTP(writer, request)
    })

    recorder := httptest.NewRecorder()
    pathOnlyRewrite.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/api/admin%2Fusers", nil))

    if nethttp.StatusBadRequest != recorder.Code || "" != reached {
        t.Fatalf("expected a raw path the front handler left stale to be refused with no handler run, got %d reaching %q", recorder.Code, reached)
    }

    recorder = httptest.NewRecorder()
    nethttp.StripPrefix("/api", kernelHandler).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/api/admin%2Fusers", nil))

    if nethttp.StatusOK != recorder.Code || "one-segment:admin/users" != reached {
        t.Fatalf("expected a front handler rewriting both fields to be served as the one segment, got %d reaching %q", recorder.Code, reached)
    }
}

func TestKernel_ARefusedStaleRawPathCarriesNoRouteAndLogsTheRawPath(t *testing.T) {
    router := NewRouter()
    router.HandleNamed("admin_users", nethttp.MethodGet, "/admin/users", routeRegistryTestHandler())

    recordingLogger := &warningRecordingLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    responseRouteName := "unread"
    dispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if true == ok {
                responseRouteName = responseEvent.Request().RouteName()
            }

            return nil
        },
        0,
    )

    kernelHandler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    pathOnlyRewrite := nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        request.URL.Path = strings.TrimPrefix(request.URL.Path, "/api")
        kernelHandler.ServeHTTP(writer, request)
    })

    recorder := httptest.NewRecorder()
    pathOnlyRewrite.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/api/admin%2Fusers", nil))

    if nethttp.StatusBadRequest != recorder.Code {
        t.Fatalf("expected the stale raw path refused, got %d", recorder.Code)
    }
    if "" != responseRouteName {
        t.Fatalf("expected the refusal to carry no route, the kernel.response listener read %q", responseRouteName)
    }

    warningContext, logged := recordingLogger.warningContextFor("request path refused before the handler")
    if false == logged {
        t.Fatalf("expected the refusal to leave its warning, got %v", recordingLogger.warningMessages)
    }
    if "/api/admin%2Fusers" != warningContext["rawPath"] {
        t.Fatalf("expected the refusal record to carry the raw path, got %v", warningContext["rawPath"])
    }
}

func TestKernel_AStaleRawPathLeavesOnlyTheRefusalWarning(t *testing.T) {
    router := NewRouter()
    router.HandleNamed("admin_users", nethttp.MethodGet, "/admin/users", routeRegistryTestHandler())

    recordingLogger := &warningRecordingLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    kernelHandler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    pathOnlyRewrite := nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        request.URL.Path = strings.TrimPrefix(request.URL.Path, "/api")
        kernelHandler.ServeHTTP(writer, request)
    })

    recorder := httptest.NewRecorder()
    pathOnlyRewrite.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/api/admin%2Fusers", nil))

    if nethttp.StatusBadRequest != recorder.Code {
        t.Fatalf("expected the stale raw path refused, got %d", recorder.Code)
    }
    if 1 != len(recordingLogger.warningMessages) || "request path refused before the handler" != recordingLogger.warningMessages[0] {
        t.Fatalf("expected the refusal as the only warning, got %v", recordingLogger.warningMessages)
    }
}

/* the tie-break is registration order, deliberately, and specificity is not a factor. The rule is written on the RouteHandler contract; this pins it so a future change to the selection has to be a decision rather than an accident. */
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

/* serveNegotiatedKernelProbe drives one request through a kernel whose container carries a json and a plain-text serializer, the two a client can name in Accept */
func serveNegotiatedKernelProbe(t *testing.T, method string, target string, accept string) *httptest.ResponseRecorder {
    t.Helper()

    router := NewRouter()
    router.Handle(
        nethttp.MethodPost,
        "/only-post",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "posted"), nil
        },
    )

    serviceContainer := newHttpTestContainer()
    container.MustRegister[*serializer.SerializerManager](
        serviceContainer,
        serializer.ServiceSerializerManager,
        func(resolver containercontract.Resolver) (*serializer.SerializerManager, error) {
            return serializer.NewSerializerManager(map[string]serializercontract.Serializer{
                "application/json": serializer.NewJsonSerializer(),
                "text/plain":       serializer.NewPlainTextSerializer(),
            })
        },
        container.RegisterOptions{},
    )

    request := httptest.NewRequest(method, target, nil)
    request.Header.Set("Accept", accept)
    recorder := httptest.NewRecorder()
    NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, request)

    return recorder
}

func TestKernel_AnswersAMethodNotAllowedInTheRepresentationTheClientNegotiated(t *testing.T) {
    recorder := serveNegotiatedKernelProbe(t, nethttp.MethodGet, "/only-post", "text/plain")

    if nethttp.StatusMethodNotAllowed != recorder.Code {
        t.Fatalf("expected 405, got %d", recorder.Code)
    }

    if "text/plain; charset=utf-8" != recorder.Header().Get("Content-Type") {
        t.Fatalf("expected the plain text content type, got %q", recorder.Header().Get("Content-Type"))
    }

    if false == strings.HasPrefix(recorder.Body.String(), "405 method not allowed\n") {
        t.Fatalf("expected the status line first, got %q", recorder.Body.String())
    }

    if "" == recorder.Header().Get("Allow") || false == strings.Contains(recorder.Header().Get("Allow"), nethttp.MethodPost) {
        t.Fatalf("expected the Allow header naming POST, got %q", recorder.Header().Get("Allow"))
    }
}

func TestKernel_KeepsTheJsonMethodNotAllowedAndItsAllowForAJsonClient(t *testing.T) {
    recorder := serveNegotiatedKernelProbe(t, nethttp.MethodGet, "/only-post", "application/json")

    if nethttp.StatusMethodNotAllowed != recorder.Code {
        t.Fatalf("expected 405, got %d", recorder.Code)
    }

    if false == strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/json") {
        t.Fatalf("expected json, got %q", recorder.Header().Get("Content-Type"))
    }

    if false == strings.Contains(recorder.Body.String(), "method not allowed") {
        t.Fatalf("expected the message in the json body, got %q", recorder.Body.String())
    }

    if false == strings.Contains(recorder.Header().Get("Allow"), nethttp.MethodPost) {
        t.Fatalf("expected the Allow header naming POST, got %q", recorder.Header().Get("Allow"))
    }
}

func TestKernel_AnswersANotFoundWithoutAHandlerInTheRepresentationTheClientNegotiated(t *testing.T) {
    recorder := serveNegotiatedKernelProbe(t, nethttp.MethodGet, "/no-such-route", "text/plain")

    if nethttp.StatusNotFound != recorder.Code {
        t.Fatalf("expected 404, got %d", recorder.Code)
    }

    if "text/plain; charset=utf-8" != recorder.Header().Get("Content-Type") {
        t.Fatalf("expected the plain text content type, got %q", recorder.Header().Get("Content-Type"))
    }

    if false == strings.HasPrefix(recorder.Body.String(), "404 not found\n") {
        t.Fatalf("expected the status line first, got %q", recorder.Body.String())
    }
}

/* loggedMarkSentinelErr and loggedMarkWrappedSentinelErr are the package-level failure values an application returns from more than one request */
var loggedMarkSentinelErr = exception.NewError("the shared failure", nil, nil)

var loggedMarkWrappedSentinelErr = exception.NewError("the shared wrapped failure", nil, nil)

/* servedLoggedMarkKernel serves one route over a kernel whose logger keeps every record, with the framework's exception listener installed */
func servedLoggedMarkKernel(handler httpcontract.Handler) (nethttp.Handler, *recordsCaptureLogger) {
    capture := &recordsCaptureLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})

    router := NewRouter()
    router.Handle(nethttp.MethodGet, "/fail", handler)

    return NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer), capture
}

func distinctRequestIdsOf(records []capturedRecord) map[string]bool {
    requestIds := make(map[string]bool, len(records))
    for _, record := range records {
        requestId, _ := record.context["requestId"].(string)
        requestIds[requestId] = true
    }

    return requestIds
}

/* every request that fails files its own record, whether the error value is fresh, a package-level sentinel or a sentinel wrapped by the handler: the logged mark belongs to one occurrence, never to the value. An error a lower layer filed and marked before it returned is not filed again. */
func TestKernel_EveryRequestFilesItsOwnRecordOfASharedErrorValue(t *testing.T) {
    cases := []struct {
        name            string
        handlerErr      func() error
        expectedRecords int
    }{
        {name: "fresh", handlerErr: func() error { return exception.NewError("a fresh failure", nil, nil) }, expectedRecords: 3},
        {name: "sentinel", handlerErr: func() error { return loggedMarkSentinelErr }, expectedRecords: 3},
        {name: "wrapped sentinel", handlerErr: func() error { return fmt.Errorf("while serving: %w", loggedMarkWrappedSentinelErr) }, expectedRecords: 3},
        {name: "filed by a lower layer", handlerErr: func() error { return exception.Logged(exception.NewError("filed below", nil, nil)) }, expectedRecords: 0},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            handler, capture := servedLoggedMarkKernel(
                func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                    return nil, testCase.handlerErr()
                },
            )

            for requestIndex := 0; requestIndex < 3; requestIndex++ {
                recorder := httptest.NewRecorder()
                handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/fail", nil))

                if nethttp.StatusInternalServerError != recorder.Code {
                    t.Fatalf("expected 500, got %d", recorder.Code)
                }
            }

            records := capture.failureRecords()
            if testCase.expectedRecords != len(records) {
                t.Fatalf("expected %d records over three requests, got %d", testCase.expectedRecords, len(records))
            }

            if testCase.expectedRecords != len(distinctRequestIdsOf(records)) {
                t.Fatalf("expected each record to carry the id of its own request, got %v", distinctRequestIdsOf(records))
            }
        })
    }

    for _, shared := range []*exception.Error{loggedMarkSentinelErr, loggedMarkWrappedSentinelErr} {
        if true == shared.AlreadyLogged() {
            t.Fatalf("expected the shared value %q to carry no mark after the requests it failed", shared.Message())
        }

        if _, written := shared.Context()["requestId"]; true == written {
            t.Fatalf("expected the shared value %q to carry no request's coordinates, got %v", shared.Message(), shared.Context())
        }
    }
}

/* the waiters of one remember flight are handed one error value: each request still files its own record, under its own request id */
func TestKernel_TheWaitersOfOneRememberFlightFileOneRecordEach(t *testing.T) {
    flightErr := exception.NewError("the flight failed", nil, nil)

    cacheManager := cache.NewManager(cache.NewInMemoryBackend(cache.InMemoryBackendConfig{MaxItems: bound.Of(16), CleanupInterval: bound.Of(time.Minute), Clock: clock.NewSystemClock()}), cache.NewJsonSerializer(), cache.ManagerConfig{})

    var entered sync.WaitGroup
    entered.Add(2)

    release := make(chan struct{})

    handler, capture := servedLoggedMarkKernel(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            entered.Done()

            _, rememberErr := cache.Remember(
                context.Background(),
                cacheManager,
                "the-flight",
                cache.RememberConfig{Ttl: bound.Of(time.Minute)},
                func(ctx context.Context) (any, error) {
                    <-release

                    return nil, flightErr
                },
            )

            return nil, rememberErr
        },
    )

    var served sync.WaitGroup
    for requestIndex := 0; requestIndex < 2; requestIndex++ {
        served.Add(1)

        go func() {
            defer served.Done()

            handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/fail", nil))
        }()
    }

    entered.Wait()
    close(release)
    served.Wait()

    records := capture.failureRecords()
    if 2 != len(records) {
        t.Fatalf("expected one record per waiter of the flight, got %d", len(records))
    }

    if 2 != len(distinctRequestIdsOf(records)) {
        t.Fatalf("expected each record to carry its own request id, got %v", distinctRequestIdsOf(records))
    }
}

/* an application listener on the kernel exception event reads the mark the kernel left, as every reader of the mark does: a failure the kernel filed is not filed a second time by it */
func TestKernel_AnApplicationListenerReadsTheMarkOfTheOccurrenceTheKernelFiled(t *testing.T) {
    capture := &recordsCaptureLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})

    eventDispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
    eventDispatcher.AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            exceptionEvent, ok := eventValue.Payload().(*KernelExceptionEvent)
            if false == ok || true == exception.IsAlreadyLogged(exceptionEvent.Err()) {
                return nil
            }

            capture.Error("application record", nil)

            return nil
        },
        0,
    )

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/fail",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, loggedMarkSentinelErr
        },
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    for requestIndex := 0; requestIndex < 2; requestIndex++ {
        handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/fail", nil))
    }

    records := capture.failureRecords()
    if 2 != len(records) {
        t.Fatalf("expected one record per request and none from the application listener, got %d", len(records))
    }

    for _, record := range records {
        if "application record" == record.message {
            t.Fatalf("expected the application listener to read the occurrence as filed")
        }
    }
}

/* a panic carrying an error value other requests also raise files one record per request: the recovery hands on a marked occurrence, never a mark written into the value */
func TestKernel_EveryPanicCarryingASharedErrorValueFilesItsOwnRecord(t *testing.T) {
    sharedPanicErr := exception.NewError("the shared panic", nil, nil)

    handler, capture := servedLoggedMarkKernel(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            panic(sharedPanicErr)
        },
    )

    for requestIndex := 0; requestIndex < 3; requestIndex++ {
        handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/fail", nil))
    }

    if 3 != len(capture.failureRecords()) {
        t.Fatalf("expected one record per request that panicked, got %d", len(capture.failureRecords()))
    }

    if true == sharedPanicErr.AlreadyLogged() {
        t.Fatalf("expected the shared panic value to carry no mark")
    }
}

/* in debug mode the payload shows the request's coordinates beside the error's own context, read from the occurrence the kernel marked, while the error value keeps none */
func TestKernel_TheDebugPayloadShowsTheCoordinatesOfTheOccurrence(t *testing.T) {
    sharedErr := exception.NewError("the shared failure in debug", map[string]any{"detail": "kept"}, nil)

    serviceContainer := newHttpTestContainer()
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/fail",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, sharedErr
        },
    )

    recorder := httptest.NewRecorder()
    NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/fail", nil))

    body := recorder.Body.String()
    if false == strings.Contains(body, `"path":"/fail"`) || false == strings.Contains(body, `"detail":"kept"`) {
        t.Fatalf("expected the debug payload to carry the error's context and the request's coordinates, got %s", body)
    }

    if _, written := sharedErr.Context()["path"]; true == written {
        t.Fatalf("expected the error value to keep no request's coordinates, got %v", sharedErr.Context())
    }
}

func expectSessionCookiePolicyRefusal(t *testing.T, door string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        refusal, isRefusal := recover().(*exception.Error)
        if false == isRefusal {
            t.Fatalf("%s: expected the SameSite=None policy with Secure never refused", door)
        }

        if "SessionCookiePolicy.Secure" != refusal.Context()["field"] || "None" != refusal.Context()["sameSite"] {
            t.Fatalf("%s: expected the refusal to name the field, got %q %v", door, refusal.Message(), refusal.Context())
        }
    }()

    callback()
}

func TestKernel_RefusesASameSiteNoneNeverSecurePolicy(t *testing.T) {
    policy := httpcontract.SessionCookiePolicy{Path: "/", SameSite: nethttp.SameSiteNoneMode, Secure: httpcontract.SessionCookieSecureNever}

    expectSessionCookiePolicyRefusal(t, "NewKernel", func() { NewKernel(NewRouter(), KernelConfig{SessionCookiePolicy: policy}) })
    expectSessionCookiePolicyRefusal(t, "SetSessionCookiePolicy", func() { NewKernel(NewRouter(), KernelConfig{}).SetSessionCookiePolicy(policy) })
}

func TestKernel_AdmitsEitherHalfOfTheRefusedSessionCookiePair(t *testing.T) {
    for _, policy := range []httpcontract.SessionCookiePolicy{
        {Secure: httpcontract.SessionCookieSecureNever},
        {SameSite: nethttp.SameSiteLaxMode, Secure: httpcontract.SessionCookieSecureNever},
        {SameSite: nethttp.SameSiteStrictMode, Secure: httpcontract.SessionCookieSecureNever},
        {SameSite: nethttp.SameSiteNoneMode, Secure: httpcontract.SessionCookieSecureAlways},
        {SameSite: nethttp.SameSiteNoneMode, Secure: httpcontract.SessionCookieSecureFromScheme},
    } {
        NewKernel(NewRouter(), KernelConfig{SessionCookiePolicy: policy})
    }
}

func TestKernel_AnErrorTheApplicationErrorHandlerDeclinesKeepsItsOwnStatus(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        err      error
        expected int
    }{
        {name: "http exception", err: exception.NotFound("gone"), expected: nethttp.StatusNotFound},
        {name: "wrapped http exception", err: exception.NewError("lookup failed", nil, exception.Conflict("taken")), expected: nethttp.StatusConflict},
        {name: "plain error", err: exception.NewError("handler failed", nil, nil), expected: nethttp.StatusInternalServerError},
    } {
        handlerErr := testCase.err

        router := NewRouter()
        router.Handle(
            nethttp.MethodGet,
            "/declined",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return nil, handlerErr
            },
        )

        kernel := NewKernel(router, KernelConfig{})
        recorder := httptest.NewRecorder()
        kernel.ServeHttp(newHttpTestContainer()).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/declined", nil))

        if testCase.expected != recorder.Code {
            t.Fatalf("%s: expected %d, got %d", testCase.name, testCase.expected, recorder.Code)
        }
    }
}

func TestKernel_APanicCarryingAnHttpExceptionNoListenerAnswersKeepsItsStatus(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/panics",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            panic(exception.Conflict("taken"))
        },
    )

    kernel := NewKernel(router, KernelConfig{})
    recorder := httptest.NewRecorder()
    kernel.ServeHttp(newHttpTestContainer()).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/panics", nil))

    if nethttp.StatusConflict != recorder.Code {
        t.Fatalf("expected the panic's 409 kept, got %d", recorder.Code)
    }
}

func TestKernel_ANotFoundHandlerErrorNoListenerAnswersKeepsItsStatus(t *testing.T) {
    kernel := NewKernel(NewRouter(), KernelConfig{})
    kernel.SetNotFoundHandler(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, exception.Forbidden("hidden")
        },
    )
    recorder := httptest.NewRecorder()
    kernel.ServeHttp(newHttpTestContainer()).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/no-such-route", nil))

    if nethttp.StatusForbidden != recorder.Code {
        t.Fatalf("expected the not-found handler's 403 kept, got %d", recorder.Code)
    }
}

func TestKernel_AnAutomaticOptionsIsNotJournaledAsAMethodNotAllowed(t *testing.T) {
    for _, testCase := range []struct {
        method          string
        expectedStatus  int
        expectedWarning bool
    }{
        {method: nethttp.MethodOptions, expectedStatus: nethttp.StatusNoContent, expectedWarning: false},
        {method: nethttp.MethodDelete, expectedStatus: nethttp.StatusMethodNotAllowed, expectedWarning: true},
    } {
        router := NewRouter()
        router.Handle(
            nethttp.MethodGet,
            "/resource",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return TextResponse(nethttp.StatusOK, "resource"), nil
            },
        )

        recordingLogger := &warningRecordingLogger{}

        serviceContainer := newHttpTestContainer()
        serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

        recorder := httptest.NewRecorder()
        NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(testCase.method, "/resource", nil))

        if testCase.expectedStatus != recorder.Code {
            t.Fatalf("%s: expected %d, got %d", testCase.method, testCase.expectedStatus, recorder.Code)
        }

        if testCase.expectedWarning != recordingLogger.hasWarning("method not allowed") {
            t.Fatalf("%s: expected the method-not-allowed warning %t, got %v", testCase.method, testCase.expectedWarning, recordingLogger.warningMessages)
        }
    }
}

func TestKernel_ARefusedLongPathIsJournaledOnceAndBounded(t *testing.T) {
    recordingLogger := &warningRecordingLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    recorder := httptest.NewRecorder()
    NewKernel(NewRouter(), KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/a/../"+strings.Repeat("b", 600*1024), nil))

    if nethttp.StatusBadRequest != recorder.Code {
        t.Fatalf("expected the non-canonical path refused, got %d", recorder.Code)
    }

    if 1 != len(recordingLogger.warningMessages) || "request path refused before the handler" != recordingLogger.warningMessages[0] {
        t.Fatalf("expected the refusal as the only warning, got %v", recordingLogger.warningMessages)
    }

    path, _ := recordingLogger.warningContexts[0]["path"].(string)
    if 600 < len(path) {
        t.Fatalf("expected the refusal's path bounded, got %d bytes", len(path))
    }
}

func TestKernel_AnUnroutedLongPathLeavesABoundedNoRouteRecord(t *testing.T) {
    recordingLogger := &warningRecordingLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    recorder := httptest.NewRecorder()
    NewKernel(NewRouter(), KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/"+strings.Repeat("b", 600*1024), nil))

    if nethttp.StatusNotFound != recorder.Code {
        t.Fatalf("expected the unrouted path answered 404, got %d", recorder.Code)
    }

    warningContext, logged := recordingLogger.warningContextFor("no route matched")
    if false == logged {
        t.Fatalf("expected the no-route record, got %v", recordingLogger.warningMessages)
    }

    path, _ := warningContext["path"].(string)
    if 600 < len(path) {
        t.Fatalf("expected the no-route record's path bounded, got %d bytes", len(path))
    }
}

func TestKernel_AnUnroutedRequestBoundsItsMethodAndHostInTheNoRouteRecord(t *testing.T) {
    recordingLogger := &warningRecordingLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/missing", nil)
    httpRequest.Method = strings.Repeat("M", 600*1024)
    httpRequest.Host = strings.Repeat("h", 600*1024)

    recorder := httptest.NewRecorder()
    NewKernel(NewRouter(), KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httpRequest)

    warningContext, logged := recordingLogger.warningContextFor("no route matched")
    if false == logged {
        t.Fatalf("expected the no-route record, got %v", recordingLogger.warningMessages)
    }

    for _, key := range []string{"method", "host"} {
        value, _ := warningContext[key].(string)
        if 600 < len(value) || false == strings.Contains(value, "...(truncated ") {
            t.Fatalf("expected the no-route record's %s bounded, got %d bytes", key, len(value))
        }
    }
}

func newKernelConfigTestRouter() *Router {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/articles",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "articles"), nil
        },
    )

    return router
}

func serveKernelConfigTestRequest(kernelInstance *Kernel, method string, target string) *httptest.ResponseRecorder {
    recorder := httptest.NewRecorder()
    kernelInstance.ServeHttp(newHttpTestContainer()).ServeHTTP(recorder, httptest.NewRequest(method, target, nil))

    return recorder
}

func TestNewKernel_TheZeroConfigServesHeadThroughTheGetRoute(t *testing.T) {
    recorder := serveKernelConfigTestRequest(NewKernel(newKernelConfigTestRouter(), KernelConfig{}), nethttp.MethodHead, "/articles")

    if nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected the zero policy to serve HEAD through the GET route, got %d", recorder.Code)
    }
}

func TestNewKernel_HeadFallbackDisabledRefusesTheFallback(t *testing.T) {
    kernelInstance := NewKernel(
        newKernelConfigTestRouter(),
        KernelConfig{MethodPolicy: MethodPolicy{HeadFallbackDisabled: true}},
    )

    recorder := serveKernelConfigTestRequest(kernelInstance, nethttp.MethodHead, "/articles")

    if nethttp.StatusMethodNotAllowed != recorder.Code {
        t.Fatalf("expected HEAD refused with %d, got %d", nethttp.StatusMethodNotAllowed, recorder.Code)
    }
}

func TestNewKernel_AutomaticOptionsDisabledAnswers405(t *testing.T) {
    kernelInstance := NewKernel(
        newKernelConfigTestRouter(),
        KernelConfig{MethodPolicy: MethodPolicy{AutomaticOptionsDisabled: true}},
    )

    recorder := serveKernelConfigTestRequest(kernelInstance, nethttp.MethodOptions, "/articles")

    if nethttp.StatusMethodNotAllowed != recorder.Code {
        t.Fatalf("expected the unrouted OPTIONS refused with %d, got %d", nethttp.StatusMethodNotAllowed, recorder.Code)
    }
}

func TestNewKernel_TheConfigFieldsReachTheKernel(t *testing.T) {
    trustedProxyList := []string{"10.0.0.0/8"}

    kernelInstance := NewKernel(
        newKernelConfigTestRouter(),
        KernelConfig{
            ForwardedHeadersPolicy: httpcontract.ForwardedHeadersPolicy{
                TrustForwardedHeaders: true,
                TrustedProxyList:      trustedProxyList,
            },
            SessionCookiePolicy: httpcontract.SessionCookiePolicy{Path: "/app"},
            NotFoundHandler: func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return TextResponse(nethttp.StatusTeapot, "not here"), nil
            },
        },
    )

    trustedProxyList[0] = "0.0.0.0/0"

    if false == kernelInstance.policies.forwardedHeadersPolicy.TrustForwardedHeaders || "10.0.0.0/8" != kernelInstance.policies.forwardedHeadersPolicy.TrustedProxyList[0] {
        t.Fatalf("expected the forwarded headers policy installed as a copy, got %+v", kernelInstance.policies.forwardedHeadersPolicy)
    }

    if "/app" != kernelInstance.policies.sessionCookiePolicy.Path {
        t.Fatalf("expected the session cookie policy installed, got %+v", kernelInstance.policies.sessionCookiePolicy)
    }

    recorder := serveKernelConfigTestRequest(kernelInstance, nethttp.MethodGet, "/missing")
    if nethttp.StatusTeapot != recorder.Code {
        t.Fatalf("expected the not-found handler to answer, got %d", recorder.Code)
    }
}

func TestNewKernel_RefusesAMalformedTrustedProxyEntryInTheConfig(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewKernel(
                NewRouter(),
                KernelConfig{
                    ForwardedHeadersPolicy: httpcontract.ForwardedHeadersPolicy{
                        TrustForwardedHeaders: true,
                        TrustedProxyList:      []string{"10.0.0"},
                    },
                },
            )
        },
        "trusted proxy entry is neither a CIDR prefix nor an address",
    )
}

func TestNewKernel_AppliesTheMiddlewaresInOrder(t *testing.T) {
    order := make([]string, 0)

    recording := func(name string) httpcontract.Middleware {
        return func(next httpcontract.Handler) httpcontract.Handler {
            return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                order = append(order, name)

                return next(runtimeInstance, writer, request)
            }
        }
    }

    kernelInstance := NewKernel(
        newKernelConfigTestRouter(),
        KernelConfig{Middlewares: []httpcontract.Middleware{recording("first"), recording("second")}},
    )

    serveKernelConfigTestRequest(kernelInstance, nethttp.MethodGet, "/articles")

    if 2 != len(order) || "first" != order[0] || "second" != order[1] {
        t.Fatalf("expected the middlewares run in their order, got %v", order)
    }
}

func newFailingKernelTestRouter(failure error) *Router {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/fail",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, failure
        },
    )

    return router
}

func TestKernel_ABuiltInListenerAnswersWhenNoApplicationListenerDid(t *testing.T) {
    serviceContainer := newHttpTestContainer()

    observed := 0
    event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            observed++

            return nil
        },
        0,
    )

    recorder := httptest.NewRecorder()
    NewKernel(newFailingKernelTestRouter(exception.NotFound("no such article")), KernelConfig{}).
        ServeHttp(serviceContainer).
        ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/fail", nil))

    if nethttp.StatusNotFound != recorder.Code || false == strings.Contains(recorder.Body.String(), "no such article") {
        t.Fatalf("expected the renderer's 404, got %d: %s", recorder.Code, recorder.Body.String())
    }

    if 1 != observed {
        t.Fatalf("expected the observer to run once, ran %d times", observed)
    }
}

func TestKernel_AnObserverAtAHigherPrioritySeesTheErrorBeforeTheBuiltInAnswers(t *testing.T) {
    serviceContainer := newHttpTestContainer()

    failure := exception.Conflict("taken")
    observedResponseSet := true
    var observedErr error

    event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            exceptionEvent := eventValue.Payload().(*KernelExceptionEvent)
            observedErr = exceptionEvent.Err()
            observedResponseSet = nil != exceptionEvent.Response()

            return nil
        },
        BuiltInExceptionListenerPriority+1,
    )

    recorder := httptest.NewRecorder()
    NewKernel(newFailingKernelTestRouter(failure), KernelConfig{}).
        ServeHttp(serviceContainer).
        ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/fail", nil))

    if false == errors.Is(observedErr, failure) || true == observedResponseSet {
        t.Fatalf("expected the observer to see the error with no response yet, got %v, %v", observedErr, observedResponseSet)
    }

    if nethttp.StatusConflict != recorder.Code {
        t.Fatalf("expected the built-in's 409, got %d", recorder.Code)
    }
}

func TestKernel_AnApplicationListenerAnswersAheadOfTheBuiltIn(t *testing.T) {
    serviceContainer := newHttpTestContainer()

    event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.Payload().(*KernelExceptionEvent).SetResponse(TextResponse(nethttp.StatusTeapot, "application body"))

            return nil
        },
        0,
    )

    recorder := httptest.NewRecorder()
    NewKernel(newFailingKernelTestRouter(exception.NotFound("no such article")), KernelConfig{}).
        ServeHttp(serviceContainer).
        ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/fail", nil))

    if nethttp.StatusTeapot != recorder.Code || "application body" != recorder.Body.String() {
        t.Fatalf("expected the application's answer, got %d: %s", recorder.Code, recorder.Body.String())
    }
}

func TestKernel_AListenerThatPanicsAfterSettingAResponseDoesNotGetItServed(t *testing.T) {
    capture := &recordsCaptureLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})

    bodyReader := &closeTrackingReader{}

    event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            response := &Response{
                statusCode: nethttp.StatusTeapot,
                headers:    nethttp.Header{"Content-Type": []string{"application/secret"}},
                bodyReader: bodyReader,
            }

            eventValue.Payload().(*KernelExceptionEvent).SetResponse(response)

            panic("listener exploded")
        },
        0,
    )

    recorder := httptest.NewRecorder()
    NewKernel(newFailingKernelTestRouter(exception.NotFound("no such article")), KernelConfig{}).
        ServeHttp(serviceContainer).
        ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/fail", nil))

    if nethttp.StatusNotFound != recorder.Code {
        t.Fatalf("expected the original 404, got %d", recorder.Code)
    }

    if false == bodyReader.closed.Load() {
        t.Fatalf("expected the dropped response's body closed")
    }

    found := false
    for _, record := range capture.failureRecords() {
        if "the kernel.exception dispatch failed after a listener set a response, which is dropped" != record.message {
            continue
        }

        found = true

        if nethttp.StatusTeapot != record.context["droppedResponseStatus"] || "application/secret" != record.context["droppedResponseContentType"] {
            t.Fatalf("expected the record to name the status and the type, got %v", record.context)
        }

        for key := range record.context {
            if "droppedResponseStatus" != key && "droppedResponseContentType" != key && "requestId" != key {
                t.Fatalf("expected the record to carry nothing else of the response, got %v", record.context)
            }
        }
    }

    if false == found {
        t.Fatalf("expected a record of the dropped response, got %v", capture.failureRecords())
    }
}

func TestKernel_APanickingListenerLeavesTheOriginalStatus(t *testing.T) {
    capture := &recordsCaptureLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})

    recorder := httptest.NewRecorder()
    NewKernel(newFailingKernelTestRouter(exception.NotFound("no such article")), KernelConfig{}).
        ServeHttp(newContainerWithPanickingExceptionListener(t, serviceContainer)).
        ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/fail", nil))

    if nethttp.StatusNotFound != recorder.Code || false == strings.Contains(recorder.Body.String(), "no such article") {
        t.Fatalf("expected the renderer's 404, got %d: %s", recorder.Code, recorder.Body.String())
    }

    for _, record := range capture.failureRecords() {
        if true == strings.Contains(record.message, "which is dropped") {
            t.Fatalf("expected no dropped-response record, got %v", record)
        }
    }
}

func TestKernel_APropagationStopWithoutAResponseIsRenderedByTheKernel(t *testing.T) {
    serviceContainer := newHttpTestContainer()

    event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.StopPropagation()

            return nil
        },
        0,
    )

    recorder := httptest.NewRecorder()
    NewKernel(newFailingKernelTestRouter(exception.NotFound("no such article")), KernelConfig{}).
        ServeHttp(serviceContainer).
        ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/fail", nil))

    if nethttp.StatusNotFound != recorder.Code {
        t.Fatalf("expected the kernel to render the 404, got %d", recorder.Code)
    }
}

func TestKernel_ARequiredListenerSkippedByTheResponderDropsTheAnswer(t *testing.T) {
    serviceContainer := newHttpTestContainer()

    eventDispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
    eventDispatcher.AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.Payload().(*KernelExceptionEvent).SetResponse(TextResponse(nethttp.StatusTeapot, "application body"))

            return nil
        },
        10,
    )
    eventDispatcher.(eventcontract.RequiredListenerRegistrar).MarkListenerRequired(
        eventDispatcher.AddListener(
            kernelcontract.EventKernelException,
            func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                return nil
            },
            0,
        ),
    )

    recorder := httptest.NewRecorder()
    NewKernel(newFailingKernelTestRouter(exception.NotFound("no such article")), KernelConfig{}).
        ServeHttp(serviceContainer).
        ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/fail", nil))

    if nethttp.StatusNotFound != recorder.Code {
        t.Fatalf("expected the original 404, got %d: %s", recorder.Code, recorder.Body.String())
    }
}

type panickingMarshalParameter struct{}

func (instance panickingMarshalParameter) MarshalJSON() ([]byte, error) {
    panic("the parameter cannot be rendered")
}

func TestRenderExceptionSafely_ARendererThatPanicsAnswersTheStatic500(t *testing.T) {
    capture := &recordsCaptureLogger{}

    failure := exception.NewHttpError(
        nethttp.StatusConflict,
        exception.HttpErrorConfig{Message: "taken", Parameters: map[string]any{"poison": panickingMarshalParameter{}}},
    )

    request := httptest.NewRequest(nethttp.MethodGet, "/fail", nil)
    request.Header.Set("Accept", "application/json")

    response := renderExceptionSafely(newTestRuntime(), testhelper.NewHttpTestRequestFromHttpRequest(request), failure, capture, ExceptionRenderConfig{})

    if nethttp.StatusInternalServerError != response.StatusCode() {
        t.Fatalf("expected the static 500, got %d", response.StatusCode())
    }

    if body := readResponseBody(t, response); false == strings.Contains(body, "internal server error") {
        t.Fatalf("expected the static body, got %s", body)
    }

    if records := capture.failureRecords(); 1 != len(records) || "the exception renderer panicked" != records[0].message {
        t.Fatalf("expected the renderer's panic on the journal, got %v", records)
    }
}

func TestKernel_ServeHttpInstallsTheBuiltInListenerOnce(t *testing.T) {
    serviceContainer := newHttpTestContainer()
    eventDispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)

    kernelInstance := NewKernel(NewRouter(), KernelConfig{})
    kernelInstance.ServeHttp(serviceContainer)
    kernelInstance.ServeHttp(serviceContainer)

    NewKernel(NewRouter(), KernelConfig{}).ServeHttp(serviceContainer)

    inspector := eventDispatcher.(eventcontract.EventDispatcherInspector)
    for _, registeredEvent := range inspector.RegisteredEvents() {
        if kernelcontract.EventKernelException != registeredEvent.EventName {
            continue
        }

        if 1 != len(registeredEvent.Listeners) || math.MinInt != registeredEvent.Listeners[0].Priority {
            t.Fatalf("expected one listener at the lowest priority, got %+v", registeredEvent.Listeners)
        }

        return
    }

    t.Fatalf("expected the built-in listener on the dispatcher")
}

func TestKernel_ServeHttpReservesTheExceptionFloor(t *testing.T) {
    serviceContainer := newHttpTestContainer()

    NewKernel(NewRouter(), KernelConfig{}).ServeHttp(serviceContainer)

    testhelper.AssertPanicsWithError(
        t,
        func() {
            event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
                kernelcontract.EventKernelException,
                func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                    return nil
                },
                BuiltInExceptionListenerPriority,
            )
        },
        "an event listener at or below the event's reserved priority can never run, so it is refused; register it above that priority",
    )
}

func newRefusingKernelRequestListener(refusal error) eventcontract.EventListener {
    return func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        eventValue.Payload().(*KernelRequestEvent).Refuse(refusal)

        return nil
    }
}

func TestKernel_ARefusedRequestNeverReachesItsHandler(t *testing.T) {
    serviceContainer := newHttpTestContainer()

    event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelRequest,
        newRefusingKernelRequestListener(exception.Forbidden("not you")),
        0,
    )

    handlerRan := 0

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/guarded",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            handlerRan++

            return TextResponse(nethttp.StatusOK, "served"), nil
        },
    )

    recorder := httptest.NewRecorder()
    NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/guarded", nil))

    if 0 != handlerRan {
        t.Fatalf("expected the handler not to run, it ran %d times", handlerRan)
    }

    if nethttp.StatusForbidden != recorder.Code || false == strings.Contains(recorder.Body.String(), "not you") {
        t.Fatalf("expected the built-in's 403, got %d: %s", recorder.Code, recorder.Body.String())
    }
}

func TestKernel_ARefusalIsRenderedThroughTheExceptionListeners(t *testing.T) {
    serviceContainer := newHttpTestContainer()
    refusal := exception.Forbidden("not you")

    eventDispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
    eventDispatcher.AddListener(kernelcontract.EventKernelRequest, newRefusingKernelRequestListener(refusal), 0)

    var observedErr error

    eventDispatcher.AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            exceptionEvent := eventValue.Payload().(*KernelExceptionEvent)
            observedErr = exceptionEvent.Err()
            exceptionEvent.SetResponse(TextResponse(nethttp.StatusTeapot, "application body"))

            return nil
        },
        0,
    )

    recorder := httptest.NewRecorder()
    NewKernel(NewRouter(), KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/guarded", nil))

    if nethttp.StatusTeapot != recorder.Code || "application body" != recorder.Body.String() {
        t.Fatalf("expected the application's answer, got %d: %s", recorder.Code, recorder.Body.String())
    }

    if false == errors.Is(observedErr, refusal) {
        t.Fatalf("expected the listener to see the refusal, got %v", observedErr)
    }
}

func TestKernel_ARefusalIsRecordedEvenWhenAnApplicationListenerAnswersFirst(t *testing.T) {
    capture := &recordsCaptureLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})

    refusal := exception.Forbidden("not you")

    eventDispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
    eventDispatcher.AddListener(kernelcontract.EventKernelRequest, newRefusingKernelRequestListener(refusal), 0)
    eventDispatcher.AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.Payload().(*KernelExceptionEvent).SetResponse(TextResponse(nethttp.StatusTeapot, "application body"))

            return nil
        },
        0,
    )

    handler := NewKernel(NewRouter(), KernelConfig{}).ServeHttp(serviceContainer)

    for served := 1; served <= 2; served++ {
        handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/guarded", nil))

        recorded := 0
        for _, record := range capture.failureRecords() {
            if "unhandled exception" == record.message && loggingcontract.LevelWarning == record.level {
                recorded++
            }
        }

        if served != recorded {
            t.Fatalf("expected one warning record per request, got %d after %d requests", recorded, served)
        }
    }
}

func TestKernel_ARefusalAlreadyLoggedIsNotFiledAgain(t *testing.T) {
    capture := &recordsCaptureLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})

    refusal := exception.NewError("already filed", nil, nil)
    _ = exception.MarkLogged(refusal)

    event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(kernelcontract.EventKernelRequest, newRefusingKernelRequestListener(refusal), 0)

    NewKernel(NewRouter(), KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/guarded", nil))

    for _, record := range capture.failureRecords() {
        if "unhandled exception" == record.message {
            t.Fatalf("expected no second record, got %v", record)
        }
    }
}

func TestKernel_ARefusalStandsOverALaterListenersFailure(t *testing.T) {
    serviceContainer := newHttpTestContainer()

    eventDispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
    eventDispatcher.AddListener(kernelcontract.EventKernelRequest, newRefusingKernelRequestListener(exception.Forbidden("not you")), 10)
    eventDispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return errors.New("a later listener failed")
        },
        0,
    )

    recorder := httptest.NewRecorder()
    NewKernel(NewRouter(), KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/guarded", nil))

    if nethttp.StatusForbidden != recorder.Code {
        t.Fatalf("expected the refusal's 403, got %d", recorder.Code)
    }
}

func TestKernel_ARefusalBesideASkippedRequiredListenerFailsClosed(t *testing.T) {
    serviceContainer := newHttpTestContainer()

    eventDispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
    eventDispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.Payload().(*KernelRequestEvent).Refuse(exception.Forbidden("not you"))
            eventValue.StopPropagation()

            return nil
        },
        10,
    )
    eventDispatcher.(eventcontract.RequiredListenerRegistrar).MarkListenerRequired(
        eventDispatcher.AddListener(
            kernelcontract.EventKernelRequest,
            func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                return nil
            },
            0,
        ),
    )

    recorder := httptest.NewRecorder()
    NewKernel(NewRouter(), KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/guarded", nil))

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the fail-closed 500, got %d", recorder.Code)
    }
}

func TestKernel_AListenerThatAnswersAndRefusesFailsClosed(t *testing.T) {
    for name, listener := range map[string]eventcontract.EventListener{
        "response then refusal": func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent := eventValue.Payload().(*KernelRequestEvent)
            requestEvent.SetResponse(TextResponse(nethttp.StatusTeapot, "answered"))
            requestEvent.Refuse(exception.Forbidden("refused"))

            return nil
        },
        "refusal then response": func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent := eventValue.Payload().(*KernelRequestEvent)
            requestEvent.Refuse(exception.Forbidden("refused"))
            requestEvent.SetResponse(TextResponse(nethttp.StatusTeapot, "answered"))

            return nil
        },
    } {
        serviceContainer := newHttpTestContainer()

        event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(kernelcontract.EventKernelRequest, listener, 0)

        recorder := httptest.NewRecorder()
        NewKernel(NewRouter(), KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/guarded", nil))

        if nethttp.StatusInternalServerError != recorder.Code {
            t.Fatalf("%s: expected the fail-closed 500, got %d", name, recorder.Code)
        }
    }
}

func TestKernel_AListenerThatRecoversTheRefusalOfASecondDecisionFailsClosed(t *testing.T) {
    for name, decide := range map[string]func(requestEvent *KernelRequestEvent){
        "a nil refusal":          func(requestEvent *KernelRequestEvent) { requestEvent.Refuse(nil) },
        "a response then refuse": func(requestEvent *KernelRequestEvent) { requestEvent.SetResponse(TextResponse(nethttp.StatusTeapot, "x")); requestEvent.Refuse(exception.Forbidden("y")) },
    } {
        serviceContainer := newHttpTestContainer()

        event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
            kernelcontract.EventKernelRequest,
            func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                defer func() { _ = recover() }()

                decide(eventValue.Payload().(*KernelRequestEvent))

                return nil
            },
            0,
        )

        handlerRan := 0

        router := NewRouter()
        router.Handle(
            nethttp.MethodGet,
            "/guarded",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                handlerRan++

                return TextResponse(nethttp.StatusOK, "served"), nil
            },
        )

        recorder := httptest.NewRecorder()
        NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/guarded", nil))

        if 0 != handlerRan || nethttp.StatusInternalServerError != recorder.Code {
            t.Fatalf("%s: expected the fail-closed 500 with the handler not run, got %d and %d runs", name, recorder.Code, handlerRan)
        }
    }
}

func TestKernel_AListenerThatAnswersAndRefusesHasTheResponseClosed(t *testing.T) {
    for name, decide := range map[string]func(requestEvent *KernelRequestEvent, response httpcontract.Response){
        "response then refusal": func(requestEvent *KernelRequestEvent, response httpcontract.Response) {
            requestEvent.SetResponse(response)
            requestEvent.Refuse(exception.Forbidden("refused"))
        },
        "refusal then response": func(requestEvent *KernelRequestEvent, response httpcontract.Response) {
            requestEvent.Refuse(exception.Forbidden("refused"))
            requestEvent.SetResponse(response)
        },
    } {
        serviceContainer := newHttpTestContainer()
        bodyReader := &closeTrackingReader{}

        event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
            kernelcontract.EventKernelRequest,
            func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                decide(eventValue.Payload().(*KernelRequestEvent), &Response{statusCode: nethttp.StatusTeapot, headers: make(nethttp.Header), bodyReader: bodyReader})

                return nil
            },
            0,
        )

        NewKernel(NewRouter(), KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/guarded", nil))

        if false == bodyReader.closed.Load() {
            t.Fatalf("%s: expected the response's body closed", name)
        }
    }
}

func TestKernel_ServeHttpOverAContainerWithoutADispatcherLeavesTheKernelConfigurable(t *testing.T) {
    kernelInstance := NewKernel(NewRouter(), KernelConfig{})

    func() {
        defer func() {
            if nil == recover() {
                t.Fatalf("expected a container without a dispatcher to be refused")
            }
        }()

        kernelInstance.ServeHttp(container.NewContainer(container.ContainerConfig{}))
    }()

    kernelInstance.SetMethodPolicy(MethodPolicy{})
}

type panickingContentTypeSerializer struct {
    serializercontract.Serializer
}

func (instance *panickingContentTypeSerializer) ContentType() string {
    panic("the serializer cannot name its type")
}

func TestKernel_ARendererBrokenByItsSerializerStillAnswersThroughTheResponsePath(t *testing.T) {
    serviceContainer := newHttpTestContainer()
    container.MustRegister[*serializer.SerializerManager](
        serviceContainer,
        serializer.ServiceSerializerManager,
        func(resolver containercontract.Resolver) (*serializer.SerializerManager, error) {
            return serializer.NewSerializerManager(map[string]serializercontract.Serializer{"application/json": &panickingContentTypeSerializer{}})
        },
        containercontract.RegisterOptions{},
    )

    responseDispatches := 0
    event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            responseDispatches++

            return nil
        },
        0,
    )

    bodyReader := &closeTrackingReader{}

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/fail",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return &Response{statusCode: nethttp.StatusOK, headers: make(nethttp.Header), bodyReader: bodyReader}, exception.NotFound("no such article")
        },
    )

    recorder := httptest.NewRecorder()
    NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/fail", nil))

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the static 500, got %d", recorder.Code)
    }

    if 1 != responseDispatches {
        t.Fatalf("expected kernel.response dispatched once, got %d", responseDispatches)
    }

    if false == bodyReader.closed.Load() {
        t.Fatalf("expected the handler's response body closed")
    }
}

func TestKernel_ACyclicChainFromAHandlerIsAnsweredWithAFiveHundred(t *testing.T) {
    for _, environment := range []string{config.EnvProduction, config.EnvDevelopment} {
        router := NewRouter()
        router.Handle(
            nethttp.MethodGet,
            "/cycle",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return nil, newCyclicChainError()
            },
        )

        handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainerInEnvironment(environment))

        recorder := httptest.NewRecorder()
        assertReturnsOnACyclicChain(t, func() {
            handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/cycle", nil))
        })

        if nethttp.StatusInternalServerError != recorder.Code {
            t.Fatalf("expected 500 in the %s environment, got %d", environment, recorder.Code)
        }
    }
}

func TestKernel_ACyclicChainFromAStreamedHandlerAfterItsHeaderIsAnswered(t *testing.T) {
    for _, environment := range []string{config.EnvProduction, config.EnvDevelopment} {
        router := NewRouter()
        router.Handle(
            nethttp.MethodGet,
            "/cycle",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                writer.WriteHeader(nethttp.StatusAccepted)

                return nil, newCyclicChainError()
            },
        )

        handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainerInEnvironment(environment))

        recorder := httptest.NewRecorder()
        assertReturnsOnACyclicChain(t, func() {
            handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/cycle", nil))
        })

        if nethttp.StatusAccepted != recorder.Code {
            t.Fatalf("expected the committed status in the %s environment, got %d", environment, recorder.Code)
        }
    }
}

func TestKernel_ACyclicChainFromTheRequestBodyIsAnsweredWithABadRequest(t *testing.T) {
    for _, environment := range []string{config.EnvProduction, config.EnvDevelopment} {
        router := NewRouter()
        router.Handle(
            nethttp.MethodPost,
            "/cycle",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return TextResponse(nethttp.StatusOK, "served"), nil
            },
        )

        handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainerInEnvironment(environment))

        netRequest := httptest.NewRequest(nethttp.MethodPost, "/cycle", &cyclicBodyReader{})
        netRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

        recorder := httptest.NewRecorder()
        assertReturnsOnACyclicChain(t, func() {
            handler.ServeHTTP(recorder, netRequest)
        })

        if nethttp.StatusBadRequest != recorder.Code {
            t.Fatalf("expected 400 in the %s environment, got %d", environment, recorder.Code)
        }
    }
}

func TestKernel_ARefusalCarryingRetryAfterIsServedWithTheHeader(t *testing.T) {
    serviceContainer := newHttpTestContainer()

    event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelRequest,
        newRefusingKernelRequestListener(exception.NewHttpError(nethttp.StatusServiceUnavailable, exception.HttpErrorConfig{Message: "down", RetryAfter: 3 * time.Second})),
        0,
    )

    recorder := httptest.NewRecorder()
    NewKernel(newFailingKernelTestRouter(exception.NotFound("unreached")), KernelConfig{}).
        ServeHttp(serviceContainer).
        ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/fail", nil))

    if nethttp.StatusServiceUnavailable != recorder.Code || "3" != recorder.Header().Get("Retry-After") {
        t.Fatalf("expected 503 with Retry-After 3, got %d %q", recorder.Code, recorder.Header().Get("Retry-After"))
    }
}

func TestKernel_TheFinalStepKeepsTheRetryAfterHeader(t *testing.T) {
    serviceContainer := newHttpTestContainer()

    eventDispatcher := event.EventDispatcherMustFromContainer(t.Context(), serviceContainer)
    eventDispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        newRefusingKernelRequestListener(exception.NewHttpError(nethttp.StatusServiceUnavailable, exception.HttpErrorConfig{Message: "down", RetryAfter: 3 * time.Second})),
        0,
    )
    eventDispatcher.AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            panic("listener exploded")
        },
        0,
    )

    recorder := httptest.NewRecorder()
    NewKernel(newFailingKernelTestRouter(exception.NotFound("unreached")), KernelConfig{}).
        ServeHttp(serviceContainer).
        ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/fail", nil))

    if nethttp.StatusServiceUnavailable != recorder.Code || "3" != recorder.Header().Get("Retry-After") {
        t.Fatalf("expected 503 with Retry-After 3 from the final step, got %d %q", recorder.Code, recorder.Header().Get("Retry-After"))
    }
}

func TestKernel_AHandlerReturningAMarkedErrorAnswers503WithTheKernelDelay(t *testing.T) {
    for name, throughTheFinalStep := range map[string]bool{"the final step": true, "the built-in listener": false} {
        router := NewRouter()
        router.Handle(
            nethttp.MethodGet,
            "/page",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return nil, exception.MarkInfrastructureFailure(errors.New("cache is down"))
            },
        )

        serviceContainer := newHttpTestContainer()
        if true == throughTheFinalStep {
            event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
                kernelcontract.EventKernelException,
                func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                    panic("listener exploded")
                },
                0,
            )
        }

        handler := NewKernel(router, KernelConfig{UnavailableRetryAfter: bound.Of(11 * time.Second)}).ServeHttp(serviceContainer)

        request := httptest.NewRequest(nethttp.MethodGet, "/page", nil)
        request.Header.Set("Accept", "application/json")
        recorder := httptest.NewRecorder()

        handler.ServeHTTP(recorder, request)

        if nethttp.StatusServiceUnavailable != recorder.Code || "11" != recorder.Header().Get("Retry-After") {
            t.Fatalf("%s: expected 503 with Retry-After 11, got %d %q", name, recorder.Code, recorder.Header().Get("Retry-After"))
        }
    }
}

func TestKernel_SetUnavailableRetryAfterRefusesADelayOutsideTheRule(t *testing.T) {
    for name, delay := range map[string]bound.Duration{
        "below a second":   bound.Of(500 * time.Millisecond),
        "above an hour":    bound.Of(2 * time.Hour),
        "a part of second": bound.Of(1500 * time.Millisecond),
        "unlimited":        bound.UnlimitedDuration(),
    } {
        t.Run(name, func(t *testing.T) {
            defer func() {
                recoveredErr, isError := recover().(error)
                if false == isError || "KernelConfig.UnavailableRetryAfter" != exception.LogContext(recoveredErr, exception.LogContextConfig{})["field"] {
                    t.Fatalf("expected a refusal naming KernelConfig.UnavailableRetryAfter, got %v", recoveredErr)
                }
            }()

            NewKernel(NewRouter(), KernelConfig{UnavailableRetryAfter: delay})
        })
    }
}

func TestLogHandlerError_AStoreStatusBeneathTheMarkIsAnError(t *testing.T) {
    capture := &exceptionListenerCaptureLogger{}

    logHandlerError(capture, "controller handler error", exception.MarkInfrastructureFailure(exception.NotFound("no such row")), httptest.NewRequest(nethttp.MethodGet, "/orders/7", nil))

    if 1 != capture.errorCalls || 0 != capture.warningCalls {
        t.Fatalf("expected one error and no warning for an outage, got %d errors %d warnings", capture.errorCalls, capture.warningCalls)
    }
}

/* a panic carrying the mark is an outage like a returned one: a session manager that cannot read its storage panics with the storage's error */
func TestKernel_APanicCarryingTheMarkAnswers503(t *testing.T) {
    for name, value := range map[string]any{
        "the marked error":           exception.MarkInfrastructureFailure(errors.New("storage is down")),
        "a melody error wrapping it": exception.NewError("could not load the session", nil, exception.MarkInfrastructureFailure(errors.New("storage is down"))),
    } {
        router := NewRouter()
        router.Handle(
            nethttp.MethodGet,
            "/page",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                panic(value)
            },
        )

        recorder := httptest.NewRecorder()
        NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer()).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/page", nil))

        if nethttp.StatusServiceUnavailable != recorder.Code || "5" != recorder.Header().Get("Retry-After") {
            t.Fatalf("%s: expected 503 with Retry-After 5, got %d %q", name, recorder.Code, recorder.Header().Get("Retry-After"))
        }
    }
}

func TestKernel_AKernelBuiltWithoutNewKernelRendersTheDefaultDelay(t *testing.T) {
    if false == (&Kernel{}).unavailableRetryAfterLimit().IsAbsent() {
        t.Fatal("expected a kernel holding no delay to hand the event an absent one, which the event reads as the default")
    }

    exceptionEvent := NewKernelExceptionEvent(nil, nil, errors.New("x"), KernelExceptionEventConfig{UnavailableRetryAfter: (&Kernel{}).unavailableRetryAfterLimit()})
    if config.DefaultUnavailableRetryAfter != exceptionEvent.UnavailableRetryAfter() {
        t.Fatalf("expected the default delay, got %s", exceptionEvent.UnavailableRetryAfter())
    }
}

type operationCountingSessionStorage struct {
    sessioncontract.Storage
    loads   int64
    saves   int64
    deletes int64
}

func (instance *operationCountingSessionStorage) Load(sessionId string) (map[string]any, bool, error) {
    atomic.AddInt64(&instance.loads, 1)

    return instance.Storage.Load(sessionId)
}

func (instance *operationCountingSessionStorage) Save(sessionId string, data map[string]any, ttl time.Duration) error {
    atomic.AddInt64(&instance.saves, 1)

    return instance.Storage.Save(sessionId, data, ttl)
}

func (instance *operationCountingSessionStorage) Delete(sessionId string) error {
    atomic.AddInt64(&instance.deletes, 1)

    return instance.Storage.Delete(sessionId)
}

func TestKernel_ARouteThatNeverReadsTheSessionCostsTheStorageNothing(t *testing.T) {
    cases := map[string]struct {
        path       string
        withCookie bool
    }{
        "a route with a cookie":        {path: "/quiet", withCookie: true},
        "a route without a cookie":     {path: "/quiet", withCookie: false},
        "an unrouted path with one":    {path: "/missing", withCookie: true},
        "an unrouted path without one": {path: "/missing", withCookie: false},
    }

    for name, testCase := range cases {
        storage := &operationCountingSessionStorage{Storage: session.NewInMemoryStorage(session.InMemoryStorageConfig{})}
        if saveErr := storage.Storage.Save(sessionOutageTestCookieValue, map[string]any{"userId": "u-1"}, time.Minute); nil != saveErr {
            t.Fatalf("unexpected error seeding the session: %v", saveErr)
        }

        sessionManager := session.NewManager(storage, session.ManagerConfig{Ttl: bound.Of(30 * time.Minute)})

        served := serveOverSessionManager(t, sessionManager, testCase.path, testCase.withCookie)

        if 0 != atomic.LoadInt64(&storage.loads) || 0 != atomic.LoadInt64(&storage.saves) || 0 != atomic.LoadInt64(&storage.deletes) {
            t.Fatalf("%s: expected no storage operation, got %d loads, %d saves, %d deletes", name, storage.loads, storage.saves, storage.deletes)
        }

        if "" != served.recorder.Header().Get("Set-Cookie") {
            t.Fatalf("%s: expected no cookie, got %q", name, served.recorder.Header().Get("Set-Cookie"))
        }
    }
}

func TestKernel_ASessionStorageOutageFilesOneRecordForTwoReaders(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/page",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            _, sessionReadErr := RequestSession(request)

            return nil, sessionReadErr
        },
    )

    countingLogger := &errorCountingLogger{}

    sessionManager := session.NewManager(&loadFailingSessionStorage{}, session.ManagerConfig{Ttl: bound.Of(30 * time.Minute)})
    serviceContainer := newHttpTestContainerWithSessionManager(sessionManager)
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, countingLogger, container.OverrideOptions{})

    listenerSawError := false
    event.EventDispatcherMustFromContainer(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent := eventValue.Payload().(*KernelRequestEvent)

            _, sessionReadErr := RequestSession(requestEvent.Request())
            listenerSawError = nil != sessionReadErr

            return nil
        },
        0,
    )

    recorder := httptest.NewRecorder()
    NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/page", nil))

    if false == listenerSawError || nethttp.StatusServiceUnavailable != recorder.Code {
        t.Fatalf("expected both readers to meet the outage and 503, got listener %v, status %d", listenerSawError, recorder.Code)
    }

    if 1 != atomic.LoadInt64(&countingLogger.errorCount) {
        t.Fatalf("expected one error record for two readers, got %d", atomic.LoadInt64(&countingLogger.errorCount))
    }
}

type outageCountingSessionStorage struct {
    saveCalls   int64
    deleteCalls int64
}

func (instance *outageCountingSessionStorage) Load(sessionId string) (map[string]any, bool, error) {
    return nil, false, exception.NewError("session backend down", nil, nil)
}

func (instance *outageCountingSessionStorage) Save(sessionId string, data map[string]any, ttl time.Duration) error {
    atomic.AddInt64(&instance.saveCalls, 1)

    return nil
}

func (instance *outageCountingSessionStorage) Delete(sessionId string) error {
    atomic.AddInt64(&instance.deleteCalls, 1)

    return nil
}

func (instance *outageCountingSessionStorage) CloseWithContext(closeContext context.Context) error {
    return nil
}

func TestKernel_EmptyOnOutageServesTheRouteAndAnswersNoCookie(t *testing.T) {
    for name, touch := range map[string]func(sessionInstance sessioncontract.Session){
        "reads":   func(sessionInstance sessioncontract.Session) { _ = sessionInstance.Get("cart") },
        "sets":    func(sessionInstance sessioncontract.Session) { sessionInstance.Set("cart", "one line") },
        "clears":  func(sessionInstance sessioncontract.Session) { sessionInstance.Clear() },
        "rotates": func(sessionInstance sessioncontract.Session) {},
    } {
        storage := &outageCountingSessionStorage{}
        sessionManager := session.NewManager(storage, session.ManagerConfig{Ttl: bound.Of(30 * time.Minute)})

        router := NewRouter()
        router.Handle(
            nethttp.MethodGet,
            "/page",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                sessionInstance, sessionErr := RequestSession(request)
                if nil != sessionErr {
                    return nil, sessionErr
                }

                touch(sessionInstance)

                if "rotates" == name {
                    _, rotateErr := RegenerateRequestSession(request)

                    return nil, rotateErr
                }

                if again, againErr := RequestSession(request); nil != againErr || again != sessionInstance {
                    return nil, exception.NewError("the second read answered another session", nil, againErr)
                }

                return TextResponse(nethttp.StatusOK, "ok"), nil
            },
        )

        countingLogger := &errorCountingLogger{}

        serviceContainer := newHttpTestContainerWithSessionManager(sessionManager)
        serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, countingLogger, container.OverrideOptions{})

        handler := NewKernel(router, KernelConfig{SessionPolicy: httpcontract.SessionPolicy{EmptyOnOutage: true}}).ServeHttp(serviceContainer)

        request := httptest.NewRequest(nethttp.MethodGet, "/page", nil)
        request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: sessionOutageTestCookieValue})

        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, request)

        expectedStatus := nethttp.StatusOK
        if "rotates" == name {
            expectedStatus = nethttp.StatusServiceUnavailable
        }

        if expectedStatus != recorder.Code {
            t.Fatalf("%s: expected %d, got %d", name, expectedStatus, recorder.Code)
        }

        if 0 != atomic.LoadInt64(&storage.saveCalls) || 0 != atomic.LoadInt64(&storage.deleteCalls) || "" != recorder.Header().Get("Set-Cookie") {
            t.Fatalf("%s: expected nothing persisted and no cookie, got %d saves, %d deletes, cookie %q", name, storage.saveCalls, storage.deleteCalls, recorder.Header().Get("Set-Cookie"))
        }

        if 1 != atomic.LoadInt64(&countingLogger.warningCount) || 0 != atomic.LoadInt64(&countingLogger.errorCount) {
            t.Fatalf("%s: expected one warning and no error record, got %d warnings and %d errors", name, countingLogger.warningCount, countingLogger.errorCount)
        }
    }
}
