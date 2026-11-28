package http

import (
    "context"
    "crypto/tls"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/event"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
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

func (instance *accessLogRecordingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *accessLogRecordingLogger) CauseChainDepth() int {
    return 8
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

func newAccessLogRuntime(recordingLogger *accessLogRecordingLogger) containercontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()

    scope.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    return runtime.NewRequest(context.Background(), scope)
}

func TestRegisterKernelTerminateAccessLogListener_RecordsTheRequestItCompleted(t *testing.T) {
    recordingLogger := &accessLogRecordingLogger{}
    runtimeInstance := newAccessLogRuntime(recordingLogger)

    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})
    RegisterKernelTerminateAccessLogListener(dispatcher, AccessLogConfig{})

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

    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})
    RegisterKernelTerminateAccessLogListener(dispatcher, AccessLogConfig{})

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

    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})
    RegisterKernelTerminateAccessLogListener(dispatcher, AccessLogConfig{})

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

    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})
    RegisterKernelTerminateAccessLogListener(dispatcher, AccessLogConfig{})

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

    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})
    RegisterKernelTerminateAccessLogListener(dispatcher, AccessLogConfig{})

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

    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})
    RegisterKernelTerminateAccessLogListener(dispatcher, AccessLogConfig{})

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

    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})
    RegisterKernelTerminateAccessLogListener(dispatcher, AccessLogConfig{})

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

func TestRegisterKernelTerminateAccessLogListener_RedactsTheQueryOfTheReferer(t *testing.T) {
    recordingLogger := &accessLogRecordingLogger{}
    runtimeInstance := newAccessLogRuntime(recordingLogger)

    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})
    RegisterKernelTerminateAccessLogListener(dispatcher, AccessLogConfig{})

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/articles", nil)
    httpRequest.Header.Set("Referer", "https://example.com/reset?token=super-secret#state=other-secret")

    terminateEvent := NewKernelTerminateEvent(
        runtimeInstance,
        testhelper.NewHttpTestRequestFromHttpRequest(httpRequest),
        NewResponse(nethttp.StatusOK, []byte("ok")),
    )

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelTerminate, terminateEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    loggedContext, logged := recordingLogger.accessLogContext()
    if false == logged {
        t.Fatalf("expected the access log record to be written")
    }

    if "https://example.com/reset?token=xxxxx" != loggedContext["referer"] {
        t.Fatalf("expected the referer journaled with its query redacted and its fragment dropped, got %v", loggedContext["referer"])
    }
}

func TestRegisterKernelTerminateAccessLogListener_BoundsEveryRequestSuppliedField(t *testing.T) {
    recordingLogger := &accessLogRecordingLogger{}
    runtimeInstance := newAccessLogRuntime(recordingLogger)

    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})
    RegisterKernelTerminateAccessLogListener(dispatcher, AccessLogConfig{})

    longText := strings.Repeat("a", 4096)

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/"+longText+"?"+longText+"=1", nil)
    httpRequest.Header.Set("User-Agent", longText)
    httpRequest.Header.Set("Referer", "https://example.com/"+longText)
    httpRequest.Host = longText
    httpRequest.Method = strings.Repeat("M", 4096)

    terminateEvent := NewKernelTerminateEvent(
        runtimeInstance,
        testhelper.NewHttpTestRequestFromHttpRequest(httpRequest),
        NewResponse(nethttp.StatusOK, []byte("ok")),
    )

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelTerminate, terminateEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    loggedContext, logged := recordingLogger.accessLogContext()
    if false == logged {
        t.Fatalf("expected the access log record to be written")
    }

    for _, key := range []string{"method", "path", "query", "host", "userAgent", "referer"} {
        value, _ := loggedContext[key].(string)
        if 600 < len(value) || false == strings.Contains(value, "...(truncated ") {
            t.Fatalf("expected %s bounded, got %d bytes", key, len(value))
        }
    }
}

