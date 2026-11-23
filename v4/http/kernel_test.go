package http

import (
    "context"
    "errors"
    "fmt"
    "go/ast"
    "go/parser"
    "go/token"
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
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/internal/httpkernelseam"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/serializer"
    serializercontract "github.com/precision-soft/melody/v4/serializer/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
    "github.com/precision-soft/melody/v4/validation"
)

func TestKernel_ResponseListenerReplacesResponseOnSuccessPath(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/hello",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "original"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if false == ok {
                return nil
            }

            responseEvent.SetResponse(TextResponse(nethttp.StatusAccepted, "replaced"))
            return nil
        },
        eventcontract.ListenerConfig{},
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
        "/boom",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            panic("boom")
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if false == ok {
                return nil
            }

            responseEvent.SetResponse(TextResponse(nethttp.StatusAccepted, "recovered-replaced"))
            return nil
        },
        eventcontract.ListenerConfig{},
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
        "/stream-then-return",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            writer.WriteHeader(nethttp.StatusOK)

            return &Response{
                statusCode: nethttp.StatusOK,
                headers:    make(nethttp.Header),
                bodyReader: body,
            }, nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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
        "/file-then-swap",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return &Response{
                statusCode: nethttp.StatusOK,
                headers:    make(nethttp.Header),
                bodyReader: body,
            }, nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if false == ok {
                return nil
            }

            responseEvent.SetResponse(TextResponse(nethttp.StatusAccepted, "replaced"))
            return nil
        },
        eventcontract.ListenerConfig{},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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
        "/stream",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            writer.WriteHeader(nethttp.StatusOK)

            _, writeErr := writer.Write([]byte("streamed"))
            if nil != writeErr {
                return nil, writeErr
            }

            return nil, nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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
        "/ws",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
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
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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
        "/ws",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            hijacker, isHijacker := writer.(nethttp.Hijacker)
            if true == isHijacker {
                _, _, _ = hijacker.Hijack()
            }

            return nil, nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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
        "/stream-panic",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            writer.WriteHeader(nethttp.StatusOK)

            _, writeErr := writer.Write([]byte("partial"))
            if nil != writeErr {
                return nil, writeErr
            }

            panic("boom after streaming")
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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
        "/save",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatal("expected the request to carry a session")
            }

            sessionInstance.Set("key", "value")

            return TextResponse(nethttp.StatusOK, "body"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(storage)
    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

func TestKernel_FailsClosedWhenAKernelRequestListenerFails(t *testing.T) {
    handlerRan := false

    router := NewRouter()
    router.Handle(
        "/guarded",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            handlerRan = true

            return TextResponse(nethttp.StatusOK, "handled"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)

    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return exception.NewError("resolution listener failed", nil, nil)
        },
        eventcontract.ListenerConfig{Priority: 50},
    )

    accessControlRan := false
    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            accessControlRan = true

            return nil
        },
        eventcontract.ListenerConfig{Priority: 20},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/guarded", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if true == accessControlRan {
        t.Fatalf("expected the lower-priority listener behind the failing one not to run")
    }

    if true == handlerRan {
        t.Fatalf("expected the handler not to run when the kernel.request dispatch aborted with partially-run listeners")
    }

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected a fail-closed 500 when the kernel.request dispatch errored, got %d", recorder.Code)
    }
}

func TestKernel_FailsClosedWhenAKernelControllerListenerFails(t *testing.T) {
    handlerRan := false

    router := NewRouter()
    router.Handle(
        "/guarded",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            handlerRan = true

            return TextResponse(nethttp.StatusOK, "handled"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)

    dispatcher.AddListener(
        kernelcontract.EventKernelController,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return exception.NewError("controller guard listener failed", nil, nil)
        },
        eventcontract.ListenerConfig{Priority: 50},
    )

    lowerPriorityRan := false
    dispatcher.AddListener(
        kernelcontract.EventKernelController,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            lowerPriorityRan = true

            return nil
        },
        eventcontract.ListenerConfig{Priority: 20},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/guarded", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if true == lowerPriorityRan {
        t.Fatalf("expected the lower-priority kernel.controller listener behind the failing one not to run")
    }

    if true == handlerRan {
        t.Fatalf("expected the handler not to run when the kernel.controller dispatch aborted with partially-run listeners")
    }

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected a fail-closed 500 when the kernel.controller dispatch errored, got %d", recorder.Code)
    }
}

func TestKernel_AKernelRequestResponseSetBesideAFailureIsDroppedForTheErrorPage(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/denied",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "handled"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)

    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*KernelRequestEvent)
            if false == ok {
                return nil
            }

            requestEvent.SetResponse(JsonErrorResponse(nethttp.StatusUnauthorized, "denied"))

            return exception.NewError("denied-event dispatch failed", nil, nil)
        },
        eventcontract.ListenerConfig{Priority: 50},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/denied", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the response set beside the failure to be dropped for the fail-closed 500, got %d", recorder.Code)
    }
}

func TestKernel_AKernelControllerResponseSetBesideAFailureIsDroppedForTheErrorPage(t *testing.T) {
    handlerRan := false

    router := NewRouter()
    router.Handle(
        "/half-made",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            handlerRan = true

            return TextResponse(nethttp.StatusOK, "handled"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)

    body := &closeRecordingReadCloser{}
    dispatcher.AddListener(
        kernelcontract.EventKernelController,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            controllerEvent, ok := eventValue.Payload().(*KernelControllerEvent)
            if false == ok {
                return nil
            }

            controllerEvent.SetResponse(
                &Response{
                    statusCode: nethttp.StatusOK,
                    headers:    make(nethttp.Header),
                    bodyReader: body,
                },
            )

            return exception.NewError("controller listener failed after answering", nil, nil)
        },
        eventcontract.ListenerConfig{Priority: 50},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/half-made", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if true == handlerRan {
        t.Fatalf("expected the handler not to run when the kernel.controller dispatch failed")
    }

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the response set beside the controller failure to be dropped for the fail-closed 500, got %d", recorder.Code)
    }

    if 1 != body.closeCount {
        t.Fatalf("expected the dropped response body to be closed exactly once, got %d", body.closeCount)
    }
}

func TestKernel_AnUnloggedFailureJoinedBesideALoggedPanicReachesTheJournal(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/joined",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "handled"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    recordingLogger := &errorContextRecordingLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)

    dispatcher.DeclareEvent(kernelcontract.EventKernelRequest, eventcontract.EventConfig{TagsToRunAfterFailure: []string{"audit"}})

    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            panic("first listener panicked")
        },
        eventcontract.ListenerConfig{Priority: 50},
    )

    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return errors.New("second listener failed unlogged")
        },
        eventcontract.ListenerConfig{Priority: 20, Tags: []string{"audit"}},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/joined", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the fail-closed 500, got %d", recorder.Code)
    }

    recordingLogger.mutex.Lock()
    journal := fmt.Sprintf("%v %v", recordingLogger.errorMessages, recordingLogger.errorContexts)
    recordingLogger.mutex.Unlock()

    if false == strings.Contains(journal, "second listener failed unlogged") {
        t.Fatalf("expected the unlogged failure joined beside the logged panic to reach the journal, got %s", journal)
    }
}

func TestKernel_AJoinWhoseEveryFailureIsLoggedWritesNoSecondRecord(t *testing.T) {
    taggedRan := false

    router := NewRouter()
    router.Handle(
        "/joined",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "handled"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    recordingLogger := &errorContextRecordingLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)

    dispatcher.DeclareEvent(kernelcontract.EventKernelRequest, eventcontract.EventConfig{TagsToRunAfterFailure: []string{"audit"}})

    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            panic("first listener panicked")
        },
        eventcontract.ListenerConfig{Priority: 50},
    )

    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            taggedRan = true

            return exception.MarkLogged(exception.NewError("second listener failed logged", nil, nil))
        },
        eventcontract.ListenerConfig{Priority: 20, Tags: []string{"audit"}},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/joined", nil))

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the fail-closed 500, got %d", recorder.Code)
    }

    if false == taggedRan {
        t.Fatalf("expected the tagged listener to run after the panic, so the join is what the kernel read")
    }

    recordingLogger.mutex.Lock()
    defer recordingLogger.mutex.Unlock()

    for _, message := range recordingLogger.errorMessages {
        if "kernel request error" == message {
            t.Fatalf("expected no dispatch record when every joined failure is logged, got %v", recordingLogger.errorMessages)
        }
    }
}

func TestKernel_KernelRequestStoppingListenerThatAnswersStillFailsClosedBeforeARequiredListener(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/admin",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "handled"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)

    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        eventcontract.ListenerConfig{Priority: 10, Failure: eventcontract.ListenerFailureConfig{Required: true}},
    )

    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*KernelRequestEvent)
            if false == ok {
                return nil
            }

            requestEvent.SetResponse(TextResponse(nethttp.StatusOK, "from the cache"))
            eventValue.StopPropagation()

            return nil
        },
        eventcontract.ListenerConfig{Priority: 50},
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

/* the refusal decides the status only when no required listener was skipped, so the 500 here proves the opt-out is not read for a listener that failed */
func TestKernel_KernelRequestFailingMaySkipListenerThatRefusesStillFailsClosedBeforeARequiredListener(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/admin",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "handled"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)

    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        eventcontract.ListenerConfig{Priority: 10, Failure: eventcontract.ListenerFailureConfig{Required: true}},
    )

    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*KernelRequestEvent)
            if false == ok {
                return nil
            }

            requestEvent.Refuse(exception.Forbidden("not from the cache"))

            return errors.New("the cache listener's own failure")
        },
        eventcontract.ListenerConfig{Priority: 50, Failure: eventcontract.ListenerFailureConfig{MaySkipRequiredListeners: true}},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/admin", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the fail-closed 500 rather than the refusal's 403, got %d", recorder.Code)
    }
}

func TestKernel_KernelControllerStoppingListenerThatAnswersStillFailsClosedBeforeARequiredListener(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/admin",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "handled"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)

    dispatcher.AddListener(
        kernelcontract.EventKernelController,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        eventcontract.ListenerConfig{Priority: 10, Failure: eventcontract.ListenerFailureConfig{Required: true}},
    )

    dispatcher.AddListener(
        kernelcontract.EventKernelController,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            controllerEvent, ok := eventValue.Payload().(*KernelControllerEvent)
            if false == ok {
                return nil
            }

            controllerEvent.SetResponse(TextResponse(nethttp.StatusOK, "from the cache"))
            eventValue.StopPropagation()

            return nil
        },
        eventcontract.ListenerConfig{Priority: 50},
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
        "/still-served",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "served"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return exception.NewError("response listener failed", nil, nil)
        },
        eventcontract.ListenerConfig{},
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

func (instance *failingSessionStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    return nil, false, nil
}

func (instance *failingSessionStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    return exception.NewError("session backend down", nil, nil)
}

func (instance *failingSessionStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    return exception.NewError("session backend down", nil, nil)
}

func (instance *failingSessionStorage) CloseWithContext(closeContext context.Context) error {
    return nil
}

func TestKernel_SessionSaveFailureAnswersServiceUnavailable(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/session-write",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatalf("expected the session request attribute to be present")
            }
            sessionInstance.Set("key", "value")

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(&failingSessionStorage{})

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/session-write", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusServiceUnavailable != recorder.Code || "5" != recorder.Header().Get("Retry-After") {
        t.Fatalf("expected a session-store outage to answer 503 with Retry-After rather than the handler's success, got %d and %q", recorder.Code, recorder.Header().Get("Retry-After"))
    }

    if "ok" == recorder.Body.String() {
        t.Fatalf("expected the handler's success body to be replaced when its session write was lost")
    }

    if "" != recorder.Header().Get("Set-Cookie") {
        t.Fatalf("expected no session cookie when the session could not be persisted, got %q", recorder.Header().Get("Set-Cookie"))
    }
}

