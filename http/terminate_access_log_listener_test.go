package http

import (
    "context"
    "crypto/tls"
    nethttp "net/http"
    "net/http/httptest"
    "sync"
    "testing"

    "github.com/precision-soft/melody/clock"
    "github.com/precision-soft/melody/container"
    "github.com/precision-soft/melody/event"
    "github.com/precision-soft/melody/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

type accessLogRecordingLogger struct {
    mutex        sync.Mutex
    infoMessages []string
    infoContexts []loggingcontract.Context
}

func (instance *accessLogRecordingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *accessLogRecordingLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *accessLogRecordingLogger) Info(message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.infoMessages = append(instance.infoMessages, message)
    instance.infoContexts = append(instance.infoContexts, context)
}

func (instance *accessLogRecordingLogger) Warning(message string, context loggingcontract.Context) {
}

func (instance *accessLogRecordingLogger) Error(message string, context loggingcontract.Context) {}

func (instance *accessLogRecordingLogger) Emergency(message string, context loggingcontract.Context) {
}

func (instance *accessLogRecordingLogger) accessLogContext() (loggingcontract.Context, bool) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for index, message := range instance.infoMessages {
        if "request completed" == message {
            return instance.infoContexts[index], true
        }
    }

    return nil, false
}

func newAccessLogRuntime(recordingLogger *accessLogRecordingLogger) runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()

    scope.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger)

    return runtime.New(context.Background(), scope, serviceContainer)
}

/* @info the access log listener sat at 1.9% coverage — every branch that reads a field off the request was unexecuted, so the one record that says a request happened at all was carried by code nothing ran. A field silently dropped here is invisible until an incident needs it. */

func TestRegisterKernelTerminateAccessLogListener_RecordsTheRequestItCompleted(t *testing.T) {
    recordingLogger := &accessLogRecordingLogger{}
    runtimeInstance := newAccessLogRuntime(recordingLogger)

    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    RegisterKernelTerminateAccessLogListener(dispatcher)

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/articles?draft=1", nil)
    httpRequest.Host = "api.example.com"
    httpRequest.RemoteAddr = "203.0.113.7:5555"
    httpRequest.Header.Set("User-Agent", "melody-test")
    httpRequest.Header.Set("Referer", "https://example.com/from")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(httpRequest)

    terminateEvent := NewKernelTerminateEvent(
        runtimeInstance,
        melodyRequest,
        NewResponse(nethttp.StatusCreated, []byte("ok")),
    )

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelTerminate, terminateEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    loggedContext, logged := recordingLogger.accessLogContext()
    if false == logged {
        t.Fatalf("expected the access log record to be written, got messages: %v", recordingLogger.infoMessages)
    }

    for key, expected := range map[string]any{
        "method":     nethttp.MethodPost,
        "path":       "/articles",
        "query":      "draft=1",
        "host":       "api.example.com",
        "remoteAddr": "203.0.113.7:5555",
        "userAgent":  "melody-test",
        "referer":    "https://example.com/from",
        "statusCode": nethttp.StatusCreated,
    } {
        if expected != loggedContext[key] {
            t.Fatalf("unexpected %q in the access log record: %v (expected %v)", key, loggedContext[key], expected)
        }
    }

    if _, present := loggedContext["durationMs"]; false == present {
        t.Fatalf("expected the record to carry how long the request took")
    }

    /* the id is asserted by VALUE against the one the request carries, not merely by the key being present: an accessor answering the empty string leaves the key there and the record still reads as complete, while every line of that request becomes uncorrelatable with every other. */
    loggedRequestId, present := loggedContext["requestId"]
    if false == present {
        t.Fatalf("expected the record to carry the request id")
    }

    if "" == loggedRequestId {
        t.Fatalf("expected a non-empty request id in the access log record")
    }

    if melodyRequest.RequestContext().RequestId() != loggedRequestId {
        t.Fatalf(
            "expected the logged id to be the one the request carries: %v vs %v",
            loggedRequestId,
            melodyRequest.RequestContext().RequestId(),
        )
    }
}

/* @info the scheme is read from the attribute the kernel published, which is the one the forwarded-headers policy resolved. Re-detecting it here would log http for every request behind a proxy that terminates TLS, while the kernel served the very same request as https — two answers to one question, and the log is the one an incident reads. */

func TestRegisterKernelTerminateAccessLogListener_PrefersTheSchemeTheKernelResolved(t *testing.T) {
    recordingLogger := &accessLogRecordingLogger{}
    runtimeInstance := newAccessLogRuntime(recordingLogger)

    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    RegisterKernelTerminateAccessLogListener(dispatcher)

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/articles", nil)
    httpRequest.Header.Set("X-Forwarded-Proto", "https")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(httpRequest)
    melodyRequest.Attributes().Set(RequestAttributeScheme, "https")

    terminateEvent := NewKernelTerminateEvent(
        runtimeInstance,
        melodyRequest,
        NewResponse(nethttp.StatusOK, []byte("ok")),
    )

    if _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelTerminate, terminateEvent); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    loggedContext, logged := recordingLogger.accessLogContext()
    if false == logged {
        t.Fatalf("expected the access log record to be written")
    }

    if "https" != loggedContext["scheme"] {
        t.Fatalf("expected the resolved scheme to be logged, got: %v", loggedContext["scheme"])
    }
}

