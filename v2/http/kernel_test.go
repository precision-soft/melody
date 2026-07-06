package http

import (
    nethttp "net/http"
    "net/http/httptest"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v2/event"
    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    "github.com/precision-soft/melody/v2/exception"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    "github.com/precision-soft/melody/v2/session"
    sessioncontract "github.com/precision-soft/melody/v2/session/contract"
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

    req := httptest.NewRequest(nethttp.MethodGet, "/hello", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if nethttp.StatusAccepted != rec.Code {
        t.Fatalf("expected listener-replaced status %d, got %d", nethttp.StatusAccepted, rec.Code)
    }

    if "replaced" != rec.Body.String() {
        t.Fatalf("expected listener-replaced body, got %q", rec.Body.String())
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

    req := httptest.NewRequest(nethttp.MethodGet, "/boom", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if nethttp.StatusAccepted != rec.Code {
        t.Fatalf("expected listener-replaced status %d on panic-recovery path, got %d", nethttp.StatusAccepted, rec.Code)
    }

    if "recovered-replaced" != rec.Body.String() {
        t.Fatalf("expected listener-replaced body on panic-recovery path, got %q", rec.Body.String())
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

    /* @important an EventKernelResponse listener swapped the response, so the original file-backed body must be closed rather than leaked */
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

    /* @info the write fails after the headers were committed, so the first writeResponse persists the session and then panics, and the panic-recovery path re-enters writeResponse. */
    handler.ServeHTTP(&writeFailingResponseWriter{}, request)

    if 1 != storage.saveCount {
        t.Fatalf("expected the session to be persisted exactly once across the first write and the panic-recovery write, got %d", storage.saveCount)
    }
}

func TestKernel_ServeHttpClosesScopeWhenRequestLoggerSetupFails(t *testing.T) {
    recordingContainer := &scopeRecordingContainer{
        Container:    newHttpTestContainer(),
        failOverride: true,
    }

    handler := NewKernel(NewRouter()).ServeHttp(recordingContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/", nil)
    recorder := httptest.NewRecorder()

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected ServeHttp to panic when request logger setup fails")
        }

        if nil == recordingContainer.scope {
            t.Fatalf("expected a request scope to have been created")
        }

        if false == recordingContainer.scope.closed {
            t.Fatalf("expected the request scope to be closed even when request logger setup fails")
        }
    }()

    handler.ServeHTTP(recorder, request)
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

    req := httptest.NewRequest(nethttp.MethodGet, "/guarded", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if true == accessControlRan {
        t.Fatalf("expected the lower-priority listener to be skipped by the dispatcher abort")
    }

    if true == handlerRan {
        t.Fatalf("expected the handler not to run when the kernel.request dispatch aborted with partially-run listeners")
    }

    if nethttp.StatusInternalServerError != rec.Code {
        t.Fatalf("expected a fail-closed 500 when the kernel.request dispatch errored, got %d", rec.Code)
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

    req := httptest.NewRequest(nethttp.MethodGet, "/guarded", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if true == lowerPriorityRan {
        t.Fatalf("expected the lower-priority kernel.controller listener to be skipped by the dispatcher abort")
    }

    if true == handlerRan {
        t.Fatalf("expected the handler not to run when the kernel.controller dispatch aborted with partially-run listeners")
    }

    if nethttp.StatusInternalServerError != rec.Code {
        t.Fatalf("expected a fail-closed 500 when the kernel.controller dispatch errored, got %d", rec.Code)
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

    req := httptest.NewRequest(nethttp.MethodGet, "/denied", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if nethttp.StatusUnauthorized != rec.Code {
        t.Fatalf("expected the listener-set 401 to win over the synthesized 500, got %d", rec.Code)
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

    req := httptest.NewRequest(nethttp.MethodGet, "/still-served", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if nethttp.StatusOK != rec.Code {
        t.Fatalf("expected the response to be written despite the kernel.response dispatch error, got %d", rec.Code)
    }

    if "served" != rec.Body.String() {
        t.Fatalf("expected the handler body to be written, got %q", rec.Body.String())
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

func TestKernel_SessionSaveFailureDegradesToResponseWithoutPanic(t *testing.T) {
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

    req := httptest.NewRequest(nethttp.MethodGet, "/session-write", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if nethttp.StatusOK != rec.Code {
        t.Fatalf("expected the response to be delivered despite the session-store outage, got %d", rec.Code)
    }

    if "ok" != rec.Body.String() {
        t.Fatalf("expected the handler body despite the session-store outage, got %q", rec.Body.String())
    }

    if "" != rec.Header().Get("Set-Cookie") {
        t.Fatalf("expected no session cookie when the session could not be persisted, got %q", rec.Header().Get("Set-Cookie"))
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

    req := httptest.NewRequest(nethttp.MethodGet, "/session-write-boom", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if nethttp.StatusInternalServerError != rec.Code {
        t.Fatalf("expected the recovered 500 to be delivered despite the session-store outage, got %d", rec.Code)
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

    req := httptest.NewRequest(nethttp.MethodGet, "/logged-once", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if nethttp.StatusOK != rec.Code {
        t.Fatalf("expected the response despite the dispatch error, got %d", rec.Code)
    }

    /* @important the dispatcher already logs "event listener error" once and marks the returned wrapper as logged; the handler-response finalization block must respect that mark (logEventDispatchError) instead of re-logging it — the pre-fix inline logging produced two error lines for one failure */
    if 1 != atomic.LoadInt64(&countingLogger.errorCount) {
        t.Fatalf("expected the dispatch error to be logged exactly once on the handler-response path, got %d error logs", atomic.LoadInt64(&countingLogger.errorCount))
    }
}