func TestKernel_SessionSaveFailureAnswersTheKernelsRetryAfter(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/session-write",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatalf("expected the session request attribute to be present")
            }
            sessionInstance.Set("key", "value")

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(&failingSessionStorage{})

    handler := NewKernel(router, KernelConfig{UnavailableRetryAfter: bound.Of(30 * time.Second)}).ServeHttp(serviceContainer)

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/session-write", nil))

    if nethttp.StatusServiceUnavailable != recorder.Code || "30" != recorder.Header().Get("Retry-After") {
        t.Fatalf("expected the failed save answered 503 with the kernel's Retry-After, got %d and %q", recorder.Code, recorder.Header().Get("Retry-After"))
    }
}

func TestKernel_SessionSaveFailureOnPanicRecoveryPathStillDeliversAResponse(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/session-write-boom",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, _ := RequestSession(request)
            sessionInstance.Set("key", "value")

            panic("handler exploded after touching the session")
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(&failingSessionStorage{})

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/session-write-boom", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusServiceUnavailable != recorder.Code {
        t.Fatalf("expected the session-store outage to answer the recovered request 503, got %d", recorder.Code)
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

func (instance *errorCountingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *errorCountingLogger) CauseChainDepth() int {
    return 8
}

func TestKernel_HandlerPathResponseDispatchErrorRespectsAlreadyLogged(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/logged-once",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    countingLogger := &errorCountingLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, countingLogger, container.OverrideOptions{})

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return exception.NewError("response listener failure", nil, nil)
        },
        eventcontract.ListenerConfig{},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

func (instance *panicOnceSessionStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    return nil, false, nil
}

func (instance *panicOnceSessionStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    if true == instance.panicked.CompareAndSwap(false, true) {
        panic("session backend exploded")
    }

    return nil
}

func (instance *panicOnceSessionStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    return nil
}

func (instance *panicOnceSessionStorage) CloseWithContext(closeContext context.Context) error {
    return nil
}

func TestKernel_PanicRecoveryClosesTheDiscardedFileBackedResponse(t *testing.T) {
    bodyReader := &closeTrackingReader{}
    storage := &panicOnceSessionStorage{}

    router := NewRouter()
    router.Handle(
        "/file",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatal("expected the request to carry a session")
            }

            sessionInstance.Set("key", "value")

            response := &Response{}
            response.SetStatusCode(nethttp.StatusOK)
            response.SetHeaders(make(nethttp.Header))
            response.SetBodyReader(bodyReader)

            return response, nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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

func TestKernel_PanicRecoveryClosesTheDiscardedResponseWhenAListenerLeavesByExit(t *testing.T) {
    original := &closeRecordingReadCloser{}
    storage := &panicOnceSessionStorage{}

    router := NewRouter()
    router.Handle(
        "/file-then-exit",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
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
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(storage)

    publishes := 0
    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            publishes++
            if 2 > publishes {
                return nil
            }

            exception.Exit(exception.NewExitError(7, exception.NewError("the listener is leaving", nil, nil)))

            return nil
        },
        eventcontract.ListenerConfig{},
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

    if 0 == recorder.Body.Len() {
        t.Fatal("expected the kernel's last guard to have answered; the request produced no response at all")
    }

    if 1 != original.closeCount {
        t.Fatalf("expected the discarded file-backed body to be closed exactly once, got %d: one descriptor leaks per request", original.closeCount)
    }
}

func TestKernel_PanicRecoveryClosesTheResponseAListenerSwappedInNotTheDiscardedOneTwice(t *testing.T) {
    original := &closeRecordingReadCloser{}
    swapped := &closeRecordingReadCloser{}
    storage := &panicOnceSessionStorage{}

    router := NewRouter()
    router.Handle(
        "/file-then-swap",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
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
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(storage)

    swappedOnce := false
    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
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
        eventcontract.ListenerConfig{},
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

func TestKernel_PanicRecoveryDoesNotCloseAResponseTheRepublishHandsBack(t *testing.T) {
    original := &closeRecordingReadCloser{}
    swapped := &closeRecordingReadCloser{}
    storage := &panicOnceSessionStorage{}

    router := NewRouter()
    router.Handle(
        "/file-then-swap-always",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
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
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainerWithSessionStorage(storage)

    swappedResponse := &Response{
        statusCode: nethttp.StatusAccepted,
        headers:    make(nethttp.Header),
        bodyReader: swapped,
    }

    publishes := 0
    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if false == ok {
                return nil
            }

            publishes++
            responseEvent.SetResponse(swappedResponse)

            return nil
        },
        eventcontract.ListenerConfig{},
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

    if 1 != swapped.closeCount {
        t.Fatalf("expected the body the re-publish handed back to be closed once, by the write that served it, got %d", swapped.closeCount)
    }
}

func TestKernel_AbortHandlerPanicClosesTheConnectionWithoutAResponse(t *testing.T) {
    router := NewRouter()

    router.Handle(
        "/abort",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            panic(nethttp.ErrAbortHandler)
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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

func TestKernel_PublishesThePolicyResolvedSchemeOnTheRequest(t *testing.T) {
    router := NewRouter()

    observedScheme := ""

    router.Handle(
        "/scheme",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            if schemeValue, exists := request.Attributes().Get(RequestAttributeScheme); true == exists {
                if schemeString, isString := schemeValue.(string); true == isString {
                    observedScheme = schemeString
                }
            }

            return TextResponse(200, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    kernelInstance := NewKernel(router, KernelConfig{ForwardedHeadersPolicy: httpcontract.ForwardedHeadersPolicy{
    TrustForwardedHeaders: true,
    TrustedProxyList:      []string{"10.0.0.0/8"},
}})

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

func (instance *loadFailingSessionStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    return nil, false, exception.NewError("session backend down", nil, nil)
}

func (instance *loadFailingSessionStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    return nil
}

func (instance *loadFailingSessionStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    return nil
}

func (instance *loadFailingSessionStorage) CloseWithContext(closeContext context.Context) error {
    return nil
}

type erroringSessionManager struct {
    inner              sessioncontract.Manager
    sessionErr         error
    sessionBesideError sessioncontract.Session
    newSessionErr      error
    newSessionCalls    int
}

func (instance *erroringSessionManager) Session(runtimeInstance containercontract.Runtime, sessionId string) (sessioncontract.Session, error) {
    if nil != instance.sessionErr {
        return instance.sessionBesideError, instance.sessionErr
    }

    return instance.inner.Session(runtimeInstance, sessionId)
}

func (instance *erroringSessionManager) NewSession(runtimeInstance containercontract.Runtime) (sessioncontract.Session, error) {
    instance.newSessionCalls = instance.newSessionCalls + 1

    if nil != instance.newSessionErr {
        return nil, instance.newSessionErr
    }

    return instance.inner.NewSession(runtimeInstance)
}

func (instance *erroringSessionManager) RegenerateSession(runtimeInstance containercontract.Runtime, sessionInstance sessioncontract.Session) (sessioncontract.Session, error) {
    return instance.inner.RegenerateSession(runtimeInstance, sessionInstance)
}

func (instance *erroringSessionManager) SaveSession(runtimeInstance containercontract.Runtime, sessionInstance sessioncontract.Session) error {
    return instance.inner.SaveSession(runtimeInstance, sessionInstance)
}

func (instance *erroringSessionManager) DeleteSession(runtimeInstance containercontract.Runtime, sessionId string) error {
    return instance.inner.DeleteSession(runtimeInstance, sessionId)
}

func (instance *erroringSessionManager) StorageTimeout() bound.Duration {
    return bound.Duration{}
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
        "/page",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            served.handlerCalls = served.handlerCalls + 1

            publishedSession, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                return nil, sessionReadErr
            }

            served.handlerSawSession = false == internal.IsNilInterface(publishedSession)

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )
    router.Handle(
        "/quiet",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            served.handlerCalls = served.handlerCalls + 1

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    countingLogger := &errorCountingLogger{}

    serviceContainer := newHttpTestContainerWithSessionManager(sessionManager)
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, countingLogger, container.OverrideOptions{})

    event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelTerminate,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            served.terminateCalls = served.terminateCalls + 1

            return nil
        },
        eventcontract.ListenerConfig{},
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
        "/page",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            _, sessionReadErr := RequestSession(request)

            return nil, sessionReadErr
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    countingLogger := &errorCountingLogger{}

    sessionManager := session.NewManager(&loadFailingSessionStorage{}, session.ManagerConfig{Ttl: bound.Of(30 * time.Minute)})
    serviceContainer := newHttpTestContainerWithSessionManager(sessionManager)
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, countingLogger, container.OverrideOptions{})

    listenerCalls := 0
    listenerSawUnavailable := false
    event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            exceptionEvent := eventValue.Payload().(*KernelExceptionEvent)

            listenerCalls = listenerCalls + 1
            listenerSawUnavailable = exception.AnswersUnavailable(exceptionEvent.Err())

            exceptionEvent.SetResponse(TextResponse(nethttp.StatusTeapot, "answered by the application"))

            return nil
        },
        eventcontract.ListenerConfig{},
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

type typedNilErrorSessionManager struct {
    sessioncontract.Manager
}

func (instance *typedNilErrorSessionManager) Session(runtimeInstance containercontract.Runtime, sessionId string) (sessioncontract.Session, error) {
    var typedNil *exception.Error

    sessionInstance, _ := instance.Manager.Session(runtimeInstance, sessionId)

    return sessionInstance, typedNil
}

func (instance *typedNilErrorSessionManager) NewSession(runtimeInstance containercontract.Runtime) (sessioncontract.Session, error) {
    var typedNil *exception.Error

    sessionInstance, _ := instance.Manager.NewSession(runtimeInstance)

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

    halfMade := mustSession(innerManager.NewSession(sessionRuntime()))
    halfMade.Set("user", "alice")

    sessionManager := &erroringSessionManager{
        inner:              innerManager,
        sessionErr:         exception.MarkInfrastructureFailure(errors.New("session store is down")),
        sessionBesideError: halfMade,
    }

    router := NewRouter()
    router.Handle(
        "/page",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            _, sessionReadErr := RequestSession(request)

            return nil, sessionReadErr
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )
    serviceContainer := newHttpTestContainerWithSessionManager(sessionManager)

    responsePanics := true
    event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            if true == responsePanics {
                responsePanics = false

                panic(exception.NewExitError(1, exception.NewError("response listener exited", nil, nil)))
            }

            return nil
        },
        eventcontract.ListenerConfig{},
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
    sessionManager := session.SessionMustFromResolver(t.Context(), serviceContainer)

    republishedId := ""

    router := NewRouter()
    router.Handle(
        "/republish",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            replacement := mustSession(sessionManager.NewSession(sessionRuntime()))
            replacement.Set("userId", "u-1")

            _ = SetRequestSession(request, replacement)

            republishedId = replacement.Id()

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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
    sessionManager := session.SessionMustFromResolver(t.Context(), serviceContainer)

    preRotationId := ""
    rotatedId := ""

    router := NewRouter()
    router.Handle(
        "/login",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatal("expected the request to carry a session")
            }

            sessionInstance.Set("userId", "u-1")

            saveErr := sessionManager.SaveSession(sessionRuntime(), sessionInstance)
            if nil != saveErr {
                return nil, saveErr
            }

            preRotationId = sessionInstance.Id()

            rotated, rotateErr := sessionManager.RegenerateSession(sessionRuntime(), sessionInstance)
            if nil != rotateErr {
                return nil, rotateErr
            }

            _ = SetRequestSession(request, rotated)

            rotatedId = rotated.Id()

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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

    if nil != mustSession(sessionManager.Session(sessionRuntime(), preRotationId)) {
        t.Fatalf("expected the pre-rotation session to be gone from storage")
    }

    reloaded := mustSession(sessionManager.Session(sessionRuntime(), rotatedId))
    if nil == reloaded {
        t.Fatalf("expected the rotated session to be stored")
    }

    if "u-1" != reloaded.String("userId") {
        t.Fatalf("expected the values to survive the rotation, got %q", reloaded.String("userId"))
    }
}

func TestKernel_RotationWithoutRepublishingClearsTheAbandonedSession(t *testing.T) {
    serviceContainer := newHttpTestContainerWithSessionStorage(session.NewInMemoryStorage(session.InMemoryStorageConfig{}))
    sessionManager := session.SessionMustFromResolver(t.Context(), serviceContainer)

    existingSession := mustSession(sessionManager.NewSession(sessionRuntime()))
    existingSession.Set("userId", "anonymous")

    saveErr := sessionManager.SaveSession(sessionRuntime(), existingSession)
    if nil != saveErr {
        t.Fatalf("unexpected error: %v", saveErr)
    }

    existingId := existingSession.Id()

    router := NewRouter()
    router.Handle(
        "/login-without-republish",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatal("expected the request to carry a session")
            }

            rotated, rotateErr := sessionManager.RegenerateSession(sessionRuntime(), sessionInstance)
            if nil != rotateErr {
                return nil, rotateErr
            }

            rotated.Set("userId", "u-1")

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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

    if nil != mustSession(sessionManager.Session(sessionRuntime(), existingId)) {
        t.Fatalf("expected the pre-rotation session to be gone from storage")
    }
}

func TestKernel_SessionCookiePolicyWithoutSameSiteKeepsTheLaxDefault(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/session-write",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatal("expected the request to carry a session")
            }

            sessionInstance.Set("key", "value")

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    kernelInstance := NewKernel(router, KernelConfig{SessionCookiePolicy: httpcontract.SessionCookiePolicy{
        Domain: "example.com",
    }})

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

    router.Handle(
        "/owned",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
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
        "/nothing",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, nil
        },
        RouteOptions{Methods: []string{nethttp.MethodDelete}},
    )

    serviceContainer := newHttpTestContainer()

    dispatchCount := 0
    observedStatusCode := -1

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
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
        eventcontract.ListenerConfig{},
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

func TestKernel_ResponseListenerMayReplaceTheSynthesizedEmptyResponse(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/nothing",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if false == ok {
                return nil
            }

            responseEvent.SetResponse(TextResponse(nethttp.StatusTeapot, "replaced"))

            return nil
        },
        eventcontract.ListenerConfig{},
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

func TestKernel_PanicRecoveryLogsPanicSiteStack(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/boom",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            panic("handler exploded")
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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

    event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            panic("listener exploded")
        },
        eventcontract.ListenerConfig{},
    )

    return serviceContainer
}

func TestKernel_AListenerPanicOnHandlerErrorPathClosesBodyAndDelivers500(t *testing.T) {
    bodyReader := &closeTrackingReader{}

    router := NewRouter()
    router.Handle(
        "/fail-with-open-body",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            response := &Response{
                statusCode: nethttp.StatusOK,
                headers:    make(nethttp.Header),
                bodyReader: bodyReader,
            }

            return response, exception.NewError("handler failed after opening the body", nil, nil)
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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

    kernel := NewKernel(NewRouter(), KernelConfig{NotFoundHandler: func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        response := &Response{
            statusCode: nethttp.StatusNotFound,
            headers:    make(nethttp.Header),
            bodyReader: bodyReader,
        }

        return response, exception.NewError("not found handler failed after opening the body", nil, nil)
    }})
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
        "/session-write-then-file",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, _ := RequestSession(request)
            sessionInstance.Set("key", "value")

            response := &Response{
                statusCode: nethttp.StatusOK,
                headers:    make(nethttp.Header),
                bodyReader: bodyReader,
            }

            return response, nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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

func TestKernel_OversizedUrlencodedForm_TheReadAnswers413(t *testing.T) {
    handlerInvoked := false

    router := NewRouter()
    router.Handle(
        "/form",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            if _, formErr := request.Post(); nil != formErr {
                return nil, formErr
            }

            handlerInvoked = true

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodPost}},
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
        t.Fatalf("expected the handler to be answered the form error for an oversized urlencoded form")
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

func TestKernel_BrokenUrlencodedBody_TheReadAnswers400(t *testing.T) {
    handlerInvoked := false

    router := NewRouter()
    router.Handle(
        "/form",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            if _, formErr := request.Post(); nil != formErr {
                return nil, formErr
            }

            handlerInvoked = true

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodPost}},
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
        t.Fatalf("expected the handler to be answered the form error for a broken urlencoded body")
    }
}