func dispatchLongAccessLogRequest(t *testing.T, config AccessLogConfig) (loggingcontract.Context, bool) {
    t.Helper()

    recordingLogger := &accessLogRecordingLogger{}
    runtimeInstance := newAccessLogRuntime(recordingLogger)

    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})
    RegisterKernelTerminateAccessLogListener(dispatcher, config)

    longText := strings.Repeat("a", 4096)

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/"+longText+"?"+longText+"=1", nil)
    httpRequest.Header.Set("User-Agent", longText)
    httpRequest.Header.Set("Referer", "https://example.com/"+longText)
    httpRequest.Host = longText
    httpRequest.Method = strings.Repeat("M", 4096)

    terminateEvent := NewKernelTerminateEvent(
        runtimeInstance,
        testhelper.NewHttpTestRequestFromHttpRequest(httpRequest),
        NewResponse(nethttp.StatusOK, []byte("ok")),
    )

    if _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelTerminate, terminateEvent); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    return recordingLogger.accessLogContext()
}

func TestRegisterKernelTerminateAccessLogListener_CutsEveryRequestSuppliedFieldAtMaxFieldBytes(t *testing.T) {
    for _, testCase := range []struct {
        maxFieldBytes bound.Count
        expected      int
    }{
        {bound.Count{}, 512},
        {bound.Of(1024), 1024},
        {bound.Of(1), 1},
    } {
        loggedContext, logged := dispatchLongAccessLogRequest(t, AccessLogConfig{MaxFieldBytes: testCase.maxFieldBytes})
        if false == logged {
            t.Fatalf("%v: expected the access log record to be written", testCase.maxFieldBytes)
        }

        for _, key := range []string{"method", "path", "query", "host", "userAgent", "referer"} {
            value, _ := loggedContext[key].(string)
            kept, _, isCut := strings.Cut(value, "...(truncated ")
            if false == isCut || testCase.expected != len(kept) {
                t.Fatalf("%v: expected %s cut at %d bytes, got %d kept of %q", testCase.maxFieldBytes, key, testCase.expected, len(kept), value[:min(len(value), 64)])
            }
        }
    }
}

func TestRegisterKernelTerminateAccessLogListener_AnUnlimitedMaxFieldBytesKeepsEveryFieldWhole(t *testing.T) {
    loggedContext, logged := dispatchLongAccessLogRequest(t, AccessLogConfig{MaxFieldBytes: bound.UnlimitedCount()})
    if false == logged {
        t.Fatalf("expected the access log record to be written")
    }

    for _, key := range []string{"method", "path", "host", "userAgent"} {
        value, _ := loggedContext[key].(string)
        if 4096 > len(value) || true == strings.Contains(value, "...(truncated ") {
            t.Fatalf("expected %s kept whole, got %d bytes", key, len(value))
        }
    }
}

func TestRegisterKernelTerminateAccessLogListener_DisabledRegistersNoListener(t *testing.T) {
    if _, logged := dispatchLongAccessLogRequest(t, AccessLogConfig{Disabled: true}); true == logged {
        t.Fatalf("expected no access log record under Disabled")
    }

    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})
    RegisterKernelTerminateAccessLogListener(dispatcher, AccessLogConfig{Disabled: true})
    if registeredEvents := dispatcher.RegisteredEvents(); 0 != len(registeredEvents) {
        t.Fatalf("expected no listener under Disabled, got %v", registeredEvents)
    }
}

func TestRegisterKernelTerminateAccessLogListener_RefusesAMaxFieldBytesBelowOneNamingTheFieldDisabledOrNot(t *testing.T) {
    for _, disabled := range []bool{false, true} {
        func() {
            defer func() {
                refusal, isRefusal := recover().(*exception.Error)
                if false == isRefusal || "AccessLogConfig.MaxFieldBytes" != refusal.Context()["field"] {
                    t.Fatalf("disabled %v: expected the field named, got %v", disabled, refusal)
                }
            }()

            RegisterKernelTerminateAccessLogListener(event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()}), AccessLogConfig{Disabled: disabled, MaxFieldBytes: bound.Of(0)})
        }()
    }

    if rule := AccessLogMaxFieldBytesRule(); 512 != rule.Default || 1 != rule.Minimum || false == rule.UnlimitedAdmitted || "AccessLogConfig.MaxFieldBytes" != rule.Name {
        t.Fatalf("unexpected rule %+v", rule)
    }
}
