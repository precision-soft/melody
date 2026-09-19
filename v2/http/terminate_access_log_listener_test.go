package http

import (
    "context"
    "crypto/tls"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v2/clock"
    "github.com/precision-soft/melody/v2/container"
    "github.com/precision-soft/melody/v2/event"
    "github.com/precision-soft/melody/v2/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    "github.com/precision-soft/melody/v2/runtime"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
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

func TestRegisterKernelTerminateAccessLogListener_RecordsTheRequestItCompleted(t *testing.T) {
    recordingLogger := &accessLogRecordingLogger{}
    runtimeInstance := newAccessLogRuntime(recordingLogger)

    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    RegisterKernelTerminateAccessLogListener(dispatcher)

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/articles?draft=1&token=super-secret", nil)
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
        "query":      "draft=xxxxx&token=xxxxx",
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

func TestRegisterKernelTerminateAccessLogListener_RedactsEveryQueryValue(t *testing.T) {
    recordingLogger := &accessLogRecordingLogger{}
    runtimeInstance := newAccessLogRuntime(recordingLogger)

    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    RegisterKernelTerminateAccessLogListener(dispatcher)

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/reset?apiKey=live-credential&next=/home", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(httpRequest)

    terminateEvent := NewKernelTerminateEvent(
        runtimeInstance,
        melodyRequest,
        NewResponse(nethttp.StatusOK, []byte("ok")),
    )

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelTerminate, terminateEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    loggedContext, logged := recordingLogger.accessLogContext()
    if false == logged {
        t.Fatalf("expected the access log record to be written, got messages: %v", recordingLogger.infoMessages)
    }

    queryValue, isString := loggedContext["query"].(string)
    if false == isString {
        t.Fatalf("expected the record to carry the query as a string, got %T", loggedContext["query"])
    }

    if true == strings.Contains(queryValue, "live-credential") {
        t.Fatalf("the credential reached the access log record: %q", queryValue)
    }

    for _, parameterName := range []string{"apiKey", "next"} {
        if false == strings.Contains(queryValue, parameterName) {
            t.Fatalf("expected the parameter name %q to survive redaction, got %q", parameterName, queryValue)
        }
    }
}