type typedNilReturningSessionManager struct {
    delegate sessioncontract.Manager
}

func (instance *typedNilReturningSessionManager) Session(runtimeInstance containercontract.Runtime, sessionId string) (sessioncontract.Session, error) {
    var typedNil *session.Session

    return typedNil, nil
}

func (instance *typedNilReturningSessionManager) NewSession(runtimeInstance containercontract.Runtime) (sessioncontract.Session, error) {
    return instance.delegate.NewSession(runtimeInstance)
}

func (instance *typedNilReturningSessionManager) RegenerateSession(runtimeInstance containercontract.Runtime, sessionInstance sessioncontract.Session) (sessioncontract.Session, error) {
    return instance.delegate.RegenerateSession(runtimeInstance, sessionInstance)
}

func (instance *typedNilReturningSessionManager) SaveSession(runtimeInstance containercontract.Runtime, sessionInstance sessioncontract.Session) error {
    return instance.delegate.SaveSession(runtimeInstance, sessionInstance)
}

func (instance *typedNilReturningSessionManager) DeleteSession(runtimeInstance containercontract.Runtime, sessionId string) error {
    return instance.delegate.DeleteSession(runtimeInstance, sessionId)
}

func (instance *typedNilReturningSessionManager) StorageTimeout() bound.Duration {
    return bound.Duration{}
}

func (instance *typedNilReturningSessionManager) CloseWithContext(closeContext context.Context) error {
    return instance.delegate.CloseWithContext(context.Background())
}

func TestKernel_MintsASessionWhenTheManagerAnswersWithATypedNil(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/session",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            publishedSession, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                return TextResponse(nethttp.StatusInternalServerError, "no session"), nil
            }

            publishedSession.Set("touched", "yes")

            return TextResponse(nethttp.StatusOK, publishedSession.Id()), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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

