package http

import (
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/event"
    eventcontract "github.com/precision-soft/melody/event/contract"
    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/precision-soft/melody/session"
    sessioncontract "github.com/precision-soft/melody/session/contract"
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

    /* @important the dispatcher already logs "event listener error" once and marks the returned wrapper as logged; the handler-response finalization block must respect that mark (logEventDispatchError) instead of re-logging it — inline logging here would produce two error lines for one failure */
    if 1 != atomic.LoadInt64(&countingLogger.errorCount) {
        t.Fatalf("expected the dispatch error to be logged exactly once on the handler-response path, got %d error logs", atomic.LoadInt64(&countingLogger.errorCount))
    }
}

/* closeTrackingReader stands in for a file-backed response body (FileResponse / static ServeReader): the
only thing that matters here is whether the kernel closed the descriptor. */
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

/* panicOnceSessionStorage blows up on its first Save, the way a database driver does on a lost connection.
writeResponse persists the session before WriteToHttpResponseWriter registers the body's deferred Close, so
that panic unwinds with the response's descriptor still open. It succeeds afterwards so the kernel's recovery
path can finish and write the error response. */
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

/* @info writeResponse persists the session BEFORE WriteToHttpResponseWriter registers the body's deferred Close, so a panic in the session backend unwinds with the file-backed response assigned to finalResponse and its descriptor still open. The recover handler replaces finalResponse with an error response; unless it closes the discarded one, every such request leaks a file descriptor. */
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
    handler := NewKernel(router).ServeHttp(serviceContainer)

    handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/file", nil))

    if false == storage.panicked.Load() {
        t.Fatal("the session backend never panicked; the test does not exercise the recovery path")
    }

    if false == bodyReader.closed.Load() {
        t.Fatal("the discarded file-backed response body was never closed: one file descriptor leaks per request")
    }
}

/* @info net/http documents this sentinel as "abort the connection and suppress the log"; converting it into an error answered an aborted upload with a 500 and an error line, and a reverse proxy panics with it on every client disconnect mid-stream */
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

/* @info the kernel resolves the scheme through the configured forwarded-headers policy and publishes it on the request; a listener has no access to that policy, so without the attribute the access log reported http for every request a trusted proxy terminated as https */
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

/* @info A handler returning (nil, nil) was answered with an empty 204 written straight out, without kernel.response ever being dispatched — so the one hook that decorates a response never saw it. Measured with the framework's own cross-origin wiring, a nil-returning DELETE came back with no Access-Control-Allow-Origin at all while the identical explicit 204 carried the full set, and the access log recorded status 0. */
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

/* @info A listener may still replace the synthesized empty response, the same way it may replace any other. */
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

/* errorContextRecordingLogger captures every Error call with its context, so a test can assert not just
that something was logged but what the record carries. */
type errorContextRecordingLogger struct {
    mutex         sync.Mutex
    errorMessages []string
    errorContexts []loggingcontract.Context
}

func (instance *errorContextRecordingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *errorContextRecordingLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *errorContextRecordingLogger) Info(message string, context loggingcontract.Context) {}

func (instance *errorContextRecordingLogger) Warning(message string, context loggingcontract.Context) {
}

func (instance *errorContextRecordingLogger) Error(message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.errorMessages = append(instance.errorMessages, message)
    instance.errorContexts = append(instance.errorContexts, context)
}

func (instance *errorContextRecordingLogger) Emergency(message string, context loggingcontract.Context) {
}

func (instance *errorContextRecordingLogger) errorContextFor(message string) (loggingcontract.Context, bool) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for index, loggedMessage := range instance.errorMessages {
        if message == loggedMessage {
            return instance.errorContexts[index], true
        }
    }

    return nil, false
}

/* @info the recovery boundary must record the stack of the panic site: the error value alone names the symptom but not the line that raised it, and net/http's own stack print never fires for a panic this recovery absorbs — without the frames a production nil-pointer is unlocatable from the logs */

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

/* @info the error handler is application code invoked while the failed response's body is still open and held only in a local the recovery defer cannot see: a panic escaping it must not leak that body — the kernel recovers it, logs it with its stack, and serves the default error response with every close still running */

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

/* @info a urlencoded body whose read failed must refuse the request the way the json path refuses the identical condition: dispatching it would hand the handler a syntactically valid request whose form is simply empty, and an oversized submission would be processed as an empty one */

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

/* Session reports "no such session" the way a careless implementation does: by returning a nil pointer of its own session type, which is not equal to nil once it is carried in the interface. */
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

/* @info The session manager is a replaceable service, so the kernel must test what it hands back with IsNilInterface rather than against nil: a typed nil passes a bare comparison, the kernel skips NewSession and publishes it, and the first handler that touches the session dereferences nil. The request must be served a working session instead. */
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

            /* a typed nil reaches here as a non-nil interface and panics on the first call */
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
}

func (instance *warningRecordingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *warningRecordingLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *warningRecordingLogger) Info(message string, context loggingcontract.Context) {}

func (instance *warningRecordingLogger) Warning(message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.warningMessages = append(instance.warningMessages, message)
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

/* @info A handler that commits its own response and then rotates the session loses everything the session held: the rotation deletes the previous entry, and the response path refuses to store the replacement because no Set-Cookie can reach the client on a committed response. Refusing the write is right, but it must not be silent — without a line in the log this is indistinguishable from the ordinary case the branch exists for, a first-time visitor on a stream with nothing worth storing. */
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

/* @info a listener that stops propagation is entitled to answer the request, but not to answer it with a listener marked required never consulted: the fail-closed branch tested for the absence of a response, so a cache listener that stopped propagation ahead of access control had its cached page served to whoever asked */
func TestKernel_FailsClosedWhenARequiredListenerWasSkippedEvenThoughAResponseWasProduced(t *testing.T) {
    handlerRan := false

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/hello",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            handlerRan = true

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

    requiredRan := false
    requiredRegistration := dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            requiredRan = true

            return nil
        },
        20,
    )

    registrar, ok := dispatcher.(eventcontract.RequiredListenerRegistrar)
    if false == ok {
        t.Fatalf("expected the dispatcher to support required listeners")
    }
    registrar.MarkListenerRequired(requiredRegistration)

    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/hello", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if true == requiredRan {
        t.Fatalf("the required listener must not have run")
    }

    if true == handlerRan {
        t.Fatalf("the handler must not have run")
    }

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the request to fail closed with %d, got %d", nethttp.StatusInternalServerError, recorder.Code)
    }

    if true == strings.Contains(recorder.Body.String(), "from the cache") {
        t.Fatalf("the response produced by the stopping listener must not be served")
    }
}

/* @info a stopping listener that skipped nothing required keeps answering the request: the refusal must not cost every short-circuiting listener its response */
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