/* @info with no published attribute the listener falls back to detectScheme, which trusts no forwarded header at all: a plain request logs http even when a client claims otherwise, so a forged X-Forwarded-Proto cannot rewrite the access log of a deployment that never configured a trusted proxy. */

func TestRegisterKernelTerminateAccessLogListener_FallsBackToTheUntrustedSchemeDetection(t *testing.T) {
    recordingLogger := &accessLogRecordingLogger{}
    runtimeInstance := newAccessLogRuntime(recordingLogger)

    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    RegisterKernelTerminateAccessLogListener(dispatcher)

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/articles", nil)
    httpRequest.Header.Set("X-Forwarded-Proto", "https")

    terminateEvent := NewKernelTerminateEvent(
        runtimeInstance,
        testhelper.NewHttpTestRequestFromHttpRequest(httpRequest),
        NewResponse(nethttp.StatusOK, []byte("ok")),
    )

    if _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelTerminate, terminateEvent); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    loggedContext, logged := recordingLogger.accessLogContext()
    if false == logged {
        t.Fatalf("expected the access log record to be written")
    }

    if "http" != loggedContext["scheme"] {
        t.Fatalf("expected an unclaimed scheme to read as http rather than trust the header, got: %v", loggedContext["scheme"])
    }
}

/* @info a request served over real TLS reads as https without any header at all — the one signal detectScheme does trust, because it comes from the connection rather than from the client. */

func TestRegisterKernelTerminateAccessLogListener_DetectsTlsFromTheConnection(t *testing.T) {
    recordingLogger := &accessLogRecordingLogger{}
    runtimeInstance := newAccessLogRuntime(recordingLogger)

    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    RegisterKernelTerminateAccessLogListener(dispatcher)

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/articles", nil)
    httpRequest.TLS = &tls.ConnectionState{}

    terminateEvent := NewKernelTerminateEvent(
        runtimeInstance,
        testhelper.NewHttpTestRequestFromHttpRequest(httpRequest),
        NewResponse(nethttp.StatusOK, []byte("ok")),
    )

    if _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelTerminate, terminateEvent); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    loggedContext, logged := recordingLogger.accessLogContext()
    if false == logged {
        t.Fatalf("expected the access log record to be written")
    }

    if "https" != loggedContext["scheme"] {
        t.Fatalf("expected a tls connection to read as https, got: %v", loggedContext["scheme"])
    }
}

/* @info an event carrying something other than a terminate payload is ignored rather than misread. The dispatcher is shared, so any listener registered on the same name receives every payload dispatched under it, and reading the wrong one would panic inside a listener that runs after the response has already gone. */

func TestRegisterKernelTerminateAccessLogListener_IgnoresAForeignPayload(t *testing.T) {
    recordingLogger := &accessLogRecordingLogger{}
    runtimeInstance := newAccessLogRuntime(recordingLogger)

    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    RegisterKernelTerminateAccessLogListener(dispatcher)

    if _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelTerminate, "not a terminate event"); nil != dispatchErr {
        t.Fatalf("expected a foreign payload to be ignored, got: %v", dispatchErr)
    }

    if _, logged := recordingLogger.accessLogContext(); true == logged {
        t.Fatalf("expected no access log record for a foreign payload")
    }
}

/* @info a response the kernel never produced — a request whose connection died before anything was written — logs a zero status rather than dereferencing what is not there. The listener runs after the response has gone, outside the kernel's recovery, so a dereference here takes down the process. */

func TestRegisterKernelTerminateAccessLogListener_RecordsAZeroStatusWithoutAResponse(t *testing.T) {
    recordingLogger := &accessLogRecordingLogger{}
    runtimeInstance := newAccessLogRuntime(recordingLogger)

    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    RegisterKernelTerminateAccessLogListener(dispatcher)

    terminateEvent := NewKernelTerminateEvent(
        runtimeInstance,
        testhelper.NewHttpTestRequestFromHttpRequest(httptest.NewRequest(nethttp.MethodGet, "/articles", nil)),
        nil,
    )

    if _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelTerminate, terminateEvent); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    loggedContext, logged := recordingLogger.accessLogContext()
    if false == logged {
        t.Fatalf("expected the access log record to be written even without a response")
    }

    if 0 != loggedContext["statusCode"] {
        t.Fatalf("expected a zero status without a response, got: %v", loggedContext["statusCode"])
    }
}