func (instance *warningRecordingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *warningRecordingLogger) CauseChainDepth() int {
    return 8
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

func TestKernel_TheMethodNotAllowedRecordRedactsQueryValues(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/only-post",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "posted"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodPost}},
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

func TestKernel_LogsWhenACommittedResponseDropsARotatedSession(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/stream",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            writer.WriteHeader(nethttp.StatusOK)
            _, _ = writer.Write([]byte("streamed"))

            if _, err := RegenerateRequestSession(request); nil != err {
                return nil, err
            }

            return nil, nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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

func TestKernel_ServesTheResponseOfAStoppingListenerWhenNothingRequiredWasSkipped(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/hello",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "handler"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*KernelRequestEvent)
            if false == ok {
                return nil
            }

            requestEvent.SetResponse(TextResponse(nethttp.StatusOK, "from the cache"))
            eventValue.StopPropagation()

            return nil
        },
        eventcontract.ListenerConfig{Priority: 100},
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

func TestKernel_ServeHttpAnswersARequestWhoseTerminateListenerPanics(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/hello",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "handler"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainer()

    event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelTerminate,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            panic("terminate listener")
        },
        eventcontract.ListenerConfig{},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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
        "/hello",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            var unassignedResponse *Response

            return unassignedResponse, nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainer()

    responseSeenByListener := false

    event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if false == ok {
                return nil
            }

            responseSeenByListener = nil != responseEvent.Response()

            return nil
        },
        eventcontract.ListenerConfig{},
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

type explodingError struct{}

func (instance *explodingError) Error() string {
    panic("the error cannot render itself")
}

func TestKernel_ServeHttpClosesTheChainResponseWhenTheErrorLogPanics(t *testing.T) {
    bodyReader := &closeRecordingReadCloser{}

    router := NewRouter()
    router.Handle(
        "/hello",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            response := EmptyResponse(nethttp.StatusOK)
            response.SetBodyReader(bodyReader)

            return response, &explodingError{}
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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

func (instance *failingSaveSessionManager) Session(runtimeInstance containercontract.Runtime, sessionId string) (sessioncontract.Session, error) {
    return instance.inner.Session(runtimeInstance, sessionId)
}

func (instance *failingSaveSessionManager) NewSession(runtimeInstance containercontract.Runtime) (sessioncontract.Session, error) {
    return instance.inner.NewSession(runtimeInstance)
}

func (instance *failingSaveSessionManager) RegenerateSession(runtimeInstance containercontract.Runtime, sessionInstance sessioncontract.Session) (sessioncontract.Session, error) {
    return instance.inner.RegenerateSession(runtimeInstance, sessionInstance)
}

func (instance *failingSaveSessionManager) SaveSession(runtimeInstance containercontract.Runtime, sessionInstance sessioncontract.Session) error {
    return exception.NewError("session backend is down", nil, exception.MarkInfrastructureFailure(errors.New("connection refused")))
}

func (instance *failingSaveSessionManager) DeleteSession(runtimeInstance containercontract.Runtime, sessionId string) error {
    return instance.inner.DeleteSession(runtimeInstance, sessionId)
}

func (instance *failingSaveSessionManager) StorageTimeout() bound.Duration {
    return bound.Duration{}
}

func (instance *failingSaveSessionManager) CloseWithContext(closeContext context.Context) error {
    return instance.inner.CloseWithContext(context.Background())
}

func TestKernel_ASessionSaveOutageReachesTerminateAsTheStatusTheClientReceived(t *testing.T) {
    manager := &failingSaveSessionManager{
        inner: session.NewManager(session.NewInMemoryStorage(session.InMemoryStorageConfig{}), session.ManagerConfig{Ttl: bound.Of(30*time.Minute)}),
    }

    serviceContainer := newHttpTestContainerWithSessionManager(manager)

    router := NewRouter()
    router.Handle(
        "/login",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatalf("expected the kernel to publish a session on the request")
            }
            sessionInstance.Set("identity", "someone")

            return TextResponse(nethttp.StatusOK, "welcome"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    terminateStatus := -1
    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelTerminate,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            terminateEvent, ok := eventValue.Payload().(*KernelTerminateEvent)
            if false == ok || nil == terminateEvent.Response() {
                return nil
            }

            terminateStatus = terminateEvent.Response().StatusCode()

            return nil
        },
        eventcontract.ListenerConfig{},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/login", nil))

    if nethttp.StatusServiceUnavailable != recorder.Code {
        t.Fatalf("expected the save outage to answer the client 503, got %d", recorder.Code)
    }

    if nethttp.StatusServiceUnavailable != terminateStatus {
        t.Fatalf("expected kernel.terminate to see the 503 the client received, got %d", terminateStatus)
    }
}

func TestKernel_AnOuterMiddlewarePanicAfterNextClosesTheChainResponse(t *testing.T) {
    bodyReader := &closeRecordingReadCloser{}

    router := NewRouter()
    router.Handle(
        "/stream",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            response := EmptyResponse(nethttp.StatusOK)
            response.SetBodyReader(bodyReader)

            return response, nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    panickingOuterMiddleware := func(next httpcontract.Handler) httpcontract.Handler {
        return func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            response, nextErr := next(runtimeInstance, writer, request)
            _ = response
            _ = nextErr

            panic(exception.NewError("outer middleware failed after the handler answered", nil, nil))
        }
    }

    kernelInstance := NewKernel(router, KernelConfig{Middlewares: []httpcontract.Middleware{panickingOuterMiddleware}})
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
        "/fail",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, exception.NewError("kernel test failure", nil, nil)
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )
    router.Handle(
        "/ok",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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

func TestLogHandlerError_ACancellationWithALiveRequestContextStaysAnError(t *testing.T) {
    capture := &exceptionListenerCaptureLogger{}

    logHandlerError(capture, "controller handler error", context.Canceled, httptest.NewRequest(nethttp.MethodGet, "/slow", nil))

    if 1 != capture.errorCalls || 0 != capture.warningCalls {
        t.Fatalf("expected one error and no warning, got %d errors %d warnings", capture.errorCalls, capture.warningCalls)
    }
}

func TestNewKernel_CopiesTheTrustedProxyList(t *testing.T) {
    trustedProxyList := []string{"10.0.0.0/8"}
    kernel := NewKernel(NewRouter(), KernelConfig{ForwardedHeadersPolicy: httpcontract.ForwardedHeadersPolicy{
        TrustForwardedHeaders: true,
        TrustedProxyList:      trustedProxyList,
    }})

    trustedProxyList[0] = "0.0.0.0/0"

    if "10.0.0.0/8" != kernel.policies.forwardedHeadersPolicy.TrustedProxyList[0] {
        t.Fatalf("expected the kernel to keep its own copy of the trusted list, got %q", kernel.policies.forwardedHeadersPolicy.TrustedProxyList[0])
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

func (instance *recordCountingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *recordCountingLogger) CauseChainDepth() int {
    return 8
}

func serveAndCountErrorRecords(t *testing.T, handler httpcontract.Handler) *recordCountingLogger {
    t.Helper()

    router := NewRouter()
    router.Handle("/subject", handler, RouteOptions{Methods: []string{nethttp.MethodGet}})

    countingLogger := &recordCountingLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, countingLogger, container.OverrideOptions{})
    NewKernel(router, KernelConfig{}).
        ServeHttp(serviceContainer).
        ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/subject", nil))

    return countingLogger
}

func TestKernel_AForeignHandlerErrorFilesOneRecordNotTwo(t *testing.T) {
    countingLogger := serveAndCountErrorRecords(
        t,
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
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
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
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
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
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
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
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
        "/articles",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "articles"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainer()

    kernelInstance := NewKernel(router, KernelConfig{MethodPolicy: MethodPolicy{AutomaticOptionsDisabled: true}})

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
        "/articles",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "articles"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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
        "/slow",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            close(handlerEntered)
            <-releaseHandler

            return TextResponse(nethttp.StatusOK, "served"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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

func TestKernel_OpenRequestScopesIsReleasedWhenTheHandlerPanics(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/boom",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            panic("boom")
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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
        "/admin/*path...",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            handlerRan = true
            return TextResponse(nethttp.StatusOK, "admin"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    serviceContainer := newHttpTestContainer()
    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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
            "/*path...",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                handlerRan = true
                return TextResponse(nethttp.StatusOK, "admin"), nil
            },
            RouteOptions{Methods: []string{nethttp.MethodGet}},
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

func TestKernel_RefusesALeadingWhitespacePathAHandlerInFrontHandedIt(t *testing.T) {
    for _, rawPath := range []string{"/api%20/public", "/api%09/public", "/api%C2%A0/public"} {
        handlerRan := false

        router := NewRouter()
        router.Handle(
            "/*path...",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                handlerRan = true
                return TextResponse(nethttp.StatusOK, "public"), nil
            },
            RouteOptions{Methods: []string{nethttp.MethodGet}},
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
            "/public",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                reached = "public"
                return TextResponse(nethttp.StatusOK, "public"), nil
            },
            RouteOptions{Methods: []string{nethttp.MethodGet}},
        )
        router.Handle(
            "/*path...",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                reached = "catch-all"
                return TextResponse(nethttp.StatusOK, "catch-all"), nil
            },
            RouteOptions{Methods: []string{nethttp.MethodGet}},
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
        "/admin/*path...",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            handlerRan = true
            return TextResponse(nethttp.StatusOK, "admin"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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

func TestNormalizeBodyLimitError_MapsMaxBytesErrorTo413(t *testing.T) {
    maxBytesError := &nethttp.MaxBytesError{Limit: 1048576}

    normalized := normalizeBodyLimitError(maxBytesError, 1048576)

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
    if false == exception.ChainIs(normalized, ErrRequestBodyTooLarge) {
        t.Fatalf("expected the sentinel in the cause chain")
    }
}

func TestNormalizeBodyLimitError_KeepsTheFrameworksOwn413(t *testing.T) {
    built := newRequestBodyTooLargeError(16, &nethttp.MaxBytesError{Limit: 16})

    if built != normalizeBodyLimitError(built, 16) {
        t.Fatalf("expected the framework's 413 to be returned unchanged")
    }
}

func TestNormalizeBodyLimitError_LeavesOtherErrorsUntouched(t *testing.T) {
    plainErr := errors.New("some handler failure")

    if plainErr != normalizeBodyLimitError(plainErr, 1) {
        t.Fatalf("expected a non-body-limit error to be returned unchanged")
    }

    if nil != normalizeBodyLimitError(nil, 1) {
        t.Fatalf("expected nil to stay nil")
    }
}

func TestKernel_RefusesAMalformedTrustedProxyEntry(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewKernel(NewRouter(), KernelConfig{ForwardedHeadersPolicy: httpcontract.ForwardedHeadersPolicy{
                TrustForwardedHeaders: true,
                TrustedProxyList:      []string{"10.0.0.0/8", "10.0.0"},
            }})
        },
        "trusted proxy entry is neither a CIDR prefix nor an address",
    )
}

func TestKernel_InstallMiddlewares_AfterServing_Refused(t *testing.T) {
    kernel := NewKernel(NewRouter(), KernelConfig{})

    _ = kernel.ServeHttp(newHttpTestContainer())

    defer func() {
        refusal, isRefusal := recover().(*exception.Error)
        if false == isRefusal || "may not configure the http kernel after it started serving" != refusal.Message() || "InstallMiddlewares" != refusal.Context()["door"] {
            t.Fatalf("expected the serving refusal naming the door, got %v", refusal)
        }
    }()

    kernel.InstallMiddlewares(httpkernelseam.NewToken(), []httpcontract.Middleware{passThroughKernelMiddleware})
}

func TestKernel_InstallMiddlewares_ZeroToken_Refused(t *testing.T) {
    kernel := NewKernel(NewRouter(), KernelConfig{})

    testhelper.AssertPanicsWithError(t, func() {
        kernel.InstallMiddlewares(httpkernelseam.Token{}, []httpcontract.Middleware{passThroughKernelMiddleware})
    }, "http kernel InstallMiddlewares is called by the framework only")

    if 0 != len(kernel.middlewares) {
        t.Fatalf("expected the refused installation to install nothing, got %d", len(kernel.middlewares))
    }
}

func TestKernel_InstallMiddlewares_Twice_Refused(t *testing.T) {
    kernel := NewKernel(NewRouter(), KernelConfig{})

    kernel.InstallMiddlewares(httpkernelseam.NewToken(), nil)

    testhelper.AssertPanicsWithError(t, func() {
        kernel.InstallMiddlewares(httpkernelseam.NewToken(), []httpcontract.Middleware{passThroughKernelMiddleware})
    }, "the http kernel's middlewares are already installed")
}

func TestKernel_InstallMiddlewares_RunAfterTheConfigMiddlewares(t *testing.T) {
    order := make([]string, 0, 2)
    recordingMiddleware := func(name string) httpcontract.Middleware {
        return func(next httpcontract.Handler) httpcontract.Handler {
            return func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                order = append(order, name)

                return next(runtimeInstance, writer, request)
            }
        }
    }

    router := NewRouter()
    router.Handle("/ordered", routeRegistryTestHandler(), RouteOptions{Methods: []string{nethttp.MethodGet}})

    kernel := NewKernel(router, KernelConfig{Middlewares: []httpcontract.Middleware{recordingMiddleware("config")}})
    kernel.InstallMiddlewares(httpkernelseam.NewToken(), []httpcontract.Middleware{recordingMiddleware("installed")})

    kernel.ServeHttp(newHttpTestContainer()).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/ordered", nil))

    if 2 != len(order) || "config" != order[0] || "installed" != order[1] {
        t.Fatalf("expected the config's middleware outside the installed one, got %v", order)
    }
}

func TestNewKernel_AppliesEveryConfigField(t *testing.T) {
    notFoundHandler := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return TextResponse(nethttp.StatusTeapot, "nothing here"), nil
    }

    kernelConfig := KernelConfig{
        MethodPolicy:           MethodPolicy{HeadFallbackDisabled: true, AutomaticOptionsDisabled: true},
        ForwardedHeadersPolicy: httpcontract.ForwardedHeadersPolicy{TrustForwardedHeaders: true, TrustedProxyList: []string{"10.0.0.0/8"}},
        SessionCookiePolicy:    httpcontract.SessionCookiePolicy{Path: "/app", Domain: "example.com", SameSite: nethttp.SameSiteStrictMode, Secure: httpcontract.SessionCookieSecureAlways},
        SessionPolicy:          httpcontract.SessionPolicy{EmptyOnOutage: true},
        Middlewares:            []httpcontract.Middleware{passThroughKernelMiddleware},
        NotFoundHandler:        notFoundHandler,
        UnavailableRetryAfter:  bound.Of(17 * time.Second),
    }

    kernel := NewKernel(NewRouter(), kernelConfig)

    if kernelConfig.MethodPolicy != kernel.policies.methodPolicy {
        t.Fatalf("expected the method policy, got %+v", kernel.policies.methodPolicy)
    }

    if true != kernel.policies.forwardedHeadersPolicy.TrustForwardedHeaders || 1 != len(kernel.policies.forwardedHeadersPolicy.TrustedProxyList) || "10.0.0.0/8" != kernel.policies.forwardedHeadersPolicy.TrustedProxyList[0] {
        t.Fatalf("expected the forwarded headers policy, got %+v", kernel.policies.forwardedHeadersPolicy)
    }

    if kernelConfig.SessionCookiePolicy != kernel.policies.sessionCookiePolicy {
        t.Fatalf("expected the session cookie policy, got %+v", kernel.policies.sessionCookiePolicy)
    }

    if kernelConfig.SessionPolicy != kernel.policies.sessionPolicy {
        t.Fatalf("expected the session policy, got %+v", kernel.policies.sessionPolicy)
    }

    if 1 != len(kernel.middlewares) {
        t.Fatalf("expected the config's middleware, got %d", len(kernel.middlewares))
    }

    if nil == kernel.notFoundHandler {
        t.Fatalf("expected the not found handler")
    }

    recorder := httptest.NewRecorder()
    kernel.ServeHttp(newHttpTestContainer()).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/nowhere", nil))
    if nethttp.StatusTeapot != recorder.Code {
        t.Fatalf("expected the not found handler to answer the unmatched path, got %d", recorder.Code)
    }

    if 17*time.Second != kernel.unavailableRetryAfter {
        t.Fatalf("expected the unavailable retry after, got %s", kernel.unavailableRetryAfter)
    }
}

/* a setter beside KernelConfig would be a second door for one value */
func TestKernel_ExportsNoMutator(t *testing.T) {
    fileSet := token.NewFileSet()
    parsedFile, parseErr := parser.ParseFile(fileSet, "kernel.go", nil, 0)
    if nil != parseErr {
        t.Fatalf("expected kernel.go to parse, got %v", parseErr)
    }

    methodCount := 0
    for _, declaration := range parsedFile.Decls {
        function, isFunction := declaration.(*ast.FuncDecl)
        if false == isFunction || nil == function.Recv || false == function.Name.IsExported() {
            continue
        }

        methodCount++
        if "Use" == function.Name.Name || true == strings.HasPrefix(function.Name.Name, "Set") {
            t.Fatalf("expected no exported mutator on Kernel, found %s", function.Name.Name)
        }
    }

    if 0 == methodCount {
        t.Fatalf("expected the walk to read Kernel's methods")
    }
}

func TestKernel_RefusesARouteRegisteredAfterItStartedServing(t *testing.T) {
    router := NewRouter()
    kernel := NewKernel(router, KernelConfig{})

    router.Handle("/before", routeRegistryTestHandler(), RouteOptions{Methods: []string{nethttp.MethodGet}})

    _ = kernel.ServeHttp(newHttpTestContainer())

    testhelper.AssertPanicsWithError(t, func() {
        router.Handle("/after", routeRegistryTestHandler(), RouteOptions{Methods: []string{nethttp.MethodGet}})
    }, "may not register a route after the http kernel started serving")
}

func TestKernel_LeavesTheRouteTableReadableAfterItStartedServing(t *testing.T) {
    router := NewRouter()
    kernel := NewKernel(router, KernelConfig{})

    router.Handle("/articles/:id", routeRegistryTestHandler(), RouteOptions{Name: "article.show", Methods: []string{nethttp.MethodGet}})

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
    router.Handle("/slow", func(
        runtimeInstance containercontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        return NewResponse(nethttp.StatusOK, []byte("ok")), nil
    }, RouteOptions{Methods: []string{nethttp.MethodGet}})

    kernel := NewKernel(router, KernelConfig{})
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

            kernel.InstallMiddlewares(httpkernelseam.NewToken(), []httpcontract.Middleware{passThroughKernelMiddleware})
        }()
    }

    <-finished
}

func TestKernel_AbortHandlerPanicStillClosesTheResponseInFlight(t *testing.T) {
    bodyReader := &closeTrackingReader{}

    router := NewRouter()

    router.Handle(
        "/file",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            response := &Response{}
            response.SetStatusCode(nethttp.StatusOK)
            response.SetHeaders(make(nethttp.Header))
            response.SetBodyReader(bodyReader)

            return response, nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    kernel := NewKernel(router, KernelConfig{Middlewares: []httpcontract.Middleware{
        func(next httpcontract.Handler) httpcontract.Handler {
            return func(
                runtimeInstance containercontract.Runtime,
                writer nethttp.ResponseWriter,
                request httpcontract.Request,
            ) (httpcontract.Response, error) {
                _, _ = next(runtimeInstance, writer, request)

                panic(nethttp.ErrAbortHandler)
            }
        },
    }})

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
    router.Handle("/admin/users", func(
        runtimeInstance containercontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        reached = "two-segment"

        return NewResponse(nethttp.StatusOK, []byte("ok")), nil
    }, RouteOptions{Methods: []string{nethttp.MethodGet}})
    router.Handle("/:name", func(
        runtimeInstance containercontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        reached = "one-segment:" + request.Parameters()["name"]

        return NewResponse(nethttp.StatusOK, []byte("ok")), nil
    }, RouteOptions{Methods: []string{nethttp.MethodGet}})

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
    router.Handle("/admin/users", func(
        runtimeInstance containercontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        reached = "two-segment"

        return NewResponse(nethttp.StatusOK, []byte("ok")), nil
    }, RouteOptions{Methods: []string{nethttp.MethodGet}})
    router.Handle("/:name", func(
        runtimeInstance containercontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        reached = "one-segment:" + request.Parameters()["name"]

        return NewResponse(nethttp.StatusOK, []byte("ok")), nil
    }, RouteOptions{Methods: []string{nethttp.MethodGet}})

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
    router.Handle("/admin/users", routeRegistryTestHandler(), RouteOptions{Name: "admin_users", Methods: []string{nethttp.MethodGet}})

    recordingLogger := &warningRecordingLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    responseRouteName := "unread"
    dispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if true == ok {
                responseRouteName = responseEvent.Request().RouteName()
            }

            return nil
        },
        eventcontract.ListenerConfig{},
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
    router.Handle("/admin/users", routeRegistryTestHandler(), RouteOptions{Name: "admin_users", Methods: []string{nethttp.MethodGet}})

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

func TestRouter_EqualPriorityIsWonByTheFirstRegistration(t *testing.T) {
    router := NewRouter()

    router.Handle("/users/:id", routeRegistryTestHandler(), RouteOptions{Name: "first", Methods: []string{nethttp.MethodGet}})
    router.Handle("/users/new", routeRegistryTestHandler(), RouteOptions{Name: "second", Methods: []string{nethttp.MethodGet}})

    _, parameters, attributes := router.match(nethttp.MethodGet, "/users/new", "", "https")

    if "new" != parameters["id"] {
        t.Fatalf("expected the first registration to win the tie, got parameters %v", parameters)
    }

    if "first" != attributes[RouteAttributeName] {
        t.Fatalf("expected the first registration to win the tie, got route %v", attributes[RouteAttributeName])
    }
}

func serveNegotiatedKernelProbe(t *testing.T, method string, target string, accept string) *httptest.ResponseRecorder {
    t.Helper()

    router := NewRouter()
    router.Handle(
        "/only-post",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "posted"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodPost}},
    )

    serviceContainer := newHttpTestContainer()
    container.MustRegister[*serializer.SerializerManager](
        serviceContainer,
        serializer.ServiceSerializerManager,
        func(resolver containercontract.Resolver) (*serializer.SerializerManager, error) {
            return serializer.NewSerializerManager(map[string]serializercontract.Serializer{
                "application/json": serializer.NewJsonSerializer(),
                "text/plain":       serializer.NewPlainTextSerializer(serializer.PlainTextSerializerConfig{}),
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

var loggedMarkSentinelErr = exception.NewError("the shared failure", nil, nil)

var loggedMarkWrappedSentinelErr = exception.NewError("the shared wrapped failure", nil, nil)

func servedLoggedMarkKernel(handler httpcontract.Handler) (nethttp.Handler, *recordsCaptureLogger) {
    capture := &recordsCaptureLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})

    router := NewRouter()
    router.Handle("/fail", handler, RouteOptions{Methods: []string{nethttp.MethodGet}})

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
                func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
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

func TestKernel_TheWaitersOfOneRememberFlightFileOneRecordEach(t *testing.T) {
    flightErr := exception.NewError("the flight failed", nil, nil)

    cacheManager := cache.NewManager(cache.NewInMemoryBackend(cache.InMemoryBackendConfig{MaxItems: bound.Of(16), CleanupInterval: bound.Of(time.Minute), Clock: clock.NewSystemClock()}), cache.NewJsonSerializer(), cache.ManagerConfig{})

    var entered sync.WaitGroup
    entered.Add(2)

    release := make(chan struct{})

    handler, capture := servedLoggedMarkKernel(
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            entered.Done()

            _, rememberErr := cache.Remember(
                cache.For(runtime.WithContext(runtimeInstance, context.Background()), cacheManager),
                "the-flight",
                cache.RememberConfig{Ttl: bound.Of(time.Minute)},
                func(_ containercontract.Runtime, _ *cache.RememberItem) (any, error) {
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

func TestKernel_AnApplicationListenerReadsTheMarkOfTheOccurrenceTheKernelFiled(t *testing.T) {
    capture := &recordsCaptureLogger{}

    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})

    eventDispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    eventDispatcher.AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            exceptionEvent, ok := eventValue.Payload().(*KernelExceptionEvent)
            if false == ok || true == exception.IsAlreadyLogged(exceptionEvent.Err()) {
                return nil
            }

            capture.Error("application record", nil)

            return nil
        },
        eventcontract.ListenerConfig{},
    )

    router := NewRouter()
    router.Handle(
        "/fail",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, loggedMarkSentinelErr
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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

func TestKernel_EveryPanicCarryingASharedErrorValueFilesItsOwnRecord(t *testing.T) {
    sharedPanicErr := exception.NewError("the shared panic", nil, nil)

    handler, capture := servedLoggedMarkKernel(
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
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

func TestKernel_TheDebugPayloadShowsTheCoordinatesOfTheOccurrence(t *testing.T) {
    sharedErr := exception.NewError("the shared failure in debug", map[string]any{"detail": "kept"}, nil)

    serviceContainer := newHttpTestContainer()
    router := NewRouter()
    router.Handle(
        "/fail",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, sharedErr
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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
            "/declined",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return nil, handlerErr
            },
            RouteOptions{Methods: []string{nethttp.MethodGet}},
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
        "/panics",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            panic(exception.Conflict("taken"))
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    kernel := NewKernel(router, KernelConfig{})
    recorder := httptest.NewRecorder()
    kernel.ServeHttp(newHttpTestContainer()).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/panics", nil))

    if nethttp.StatusConflict != recorder.Code {
        t.Fatalf("expected the panic's 409 kept, got %d", recorder.Code)
    }
}

func TestKernel_ANotFoundHandlerErrorNoListenerAnswersKeepsItsStatus(t *testing.T) {
    kernel := NewKernel(NewRouter(), KernelConfig{NotFoundHandler: func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return nil, exception.Forbidden("hidden")
    }})
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
            "/resource",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return TextResponse(nethttp.StatusOK, "resource"), nil
            },
            RouteOptions{Methods: []string{nethttp.MethodGet}},
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
        "/articles",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "articles"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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

func TestNewKernel_ARouteDeclaredWithALowercaseMethodServesHeadAndAdvertisesTheMethodInUpperCase(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/articles",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "articles"), nil
        },
        RouteOptions{Methods: []string{" get "}},
    )
    kernelInstance := NewKernel(router, KernelConfig{})

    if recorder := serveKernelConfigTestRequest(kernelInstance, nethttp.MethodHead, "/articles"); nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected HEAD served through the route declared as get, got %d", recorder.Code)
    }

    recorder := serveKernelConfigTestRequest(kernelInstance, nethttp.MethodPost, "/articles")
    if nethttp.StatusMethodNotAllowed != recorder.Code {
        t.Fatalf("expected 405, got %d", recorder.Code)
    }

    if allow := recorder.Header().Get("Allow"); "GET, HEAD, OPTIONS" != allow {
        t.Fatalf("expected Allow: GET, HEAD, OPTIONS, got %q", allow)
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
            NotFoundHandler: func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
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
            return func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
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
        "/fail",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, failure
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    return router
}

func TestKernel_ABuiltInListenerAnswersWhenNoApplicationListenerDid(t *testing.T) {
    serviceContainer := newHttpTestContainer()

    observed := 0
    event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            observed++

            return nil
        },
        eventcontract.ListenerConfig{},
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

    event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            exceptionEvent := eventValue.Payload().(*KernelExceptionEvent)
            observedErr = exceptionEvent.Err()
            observedResponseSet = nil != exceptionEvent.Response()

            return nil
        },
        eventcontract.ListenerConfig{Priority: BuiltInExceptionListenerPriority+1},
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

    event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.Payload().(*KernelExceptionEvent).SetResponse(TextResponse(nethttp.StatusTeapot, "application body"))

            return nil
        },
        eventcontract.ListenerConfig{},
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

    event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            response := &Response{
                statusCode: nethttp.StatusTeapot,
                headers:    nethttp.Header{"Content-Type": []string{"application/secret"}},
                bodyReader: bodyReader,
            }

            eventValue.Payload().(*KernelExceptionEvent).SetResponse(response)

            panic("listener exploded")
        },
        eventcontract.ListenerConfig{},
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
        if "the kernel.exception dispatch failed with a response set, which is dropped" != record.message {
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

    /* the floor's neighbour: had the dispatch gone on past the panicking listener, it would have run before the floor */
    aboveFloorRan := false
    event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            aboveFloorRan = true

            return nil
        },
        eventcontract.ListenerConfig{Priority: BuiltInExceptionListenerPriority + 1},
    )

    recorder := httptest.NewRecorder()
    NewKernel(newFailingKernelTestRouter(exception.NotFound("no such article")), KernelConfig{}).
        ServeHttp(newContainerWithPanickingExceptionListener(t, serviceContainer)).
        ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/fail", nil))

    if nethttp.StatusNotFound != recorder.Code || false == strings.Contains(recorder.Body.String(), "no such article") {
        t.Fatalf("expected the kernel's own rendering of the 404, got %d: %s", recorder.Code, recorder.Body.String())
    }

    if true == aboveFloorRan {
        t.Fatalf("expected the dispatch to end at the panicking listener, before the floor")
    }

    for _, record := range capture.failureRecords() {
        if true == strings.Contains(record.message, "which is dropped") {
            t.Fatalf("expected no dropped-response record, got %v", record)
        }
    }
}

func TestKernel_APropagationStopWithoutAResponseIsRenderedByTheKernel(t *testing.T) {
    serviceContainer := newHttpTestContainer()

    event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.StopPropagation()

            return nil
        },
        eventcontract.ListenerConfig{},
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

    eventDispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    eventDispatcher.AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.Payload().(*KernelExceptionEvent).SetResponse(TextResponse(nethttp.StatusTeapot, "application body"))

            return nil
        },
        eventcontract.ListenerConfig{Priority: 10},
    )
    eventDispatcher.AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        eventcontract.ListenerConfig{Failure: eventcontract.ListenerFailureConfig{Required: true}},
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
    eventDispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)

    kernelInstance := NewKernel(NewRouter(), KernelConfig{})
    kernelInstance.ServeHttp(serviceContainer)
    kernelInstance.ServeHttp(serviceContainer)

    NewKernel(NewRouter(), KernelConfig{}).ServeHttp(serviceContainer)

    inspector := eventDispatcher
    for _, registeredEvent := range inspector.RegisteredEvents() {
        if kernelcontract.EventKernelException != registeredEvent.EventName {
            continue
        }

        if 1 != len(registeredEvent.Listeners) || math.MinInt != registeredEvent.Listeners[0].Config.Priority {
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
            event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
                kernelcontract.EventKernelException,
                func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
                    return nil
                },
                eventcontract.ListenerConfig{Priority: BuiltInExceptionListenerPriority},
            )
        },
        "an event listener at or below the event's reserved priority can never run, so it is refused; register it above that priority",
    )
}

func newRefusingKernelRequestListener(refusal error) eventcontract.EventListener {
    return func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        eventValue.Payload().(*KernelRequestEvent).Refuse(refusal)

        return nil
    }
}

func TestKernel_ARefusedRequestNeverReachesItsHandler(t *testing.T) {
    serviceContainer := newHttpTestContainer()

    event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelRequest,
        newRefusingKernelRequestListener(exception.Forbidden("not you")),
        eventcontract.ListenerConfig{},
    )

    handlerRan := 0

    router := NewRouter()
    router.Handle(
        "/guarded",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            handlerRan++

            return TextResponse(nethttp.StatusOK, "served"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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

    eventDispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    eventDispatcher.AddListener(kernelcontract.EventKernelRequest, newRefusingKernelRequestListener(refusal), eventcontract.ListenerConfig{})

    var observedErr error

    eventDispatcher.AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            exceptionEvent := eventValue.Payload().(*KernelExceptionEvent)
            observedErr = exceptionEvent.Err()
            exceptionEvent.SetResponse(TextResponse(nethttp.StatusTeapot, "application body"))

            return nil
        },
        eventcontract.ListenerConfig{},
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

    eventDispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    eventDispatcher.AddListener(kernelcontract.EventKernelRequest, newRefusingKernelRequestListener(refusal), eventcontract.ListenerConfig{})
    eventDispatcher.AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.Payload().(*KernelExceptionEvent).SetResponse(TextResponse(nethttp.StatusTeapot, "application body"))

            return nil
        },
        eventcontract.ListenerConfig{},
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

    event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(kernelcontract.EventKernelRequest, newRefusingKernelRequestListener(refusal), eventcontract.ListenerConfig{})

    NewKernel(NewRouter(), KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/guarded", nil))

    for _, record := range capture.failureRecords() {
        if "unhandled exception" == record.message {
            t.Fatalf("expected no second record, got %v", record)
        }
    }
}

func TestKernel_ARefusalStandsOverALaterListenersFailure(t *testing.T) {
    serviceContainer := newHttpTestContainer()

    eventDispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    eventDispatcher.AddListener(kernelcontract.EventKernelRequest, newRefusingKernelRequestListener(exception.Forbidden("not you")), eventcontract.ListenerConfig{Priority: 10})
    eventDispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return errors.New("a later listener failed")
        },
        eventcontract.ListenerConfig{},
    )

    recorder := httptest.NewRecorder()
    NewKernel(NewRouter(), KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/guarded", nil))

    if nethttp.StatusForbidden != recorder.Code {
        t.Fatalf("expected the refusal's 403, got %d", recorder.Code)
    }
}

func TestKernel_ARefusalBesideASkippedRequiredListenerFailsClosed(t *testing.T) {
    serviceContainer := newHttpTestContainer()

    eventDispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    eventDispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.Payload().(*KernelRequestEvent).Refuse(exception.Forbidden("not you"))
            eventValue.StopPropagation()

            return nil
        },
        eventcontract.ListenerConfig{Priority: 10},
    )
    eventDispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        eventcontract.ListenerConfig{Failure: eventcontract.ListenerFailureConfig{Required: true}},
    )

    recorder := httptest.NewRecorder()
    NewKernel(NewRouter(), KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/guarded", nil))

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the fail-closed 500, got %d", recorder.Code)
    }
}

/* a refusal made above a failing listener stands beside a skipped required listener, so the dispatch's refusal of the skip decides: 500, not the refusal's 403 */
func TestKernel_ARefusalAboveAFailureAheadOfARequiredListenerFailsClosed(t *testing.T) {
    serviceContainer := newHttpTestContainer()

    eventDispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    eventDispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.Payload().(*KernelRequestEvent).Refuse(exception.Forbidden("not you"))

            return nil
        },
        eventcontract.ListenerConfig{Priority: 50},
    )
    eventDispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return errors.New("the listener below the refusal failed")
        },
        eventcontract.ListenerConfig{Priority: 30},
    )
    eventDispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        eventcontract.ListenerConfig{Priority: 10, Failure: eventcontract.ListenerFailureConfig{Required: true}},
    )

    recorder := httptest.NewRecorder()
    NewKernel(NewRouter(), KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/guarded", nil))

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the fail-closed 500, got %d", recorder.Code)
    }
}

func TestKernel_AListenerThatAnswersAndRefusesFailsClosed(t *testing.T) {
    for name, listener := range map[string]eventcontract.EventListener{
        "response then refusal": func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent := eventValue.Payload().(*KernelRequestEvent)
            requestEvent.SetResponse(TextResponse(nethttp.StatusTeapot, "answered"))
            requestEvent.Refuse(exception.Forbidden("refused"))

            return nil
        },
        "refusal then response": func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent := eventValue.Payload().(*KernelRequestEvent)
            requestEvent.Refuse(exception.Forbidden("refused"))
            requestEvent.SetResponse(TextResponse(nethttp.StatusTeapot, "answered"))

            return nil
        },
    } {
        serviceContainer := newHttpTestContainer()

        event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(kernelcontract.EventKernelRequest, listener, eventcontract.ListenerConfig{})

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

        event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
            kernelcontract.EventKernelRequest,
            func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
                defer func() { _ = recover() }()

                decide(eventValue.Payload().(*KernelRequestEvent))

                return nil
            },
            eventcontract.ListenerConfig{},
        )

        handlerRan := 0

        router := NewRouter()
        router.Handle(
            "/guarded",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                handlerRan++

                return TextResponse(nethttp.StatusOK, "served"), nil
            },
            RouteOptions{Methods: []string{nethttp.MethodGet}},
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

        event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
            kernelcontract.EventKernelRequest,
            func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
                decide(eventValue.Payload().(*KernelRequestEvent), &Response{statusCode: nethttp.StatusTeapot, headers: make(nethttp.Header), bodyReader: bodyReader})

                return nil
            },
            eventcontract.ListenerConfig{},
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

    kernelInstance.InstallMiddlewares(httpkernelseam.NewToken(), nil)
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
    event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            responseDispatches++

            return nil
        },
        eventcontract.ListenerConfig{},
    )

    bodyReader := &closeTrackingReader{}

    router := NewRouter()
    router.Handle(
        "/fail",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return &Response{statusCode: nethttp.StatusOK, headers: make(nethttp.Header), bodyReader: bodyReader}, exception.NotFound("no such article")
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
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
    for _, environment := range []string{config.EnvironmentProduction, config.EnvironmentDevelopment} {
        router := NewRouter()
        router.Handle(
            "/cycle",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return nil, newCyclicChainError()
            },
            RouteOptions{Methods: []string{nethttp.MethodGet}},
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
    for _, environment := range []string{config.EnvironmentProduction, config.EnvironmentDevelopment} {
        router := NewRouter()
        router.Handle(
            "/cycle",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                writer.WriteHeader(nethttp.StatusAccepted)

                return nil, newCyclicChainError()
            },
            RouteOptions{Methods: []string{nethttp.MethodGet}},
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
    for _, environment := range []string{config.EnvironmentProduction, config.EnvironmentDevelopment} {
        router := NewRouter()
        router.Handle(
            "/cycle",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                if _, formErr := request.Post(); nil != formErr {
                    return nil, formErr
                }

                return TextResponse(nethttp.StatusOK, "served"), nil
            },
            RouteOptions{Methods: []string{nethttp.MethodPost}},
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

    event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelRequest,
        newRefusingKernelRequestListener(exception.NewHttpError(nethttp.StatusServiceUnavailable, exception.HttpErrorConfig{Message: "down", RetryAfter: 3 * time.Second})),
        eventcontract.ListenerConfig{},
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

    eventDispatcher := event.EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    eventDispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        newRefusingKernelRequestListener(exception.NewHttpError(nethttp.StatusServiceUnavailable, exception.HttpErrorConfig{Message: "down", RetryAfter: 3 * time.Second})),
        eventcontract.ListenerConfig{},
    )
    eventDispatcher.AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            panic("listener exploded")
        },
        eventcontract.ListenerConfig{},
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
            "/page",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return nil, exception.MarkInfrastructureFailure(errors.New("cache is down"))
            },
            RouteOptions{Methods: []string{nethttp.MethodGet}},
        )

        serviceContainer := newHttpTestContainer()
        if true == throughTheFinalStep {
            event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
                kernelcontract.EventKernelException,
                func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
                    panic("listener exploded")
                },
                eventcontract.ListenerConfig{},
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

func TestNewKernel_RefusesAnUnavailableRetryAfterOutsideItsRule(t *testing.T) {
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

func TestKernel_APanicCarryingTheMarkAnswers503(t *testing.T) {
    for name, value := range map[string]any{
        "the marked error":           exception.MarkInfrastructureFailure(errors.New("storage is down")),
        "a melody error wrapping it": exception.NewError("could not load the session", nil, exception.MarkInfrastructureFailure(errors.New("storage is down"))),
    } {
        router := NewRouter()
        router.Handle(
            "/page",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                panic(value)
            },
            RouteOptions{Methods: []string{nethttp.MethodGet}},
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

func (instance *operationCountingSessionStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    atomic.AddInt64(&instance.loads, 1)

    return instance.Storage.Load(runtimeInstance, sessionId)
}

func (instance *operationCountingSessionStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    atomic.AddInt64(&instance.saves, 1)

    return instance.Storage.Save(runtimeInstance, sessionId, data, ttl)
}

func (instance *operationCountingSessionStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    atomic.AddInt64(&instance.deletes, 1)

    return instance.Storage.Delete(runtimeInstance, sessionId)
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
        if saveErr := storage.Storage.Save(sessionRuntime(), sessionOutageTestCookieValue, map[string]any{"userId": "u-1"}, bound.Of(time.Minute)); nil != saveErr {
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
        "/page",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            _, sessionReadErr := RequestSession(request)

            return nil, sessionReadErr
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    countingLogger := &errorCountingLogger{}

    sessionManager := session.NewManager(&loadFailingSessionStorage{}, session.ManagerConfig{Ttl: bound.Of(30 * time.Minute)})
    serviceContainer := newHttpTestContainerWithSessionManager(sessionManager)
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, countingLogger, container.OverrideOptions{})

    listenerSawError := false
    event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent := eventValue.Payload().(*KernelRequestEvent)

            _, sessionReadErr := RequestSession(requestEvent.Request())
            listenerSawError = nil != sessionReadErr

            return nil
        },
        eventcontract.ListenerConfig{},
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

func (instance *outageCountingSessionStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    return nil, false, exception.NewError("session backend down", nil, nil)
}

func (instance *outageCountingSessionStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    atomic.AddInt64(&instance.saveCalls, 1)

    return nil
}

func (instance *outageCountingSessionStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
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
            "/page",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
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
            RouteOptions{Methods: []string{nethttp.MethodGet}},
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

func persistenceTestKernel(t *testing.T, manager sessioncontract.Manager, path string, handlerFunc func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error)) (nethttp.Handler, *errorContextRecordingLogger) {
    t.Helper()

    router := NewRouter()
    router.Handle(path, handlerFunc, RouteOptions{Methods: []string{nethttp.MethodGet}})

    serviceContainer := newHttpTestContainerWithSessionManager(manager)
    recordingLogger := &errorContextRecordingLogger{}
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    return NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer), recordingLogger
}

func TestKernel_ALoadBlockedInTheStorageEndsWhenTheRequestsContextIsCancelled(t *testing.T) {
    storage := newContextBlockingSessionStorage()
    manager := session.NewManager(storage, session.ManagerConfig{})
    sessionId, _ := seedStoredSession(t, manager)
    storage.blockLoad.Store(true)

    var readerErr error
    handler, _ := persistenceTestKernel(t, manager, "/read", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        _, readerErr = RequestSession(request)

        return TextResponse(nethttp.StatusOK, "ok"), nil
    })

    requestContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    request := httptest.NewRequest(nethttp.MethodGet, "/read", nil).WithContext(requestContext)
    request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: sessionId})

    savesBefore, deletesBefore := storage.saves.Load(), storage.deletes.Load()

    done := make(chan struct{})
    go func() {
        defer close(done)
        handler.ServeHTTP(httptest.NewRecorder(), request)
    }()

    awaitStorageDoor(t, storage.entered, "Load")
    cancel()
    awaitClosed(t, done, "ServeHTTP to return")

    if false == exception.ChainIs(readerErr, session.ErrSessionUnavailable) || false == exception.ChainIs(readerErr, context.Canceled) {
        t.Fatalf("expected the reader to get the unavailable session with the cancellation, got %v", readerErr)
    }

    if savesBefore != storage.saves.Load() || deletesBefore != storage.deletes.Load() {
        t.Fatalf("expected a cut load to persist nothing")
    }
}

func TestKernel_AClientHangupBeforePersistenceStillRecordsTheDelete(t *testing.T) {
    storage := newContextBlockingSessionStorage()
    manager := session.NewManager(storage, session.ManagerConfig{})
    sessionId, copyLoaded := seedStoredSession(t, manager)

    requestContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    handler, _ := persistenceTestKernel(t, manager, "/logout", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        sessionInstance, sessionErr := RequestSession(request)
        if nil != sessionErr {
            return nil, sessionErr
        }

        sessionInstance.Clear()
        cancel()

        return TextResponse(nethttp.StatusOK, "bye"), nil
    })

    request := httptest.NewRequest(nethttp.MethodGet, "/logout", nil).WithContext(requestContext)
    request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: sessionId})
    handler.ServeHTTP(httptest.NewRecorder(), request)

    if 1 != storage.deletes.Load() {
        t.Fatalf("expected the logout's delete recorded after the hangup, got %d deletes", storage.deletes.Load())
    }

    if _, held, _ := storage.inner.Load(sessionRuntime(), sessionId); true == held {
        t.Fatalf("expected the storage no longer to hold the session")
    }

    copyLoaded.Set("userId", "u-2")
    if saveErr := manager.SaveSession(sessionRuntime(), copyLoaded); false == exception.ChainIs(saveErr, session.ErrSessionDeleted) {
        t.Fatalf("expected a copy loaded before the logout refused, got %v", saveErr)
    }
}

func TestKernel_AClientHangupBeforePersistenceStillStoresTheSave(t *testing.T) {
    storage := newContextBlockingSessionStorage()
    manager := session.NewManager(storage, session.ManagerConfig{})
    sessionId, _ := seedStoredSession(t, manager)

    requestContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    handler, _ := persistenceTestKernel(t, manager, "/write", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        sessionInstance, sessionErr := RequestSession(request)
        if nil != sessionErr {
            return nil, sessionErr
        }

        sessionInstance.Set("cart", "written")
        cancel()

        return TextResponse(nethttp.StatusOK, "ok"), nil
    })

    request := httptest.NewRequest(nethttp.MethodGet, "/write", nil).WithContext(requestContext)
    request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: sessionId})
    handler.ServeHTTP(httptest.NewRecorder(), request)

    data, held, _ := storage.inner.Load(sessionRuntime(), sessionId)
    if false == held || "written" != data["cart"] {
        t.Fatalf("expected the save stored after the hangup, got %v", data)
    }
}

func TestKernel_AHungSaveIsCutByTheStorageTimeout(t *testing.T) {
    storage := newContextBlockingSessionStorage()
    manager := session.NewManager(storage, session.ManagerConfig{StorageTimeout: bound.Of(30 * time.Millisecond)})
    sessionId, _ := seedStoredSession(t, manager)
    storage.blockSave.Store(true)

    handler, recordingLogger := persistenceTestKernel(t, manager, "/write", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        sessionInstance, sessionErr := RequestSession(request)
        if nil != sessionErr {
            return nil, sessionErr
        }

        sessionInstance.Set("cart", "lost")

        return TextResponse(nethttp.StatusOK, "ok"), nil
    })

    request := httptest.NewRequest(nethttp.MethodGet, "/write", nil)
    request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: sessionId})

    recorder := httptest.NewRecorder()
    done := make(chan struct{})
    go func() {
        defer close(done)
        handler.ServeHTTP(recorder, request)
    }()
    awaitClosed(t, done, "the hung save to be cut")

    if nethttp.StatusServiceUnavailable != recorder.Code || "" == recorder.Header().Get("Retry-After") || "" != recorder.Header().Get("Set-Cookie") {
        t.Fatalf("expected 503 with Retry-After and no cookie, got %d, %q and %q", recorder.Code, recorder.Header().Get("Retry-After"), recorder.Header().Get("Set-Cookie"))
    }

    if _, recorded := recordingLogger.errorContextFor("failed to save session"); false == recorded {
        t.Fatalf("expected one error record of the failed save")
    }
}

func TestKernel_ALogoutWhoseDeleteWasCutAnswers503AndKeepsTheCookie(t *testing.T) {
    storage := newContextBlockingSessionStorage()
    manager := session.NewManager(storage, session.ManagerConfig{StorageTimeout: bound.Of(30 * time.Millisecond)})
    sessionId, _ := seedStoredSession(t, manager)
    storage.blockDelete.Store(true)

    handler, recordingLogger := persistenceTestKernel(t, manager, "/logout", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        sessionInstance, sessionErr := RequestSession(request)
        if nil != sessionErr {
            return nil, sessionErr
        }

        sessionInstance.Clear()

        return TextResponse(nethttp.StatusOK, "bye"), nil
    })

    request := httptest.NewRequest(nethttp.MethodGet, "/logout", nil)
    request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: sessionId})

    recorder := httptest.NewRecorder()
    done := make(chan struct{})
    go func() {
        defer close(done)
        handler.ServeHTTP(recorder, request)
    }()
    awaitClosed(t, done, "the hung delete to be cut")

    if nethttp.StatusServiceUnavailable != recorder.Code || "" == recorder.Header().Get("Retry-After") {
        t.Fatalf("expected the cut logout answered 503 with Retry-After, got %d %q", recorder.Code, recorder.Header().Get("Retry-After"))
    }

    if cookies := recorder.Result().Cookies(); 0 != len(cookies) {
        t.Fatalf("expected the cookie kept, so the retry names the session, got %v", cookies)
    }

    recordContext, recorded := recordingLogger.errorContextFor("failed to delete session")
    if false == recorded || session.StorageTimeoutRule().Name != recordContext["bound"] {
        t.Fatalf("expected one error record of the failed delete naming the bound, got %v", recordContext)
    }
}

func TestKernel_ARetriedLogoutAfterThe503RemovesTheSession(t *testing.T) {
    storage := newContextBlockingSessionStorage()
    manager := session.NewManager(storage, session.ManagerConfig{StorageTimeout: bound.Of(30 * time.Millisecond)})
    sessionId, _ := seedStoredSession(t, manager)
    storage.blockDelete.Store(true)

    handler, _ := persistenceTestKernel(t, manager, "/logout", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        sessionInstance, sessionErr := RequestSession(request)
        if nil != sessionErr {
            return nil, sessionErr
        }

        sessionInstance.Clear()

        return TextResponse(nethttp.StatusOK, "bye"), nil
    })

    logout := func() *httptest.ResponseRecorder {
        request := httptest.NewRequest(nethttp.MethodGet, "/logout", nil)
        request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: sessionId})

        recorder := httptest.NewRecorder()
        done := make(chan struct{})
        go func() {
            defer close(done)
            handler.ServeHTTP(recorder, request)
        }()
        awaitClosed(t, done, "the logout to answer")

        return recorder
    }

    if first := logout(); nethttp.StatusServiceUnavailable != first.Code || 0 != len(first.Result().Cookies()) {
        t.Fatalf("expected the cut logout answered 503 keeping the cookie, got %d %v", first.Code, first.Result().Cookies())
    }

    storage.blockDelete.Store(false)

    second := logout()
    if cookies := second.Result().Cookies(); nethttp.StatusOK != second.Code || 1 != len(cookies) || 0 <= cookies[0].MaxAge {
        t.Fatalf("expected the retried logout answered 200 with the expiring cookie, got %d %v", second.Code, cookies)
    }

    if stored, loadErr := manager.Session(sessionRuntime(), sessionId); nil != loadErr || nil != stored {
        t.Fatalf("expected the retried logout to remove the session, got %v %v", stored, loadErr)
    }
}

func TestKernel_ASessionPersistedCallbackSeesTheDeleteTheBoundCut(t *testing.T) {
    storage := newContextBlockingSessionStorage()
    manager := session.NewManager(storage, session.ManagerConfig{StorageTimeout: bound.Of(30 * time.Millisecond)})
    sessionId, _ := seedStoredSession(t, manager)
    storage.blockDelete.Store(true)

    persistErrs := []error{}
    handler, _ := persistenceTestKernel(t, manager, "/logout", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        sessionInstance, sessionErr := RequestSession(request)
        if nil != sessionErr {
            return nil, sessionErr
        }

        sessionInstance.Clear()

        registerErr := OnSessionPersisted(request, func(persistErr error) error {
            persistErrs = append(persistErrs, persistErr)

            return nil
        })

        return TextResponse(nethttp.StatusOK, "bye"), registerErr
    })

    request := httptest.NewRequest(nethttp.MethodGet, "/logout", nil)
    request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: sessionId})

    recorder := httptest.NewRecorder()
    done := make(chan struct{})
    go func() {
        defer close(done)
        handler.ServeHTTP(recorder, request)
    }()
    awaitClosed(t, done, "the hung delete to be cut")

    if nethttp.StatusServiceUnavailable != recorder.Code {
        t.Fatalf("expected the cut logout answered 503, got %d", recorder.Code)
    }

    if 1 != len(persistErrs) || false == exception.AnswersUnavailable(persistErrs[0]) {
        t.Fatalf("expected one callback with the cut delete's outage, got %v", persistErrs)
    }
}

func TestKernel_ASessionPersistedCallbackErrorIsRaisedAsAHandlerError(t *testing.T) {
    storage := newContextBlockingSessionStorage()
    manager := session.NewManager(storage, session.ManagerConfig{})
    sessionId, _ := seedStoredSession(t, manager)

    handler, recordingLogger := persistenceTestKernel(t, manager, "/logout", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        sessionInstance, sessionErr := RequestSession(request)
        if nil != sessionErr {
            return nil, sessionErr
        }

        sessionInstance.Clear()

        registerErr := OnSessionPersisted(request, func(persistErr error) error {
            return exception.NewError("audit unavailable", nil, nil)
        })

        return TextResponse(nethttp.StatusOK, "bye"), registerErr
    })

    request := httptest.NewRequest(nethttp.MethodGet, "/logout", nil)
    request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: sessionId})

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the callback's error answered 500, got %d", recorder.Code)
    }

    if cookies := recorder.Result().Cookies(); 1 != len(cookies) || 0 <= cookies[0].MaxAge {
        t.Fatalf("expected the expiring cookie kept, got %v", cookies)
    }

    if _, recorded := recordingLogger.errorContextFor("session persisted callback error"); false == recorded {
        t.Fatal("expected the callback's error filed as a handler error")
    }
}

func TestKernel_APersistenceWaitingOnAHeldStripeIsCutByTheStorageTimeout(t *testing.T) {
    storage := newContextBlockingSessionStorage()
    manager := session.NewManager(storage, session.ManagerConfig{StorageTimeout: bound.Of(30 * time.Millisecond)})
    sessionId, holderCopy := seedStoredSession(t, manager)
    storage.blockSave.Store(true)

    holderContext, releaseHolder := context.WithCancel(context.Background())
    holderDone := make(chan struct{})
    go func() {
        defer close(holderDone)
        holderCopy.Set("holder", true)
        _ = manager.SaveSession(sessionRuntimeUnder(holderContext), holderCopy)
    }()
    awaitStorageDoor(t, storage.entered, "Save")

    savesWhileHeld := storage.saves.Load()

    handler, _ := persistenceTestKernel(t, manager, "/write", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        sessionInstance, sessionErr := RequestSession(request)
        if nil != sessionErr {
            return nil, sessionErr
        }

        sessionInstance.Set("cart", "waiting")

        return TextResponse(nethttp.StatusOK, "ok"), nil
    })

    request := httptest.NewRequest(nethttp.MethodGet, "/write", nil)
    request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: sessionId})

    recorder := httptest.NewRecorder()
    done := make(chan struct{})
    go func() {
        defer close(done)
        handler.ServeHTTP(recorder, request)
    }()
    awaitClosed(t, done, "the wait on the held stripe to be cut")

    releaseHolder()
    awaitClosed(t, holderDone, "the holder to leave")

    if nethttp.StatusServiceUnavailable != recorder.Code {
        t.Fatalf("expected the cut wait answered 503, got %d", recorder.Code)
    }

    if savesWhileHeld != storage.saves.Load() {
        t.Fatalf("expected the cut wait never to reach the storage's save")
    }
}

func TestKernel_RouteCap_RaisesTheCapOnThatRouteOnly(t *testing.T) {
    handler, _ := newFormRoutedKernel(t, 64, RouteOptions{Protection: RouteProtectionConfig{MaxBodyBytes: bound.Of(1024)}}, config.EnvironmentProduction)

    if recorder := serveUrlEncodedForm(handler, "/form", 512); nethttp.StatusOK != recorder.Code || "512" != recorder.Body.String() {
        t.Fatalf("expected the route's raised cap to admit the body, got %d %q", recorder.Code, recorder.Body.String())
    }

    if recorder := serveUrlEncodedForm(handler, "/other", 512); nethttp.StatusRequestEntityTooLarge != recorder.Code {
        t.Fatalf("expected the application's cap on another route, got %d", recorder.Code)
    }
}

func TestKernel_RouteCap_LowersTheCapOnThatRouteOnly(t *testing.T) {
    handler, _ := newFormRoutedKernel(t, 1024, RouteOptions{Protection: RouteProtectionConfig{MaxBodyBytes: bound.Of(64)}}, config.EnvironmentProduction)

    if recorder := serveUrlEncodedForm(handler, "/form", 512); nethttp.StatusRequestEntityTooLarge != recorder.Code {
        t.Fatalf("expected the route's lowered cap to refuse the body, got %d", recorder.Code)
    }

    if recorder := serveUrlEncodedForm(handler, "/other", 512); nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected the application's cap on another route, got %d", recorder.Code)
    }
}

func TestKernel_UnmatchedPath_BodyNeverRead_404(t *testing.T) {
    handler, _ := newFormRoutedKernel(t, 64, RouteOptions{}, config.EnvironmentProduction)

    body := newCountingBody("value=" + strings.Repeat("a", 50<<20))
    request := httptest.NewRequest(nethttp.MethodPost, "/missing", nil)
    request.Body = body
    request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nethttp.StatusNotFound != recorder.Code || 0 != body.reads.Load() {
        t.Fatalf("expected a 404 with the body never read, got %d and %d reads", recorder.Code, body.reads.Load())
    }
}

func TestKernel_ListenerRefusal_BodyNeverRead(t *testing.T) {
    router := NewRouter()
    router.Handle("/form", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        _, formErr := request.Post()

        return TextResponse(nethttp.StatusOK, "served"), formErr
    }, RouteOptions{Methods: []string{nethttp.MethodPost}})

    serviceContainer := newHttpTestContainer()
    event.EventDispatcherMustFromResolver(t.Context(), serviceContainer).AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.Payload().(*KernelRequestEvent).Refuse(exception.Forbidden("refused"))

            return nil
        },
        eventcontract.ListenerConfig{Priority: 100},
    )

    body := newCountingBody("value=a")
    request := httptest.NewRequest(nethttp.MethodPost, "/form", nil)
    request.Body = body
    request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    recorder := httptest.NewRecorder()

    NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, request)

    if nethttp.StatusForbidden != recorder.Code || 0 != body.reads.Load() {
        t.Fatalf("expected the refusal with the body never read, got %d and %d reads", recorder.Code, body.reads.Load())
    }
}

func TestKernel_HandlerThatIgnoresTheForm_ServesAMalformedBody(t *testing.T) {
    router := NewRouter()
    router.Handle("/form", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return TextResponse(nethttp.StatusOK, "served"), nil
    }, RouteOptions{Methods: []string{nethttp.MethodPost}})

    request := httptest.NewRequest(nethttp.MethodPost, "/form", strings.NewReader("value=%zz"))
    request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    recorder := httptest.NewRecorder()

    NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer()).ServeHTTP(recorder, request)

    if nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected a route that never reads its form to be served, got %d", recorder.Code)
    }
}

func TestKernel_HandlerReturningTheFormError_413Or400_OneWarningRecord(t *testing.T) {
    for name, testCase := range map[string]struct {
        body           string
        expectedStatus int
    }{
        "too large": {body: "value=" + strings.Repeat("a", 128), expectedStatus: nethttp.StatusRequestEntityTooLarge},
        "malformed": {body: "value=%zz", expectedStatus: nethttp.StatusBadRequest},
    } {
        t.Run(name, func(t *testing.T) {
            handler, capture := newFormRoutedKernel(t, 64, RouteOptions{}, config.EnvironmentProduction)

            request := httptest.NewRequest(nethttp.MethodPost, "/form", strings.NewReader(testCase.body))
            request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
            recorder := httptest.NewRecorder()
            handler.ServeHTTP(recorder, request)

            if testCase.expectedStatus != recorder.Code {
                t.Fatalf("expected %d, got %d", testCase.expectedStatus, recorder.Code)
            }

            records := capture.failureRecords()
            if 1 != len(records) || loggingcontract.LevelWarning != records[0].level {
                t.Fatalf("expected one warning record, got %#v", records)
            }
        })
    }
}

func TestKernel_HandlerDroppingTheFormError_FilesTheWarning(t *testing.T) {
    capture := &recordsCaptureLogger{}
    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})

    router := NewRouter()
    router.Handle("/form", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        _, _ = request.Post()

        return TextResponse(nethttp.StatusOK, "served"), nil
    }, RouteOptions{Methods: []string{nethttp.MethodPost}})

    request := httptest.NewRequest(nethttp.MethodPost, "/form", strings.NewReader("value=%zz"))
    request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    recorder := httptest.NewRecorder()

    NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, request)

    records := capture.failureRecords()
    if nethttp.StatusOK != recorder.Code || 1 != len(records) || "a handler answered over a request form that failed to parse" != records[0].message || loggingcontract.LevelWarning != records[0].level {
        t.Fatalf("expected the response to stand beside one warning, got %d and %#v", recorder.Code, records)
    }
}

func TestKernel_HandlerReadingAValidForm_FilesNoWarning(t *testing.T) {
    handler, capture := newFormRoutedKernel(t, 64, RouteOptions{}, config.EnvironmentProduction)

    if recorder := serveUrlEncodedForm(handler, "/form", 8); nethttp.StatusOK != recorder.Code || 0 != len(capture.failureRecords()) {
        t.Fatalf("expected a served form with no failure record, got %d and %#v", recorder.Code, capture.failureRecords())
    }
}

func TestKernel_BindJson_HonoursTheRouteCap(t *testing.T) {
    router := NewRouter()
    router.Handle("/json", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        var target map[string]any
        if bindErr := request.(*Request).BindJson(&target); nil != bindErr {
            return nil, bindErr
        }

        return TextResponse(nethttp.StatusOK, "bound"), nil
    }, RouteOptions{Methods: []string{nethttp.MethodPost}, Protection: RouteProtectionConfig{MaxBodyBytes: bound.Of(8)}})

    request := httptest.NewRequest(nethttp.MethodPost, "/json", strings.NewReader(`{"value":"aaaaaaaaaa"}`))
    request.Header.Set("Content-Type", "application/json")
    recorder := httptest.NewRecorder()

    NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer()).ServeHTTP(recorder, request)

    if nethttp.StatusRequestEntityTooLarge != recorder.Code {
        t.Fatalf("expected the route's cap to bound the json body, got %d", recorder.Code)
    }
}

func TestKernel_BodyTooLarge_SameStatusInDevAndProd(t *testing.T) {
    for _, environment := range []string{config.EnvironmentProduction, config.EnvironmentDevelopment} {
        handler, _ := newFormRoutedKernel(t, 64, RouteOptions{}, environment)

        if recorder := serveUrlEncodedForm(handler, "/form", 128); nethttp.StatusRequestEntityTooLarge != recorder.Code {
            t.Fatalf("expected 413 in the %s environment, got %d", environment, recorder.Code)
        }
    }
}

func TestKernel_HandlerDroppingTheFormErrorAndAnsweringAnError_FilesNoWarning(t *testing.T) {
    capture := &recordsCaptureLogger{}
    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})

    router := NewRouter()
    router.Handle("/form", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        _, _ = request.Post()

        return TextResponse(nethttp.StatusUnprocessableEntity, "refused"), nil
    }, RouteOptions{Methods: []string{nethttp.MethodPost}})

    request := httptest.NewRequest(nethttp.MethodPost, "/form", strings.NewReader("value=%zz"))
    request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    recorder := httptest.NewRecorder()

    NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, request)

    if nethttp.StatusUnprocessableEntity != recorder.Code || 0 != len(capture.failureRecords()) {
        t.Fatalf("expected a handler's own refusal with no warning, got %d and %#v", recorder.Code, capture.failureRecords())
    }
}

func passThroughKernelMiddleware(next httpcontract.Handler) httpcontract.Handler {
    return next
}
